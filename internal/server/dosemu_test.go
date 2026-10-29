package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/door"
)

// TestDosemuDoor: a caller plays a DOS door (testdoor/FOSDOOR.COM, which
// talks by FOSSIL) in the bbs-dosemu2 image, when Docker has it.
func TestDosemuDoor(t *testing.T) {
	if err := exec.Command("docker", "image", "inspect", "bbs-dosemu2").Run(); err != nil {
		t.Skip("no bbs-dosemu2 image (docker build -t bbs-dosemu2 doors/dosemu2)")
	}
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	doors := filepath.Join(e.dir, "dos")
	os.MkdirAll(filepath.Join(doors, "TEST"), 0755)
	com, err := os.ReadFile("../../doors/dosemu2/testdoor/FOSDOOR.COM")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(doors, "TEST", "FOSDOOR.COM"), com, 0644)
	// the node's directory, writable by the door's user (the BBS's own, but
	// these tests may run as root, which dosemu2 will not run doors as)
	node := filepath.Join(e.dir, "node1")
	os.Mkdir(node, 0777)
	os.Chmod(node, 0777)
	cmd, _ := door.Split(`docker run --rm -i --name bbs-test-door{node} --user 4242:4242 -v ` + doors +
		`:/doors -v {dir}:/node bbs-dosemu2 'F:\TEST' 'FOSDOOR.COM G:\DOOR.SYS'`)
	e.b.Doors = []*door.Door{{Name: "FOSSIL test", Command: cmd, Dir: filepath.Join(e.dir, "node{node}"),
		DropFiles: door.DropFiles, Charset: door.CP437, TimeLimit: time.Minute, CommType: 1,
		Cleanup: []string{"docker", "rm", "-f", "bbs-test-door{node}"}}}
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("Command: ")
	d.send("D")
	d.expect("Door number (Enter to go back): ")
	d.send("1\r")
	d.expect("FOSDOOR: the FOSSIL works.")
	d.expect("Hello, alice")
	d.expect("Type keys, Q to quit.")
	d.send("z")
	d.expect("You typed z")
	d.send("q")
	d.expect("Bye!")
	d.expect("Back on ")

	// in again, and hang up in it: the door's container goes
	d.expect("Door number (Enter to go back): ")
	d.send("1\r")
	d.expect("Type keys, Q to quit.")
	d.c.Close()
	for i := 0; ; i++ {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "name=bbs-test-door").Output()
		if len(out) == 0 {
			break
		}
		if i == 100 {
			t.Fatal("the door's container is still running after the caller hung up")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
