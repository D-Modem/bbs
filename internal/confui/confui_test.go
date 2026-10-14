package confui

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bbssec "github.com/Neo-Desktop/bbs-sec"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// step is the sysop's doing on a sheet (or a confirm, a form).
type step func(t *testing.T, sh *view.Sheet) view.SheetResult

// script is a view.UI (and SheetUI) that does what its steps say, and
// keeps what it was told.
type script struct {
	t        *testing.T
	steps    []step
	confirms []bool
	forms    []map[string]string
	said     []string
	sheets   []string // the crumbs of each sheet drawn
}

func (s *script) Sheet(sh *view.Sheet) (view.SheetResult, error) {
	s.t.Helper()
	s.sheets = append(s.sheets, sh.Crumbs)
	if sh.Status != "" {
		s.said = append(s.said, sh.Status)
	}
	if len(s.steps) == 0 {
		s.t.Fatalf("out of steps at %s", sh.Crumbs)
	}
	st := s.steps[0]
	s.steps = s.steps[1:]
	return st(s.t, sh), nil
}

func (s *script) PageSize() int                     { return 20 }
func (s *script) Narrow() bool                      { return false }
func (s *script) Menu(m *view.Menu) (string, error) { return view.Back, nil }
func (s *script) Table(t *view.Table) (view.Choice, error) {
	return view.Choice{Action: view.Back}, nil
}
func (s *script) Message(m *view.Message) (string, error) { return view.Back, nil }
func (s *script) Panel(p *view.Panel) (string, error)     { return view.Back, nil }
func (s *script) Edit(d *view.Draft) (bool, error)        { return false, nil }
func (s *script) Say(tn view.Tone, msg string)            { s.said = append(s.said, msg) }
func (s *script) Form(f *view.Form) (map[string]string, bool, error) {
	if len(s.forms) == 0 {
		s.t.Fatalf("out of forms at %s", f.Title)
	}
	v := s.forms[0]
	s.forms = s.forms[1:]
	return v, true, nil
}
func (s *script) Confirm(q string, def bool) (bool, error) {
	s.said = append(s.said, q)
	if len(s.confirms) == 0 {
		s.t.Fatalf("out of confirms at %q", q)
	}
	a := s.confirms[0]
	s.confirms = s.confirms[1:]
	return a, nil
}
func (s *script) Show(title string, ls []view.Line) error {
	s.said = append(s.said, title)
	for _, l := range ls {
		s.said = append(s.said, l.Label+l.Text)
	}
	return nil
}

func (s *script) told(sub string) bool {
	return strings.Contains(strings.Join(s.said, "\n"), sub)
}

// entry is the index of the entry labelled label.
func entry(t *testing.T, sh *view.Sheet, label string) int {
	t.Helper()
	for i, e := range sh.Entries {
		if e.Label == label {
			return i
		}
	}
	var ls []string
	for _, e := range sh.Entries {
		ls = append(ls, e.Label)
	}
	t.Fatalf("%s: no %q in %q", sh.Crumbs, label, ls)
	return -1
}

func open(label string) step {
	return func(t *testing.T, sh *view.Sheet) view.SheetResult {
		return view.SheetResult{Action: view.Pick, Entry: entry(t, sh, label)}
	}
}

func set(label, v string) step {
	return func(t *testing.T, sh *view.Sheet) view.SheetResult {
		i := entry(t, sh, label)
		return view.SheetResult{Action: view.SheetSet, Entry: i, Values: map[string]string{sh.Entries[i].ID: v}}
	}
}

// expect checks an entry as drawn, and goes on with next.
func expect(label, value string, changed bool, next step) step {
	return func(t *testing.T, sh *view.Sheet) view.SheetResult {
		e := sh.Entries[entry(t, sh, label)]
		if e.Value != value || e.Changed != changed {
			t.Errorf("%s %s: %q changed %v, want %q %v", sh.Crumbs, label, e.Value, e.Changed, value, changed)
		}
		return next(t, sh)
	}
}

func key(action string) step {
	return func(t *testing.T, sh *view.Sheet) view.SheetResult { return view.SheetResult{Action: action} }
}

var (
	back = key(view.Back)
	save = key(view.SheetSave)
	quit = key(view.SheetQuit)
)

