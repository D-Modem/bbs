package bbs

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
)

func TestQWK(t *testing.T) {
	b, ns := chatBBS(t)
	alice, bob := ns["alice"], ns["bob"]
	b.Name = "The Test Board"
	if b.BBSID() != "THETESTB" {
		t.Errorf("BBSID %q", b.BBSID())
	}
	gen, _ := b.Store.CreateArea("general", "", false)
	quiet, _ := b.Store.CreateArea("quiet", "", false)
	first, err := bob.Post(gen, "All", "Hello", []string{"first"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	bob.Post(gen, "alice", "For you", []string{"second"}, 0)
	bob.Post(quiet, "All", "Shh", []string{"third"}, 0)
	b.Store.SetScan(alice.User().ID, quiet.ID, false)
	if err := bob.SendMail("alice", "Private", []string{"just us"}, 0); err != nil {
		t.Fatal(err)
	}

	out, err := alice.QWKPacket()
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "THETESTB.QWK" || out.Messages != 3 || out.Mail != 1 {
		t.Errorf("packet %s: %d messages, %d mail", out.Name, out.Messages, out.Mail)
	}
	p, err := qwk.ReadPacket(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Confs) != 3 || p.Messages[0].Conf != 0 || p.Messages[0].Subject != "Private" ||
		p.Messages[1].Conf != int(gen.ID) || p.Messages[2].Subject != "For you" {
		t.Errorf("packet %+v", p)
	}
	// not taken yet: the same again
	if again, _ := alice.QWKPacket(); again.Messages != 3 {
		t.Errorf("before Done: %d", again.Messages)
	}
	if err := out.Done(); err != nil {
		t.Fatal(err)
	}
	if again, _ := alice.QWKPacket(); again.Messages != 0 {
		t.Errorf("after Done: %d", again.Messages)
	}

	var rep bytes.Buffer
	qwk.WriteREP(&rep, "THETESTB", []qwk.Message{
		{Conf: int(gen.ID), To: "bob", From: "forged", Subject: "Re: Hello", Ref: int(first), Body: "hi bob"},
		{Conf: 0, To: "bob", Subject: "Re: Private", Body: "ok"},
		{Conf: 999, To: "All", Subject: "Nowhere", Body: "x"},
	})
	res, err := alice.ImportREP(rep.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if res.Posted != 1 || res.Mailed != 1 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "no conference 999") {
		t.Errorf("result %+v", res)
	}
	ms, _ := b.Store.Messages(gen.ID, first, 10)
	if m := ms[len(ms)-1]; m.From != "alice" || m.ReplyTo != first || m.Body != "hi bob" {
		t.Errorf("posted %+v", m)
	}
	if n, _ := b.Store.UnreadMail(bob.User().ID); n != 1 {
		t.Errorf("bob's mail: %d", n)
	}
	// the same REP again posts nothing
	if res, _ := alice.ImportREP(rep.Bytes()); res.Posted+res.Mailed != 0 || res.Duplicates != 2 {
		t.Errorf("again: %+v", res)
	}
	if _, err := alice.ImportREP([]byte("junk")); err == nil {
		t.Error("junk taken")
	}
}
