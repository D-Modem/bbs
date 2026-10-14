package sockets

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
)

// TestInherit: sockets named in the environment are taken by Listen for
// their addresses; what nothing takes is closed by Left.
func TestInherit(t *testing.T) {
	a, _ := net.Listen("tcp", "127.0.0.1:0")
	b, _ := net.Listen("tcp", "127.0.0.1:0")
	fa, _ := a.(*net.TCPListener).File()
	fb, _ := b.(*net.TCPListener).File()
	addrA, addrB := a.Addr().String(), b.Addr().String()
	a.Close()
	b.Close()
	os.Setenv(env, fmt.Sprintf("%d tcp %s\n%d tcp %s", fa.Fd(), addrA, fb.Fd(), addrB))
	if err := Inherit(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(env) != "" {
		t.Error("the environment keeps it")
	}
	ln, err := Listen("tcp", addrA)
	if err != nil || ln.Addr().String() != addrA {
		t.Fatalf("%v %v", ln, err)
	}
	defer ln.Close()
	go func() {
		if c, err := net.Dial("tcp", addrA); err == nil {
			c.Write([]byte("hi"))
			c.Close()
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if Take("tcp", addrA) != nil {
		t.Error("taken twice")
	}
	if left := Left(); len(left) != 1 || left[0] != "tcp "+addrB {
		t.Errorf("left %q", left)
	}
	os.Setenv(env, "x tcp :1")
	if err := Inherit(); err == nil {
		t.Error("a bad descriptor taken")
	}
}

func TestLookup(t *testing.T) {
	if _, err := Lookup("no-such-user-here", ""); err == nil || !strings.Contains(err.Error(), "no such user") {
		t.Errorf("%v", err)
	}
	if _, err := Lookup("root", ""); err == nil {
		t.Error("root taken")
	}
	if a, err := Lookup("nobody", ""); err == nil && (a.UID == 0 || a.User != "nobody") {
		t.Errorf("%+v", a)
	}
	if _, err := Lookup("nobody", "no-such-group-here"); err == nil {
		t.Error("a group that is not there")
	}
}
