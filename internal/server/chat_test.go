package server

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// login is a caller on modem.sock logged in as name, at the main menu.
func login(e *env, name, pw string) *dialog {
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send(name + "\r")
	d.expect("Password: ")
	d.send(pw + "\r")
	d.expect("Command: ")
	return d
}

// TestChatAndPage: two callers on the text BBS: the sysop waits in chat,
// alice pages, the sysop answers, they talk; a node message; the main room.
func TestChatAndPage(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	sysop := login(e, "sysop", "sysoppass1")
	sysop.send("C")
	sysop.expect("You are in main.")
	sysop.expect("> ")

	alice := login(e, "alice", "alicepass1")
	alice.send("S")
	alice.expect("Page the sysop about (Enter to give up): ")
	alice.send("my upload failed\r")
	alice.expect("The sysop has been paged.")
	alice.expect("You are in sysop-2.")

	sysop.expect("** alice (node 2) is paging you: my upload failed  (Chat, then /answer) **")
	sysop.send("/answer\r")
	sysop.expect("You are with the caller on node 2 (room sysop-2).")
	alice.expect("-> sysop came in")
	alice.expect("sysop answered your page.")
	sysop.send("hello alice, what file?\r")
	alice.expect("sysop: hello alice, what file?")
	alice.send("doom.zip\r")
	sysop.expect("alice: doom.zip")
	alice.send("/quit\r")
	alice.expect("You left the chat.")
	sysop.expect("<- alice left")
	sysop.send("/join main\r")
	sysop.expect("You are in main.")

	// a node message, from who's online
	alice.expect("Command: ")
	alice.send("W")
	alice.expect("Send a node a message (its number, Enter for none): ")
	alice.send("1\r")
	alice.expect("Message: ")
	alice.send("thanks!\r")
	alice.expect("Sent.")
	sysop.expect("** Message from alice (node 2): thanks! **")

	// the main room, and /who
	alice.expect("Command: ")
	alice.send("C")
	alice.expect("You are in main.")
	sysop.expect("-> alice came in")
	alice.send("/who\r")
	alice.expect("In main: alice (2), sysop (1)")
	alice.send("/me waves\r")
	sysop.expect("* alice waves")
	sysop.send("/msg 9 hi\r")
	sysop.expect("there is no one on that node.")
}

// TestChat3270: a 3270 caller (s3270) gets a node message on the main
// menu's message line, and chats with a text caller.
func TestChat3270(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true},
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	sysop := login(e, "sysop", "sysoppass1")

	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)",
		`String("alice")`, "Tab()", `String("alicepass1")`, "Enter()", "Wait(5,InputField)",
		"Wait(2,Seconds)", "Enter()", "Wait(5,InputField)", "Ascii()",
		`String("c")`, "Enter()", "Wait(5,InputField)",
		`String("hello from a 3270")`, "Enter()", "Wait(5,InputField)",
		"Wait(2,Seconds)", "PF(5)", "Wait(5,InputField)", "Ascii()",
		"PF(3)", "Wait(5,InputField)", "Quit()", ""}, "\n")
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		cmd := exec.Command("s3270", "-model", "3279-2")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		done <- result{out, err}
	}()
	// alice is on node 2 once logged in
	aliceNode := 0
	for i := 0; i < 250 && aliceNode == 0; i++ {
		for _, n := range e.b.Nodes() {
			if n.User == "alice" {
				aliceNode = n.Num
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	sysop.send("W")
	sysop.expect("Send a node a message (its number, Enter for none): ")
	sysop.send(fmt.Sprintf("%d\r", aliceNode))
	sysop.expect("Message: ")
	sysop.send("welcome to the board\r")
	sysop.expect("Sent.")
	sysop.expect("Command: ")
	sysop.send("C")
	sysop.expect("-> alice came in")
	sysop.expect("alice: hello from a 3270")
	sysop.send("hi there, 3270\r")
	r := <-done
	if r.err != nil {
		t.Fatalf("s3270: %v\n%s", r.err, r.out)
	}
	out := string(r.out)
	for _, want := range []string{"Message from sysop (node " + fmt.Sprint(sysopNode(e)) + "): welcome to the board",
		"alice: hello from a 3270", "sysop: hi there, 3270", "Chat: main"} {
		if !strings.Contains(out, want) {
			t.Errorf("the 3270 did not show %q:\n%s", want, out)
		}
	}
	sysop.expect("<- alice left")
}

// sysopNode is the sysop's node, 0 if the sysop is not on.
func sysopNode(e *env) int {
	for _, n := range e.b.Nodes() {
		if n.User == "sysop" {
			return n.Num
		}
	}
	return 0
}
