package ntlm

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func unhex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// MS-NLMP 4.2: the password "Password", user "User", domain "Domain", the
// server challenge 0123456789abcdef and the client's aaaaaaaaaaaaaaaa.
var (
	pw        = "Password"
	challenge = unhex("0123456789abcdef")
	client    = unhex("aaaaaaaaaaaaaaaa")
)

func TestV1(t *testing.T) {
	lm, nt := LMHash(pw), NTHash(pw)
	if got := hex.EncodeToString(DESL(lm, challenge)); got != "98def7b87f88aa5dafe2df779688a172def11c7d5ccdef13" {
		t.Errorf("LM response: %s", got)
	}
	ntResp := DESL(nt, challenge)
	if got := hex.EncodeToString(ntResp); got != "67c43011f30298a2ad35ece64f16331c44bdbed927841f94" {
		t.Errorf("NT response: %s", got)
	}
	a := &Auth{User: "User", NT: ntResp, LM: DESL(lm, challenge)}
	if !a.Verify(lm, nt, challenge) {
		t.Error("v1 not accepted")
	}
	if a.Verify(LMHash("other"), NTHash("other"), challenge) {
		t.Error("v1 with the wrong password accepted")
	}
	if got := hex.EncodeToString(a.SessionKey(lm, nt, challenge)); got != "d87262b0cde4b1cb7499becccdf10784" {
		t.Errorf("session base key: %s", got)
	}
	// LM alone, as MSN Classic's 1996 client sends it
	if !(&Auth{LM: DESL(lm, challenge)}).Verify(lm, nt, challenge) {
		t.Error("LM alone not accepted")
	}
}

// 4.2.3: NTLMv1 with extended session security.
func TestESS(t *testing.T) {
	a := &Auth{Flags: FlagESS, User: "User", LM: append(append([]byte(nil), client...), make([]byte, 16)...),
		NT: unhex("7537f803ae367128ca458204bde7caf81e97ed2683267232")}
	if !a.Verify(LMHash(pw), NTHash(pw), challenge) {
		t.Error("ESS not accepted")
	}
	if a.Verify(LMHash(pw), NTHash("other"), challenge) {
		t.Error("ESS with the wrong password accepted")
	}
}

// 4.2.4: NTLMv2.
func TestV2(t *testing.T) {
	av := func(id uint16, s string) []byte {
		v := utf16le(s)
		b := binary.LittleEndian.AppendUint16(nil, id)
		b = binary.LittleEndian.AppendUint16(b, uint16(len(v)))
		return append(b, v...)
	}
	info := append(append(av(2, "Domain"), av(1, "Server")...), 0, 0, 0, 0)
	temp := append([]byte{1, 1, 0, 0, 0, 0, 0, 0}, make([]byte, 8)...) // time 0
	temp = append(append(append(temp, client...), 0, 0, 0, 0), info...)
	temp = append(temp, 0, 0, 0, 0)
	a := &Auth{User: "User", Domain: "Domain",
		NT: append(unhex("68cd0ab851e51c96aabc927bebef6a1c"), temp...)}
	if !a.Verify(nil, NTHash(pw), challenge) {
		t.Error("v2 not accepted")
	}
	if a.Verify(nil, NTHash("other"), challenge) {
		t.Error("v2 with the wrong password accepted")
	}
	lmv2 := &Auth{User: "User", Domain: "Domain", LM: append(unhex("86c35097ac9cec102554764a57cccc19"), client...)}
	if !lmv2.Verify(LMHash(pw), NTHash(pw), challenge) {
		t.Error("LMv2 not accepted")
	}
}

func TestSASL(t *testing.T) {
	var got *Auth
	s := &SASL{Exchange: Exchange{Target: "BBS"}, Login: func(a *Auth, c []byte) error {
		if !a.Verify(LMHash(pw), NTHash(pw), c) {
			return ErrFailed
		}
		got = a
		return nil
	}}
	if c, done, err := s.Next(nil); c != nil || done || err != nil {
		t.Fatalf("first: %x %v %v", c, done, err)
	}
	c, done, err := s.Next(ClientNegotiate(FlagUnicode | FlagNTLM | FlagRequestTarget))
	if err != nil || done || Type(c) != TypeChallenge {
		t.Fatalf("challenge: %x %v %v", c, done, err)
	}
	flags := binary.LittleEndian.Uint32(c[20:])
	if flags&FlagUnicode == 0 || flags&FlagTargetInfo == 0 {
		t.Fatalf("flags %#x", flags)
	}
	n, off := binary.LittleEndian.Uint16(c[12:]), binary.LittleEndian.Uint32(c[16:])
	if !bytes.Equal(c[off:off+uint32(n)], utf16le("BBS")) {
		t.Fatalf("target name: %x", c[off:off+uint32(n)])
	}
	ch := c[24:32]
	if _, done, err := s.Next(ClientAuthenticate(flags, "BBS", "alice", nil, DESL(NTHash(pw), ch))); !done || err != nil {
		t.Fatalf("authenticate: %v %v", done, err)
	}
	if got == nil || got.User != "alice" || got.Domain != "BBS" || got.Workstation != "WS" {
		t.Fatalf("auth: %+v", got)
	}
	// the challenge answers once
	s = &SASL{Login: func(a *Auth, c []byte) error { return nil }}
	s.Next(ClientNegotiate(FlagOEM | FlagNTLM))
	s.Next(ClientAuthenticate(0, "", "x", nil, nil))
	if _, _, err := s.Next(nil); err == nil {
		t.Fatal("a third step accepted")
	}
}
