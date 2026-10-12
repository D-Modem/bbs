package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/guard"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// fake is a front end that answers from a script, in order, and keeps
// what it was shown.
type fake struct {
	t      *testing.T
	b      *bbs.BBS
	script []any // a string (Menu, Message, Panel), view.Choice, map[string]string (Form), bool (Confirm), draft
	shown  []string
	said   []string
	// what SendData sent, what ReceiveData gives
	sentData, upload []byte
}

// draft is Edit's answer: what is sent.
type draft struct {
	to, subject string
	body        []string
}

func (f *fake) next(what string) any {
	f.t.Helper()
	if len(f.script) == 0 {
		if what == "menu" {
			return "G" // the end: log off
		}
		f.t.Fatalf("the script ran out at a %s; shown %q", what, f.shown)
	}
	a := f.script[0]
	f.script = f.script[1:]
	return a
}

// get is the script's next answer, which must be a T.
func get[T any](f *fake, what string) T {
	f.t.Helper()
	a, ok := f.next(what).(T)
	if !ok {
		f.t.Fatalf("the script has no %s answer next; shown %q", what, f.shown)
	}
	return a
}

func (f *fake) PageSize() int { return 10 }
func (f *fake) Narrow() bool  { return false }
func (f *fake) Menu(m *view.Menu) (string, error) {
	brief := ""
	if m.Brief {
		brief = " (brief)"
	}
	f.shown = append(f.shown, "menu "+m.Title+brief)
	return get[string](f, "menu"), nil
}
func (f *fake) Table(t *view.Table) (view.Choice, error) {
	var rows []string
	for _, r := range t.Rows {
		rows = append(rows, strings.Join(r.Cells, "|"))
	}
	f.shown = append(f.shown, "table "+t.Title+": "+strings.Join(rows, "; "))
	c := get[view.Choice](f, "table")
	if c.Action == view.Pick && c.Row >= 0 {
		c.Num = t.Rows[c.Row].Num
	}
	return c, nil
}
func (f *fake) Message(m *view.Message) (string, error) {
	f.shown = append(f.shown, "message "+m.Subject+": "+m.Body)
	return get[string](f, "message"), nil
}
func (f *fake) Form(fm *view.Form) (map[string]string, bool, error) {
	f.shown = append(f.shown, "form "+fm.Title)
	v := get[map[string]string](f, "form")
	return v, v != nil, nil
}
func (f *fake) Panel(p *view.Panel) (string, error) {
	var ls []string
	for _, l := range p.Lines {
		ls = append(ls, l.Label+l.Text)
	}
	f.shown = append(f.shown, "panel "+p.Title+": "+strings.Join(ls, "; "))
	return get[string](f, "panel"), nil
}
func (f *fake) Edit(d *view.Draft) (bool, error) {
	f.shown = append(f.shown, "edit "+d.Title)
	a := get[draft](f, "edit")
	if d.CheckTo != nil {
		if err := d.CheckTo(a.to); err != nil {
			f.said = append(f.said, err.Error())
			return false, nil
		}
	}
	if err := d.Send(a.to, a.subject, a.body); err != nil {
		f.said = append(f.said, err.Error())
		return false, nil
	}
	return true, nil
}
func (f *fake) Confirm(q string, def bool) (bool, error) {
	f.shown = append(f.shown, "confirm "+q)
	return get[bool](f, "confirm"), nil
}
func (f *fake) Say(tn view.Tone, msg string) { f.said = append(f.said, msg) }
func (f *fake) Show(title string, ls []view.Line) error {
	var out []string
	for _, l := range ls {
		out = append(out, l.Label+l.Text)
	}
	f.shown = append(f.shown, "show "+title+": "+strings.Join(out, "; "))
	return nil
}

