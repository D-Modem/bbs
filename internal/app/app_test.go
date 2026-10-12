package app

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// fake is a front end that answers from a script, in order, and keeps
// what it was shown.
type fake struct {
	t      *testing.T
	script []any // a string (Menu, Message, Panel), view.Choice, map[string]string (Form), bool (Confirm), draft
	shown  []string
	said   []string
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
	f.shown = append(f.shown, "menu "+m.Title)
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
	f.shown = append(f.shown, "show "+title)
	return nil
}

func (f *fake) Kind() string                                        { return Text }
func (f *fake) ShowScreen(string, bool) (bool, error)               { return false, nil }
func (f *fake) Chat(room, note string) error                        { return nil }
func (f *fake) Doors() []*door.Door                                 { return nil }
func (f *fake) OpenDoor(*door.Door) error                           { return nil }
func (f *fake) Transfers() bool                                     { return true }
func (f *fake) Download(*store.FileArea, *store.File) error         { return nil }
func (f *fake) Upload(*store.FileArea) error                        { return nil }
func (f *fake) FileCommand(*store.FileArea, string) (string, error) { return "", nil }
func (f *fake) Terminal() string                                    { return "test" }
func (f *fake) TerminalActions() []view.Action                      { return nil }
func (f *fake) TerminalAction(string) error                         { return nil }
func (f *fake) TerminalInfo() error                                 { return nil }
func (f *fake) Logoff() (bool, error)                               { return true, nil }

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
	f := &fake{t: t, script: script}
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
