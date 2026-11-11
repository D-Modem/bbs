package marvel

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/ntlm"
	"github.com/D-Modem/bbs/internal/store"
)

// LOGSRV signs the client on.  Its one interface, {00028BC2-...}, is
// called by the Sign In dialog, the Password dialog and the billing pages.
type logsrv struct {
	c *conn

	// the 1996 client's NTLM sign-on (ntlm.go), and the sealing it leaves
	challenge []byte
	flags     uint32
	seal      *sealer
}

const logsrvIface = 0x06

// LOGSRV methods
const (
	logLogin          = 0x00
	logPassword       = 0x01
	logSignupTransfer = 0x02
	logSignupQuery    = 0x07
	logBilling        = 0x0A
	logPaymentCommit  = 0x0B
	logAddressCommit  = 0x0C
	logPostSignup     = 0x0D
	logPhoneBook      = 0x0E
	logNTLMNegotiate  = 0x0F
	logNTLMAuth       = 0x10
	logNTLMAccount    = 0x11 // what an accepted NTLM sign-on reads next
	logSealedPassword = 0x12
	logSealedBilling  = 0x13
	logSealedPayment  = 0x14
)

// The sign-on result: the first dword of the login reply.  The client
// shows its own message for each; any code it doesn't know is "Password
// not valid".
const (
	loginOK          = 0x00
	loginNoMember    = 0x02 // "This member ID is not valid."
	loginBadPassword = 0x03
	loginLocked      = 0x0D // "This account has been locked."
	loginBusy        = 0x22 // "Network busy."
)

// The credential blob: 4 unused bytes, the member ID in 65, the password
// in 19, both NUL-ended and in the clear.
const (
	blobLen      = 0x58
	blobMemberAt = 0x04
	blobMemberN  = 0x41
	blobPassAt   = 0x45
	blobPassN    = 0x13
)

func (l *logsrv) ifaces() []iface { return []iface{mosIID(0xC2, logsrvIface)} }

func (l *logsrv) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false // the address commit's own chunks
	}
	ps, _ := parseParams(h.Params)
	switch h.Method {
	case logLogin:
		return l.login(ps), true
	case logPassword:
		return l.password(ps), true
	case logSignupTransfer, logPostSignup:
		return reply{}.bytes_(nil), true
	case logSignupQuery:
		// takes nothing back: anything here is a "Server error"
		return nil, true
	case logBilling:
		return reply{}.bytes_(l.account()), true
	case logPaymentCommit, logAddressCommit:
		// status 0: dismissed quietly.  Nothing is kept: the BBS has no
		// billing.
		return reply{}.bytes_(make([]byte, 4)), true
	case logPhoneBook:
		return reply{}.dword(0), true
	case logNTLMNegotiate:
		t := ntlmToken(ps, ntlmTypeNegotiate)
		if t == nil || len(t) < 16 {
			return reply{}.bytes_(nil), true // no token: the client says the sign-on failed
		}
		l.flags = binary.LittleEndian.Uint32(t[12:])
		l.challenge = ntlm.NewChallenge()
		return reply{}.bytes_(wrapToken(ntlmTypeChallenge, ntlm.ChallengeMessage(l.flags, l.challenge, ""))), true
	case logNTLMAuth:
		result, ok := l.ntlmSignOn(ps)
		word := uint16(ntlmRefused)
		if ok {
			word = ntlmAccepted
		}
		return reply{}.word(word).dword(result).endStatic(), true
	case logNTLMAccount:
		// five dwords, 16 bytes and five more, all nothing: a value here
		// would ask for a software update
		r := reply{}
		for range 5 {
			r = r.dword(0)
		}
		r = r.bytes_(make([]byte, 16))
		for range 5 {
			r = r.dword(0)
		}
		return r.endStatic(), true
	case logSealedPassword:
		return l.sealedPassword(ps), true
	case logSealedBilling:
		if l.seal == nil {
			return reply{}.bytes_(l.account()).bytes_(make([]byte, sealTokenLen)), true
		}
		data, token := l.seal.seal(l.account())
		return reply{}.bytes_(data).bytes_(token), true
	case logSealedPayment:
		// read (to keep the stream in step) and not kept: the BBS has no billing
		if vs := vars(ps); l.seal != nil && len(vs) == 2 {
			l.seal.unseal(vs[0], vs[1])
		}
		return reply{}.bytes_(make([]byte, 4)), true
	}
	l.c.logf("LOGSRV: no method %#x", h.Method)
	return nil, false
}