func (f *fake) Kind() string                          { return Text }
func (f *fake) ShowScreen(string, bool) (bool, error) { return false, nil }
func (f *fake) Chat(room, note string) error          { return nil }
func (f *fake) Doors() []*door.Door                   { return nil }
func (f *fake) OpenDoor(*door.Door) error             { return nil }
func (f *fake) Transfers() bool                       { return true }
func (f *fake) Download(fs []FileRef) error {
	for _, r := range fs {
		f.b.Downloaded(nil, r.File)
		f.said = append(f.said, "sent "+r.File.Name)
	}
	return nil
}
func (f *fake) Upload(*store.FileArea) error                        { return nil }
func (f *fake) FileCommand(*store.FileArea, string) (string, error) { return "", nil }
func (f *fake) SendData(name string, data []byte) (bool, error) {
	f.said = append(f.said, fmt.Sprintf("sent %s (%d bytes)", name, len(data)))
	f.sentData = data
	return true, nil
}
func (f *fake) ReceiveData(int64) ([]byte, error) { return f.upload, nil }
func (f *fake) DataCommand(string, func(string) ([]byte, func(), error), func(string, []byte) string) (string, error) {
	return "", nil
}
func (f *fake) Terminal() string               { return "test" }
func (f *fake) TerminalActions() []view.Action { return nil }
func (f *fake) TerminalAction(string) error    { return nil }
func (f *fake) TerminalInfo() error            { return nil }
func (f *fake) Watch(n int) error {
	f.shown = append(f.shown, fmt.Sprintf("watch %d", n))
	return nil
}
func (f *fake) Logoff() (bool, error) { return true, nil }

