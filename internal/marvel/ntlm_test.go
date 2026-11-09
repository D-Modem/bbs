package marvel

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/D-Modem/bbs/internal/store"
)

// MS-NLMP 4.2.2: NTLMv1 with the password "Password" and the challenge
// 0123456789abcdef.
func TestNTLMv1Vectors(t *testing.T) {
	c, _ := hex.DecodeString("0123456789abcdef")
	if got := hex.EncodeToString(desl(store.LMHash("Password"), c)); got != "98def7b87f88aa5dafe2df779688a172def11c7d5ccdef13" {
		t.Errorf("LM response: %s", got)
	}
	if got := hex.EncodeToString(desl(store.NTHash("Password"), c)); got != "67c43011f30298a2ad35ece64f16331c44bdbed927841f94" {
		t.Errorf("NT response: %s", got)
	}
	a := &ntlmAuth{}
	if got := hex.EncodeToString(a.sessionKey(store.LMHash("Password"), store.NTHash("Password"), c)); got != "d87262b0cde4b1cb7499becccdf10784" {
		t.Errorf("session base key: %s", got)
	}
}

func TestSealer(t *testing.T) {
	key := bytes.Repeat([]byte{0x55}, 16)
	srv, _ := newSealer(0x82a2, key)
	cli, _ := newSealer(0x82a2, key)
	for i, m := range []string{"old\x00new\x00", "again"} {
		data, token := cli.seal([]byte(m))
		if bytes.Equal(data, []byte(m)) || len(token) != sealTokenLen {
			t.Fatalf("%d: not sealed", i)
		}
		if got, ok := srv.unseal(data, token); !ok || string(got) != m {
			t.Fatalf("%d: %q %v", i, got, ok)
		}
	}
	// out of order: the sequence number doesn't match
	cli.seal([]byte("lost"))
	data, token := cli.seal([]byte("next"))
	if _, ok := srv.unseal(data, token); ok {
		t.Fatal("a skipped message not noticed")
	}
}

// the 1996 client's tokens, as it builds them
const osr2Flags = 0x000082a2

func negotiateToken() []byte {
	b := append([]byte(nil), ntlmSig...)
	b = binary.LittleEndian.AppendUint32(b, ntlmTypeNegotiate)
	b = binary.LittleEndian.AppendUint32(b, osr2Flags)
	return append(b, make([]byte, 16)...)
}

// authenticateToken has the short header: five fields and no flags.
func authenticateToken(user string, lmResp []byte) []byte {
	payload := [][]byte{lmResp, nil, nil, []byte(user), []byte("WIN95")}
	b := append([]byte(nil), ntlmSig...)
	b = binary.LittleEndian.AppendUint32(b, ntlmTypeAuthenticate)
	off := 52
	for _, p := range payload {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(p)))
		b = binary.LittleEndian.AppendUint16(b, uint16(len(p)))
		b = binary.LittleEndian.AppendUint32(b, uint32(off))
		off += len(p)
	}
	for _, p := range payload {
		b = append(b, p...)
	}
	return b
}

// ntlmLogin signs on by NTLM, and gives the reply's word and result, and
// the client's sealing.
func (c *client) ntlmLogin(t *testing.T, user, pw string) (uint32, uint32, *sealer) {
	t.Helper()
	r := c.call(1, logsrvIface, logNTLMNegotiate, vparam(wrapToken(ntlmTypeNegotiate, negotiateToken())), []byte{0x84})
	if len(r) < 3+4+48 || r[0] != tagVar {
		t.Fatalf("challenge: %x", r)
	}
	ch := r[3+4:]
	if !bytes.Equal(ch[:8], ntlmSig) || binary.LittleEndian.Uint32(ch[8:]) != ntlmTypeChallenge ||
		binary.LittleEndian.Uint32(ch[20:]) != osr2Flags {
		t.Fatalf("challenge token: %x", ch[:48])
	}
	challenge := ch[24:32]
	lmResp := desl(store.LMHash(pw), challenge)
	r = c.call(1, logsrvIface, logNTLMAuth, vparam(wrapToken(ntlmTypeAuthenticate, authenticateToken(user, lmResp))), []byte{0x82, 0x83})
	if len(r) != 3+5+1 || r[0] != tagWord || r[3] != tagDword || r[8] != tagEndStatic {
		t.Fatalf("sign-on reply: %x", r)
	}
	a := &ntlmAuth{flags: osr2Flags, lmResp: lmResp}
	s, _ := newSealer(osr2Flags, a.sessionKey(store.LMHash(pw), store.NTHash(pw), challenge))
	return uint32(binary.LittleEndian.Uint16(r[1:])), binary.LittleEndian.Uint32(r[4:]), s
}

func TestNTLMSignOn(t *testing.T) {
	srv, st := newServer(t)
	// bob's password was set before the hashes were kept: none yet
	st.SetKeepNTLM(false)
	if _, err := st.CreateUser("bob", "password2", "", false); err != nil {
		t.Fatal(err)
	}
	st.SetKeepNTLM(true)
	alice, _ := st.UserByName("alice")
	st.SetPassword(alice.ID, "password1") // seen in the clear: kept

	c := dial(t, srv)
	c.open(1, "LOGSRV")
	if w, r, _ := c.ntlmLogin(t, "alice", "wrong"); w != ntlmRefused || r != loginBadPassword {
		t.Fatalf("a wrong password: %d %#x", w, r)
	}
	if w, r, _ := c.ntlmLogin(t, "nobody", "password1"); w != ntlmRefused || r != loginNoMember {
		t.Fatalf("no such member: %d %#x", w, r)
	}
	if w, r, _ := c.ntlmLogin(t, "bob", "password2"); w != ntlmRefused || r != loginBadPassword {
		t.Fatalf("no hashes: %d %#x", w, r)
	}
	w, r, cli := c.ntlmLogin(t, "ALICE", "password1") // the LM hash is of capitals
	if w != ntlmAccepted || r != loginOK || c.call(1, logsrvIface, logNTLMAccount)[0] != tagDword {
		t.Fatalf("sign-on: %d %#x", w, r)
	}

	// the billing pages, sealed
	rr := c.call(1, logsrvIface, logSealedBilling, []byte{0x84, 0x84})
	if len(rr) != 3+0x41C+2+sealTokenLen || rr[0] != tagVar {
		t.Fatalf("billing: %d bytes", len(rr))
	}
	acct, ok := cli.unseal(rr[3:3+0x41C], rr[3+0x41C+2:])
	if !ok || unansi(bytes.TrimRight(acct[8+0x3B:8+0x3B+40], "\x00")) != "alice" {
		t.Fatalf("billing unsealed: %v %q", ok, acct[8+0x3B:8+0x3B+10])
	}

	// the password changed, sealed
	data, token := cli.seal([]byte("password1\x00newpass99\x00"))
	if rr := c.call(1, logsrvIface, logSealedPassword, vparam(data), vparam(token), []byte{0x83}); binary.LittleEndian.Uint32(rr[1:]) != 0 {
		t.Fatalf("password change: %x", rr)
	}
	if _, err := st.CheckPassword("alice", "newpass99"); err != nil {
		t.Fatal("password not changed")
	}
	// and a seal that doesn't check out is refused
	data, _ = cli.seal([]byte("newpass99\x00other1234\x00"))
	if rr := c.call(1, logsrvIface, logSealedPassword, vparam(data), vparam(make([]byte, 16)), []byte{0x83}); binary.LittleEndian.Uint32(rr[1:]) == 0 {
		t.Fatal("a bad seal taken")
	}
}
