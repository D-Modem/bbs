package email

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-smtp"

	"github.com/D-Modem/bbs/internal/ntlm"
)

// ntlmClient is a client's NTLM, as Outlook Express's: v1, in Unicode.
type ntlmClient struct {
	mech, user, pw string
}

func (c *ntlmClient) Start() (string, []byte, error) {
	return c.mech, ntlm.ClientNegotiate(ntlm.FlagUnicode | ntlm.FlagNTLM | ntlm.FlagRequestTarget), nil
}

func (c *ntlmClient) Next(challenge []byte) ([]byte, error) {
	return c.answer(challenge), nil
}

func (c *ntlmClient) answer(ch []byte) []byte {
	return ntlm.ClientAuthenticate(ntlm.FlagUnicode|ntlm.FlagNTLM, "B", c.user, nil, ntlm.DESL(ntlm.NTHash(c.pw), ch[24:32]))
}

func TestNTLM(t *testing.T) {
	b := newSite(t, "b.test", &fakeDNS{}, []string{"alice", "bob"}, "pop3", "imap", Submission)
	// not offered while the hashes aren't kept
	c, err := net.Dial("tcp", b.addrs["pop3"])
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	pop(t, r, c, "", false)
	if got := pop(t, r, c, "AUTH", true); strings.Contains(got, "NTLM") {
		t.Errorf("offered: %q", got)
	}
	c.Close()

	b.st.SetKeepNTLM(true)
	b.st.SetPassword(b.users["bob"].ID, "password1") // kept as it's set
	c, err = net.Dial("tcp", b.addrs["pop3"])
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r = bufio.NewReader(c)
	pop(t, r, c, "", false)
	if got := pop(t, r, c, "AUTH", true); !strings.Contains(got, "\r\nNTLM\r\nMSN\r\n") {
		t.Errorf("not offered: %q", got)
	}
	popNTLM := func(mech, user, pw string) string {
		cl := &ntlmClient{mech, user, pw}
		if got := pop(t, r, c, "AUTH "+mech, false); got != "+OK\r\n" {
			t.Fatalf("AUTH %s: %q", mech, got)
		}
		_, neg, _ := cl.Start()
		got := pop(t, r, c, base64.StdEncoding.EncodeToString(neg), false)
		ch, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(got, "+ ")))
		if err != nil || ntlm.Type(ch) != ntlm.TypeChallenge {
			t.Fatalf("challenge: %q", got)
		}
		return pop(t, r, c, base64.StdEncoding.EncodeToString(cl.answer(ch)), false)
	}
	if got := popNTLM("NTLM", "bob", "wrong"); !strings.HasPrefix(got, "-ERR") {
		t.Errorf("a wrong password: %q", got)
	}
	if got := popNTLM("NTLM", "alice", "password1"); !strings.HasPrefix(got, "-ERR") {
		t.Errorf("no hashes kept yet: %q", got)
	}
	if got := popNTLM("MSN", "bob@b.test", "password1"); !strings.HasPrefix(got, "+OK bob has ") {
		t.Errorf("by MSN's name: %q", got)
	}

	// SMTP submission and IMAP, by the same
	sc, err := smtp.Dial(b.addrs[Submission])
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Auth(&ntlmClient{"NTLM", "bob", "wrong"}); err == nil {
		t.Error("SMTP: a wrong password accepted")
	}
	if err := sc.Auth(&ntlmClient{"NTLM", "bob", "password1"}); err != nil {
		t.Errorf("SMTP: %v", err)
	}
	sc.Close()

	ic, err := imapclient.DialInsecure(b.addrs["imap"], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ic.Close()
	if err := ic.Authenticate(&ntlmClient{"MSN", "bob", "password1"}); err != nil {
		t.Errorf("IMAP: %v", err)
	}
	if _, err := ic.Select("INBOX", nil).Wait(); err != nil {
		t.Errorf("IMAP select: %v", err)
	}
}
