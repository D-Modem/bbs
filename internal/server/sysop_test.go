package server

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// writeFile writes s to p, mode 0600.
func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0600) }

// contains is strings.Contains.
func contains(s, sub string) bool { return strings.Contains(s, sub) }

// containsAny reports whether sub is in ss, joined by newlines.
func containsAny(ss []string, sub string) bool {
	return strings.Contains(strings.Join(ss, "\n"), sub)
}

// TestSysopTools: the sysop watches a caller's screen, calls them into
// chat, and is away when they page.
func TestSysopTools(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	sysop := login(e, "sysop", "sysoppass1")
	alice := login(e, "alice", "alicepass1")
	aliceNode := 0
	for _, n := range e.b.Nodes() {
		if n.User == "alice" {
			aliceNode = n.Num
		}
	}

	sysop.send("*")
	sysop.expect("[W] Watch a node")
	sysop.send("W")
	sysop.expect("Watch which node? (its number, Enter for none): ")
	sysop.send(fmt.Sprintf("%d\r", aliceNode))
	sysop.expect(fmt.Sprintf("Watching node %d", aliceNode))
	alice.send("L")
	alice.expect("Last callers")
	sysop.expect("Last callers") // what alice was sent
	sysop.send(" ")
	sysop.expect(fmt.Sprintf("Watching node %d no more.", aliceNode))

	sysop.send("C")
	sysop.expect("Chat with which node?")
	sysop.send(fmt.Sprintf("%d\r", aliceNode))
	sysop.expect("is called here at their next menu")
	sysop.expect("> ")
	alice.expect("would like a word with you")
	alice.send("\r")
	alice.expect("The sysop would like a word.")
	alice.expect(fmt.Sprintf("You are in sysop-%d.", aliceNode))
	sysop.expect("-> alice came in")
	sysop.send("all well?\r")
	alice.expect("sysop: all well?")
	alice.send("/quit\r")
	alice.expect("Command: ")
	sysop.send("/quit\r")
	sysop.expect("[A] Available: yes")

	sysop.send("A")
	sysop.expect("[A] Available: no")
	alice.send("S")
	alice.expect("Page the sysop about (Enter to give up): ")
	alice.send("a question\r")
	alice.expect("The sysop is not available just now.")
}

// TestEventConfig: [event NAME]s at a time or every so many minutes, a
// missing program a warning; no command, no time, both, bad quotes and an
// event twice refused.
func TestEventConfig(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) (*Config, error) {
		p := dir + "/bbs.conf"
		if err := writeFile(p, "[listen t]\ntype = telnet\naddress = :2300\n"+body); err != nil {
			t.Fatal(err)
		}
		return Load(p)
	}
	c, err := load("[event backup]\ncommand = /bin/echo \"nightly backup\"\nat = 03:30\ntimeout = 60\n" +
		"[event poll]\ncommand = /bin/true\nevery = 15\n")
	if err != nil || len(c.Events) != 2 || c.Events[0].At != "03:30" || c.Events[0].Command[1] != "nightly backup" ||
		c.Events[0].Timeout.Seconds() != 60 || c.Events[1].Every.Minutes() != 15 {
		t.Fatalf("%+v %v", c.Events, err)
	}
	c, err = load("[event x]\ncommand = /no/such/program\nevery = 5\n")
	if err != nil || !containsAny(c.Warnings, "/no/such/program") {
		t.Errorf("a missing program: %v %v", c.Warnings, err)
	}
	for body, want := range map[string]string{
		"[event x]\nevery = 5\n":                                                                 "command: none given",
		"[event x]\ncommand = /bin/true\n":                                                       "at = HH:MM",
		"[event x]\ncommand = /bin/true\nat = 3:30\n":                                            "not a time of day",
		"[event x]\ncommand = /bin/true\nat = 03:30\nevery = 5\n":                                "not both",
		"[event x]\ncommand = \"/bin/true\nevery = 5\n":                                          "quote",
		"[event x]\ncommand = /bin/true\nevery = 5\n[event X]\ncommand = /bin/true\nevery = 5\n": "given twice",
	} {
		if _, err := load(body); err == nil || !contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}