// login is the Sign In dialog's: seven dwords (the result first), then a
// 16-byte value.
func (l *logsrv) login(ps []param) []byte {
	result := uint32(loginNoMember)
	for _, p := range ps {
		if p.Tag == 0x04 && len(p.Data) == blobLen {
			member := strings.TrimSpace(asciiz(p.Data[blobMemberAt : blobMemberAt+blobMemberN]))
			pw := asciiz(p.Data[blobPassAt : blobPassAt+blobPassN])
			result = l.c.signOn(member, pw)
		}
	}
	r := reply{}.dword(result)
	for range 6 {
		r = r.dword(0)
	}
	return r.endStatic().bytes_(make([]byte, 16))
}

// vars are a call's 0x04 values.
func vars(ps []param) [][]byte {
	var out [][]byte
	for _, p := range ps {
		if p.Tag == 0x04 {
			out = append(out, p.Data)
		}
	}
	return out
}

// ntlmSignOn checks an AUTHENTICATE against the member's kept hashes, and
// signs on.
func (l *logsrv) ntlmSignOn(ps []param) (uint32, bool) {
	t := ntlmToken(ps, ntlmTypeAuthenticate)
	if t == nil || l.challenge == nil {
		return loginNoMember, false
	}
	challenge := l.challenge
	l.challenge = nil // one answer to each
	a, err := ntlm.ParseAuthenticate(t, l.flags)
	if err != nil {
		l.c.logf("NTLM: %v", err)
		return loginNoMember, false
	}
	b := l.c.s.B
	var lm, nt []byte
	check := b.NTLMCheck("marvel", a, challenge, func(l, n []byte) { lm, nt = l, n })
	result := l.c.signOnBy(a.User, func(node *bbs.Node, name string) (*store.User, error) {
		if node != nil {
			return node.LoginWith(name, "NTLM", check)
		}
		return b.AuthenticateBy("marvel", l.c.source, name, check)
	})
	if result != loginOK {
		return result, false
	}
	if l.seal, err = newSealer(a.Flags, a.SessionKey(lm, nt, challenge)); err != nil {
		l.c.logf("NTLM: %v", err)
	}
	return loginOK, true
}

// sealedPassword is the 1996 client's Password dialog: the old password
// and the new, each NUL-ended, sealed.
func (l *logsrv) sealedPassword(ps []param) []byte {
	vs := vars(ps)
	if l.seal == nil || len(vs) != 2 {
		return reply{}.dword(1)
	}
	data, ok := l.seal.unseal(vs[0], vs[1])
	if !ok {
		l.c.logf("password: a seal that doesn't check out")
		return reply{}.dword(1)
	}
	pws := bytes.Split(data, []byte{0})
	if len(pws) != 3 || len(pws[2]) != 0 {
		return reply{}.dword(1)
	}
	return l.changePassword(unansi(pws[0]), unansi(pws[1]))
}

// password is the Password dialog's change: the old one and the new, in
// two 17-byte fields.  Any status but 0 is "not valid".
func (l *logsrv) password(ps []param) []byte {
	var pws []string
	for _, p := range ps {
		if p.Tag == 0x04 {
			pws = append(pws, asciiz(p.Data))
		}
	}
	if len(pws) != 2 {
		return reply{}.dword(1)
	}
	return l.changePassword(pws[0], pws[1])
}

func (l *logsrv) changePassword(old, pw string) []byte {
	l.c.mu.Lock()
	n := l.c.node
	l.c.mu.Unlock()
	if n == nil || l.c.signedOn() == nil {
		return reply{}.dword(1)
	}
	if err := n.ChangePassword(old, pw); err != nil {
		l.c.logf("password: %v", err)
		return reply{}.dword(1)
	}
	return reply{}.dword(0)
}

// account is the billing pages' 0x41C-byte record: a status, the name and
// address block, the payment block.  The BBS has only a name and a
// location to give.
func (l *logsrv) account() []byte {
	buf := make([]byte, 0x41C)
	u := l.c.signedOn()
	if u == nil {
		return buf
	}
	const oi = 8
	first, last, _ := strings.Cut(u.Name, " ")
	put := func(at int, s string) { copy(buf[at:at+40], append(ansi(s), 0)) }
	put(oi+0x3B, first)
	put(oi+0x69, last)
	put(oi+0x201, u.Location)
	return buf
}