// setup is a BBS with an area, and nodes for alice and the sysop.
func setup(t *testing.T) (*bbs.BBS, *bbs.Node, *bbs.Node) {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := bbs.New(bbs.Defaults(), st, log.New(&bytes.Buffer{}, "", 0))
	if _, err := st.CreateArea("General", "Talk", false); err != nil {
		t.Fatal(err)
	}
	var nodes []*bbs.Node
	for _, u := range []struct {
		name  string
		sysop bool
	}{{"alice", false}, {"sysop", true}} {
		us, err := st.CreateUser(u.name, "password1", "Here", u.sysop)
		if err != nil {
			t.Fatal(err)
		}
		n, err := b.Enter(&bbs.Caller{Via: "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		n.SetUser(us)
		nodes = append(nodes, n)
	}
	return b, nodes[0], nodes[1]
}

func run(t *testing.T, b *bbs.BBS, n *bbs.Node, script ...any) *fake {
	t.Helper()
	f := &fake{t: t, b: b, script: script}
	if err := Run(b, n, f); err != ErrLogoff {
		t.Fatalf("Run: %v; shown %q", err, f.shown)
	}
	if len(f.script) > 0 {
		t.Errorf("left of the script: %v; shown %q", f.script, f.shown)
	}
	return f
}

func has(t *testing.T, what string, all []string, want string) {
	t.Helper()
	for _, s := range all {
		if strings.Contains(s, want) {
			return
		}
	}
	t.Errorf("no %q among the %s: %q", want, what, all)
}

func TestPostAndRead(t *testing.T) {
	b, alice, sysop := setup(t)
	f := run(t, b, alice,
		"M", view.Choice{Action: view.Pick, Row: 0}, // General
		view.Choice{Action: "post"}, draft{"All", "Hello", []string{"First line."}},
		view.Choice{Action: view.Pick, Row: 0}, "next", // the last: back to the area
		view.Choice{Action: view.Back}, view.Choice{Action: view.Back})
	has(t, "said", f.said, "Posted as #1.")
	has(t, "said", f.said, "That was the last message.")
	has(t, "screens", f.shown, "message Hello: First line.")

	// the sysop: a new scan, a reply, a number not on the page
	f = run(t, b, sysop,
		"M", view.Choice{Action: "new"}, "reply", draft{"alice", "Re: Hello", []string{"Hi."}}, "quit",
		"M", view.Choice{Action: view.Pick, Row: 0}, // a quit in a new scan leaves the areas
		view.Choice{Action: view.Pick, Row: -1, Num: "99"}, view.Choice{Action: view.Back},
		view.Choice{Action: view.Back})
	has(t, "said", f.said, "Posted as #2.")
	has(t, "said", f.said, "No such message here.")
	has(t, "screens", f.shown, "table General: alice|Hello|")
}

func TestScanToYouFind(t *testing.T) {
	b, alice, sysop := setup(t)
	other, _ := b.Store.CreateArea("Other", "More talk", false)
	general, _ := b.Store.AreaByName("General")
	for _, m := range []struct {
		area          int64
		to, sub, body string
	}{{general.ID, "alice", "For you", "hello there"}, {other.ID, "All", "Weather", "rain today"},
		{other.ID, "Alice", "Also for you", "more rain"}} {
		if _, err := sysop.Post(map[int64]*store.Area{general.ID: general, other.ID: other}[m.area],
			m.to, m.sub, []string{m.body}, 0); err != nil {
			t.Fatal(err)
		}
	}
	f := run(t, b, alice,
		"M",
		// Other out of the scan: the new scan reads General's only
		view.Choice{Action: "scan"}, view.Choice{Action: view.Pick, Row: 1}, view.Choice{Action: view.Back},
		view.Choice{Action: "new"}, "next",
		// to alice: both, newest first; the second read, then on past the end
		view.Choice{Action: "toyou"}, view.Choice{Action: view.Pick, Row: 1}, "next", view.Choice{Action: view.Back},
		// rain: the two in Other
		view.Choice{Action: "find"}, map[string]string{"text": "RAIN"}, view.Choice{Action: view.Back},
		view.Choice{Action: "find"}, map[string]string{"text": "x"},
		view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "table Your new scan: General|Talk|yes; Other|More talk|yes")
	has(t, "screens", f.shown, "table Your new scan: General|Talk|yes; Other|More talk|no")
	has(t, "screens", f.shown, "message For you: hello there")
	has(t, "screens", f.shown, "table Messages to you: Other|sysop|Also for you|")
	has(t, "said", f.said, "That was the last.")
	has(t, "screens", f.shown, "table Found: RAIN: Other|sysop|Also for you|")
	has(t, "said", f.said, "Give two characters or more.")
	for _, s := range f.shown {
		if strings.Contains(s, "message Weather") {
			t.Errorf("the new scan went into Other: %q", f.shown)
		}
	}
	if off, _ := b.Store.ScanOff(alice.User().ID); !off[other.ID] {
		t.Errorf("off %v", off)
	}
}

func TestFileBase(t *testing.T) {
	b, alice, sysop := setup(t)
	fs, err := files.Open(b.Store, t.TempDir(), 1<<20, 0)
	if err != nil {
		t.Fatal(err)
	}
	b.Files = fs
	area, _ := b.Store.CreateFileArea("Games", "Shareware", false)
	var zb bytes.Buffer
	z := zip.NewWriter(&zb)
	w, _ := z.Create("FILE_ID.DIZ")
	w.Write([]byte("DOOM shareware\nby id"))
	w, _ = z.Create("doom.exe")
	w.Write([]byte("MZ"))
	z.Close()
	sys := sysop.User()
	for _, u := range []struct {
		name string
		data []byte
	}{{"doom.zip", zb.Bytes()}, {"notes.txt", []byte("hi")}} {
		name, data := u.name, u.data
		up, err := fs.Upload(sys, area, name, "")
		if err != nil {
			t.Fatal(err)
		}
		up.Write(data)
		if _, err := up.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	f := run(t, b, alice,
		"F",
		// new files since alice's last call (never: the last 30 days)
		view.Choice{Action: "new"}, view.Choice{Action: view.Pick, Row: 1}, // doom.zip: newest first, then by id
		"view", view.Choice{Action: view.Back}, "tag", view.Back,
		view.Choice{Action: view.Back},
		// find by the DIZ's words
		view.Choice{Action: "find"}, map[string]string{"text": "id"}, view.Choice{Action: view.Back},
		// in the area: tag notes.txt too, and download the batch
		view.Choice{Action: view.Pick, Row: 0}, view.Choice{Action: "tag"}, map[string]string{"n": "2 9"},
		view.Choice{Action: "batch"}, view.Choice{Action: "download"},
		view.Choice{Action: view.Back}, view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "table New files since")
	has(t, "screens", f.shown, "panel doom.zip: Name:doom.zip;")
	has(t, "screens", f.shown, "; DOOM shareware; by id;")
	has(t, "screens", f.shown, "table In doom.zip: FILE_ID.DIZ|20 B|; doom.exe|2 B|")
	has(t, "said", f.said, "Tagged doom.zip (1 in the batch).")
	has(t, "screens", f.shown, "table Files: id: Games|doom.zip|")
	has(t, "said", f.said, "No such file: 9.")
	has(t, "said", f.said, "Tagged notes.txt (2 in the batch).")
	has(t, "said", f.said, "sent doom.zip")
	has(t, "said", f.said, "sent notes.txt")
	if fl, _ := b.Store.FileByName(area.ID, "doom.zip"); fl.Downloads != 1 {
		t.Errorf("downloads %d", fl.Downloads)
	}
}

func TestUserFeatures(t *testing.T) {
	b, alice, sysop := setup(t)
	// a call recorded by a login
	n, _ := b.Enter(&bbs.Caller{Via: "ssh"}, nil)
	if _, err := n.Login("alice", "password1"); err != nil {
		t.Fatal(err)
	}
	n.Leave()
	f := run(t, b, alice,
		"L", view.Choice{Action: view.Back},
		"K", draft{"ignored", "Feedback", []string{"Nice board."}},
		"P", "about", map[string]string{"about": "Calls from a C64"}, "expert", view.Back,
		"U", view.Choice{Action: view.Pick, Row: 1}, "mail", draft{"sysop", "Hello", []string{"Hi."}},
		view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "table Last callers: alice|Here|ssh|")
	has(t, "said", f.said, "Sent to sysop.")
	has(t, "screens", f.shown, "panel Your profile: Name:alice; ")
	has(t, "screens", f.shown, "Menus:in full")
	has(t, "screens", f.shown, "menu Main menu (brief)")
	has(t, "screens", f.shown, "panel sysop: Name:sysop; From:Here; ")
	has(t, "said", f.said, "Sent.")
	in, _ := b.Store.Inbox(sysop.User().ID)
	if len(in) != 2 || in[0].Subject+in[1].Subject != "HelloFeedback" && in[0].Subject+in[1].Subject != "FeedbackHello" {
		t.Errorf("the sysop's mail: %d", len(in))
	}
	if u := alice.Refresh(); u.About != "Calls from a C64" || alice.Prefs()["expert"] != "on" {
		t.Errorf("alice: %q %v", u.About, alice.Prefs())
	}
	// the sysop sees more of a user
	f = run(t, b, sysop, "U", view.Choice{Action: view.Pick, Row: 0}, view.Back, view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "About:Calls from a C64; Access:level 20 (Level 20)")
}

func TestSysopTools(t *testing.T) {
	b, alice, sysop := setup(t)
	dir := t.TempDir()
	b.Guard = &guard.Guard{HackLog: filepath.Join(dir, "hack.log"), BanTime: time.Hour}
	b.Guard.Ban("192.0.2.9", "too many failed logins")
	b.Events = &events.Scheduler{Events: []*events.Event{{Name: "hello", Command: []string{"/bin/echo", "hi"}, Every: time.Hour}}}
	aliceID := alice.User().ID
	f := run(t, b, sysop,
		"*",
		// alice's level and flags, locked and unlocked
		"U", view.Choice{Action: view.Pick, Row: 0}, "level", map[string]string{"level": "300"},
		"level", map[string]string{"level": "40"}, "flags", map[string]string{"flags": "+AB"}, "lock", "lock", view.Back,
		view.Choice{Action: view.Back},
		"S",
		"H", view.Choice{Action: view.Pick, Row: 0}, view.Choice{Action: "log"}, view.Choice{Action: view.Back},
		"E", view.Choice{Action: view.Pick, Row: 0}, "run", view.Choice{Action: view.Back},
		"W", view.Choice{Action: view.Pick, Row: -1, Num: "1"},
		"A", "Q")
	has(t, "said", f.said, "That is not a level.")
	if u, _ := b.Store.UserByID(aliceID); u.Level != 40 || u.Flags != "AB" || u.Locked {
		t.Errorf("alice: %+v", u)
	}
	has(t, "screens", f.shown, "show Statistics:")
	has(t, "said", f.said, "The ban on 192.0.2.9 is lifted.")
	has(t, "screens", f.shown, "ban lifted by sysop")
	has(t, "said", f.said, "hello: done.")
	has(t, "screens", f.shown, "table Scheduled events: hello|every 1h0m0s|")
	has(t, "screens", f.shown, "watch 1")
	has(t, "screens", f.shown, "menu Sysop tools")
	if b.SysopAvailable() {
		t.Error("still available")
	}
	// alice has no sysop tools
	f = run(t, b, alice, "*")
	for _, s := range f.shown {
		if strings.Contains(s, "Sysop tools") {
			t.Errorf("alice got them: %q", f.shown)
		}
	}
}

func TestMessageBase(t *testing.T) {
	b, alice, sysop := setup(t)
	gen, _ := b.Store.AreaByName("General")
	b.Store.SetAreaAliases(gen.ID, true)
	tech, _ := b.Store.CreateArea("Tech", "Machines", false)
	b.Store.SetAreaGroup(tech.ID, "Hobbies")
	f := run(t, b, alice,
		// an alias and a signature
		"P", "alias", map[string]string{"alias": "Night Owl"},
		"signature", map[string]string{"1": "-- alice", "2": "at the keys"}, view.Back,
		// a post as the alias, a poll, a reply
		"M", view.Choice{Action: view.Pick, Row: 0}, // General: ungrouped, first
		view.Choice{Action: "post"}, true, draft{"All", "Hello", []string{"First."}},
		view.Choice{Action: "poll"}, map[string]string{"q": "Tea or coffee?", "a": "Tea | Coffee | "}, false,
		view.Choice{Action: view.Pick, Row: 0}, "reply", false, draft{"Night Owl", "Re: Hello", []string{"Me too."}},
		view.Back, view.Choice{Action: view.Back}, view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "table Message areas: |General|Talk|0|; Hobbies|Tech|Machines|0|")
	has(t, "said", f.said, "Posted as #1.")
	has(t, "said", f.said, "Posted as #2.")
	has(t, "said", f.said, "Posted as #3.")
	m, _ := b.Store.Message(1)
	if m.From != "Night Owl" || m.FromID != alice.User().ID || !strings.HasSuffix(m.Body, "First.\n\n-- alice\nat the keys") {
		t.Errorf("as the alias: %+v", m)
	}
	if r, _ := b.Store.Message(3); r.From != "alice" || r.ReplyTo != 1 {
		t.Errorf("the reply: %+v", r)
	}

	// the sysop: the reply's original, its thread, a vote, the poll answered
	f = run(t, b, sysop,
		"M", view.Choice{Action: view.Pick, Row: 0},
		view.Choice{Action: view.Pick, Row: -1, Num: "3"}, "original", "vote", "U", "thread",
		view.Choice{Action: view.Pick, Row: 1}, "quit",
		view.Choice{Action: view.Pick, Row: -1, Num: "2"},
		"answer", view.Choice{Action: view.Pick, Row: 1}, view.Back,
		view.Choice{Action: view.Back}, view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "message Hello: First.")
	has(t, "screens", f.shown, "table Thread: Hello: Night Owl|Hello|")
	has(t, "screens", f.shown, "; alice|  Re: Hello|")
	has(t, "screens", f.shown, "message Tea or coffee?: Tea or coffee?")
	has(t, "said", f.said, "Your answer is counted.")
	if up, _, _, _ := b.Store.Votes(1, 0); up != 1 {
		t.Errorf("votes up %d", up)
	}
	if _, counts, _, _ := b.Store.Poll(2, 0); counts[1] != 1 {
		t.Errorf("poll %v", counts)
	}
}

func TestMailAndWho(t *testing.T) {
	b, alice, sysop := setup(t)
	f := run(t, b, alice,
		"E", view.Choice{Action: "send"}, draft{"nobody", "x", nil},
		view.Choice{Action: "send"}, draft{"sysop", "Question", []string{"How?"}},
		view.Choice{Action: view.Back},
		"W", view.Choice{Action: view.Pick, Row: 1}, map[string]string{"text": "hello"})
	has(t, "said", f.said, "there is no such user")
	has(t, "said", f.said, "Sent.")
	var got []string
	for _, e := range sysop.Pending() {
		got = append(got, e.String())
	}
	has(t, "sysop's events", got, "Message from alice (node 1): hello")

	f = run(t, b, sysop,
		"E", view.Choice{Action: view.Pick, Row: 0}, "delete",
		view.Choice{Action: view.Back})
	has(t, "screens", f.shown, "message Question: How?")
	has(t, "said", f.said, "Deleted.")
	if in, _ := b.Store.Inbox(sysop.User().ID); len(in) != 0 {
		t.Errorf("inbox %d", len(in))
	}
}

func TestProfileAndValidate(t *testing.T) {
	b, alice, sysop := setup(t)
	b.ValidatedLevel, b.NewUserLevel = 20, 10
	zed, err := b.Store.CreateUser("zed", "password1", "Mars", false)
	if err != nil {
		t.Fatal(err)
	}
	b.Store.SetLevel(zed.ID, 10)

	f := run(t, b, alice,
		"P", "location", map[string]string{"loc": "Venus"},
		"password", map[string]string{"old": "password1", "new": "password2", "again": "password3"},
		view.Back)
	has(t, "said", f.said, "They differ.")
	has(t, "screens", f.shown, "panel Your profile: Name:alice; From:Here")
	if u := alice.Refresh(); u.Location != "Venus" {
		t.Errorf("location %q", u.Location)
	}

	f = run(t, b, sysop, "V", "level", map[string]string{"level": "300"}, "validate")
	has(t, "said", f.said, "That is not a level.")
	has(t, "said", f.said, "Validated.")
	has(t, "said", f.said, "No one else is waiting.")
	if u, _ := b.Store.UserByName("zed"); u.Level != 20 {
		t.Errorf("level %d", u.Level)
	}
}

func TestOfflineMail(t *testing.T) {
	b, alice, sysop := setup(t)
	gen, _ := b.Store.AreaByName("General")
	if _, err := sysop.Post(gen, "All", "News", []string{"Read me offline."}, 0); err != nil {
		t.Fatal(err)
	}
	var rep bytes.Buffer
	qwk.WriteREP(&rep, b.BBSID(), []qwk.Message{{Conf: int(gen.ID), To: "sysop", Subject: "Re: News", Ref: 1, Body: "Read it."}})
	f := &fake{t: t, b: b, upload: rep.Bytes(), script: []any{"O",
		view.Choice{Action: "download"}, view.Choice{Action: "download"},
		view.Choice{Action: "upload"}, view.Choice{Action: "upload"},
		view.Choice{Action: view.Back}, "G"}}
	if err := Run(b, alice, f); err != ErrLogoff {
		t.Fatalf("Run: %v; shown %q", err, f.shown)
	}
	has(t, "screens", f.shown, "table Offline mail (QWK): Your packet|GOBBS.QWK")
	has(t, "screens", f.shown, "New messages|1 in 1 areas")
	has(t, "said", f.said, "sent GOBBS.QWK")
	has(t, "said", f.said, "1 messages packed (0 of them mail)")
	has(t, "said", f.said, "There is nothing new for your packet.")
	has(t, "said", f.said, "Your replies: 1 posted, 0 mailed.")
	has(t, "said", f.said, "Your replies: 0 posted, 0 mailed, 1 already in.")
	p, err := qwk.ReadPacket(f.sentData)
	if err != nil || len(p.Messages) != 1 || p.Messages[0].Body != "Read me offline." {
		t.Errorf("packet %+v %v", p, err)
	}
	ms, _ := b.Store.Messages(gen.ID, 1, 10)
	if len(ms) != 1 || ms[0].From != "alice" || ms[0].ReplyTo != 1 {
		t.Errorf("replies %+v", ms)
	}
}
