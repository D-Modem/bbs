package nntp

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/ntlm"
)

// TestNTLM: AUTHINFO GENERIC NTLM, as Internet News signs in to an MSN
// news server.
func TestNTLM(t *testing.T) {
	s := newSite(t, "news.a.test", ServerSettings{Post: true, ReadUsers: true}, nil)
	cv := dialConv(t, s.addr)
	cv.send("AUTHINFO GENERIC")
	cv.expect("485") // not while the hashes aren't kept

	st := s.b.Store
	st.SetKeepNTLM(true)
	alice, _ := st.UserByName("alice")
	st.SetPassword(alice.ID, "alicepass1")
	cv.send("AUTHINFO GENERIC")
	cv.expect("281")
	if got := cv.block(); got != "NTLM\r\nMSN\r\n" {
		t.Fatalf("mechanisms: %q", got)
	}
	login := func(mech, pw string) string {
		cv.send("AUTHINFO GENERIC " + mech)
		cv.expect("381")
		cv.send("AUTHINFO GENERIC " + base64.StdEncoding.EncodeToString(ntlm.ClientNegotiate(ntlm.FlagOEM|ntlm.FlagNTLM|ntlm.FlagRequestTarget)))
		ch, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(cv.expect("381 "), "381 "))
		if err != nil || ntlm.Type(ch) != ntlm.TypeChallenge {
			t.Fatalf("challenge: %x %v", ch, err)
		}
		cv.send("AUTHINFO GENERIC " + base64.StdEncoding.EncodeToString(
			ntlm.ClientAuthenticate(ntlm.FlagUnicode|ntlm.FlagNTLM, "", "alice", ntlm.DESL(ntlm.LMHash(pw), ch[24:32]), ntlm.DESL(ntlm.NTHash(pw), ch[24:32]))))
		return cv.expect("")
	}
	if got := login("NTLM", "wrong"); !strings.HasPrefix(got, "502") {
		t.Fatalf("a wrong password: %q", got)
	}
	if got := login("MSN", "alicepass1"); !strings.HasPrefix(got, "281") {
		t.Fatalf("login: %q", got)
	}
	cv.send("GROUP bbs.general")
	cv.expect("211")
}
