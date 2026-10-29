package script

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/view"
)

// fake is a front end answering from a script of answers.
type fake struct {
	t       *testing.T
	answers []any
	shown   []string
	said    []string
	delay   time.Duration // each screen takes this long to answer
	fail    error         // each screen fails so
}

// next is the next answer, after the fake's delay; running out of answers fails
// the test.
func (f *fake) next() any {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if len(f.answers) == 0 {
		f.t.Fatalf("out of answers; shown %q", f.shown)
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a
}

// PageSize is 10 rows.
func (f *fake) PageSize() int { return 10 }

// Narrow is false: a wide screen.
func (f *fake) Narrow() bool { return false }

// Kind is "text": a text terminal.
func (f *fake) Kind() string { return "text" }

// Menu keeps the menu's title and items and answers the next key, or fails with
// the fake's error.
func (f *fake) Menu(m *view.Menu) (string, error) {
	var ks []string
	for _, it := range m.Items {
		ks = append(ks, it.Key+"="+it.Label)
	}
	f.shown = append(f.shown, "menu "+m.Title+": "+strings.Join(ks, " "))
	if f.fail != nil {
		return "", f.fail
	}
	return f.next().(string), nil
}

// Table keeps the title and rows, with their numbers, and answers the next
// view.Choice.
func (f *fake) Table(t *view.Table) (view.Choice, error) {
	var rows []string
	for _, r := range t.Rows {
		rows = append(rows, r.Num+":"+strings.Join(r.Cells, "|"))
	}
	f.shown = append(f.shown, "table "+t.Title+": "+strings.Join(rows, "; "))
	return f.next().(view.Choice), nil
}

// Message keeps the subject and body and answers the next action.
func (f *fake) Message(m *view.Message) (string, error) {
	f.shown = append(f.shown, "message "+m.Subject+": "+m.Body)
	return f.next().(string), nil
}

// Form keeps the title and answers the next values; any other answer is the
// form left.
func (f *fake) Form(fm *view.Form) (map[string]string, bool, error) {
	f.shown = append(f.shown, "form "+fm.Title)
	v, _ := f.next().(map[string]string)
	return v, v != nil, nil
}

// Panel keeps the title and lines and answers the next action.
func (f *fake) Panel(p *view.Panel) (string, error) {
	var ls []string
	for _, l := range p.Lines {
		ls = append(ls, l.Label+l.Text)
	}
	f.shown = append(f.shown, "panel "+p.Title+": "+strings.Join(ls, "; "))
	return f.next().(string), nil
}

// Edit sends the next answer's lines as the body, to the draft's To with its
// subject; any other answer is the draft not sent.
func (f *fake) Edit(d *view.Draft) (bool, error) {
	f.shown = append(f.shown, "edit "+d.Title)
	b, _ := f.next().([]string)
	if b == nil {
		return false, nil
	}
	return true, d.Send(d.To, d.Subject, b)
}

// Confirm keeps the question and answers the next yes or no.
func (f *fake) Confirm(q string, def bool) (bool, error) {
	f.shown = append(f.shown, "confirm "+q)
	return f.next().(bool), nil
}

// Say keeps what was said.
func (f *fake) Say(tn view.Tone, msg string) { f.said = append(f.said, msg) }

// Show keeps the title and lines.
func (f *fake) Show(title string, ls []view.Line) error {
	var out []string
	for _, l := range ls {
		out = append(out, l.Label+l.Text)
	}
	f.shown = append(f.shown, "show "+title+": "+strings.Join(out, "; "))
	return nil
}

// ShowScreen has no display files: none is shown.
func (f *fake) ShowScreen(name string, wait bool) (bool, error) { return false, nil }

type world struct {
	b      *bbs.BBS
	alice  *bbs.Node
	sysop  *bbs.Node
	e      *Engine
	logged *bytes.Buffer
}

// setup is a BBS with General and News (where only the sysop posts), nodes for
// alice and the sysop, and an engine with a short limit for the scripts given,
// name to source.
func setup(t *testing.T, scripts map[string]string) *world {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logged := &bytes.Buffer{}
	b := bbs.New(bbs.Defaults(), st, log.New(logged, "", 0))
	st.CreateArea("General", "Talk", false)
	st.CreateArea("News", "From the sysop", true)
	w := &world{b: b, logged: logged}
	for _, u := range []struct {
		name  string
		sysop bool
		n     **bbs.Node
	}{{"alice", false, &w.alice}, {"sysop", true, &w.sysop}} {
		us, err := st.CreateUser(u.name, "password1", "Here", u.sysop)
		if err != nil {
			t.Fatal(err)
		}
		n, err := b.Enter(&bbs.Caller{Via: "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		n.SetUser(us)
		*u.n = n
	}
	dir := t.TempDir()
	for name, src := range scripts {
		os.WriteFile(filepath.Join(dir, name+".js"), []byte(src), 0644)
	}
	w.e = &Engine{Dir: dir, Limit: 300 * time.Millisecond, cache: map[string]compiled{}}
	return w
}

// run runs the script for n with a fake giving the answers in turn; the fake,
// for what it was shown and said, and Run's error.
func (w *world) run(t *testing.T, name string, n *bbs.Node, answers ...any) (*fake, error) {
	t.Helper()
	f := &fake{t: t, answers: answers}
	err := w.e.Run(name, w.b, n, f)
	return f, err
}

// has fails the test unless one of all (the what, for the failure) holds want.
func has(t *testing.T, what string, all []string, want string) {
	t.Helper()
	for _, s := range all {
		if strings.Contains(s, want) {
			return
		}
	}
	t.Errorf("no %q among the %s: %q", want, what, all)
}

const oneliners = `
var lines = data.get("lines") || [];
for (;;) {
	var k = ui.menu({title: "One-liners", intro: lines.slice(-5),
		items: [{key: "a", label: "Add one"}, {key: "q", label: "Quit"}]});
	if (k !== "A") break;
	var v = ui.form({title: "Add a line", fields: [{name: "line", label: "Your line", width: 60}]});
	if (v && v.line) {
		lines.push(user.name + ": " + v.line);
		data.set("lines", lines.slice(-20));
		data.user.set("count", (data.user.get("count") || 0) + 1);
		ui.say("Added.", "good");
	}
}
`

// TestScreensAndData: a one-liners script's menu and form; what it keeps for
// everyone, and for alice alone.
func TestScreensAndData(t *testing.T) {
	w := setup(t, map[string]string{"oneliners": oneliners})
	f, err := w.run(t, "oneliners", w.alice, "A", map[string]string{"line": "hello <world>"}, "Q")
	if err != nil {
		t.Fatal(err)
	}
	has(t, "screens", f.shown, "menu One-liners: A=Add one Q=Quit")
	has(t, "said", f.said, "Added.")
	f, err = w.run(t, "oneliners", w.sysop, "Q")
	if err != nil {
		t.Fatal(err)
	}
	if v, _, _ := w.b.Store.ScriptGet("oneliners", 0, "lines"); v != `["alice: hello <world>"]` {
		t.Errorf("kept %s", v)
	}
	if v, _, _ := w.b.Store.ScriptGet("oneliners", w.alice.User().ID, "count"); v != "1" {
		t.Errorf("alice's count %s", v)
	}
	if _, ok, _ := w.b.Store.ScriptGet("oneliners", w.sysop.User().ID, "count"); ok {
		t.Error("the sysop's count")
	}
}

// TestTablesMessagesAndPosting: a table of the areas, a post written in the
// editor and read back as a message, a post where only the sysop posts thrown
// and caught, and a show's labels, lines and bars.
func TestTablesMessagesAndPosting(t *testing.T) {
	w := setup(t, map[string]string{"news": `
var areas = msg.areas();
var c = ui.table({title: "Areas", pick: "Area number",
	columns: [{title: "Area", width: 20}, {title: "Msgs", width: 5, right: true}],
	rows: areas.map(function (a) { return [a.name, a.messages]; })});
if (c.action === ui.PICK) {
	var a = areas[c.row];
	var d = ui.edit({title: "Post in " + a.name, to: "All", subject: "From a script"});
	if (d) {
		var id = msg.post(a.id, {to: d.to, subject: d.subject, body: d.body});
		var m = msg.get(id);
		ui.message({where: a.name, from: m.from, to: m.to, subject: m.subject, body: m.body, date: m.date,
			actions: [{id: "next", label: "Next", key: "n", pf: 11}]});
	}
	try {
		msg.post(areas[1].id, {subject: "x", body: "y"});
	} catch (e) {
		ui.say("Refused: " + e.message, "bad");
	}
	ui.show("You", [{label: "Name:", text: user.name}, {label: "Level:", text: String(user.level)},
		"two\nlines", {label: "Share", text: "3", bar: 0.5}]);
}
`})
	f, err := w.run(t, "news", w.alice, view.Choice{Action: view.Pick, Row: 0}, []string{"Line one", "Line two"}, "next")
	if err != nil {
		t.Fatal(err)
	}
	has(t, "screens", f.shown, "table Areas: 1:General|0; 2:News|0")
	has(t, "screens", f.shown, "message From a script: Line one\nLine two")
	has(t, "said", f.said, "Refused: only the sysop posts here")
	has(t, "screens", f.shown, "show You: Name:alice; Level:20; two; lines; Share3")
	m, err := w.b.Store.Message(1)
	if err != nil || m.From != "alice" {
		t.Errorf("posted %+v %v", m, err)
	}
}

// TestRunaway: a script computing too long is stopped, and the caller told; the
// time the caller takes over a screen is not counted.
func TestRunaway(t *testing.T) {
	w := setup(t, map[string]string{"loop": `for (;;) {}`, "slow": `
var k = ui.menu({title: "Slow", items: [{key: "x", label: "X"}]});
ui.say("got " + k);
`})
	f, err := w.run(t, "loop", w.alice)
	if err != nil {
		t.Fatal(err)
	}
	has(t, "said", f.said, "The script loop took too long and was stopped.")
	// the time the caller takes is theirs
	f = &fake{t: t, answers: []any{"X"}, delay: 500 * time.Millisecond}
	if err := w.e.Run("slow", w.b, w.alice, f); err != nil {
		t.Fatal(err)
	}
	has(t, "said", f.said, "got X")
}

// TestFailures: a script's failure is logged and said (with the error, to the
// sysop); a syntax error, or a script not there, would not run; the caller
// hanging up ends a script though it catches it; exit; data's names and values;
// valid names, and the scripts in the directory.
func TestFailures(t *testing.T) {
	w := setup(t, map[string]string{
		"bad":    `null.x = 1;`,
		"syntax": `function (`,
		"catch":  `try { ui.menu({title: "M", items: [{key: "a", label: "A"}]}); } catch (e) {} for (;;) { ui.say("again"); }`,
		"exit":   `ui.say("one"); exit(); ui.say("two");`,
		"keys":   `try { data.set("", 1); } catch (e) { ui.say(e.message); } data.set("k", {a: [1, 2]}); ui.say(JSON.stringify(data.get("k")) + " " + data.keys("").join(","));`,
	})
	f, err := w.run(t, "bad", w.alice)
	if err != nil {
		t.Fatal(err)
	}
	has(t, "said", f.said, "The script bad failed.")
	f, _ = w.run(t, "bad", w.sysop)
	has(t, "said", f.said, "The script bad failed: TypeError: Cannot convert undefined or null to object")
	if !strings.Contains(w.logged.String(), "script bad: TypeError") {
		t.Errorf("log: %s", w.logged)
	}
	if err := w.e.Check("syntax"); err == nil {
		t.Error("syntax error passed")
	}
	f, _ = w.run(t, "syntax", w.alice)
	has(t, "said", f.said, "The script syntax would not run.")

	// the caller going ends the script, whatever it catches
	f = &fake{t: t, fail: io.EOF}
	if err := w.e.Run("catch", w.b, w.alice, f); !errors.Is(err, io.EOF) {
		t.Errorf("hung up: %v", err)
	}
	if len(f.said) > 1 {
		t.Errorf("went on: %q", f.said)
	}

	f, err = w.run(t, "exit", w.alice)
	if err != nil || strings.Join(f.said, ",") != "one" {
		t.Errorf("exit: %v %q", err, f.said)
	}
	f, _ = w.run(t, "keys", w.alice)
	if strings.Join(f.said, "|") != `a name is 1 to 100 characters|{"a":[1,2]} k` {
		t.Errorf("data: %q", f.said)
	}
	f, _ = w.run(t, "nosuch", w.alice)
	has(t, "said", f.said, "The script nosuch would not run.")
	if ValidName("../etc") || ValidName("Upper") || !ValidName("one-liners_2") {
		t.Error("names")
	}
	if strings.Join(w.e.Names(), " ") != "bad catch exit keys syntax" {
		t.Errorf("names %v", w.e.Names())
	}
}

// TestPollSample: the sample poll, a vote and a new question.
func TestPollSample(t *testing.T) {
	w := setup(t, nil)
	w.e.Dir = filepath.Join("..", "..", "scripts.sample")
	f, err := w.run(t, "poll", w.alice, view.Choice{Action: view.Pick, Row: 2})
	if err != nil {
		t.Fatal(err)
	}
	has(t, "screens", f.shown, "table Which terminal do you call with?: 1:An ANSI terminal; 2:A Commodore; 3:A 3270")
	has(t, "said", f.said, "Your vote is counted.")
	has(t, "screens", f.shown, "show Poll results: Which terminal do you call with?; ; An ANSI terminal0; A Commodore0; A 32701")
	// a second call: no second vote
	f, _ = w.run(t, "poll", w.alice)
	if strings.Contains(strings.Join(f.shown, "\n"), "table") {
		t.Errorf("voted twice: %q", f.shown)
	}
	f, err = w.run(t, "poll", w.sysop, view.Choice{Action: view.Back}, true,
		map[string]string{"q": "Tea or coffee?", "a": "Tea | Coffee |"})
	if err != nil {
		t.Fatal(err)
	}
	has(t, "said", f.said, "The new poll is up.")
	f, _ = w.run(t, "poll", w.alice, view.Choice{Action: view.Pick, Row: 1})
	has(t, "screens", f.shown, "table Tea or coffee?: 1:Tea; 2:Coffee")
	has(t, "screens", f.shown, "Tea0; Coffee1; ; 1 vote")
}

// TestES6AndRequire: classes, let, Map, arrow functions and template strings
// run; user.has meets alice's level but not a higher one, nor a flag she lacks.
func TestES6AndRequire(t *testing.T) {
	w := setup(t, map[string]string{"es6": "class A { constructor(n) { this.n = n; } twice() { return this.n * 2; } }\n" +
		"let m = new Map([[1, 'one']]);\n" +
		"const f = (x) => `${x}!`;\n" +
		"ui.say(f(new A(21).twice()) + ' ' + m.get(1) + ' ' + user.has('20') + ' ' + user.has('30') + ' ' + user.has('S'));"})
	f, err := w.run(t, "es6", w.alice)
	if err != nil || strings.Join(f.said, "") != "42! one true false false" {
		t.Errorf("%v %q", err, f.said)
	}
}