const file = "[general]\nname = Test BBS\n; how to reach it\n;phone = 555 0100\n\n[listen telnet]\ntype = telnet\naddress = :2300\n"

func writeConf(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	if err := os.WriteFile(p, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func check(data []byte) ([]string, error) {
	if strings.Contains(string(data), "max_nodes = lots") {
		return nil, errors.New("[general] max_nodes: \"lots\" is not a number")
	}
	return []string{"a warning"}, nil
}

// TestSaveAndExit: changes wait for F10, then the file is written whole
// (comments kept, the last as .bak) and put in force.
func TestSaveAndExit(t *testing.T) {
	p := writeConf(t)
	reloaded := false
	ui := &script{t: t, steps: []step{
		open("Board configuration"),
		set("BBS name", "New BBS"),
		set("Dial-in numbers", "+1 555 0100"),
		expect("BBS name", "New BBS", true, func(t *testing.T, sh *view.Sheet) view.SheetResult {
			if data, _ := os.ReadFile(p); string(data) != file {
				t.Error("written before the save")
			}
			if sh.Status != "2 changes not saved" {
				t.Errorf("status %q", sh.Status)
			}
			return save(t, sh)
		}),
	}}
	e := &Editor{UI: ui, Tools: bbs.ConfigTools{Path: p, Check: check,
		Reload: func() ([]string, []string, error) { reloaded = true; return []string{"in force"}, nil, nil }}}
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	want := strings.Replace(strings.Replace(file, "Test BBS", "New BBS", 1), ";phone = 555 0100\n", ";phone = 555 0100\nphone = +1 555 0100\n", 1)
	if string(data) != want {
		t.Errorf("written:\n%s", data)
	}
	if old, _ := os.ReadFile(p + ".bak"); string(old) != file {
		t.Errorf(".bak:\n%s", old)
	}
	if !reloaded || !ui.told("In force now:") || !ui.told("Saved") {
		t.Errorf("said %q", ui.said)
	}
}

// TestQuitWithoutSaving: F9 asks, and then nothing is written; no keeps
// the screens open.
func TestQuitWithoutSaving(t *testing.T) {
	p := writeConf(t)
	ui := &script{t: t, confirms: []bool{false, true}, steps: []step{
		open("Board configuration"), set("BBS name", "Gone BBS"), quit,
		// not yet: back at the main menu, the change kept
		open("Board configuration"), expect("BBS name", "Gone BBS", true, back),
		open("Quit without saving (F9)"),
	}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != file {
		t.Errorf("written:\n%s", data)
	}
	if !ui.told("Leave without saving 1 changes?") {
		t.Errorf("said %q", ui.said)
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("a .bak")
	}
}

// TestBadNotSaved: a file the BBS would not start with is said at once,
// and not saved until it is put right.
func TestBadNotSaved(t *testing.T) {
	p := writeConf(t)
	ui := &script{t: t, steps: []step{
		open("Connections and modems"), set("Callers at once", "lots"), save,
		open("Connections and modems"), set("Callers at once", "50"), save,
	}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p, Check: check}}).Run(); err != nil {
		t.Fatal(err)
	}
	if !ui.told("The BBS would not take this") || !ui.told("Not saved: the BBS would not start") {
		t.Errorf("said %q", ui.said)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "max_nodes = 50\n") {
		t.Errorf("written:\n%s", data)
	}
}

// TestChangedOnDisk: a file changed by another since it was read is not
// saved over.
func TestChangedOnDisk(t *testing.T) {
	p := writeConf(t)
	ui := &script{t: t, confirms: []bool{true}, steps: []step{
		open("Board configuration"),
		func(t *testing.T, sh *view.Sheet) view.SheetResult {
			os.WriteFile(p, []byte(file+"; by hand\n"), 0o600)
			return set("BBS name", "New BBS")(t, sh)
		},
		save, quit,
	}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	if !ui.told("has changed since it was read") {
		t.Errorf("said %q", ui.said)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "by hand") {
		t.Error("saved over")
	}
}

// TestServices: a server turned on (its section uncommented where it
// was), a listener added, and one's TN3270 turned off.
func TestServices(t *testing.T) {
	p := writeConf(t)
	os.WriteFile(p, []byte(file+"\n; finger\n;[finger]\n;address = :79\n"), 0o600)
	ui := &script{t: t, forms: []map[string]string{{"name": "ssh"}}, steps: []step{
		open("Services"),
		open("Finger"), expect("Enabled", "no", false, set("Enabled", "yes")), set("online", "no"), back,
		open("SSH"), key("add"),
		expect("type", "ssh", true, set("address", ":2222")), back, back,
		open("TN3270"), set("[listen telnet] takes 3270s", "no"), back,
		expect("Telnet", "1 listener", false, back),
		save,
	}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	for _, want := range []string{"\n[finger]\n;address = :79\nonline = no\n", "[listen ssh]\naddress = :2222\ntype = ssh\n",
		"address = :2300\ntn3270 = no\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no %q in:\n%s", want, data)
		}
	}
}

// TestAccounts: accounts changed, made and deleted on the screens are so
// in the database only at the save.
func TestAccounts(t *testing.T) {
	p := writeConf(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.CreateUser("sysop", "sysoppass1", "", true)
	alice, _ := st.CreateUser("alice", "alicepass1", "", false)
	st.CreateUser("bob", "bobpass123", "", false)
	b := bbs.New(bbs.Defaults(), st, log.New(io.Discard, "", 0))
	ui := &script{t: t, forms: []map[string]string{{"name": "carol", "pw": "carolpass1"}}, steps: []step{
		open("Users and groups"), open("User accounts"),
		open("alice"), set("Level", "30"), set("Flags", "ba"), set("Locked out", "yes"), back,
		open("bob"), open("Delete this user"), back,
		key("add"), back,
		// the sysop's own: not to be locked by themselves
		open("sysop"), func(t *testing.T, sh *view.Sheet) view.SheetResult {
			if !sh.Entries[entry(t, sh, "Locked out")].Disabled {
				t.Error("the sysop may lock themselves out")
			}
			if u, _ := st.UserByName("alice"); u.Level != alice.Level || u.Locked {
				t.Error("changed before the save")
			}
			return save(t, sh)
		},
	}}
	e := &Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}, Who: "sysop", Open: func() (*bbs.BBS, error) { return b, nil }}
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.UserByName("alice"); u.Level != 30 || u.Flags != "AB" || !u.Locked {
		t.Errorf("alice: %+v", u)
	}
	if _, err := st.UserByName("bob"); err == nil {
		t.Error("bob is there")
	}
	if u, err := st.UserByName("carol"); err != nil || u.Level != 20 {
		t.Errorf("carol: %+v %v", u, err)
	} else if _, err := st.CheckPassword("carol", "carolpass1"); err != nil {
		t.Errorf("carol's password: %v", err)
	}
	if !ui.told("user alice: level 30") || !ui.told("user bob: deleted") {
		t.Errorf("said %q", ui.said)
	}
	if data, _ := os.ReadFile(p); string(data) != file {
		t.Error("the file written for no change of its own")
	}
}

