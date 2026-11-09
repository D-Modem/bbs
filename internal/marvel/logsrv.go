package marvel

import (
	"strings"
)

// LOGSRV signs the client on.  Its one interface, {00028BC2-...}, is
// called by the Sign In dialog, the Password dialog and the billing pages.
type logsrv struct{ c *conn }

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
		// the 1996 (OSR2) client's NTLM sign-on: a challenge would need
		// the password's NT hash, which the BBS doesn't keep, so there is
		// none, and the client says the sign-on failed
		l.c.logf("an NTLM sign-on (a 1996 client), which this server can't answer")
		return reply{}.bytes_(nil), true
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

// password is the Password dialog's change: the old one and the new, in
// two 17-byte fields.  Any status but 0 is "not valid".
func (l *logsrv) password(ps []param) []byte {
	var pws []string
	for _, p := range ps {
		if p.Tag == 0x04 {
			pws = append(pws, asciiz(p.Data))
		}
	}
	l.c.mu.Lock()
	n := l.c.node
	l.c.mu.Unlock()
	if len(pws) != 2 || n == nil || l.c.signedOn() == nil {
		return reply{}.dword(1)
	}
	if err := n.ChangePassword(pws[0], pws[1]); err != nil {
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