// TestFromSample: with no file, the sample is begun from.
func TestFromSample(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	ui := &script{t: t, confirms: []bool{true}, steps: []step{save}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != string(bbssec.Sample) {
		t.Errorf("not the sample: %d bytes", len(data))
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode())
	}
}

// TestEveryMenu: every menu and page opens on the sample, and comes back.
func TestEveryMenu(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	os.WriteFile(p, bbssec.Sample, 0o600)
	var steps []step
	var walk func(depth int) step
	// open each menu entry of a sheet in turn, depth first, then back
	seen := map[string]bool{}
	walk = func(depth int) step {
		return func(t *testing.T, sh *view.Sheet) view.SheetResult {
			for i, e := range sh.Entries {
				k := sh.Crumbs + "/" + e.Label
				if e.Kind != view.EntryMenu || seen[k] || depth > 4 {
					continue
				}
				seen[k] = true
				// after it opens: walk it; then this sheet again
				ui := currentUI
				ui.steps = append([]step{walk(depth + 1), walk(depth)}, ui.steps...)
				return view.SheetResult{Action: view.Pick, Entry: i}
			}
			return back(t, sh)
		}
	}
	steps = append(steps, walk(0))
	ui := &script{t: t, steps: steps}
	currentUI = ui
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d menus opened", len(seen))
	if len(seen) < 30 {
		t.Errorf("only %d menus opened", len(seen))
	}
}

var currentUI *script
