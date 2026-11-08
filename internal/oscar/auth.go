package oscar

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// login TLVs
const (
	tlvScreenName = 0x0001
	tlvRoasted    = 0x0002
	tlvClientID   = 0x0003
	tlvErrorURL   = 0x0004
	tlvReconnect  = 0x0005
	tlvCookie     = 0x0006
	tlvErrorCode  = 0x0008
	tlvEmail      = 0x0011
	tlvRegStatus  = 0x0013
	tlvPassHash   = 0x0025
	tlvStrongHash = 0x004c
	tlvPlainPass  = 0x1339
)

// login error codes
const (
	errBadName      = 0x0001
	errBadPassword  = 0x0005
	errSuspended    = 0x0011
	errRateLimited  = 0x0018
	errICQNoUser    = 0x0008
	errTempUnavail  = 0x0014
	errInternalAuth = 0x0006
)

// roasting tables: a channel 1 login's password is XORed with one
var (
	roastAIM  = []byte{0xF3, 0x26, 0x81, 0xC4, 0x39, 0x86, 0xDB, 0x92, 0x71, 0xA3, 0xB9, 0xE6, 0x53, 0x7A, 0x95, 0x7C}
	roastJava = []byte{0xF3, 0xB3, 0x6C, 0x99, 0x95, 0x3F, 0xAC, 0xB6, 0xC5, 0xFA, 0x6B, 0x63, 0x69, 0x6C, 0xC3, 0x9A}
)

// unroast is a roasted password in the clear.
func unroast(p, table []byte) string {
	out := make([]byte, len(p))
	for i := range p {
		out[i] = p[i] ^ table[i%len(table)]
	}
	return string(out)
}

// loginResult is a login's answer: the cookie's TLVs, or an error's.
func (s *Server) loginResult(fc *flapConn, host string, sn, clientID string, u *store.User, err error) tlvs {
	var o tlvs
	icq := isUIN(sn)
	if err != nil {
		o.addStr(tlvScreenName, sn)
		code := uint16(errBadPassword)
		switch {
		case errors.Is(err, bbs.ErrThrottled):
			code = errRateLimited
		case errors.Is(err, bbs.ErrLocked), errors.Is(err, bbs.ErrNotAllowed):
			code = errSuspended
		case errors.Is(err, errNoUser) && icq:
			code = errICQNoUser
		case errors.Is(err, errNoUser):
			code = errBadName
		case !errors.Is(err, bbs.ErrBadLogin) && !errors.Is(err, errNoKeys):
			code = errTempUnavail
		}
		o.addU16(tlvErrorCode, code)
		o.addStr(tlvErrorURL, "http://www.aim.com/errors/")
		return o
	}
	name := u.Name
	if icq {
		name = s.uinOf(u.ID)
	}
	ck := s.newCookie(&cookie{user: u, sn: name, icq: icq, clientID: clientID})
	o.addStr(tlvScreenName, name)
	o.addStr(tlvReconnect, s.reconnectHost(fc.c))
	o.add(tlvCookie, ck)
	o.addStr(tlvEmail, strings.ToLower(ident(u.Name))+"@"+strings.ToLower(ident(s.B.Conf().Name)))
	o.addU16(tlvRegStatus, 3)
	o.addU16(0x008e, 0) // no SSL
	s.B.Log.Printf("oscar: %s: %s signed on (%s)", host, u.Name, clientID)
	return o
}

var (
	errNoUser = errors.New("no such user")
	errNoKeys = errors.New("no digests kept for the account yet")
)

// account is the account a login names, and that it may chat.
func (s *Server) account(sn string) (*store.User, error) {
	u := s.resolve(sn)
	if u == nil {
		return nil, errNoUser
	}
	return u, nil
}

// may checks an account logged in may use AIM: chat by its access.
func (s *Server) may(u *store.User) error {
	if !s.B.LevelOf(u).Chat {
		return bbs.ErrNotAllowed
	}
	return nil
}

// channelLogin is a login in the signon frame (AIM 1 to 3, ICQ, Java
// AIM): its password roasted, checked as any password is; answered with a
// signoff frame.
func (s *Server) channelLogin(fc *flapConn, host string, t tlvs) {
	sn := strings.TrimSpace(t.str(tlvScreenName))
	clientID := t.str(tlvClientID)
	var pw string
	switch {
	case t.has(tlvRoasted):
		v, _ := t.get(tlvRoasted)
		table := roastAIM
		if strings.Contains(clientID, "for Java") {
			table = roastJava
		}
		pw = unroast(v, table)
	case t.has(tlvPlainPass):
		pw = t.str(tlvPlainPass)
	}
	u, err := s.passwordLogin(host, sn, pw)
	fc.signoff(s.loginResult(fc, host, sn, clientID, u, err))
}

// passwordLogin checks a screen name's password in the clear.
func (s *Server) passwordLogin(host, sn, pw string) (*store.User, error) {
	name := sn
	if u := s.resolve(sn); u != nil {
		name = u.Name // a UIN, or a name without its spaces
	}
	u, err := s.B.Authenticate("oscar", "ip:"+host, name, pw)
	if err != nil {
		return nil, err
	}
	return u, s.may(u)
}

// bucp is a BUCP login (family 0x17): a challenge, then the password's
// MD5 digest, checked against the account's kept digests.
func (s *Server) bucp(fc *flapConn, host string) {
	for range 10 {
		f, err := fc.read()
		if err != nil || f.ch == chanSignoff {
			return
		}
		if f.ch != chanData {
			continue
		}
		h, body, err := parseSNAC(f.data)
		if err != nil || h.fam != 0x17 {
			return
		}
		t := parseTLVs(body)
		sn := strings.TrimSpace(t.str(tlvScreenName))
		switch h.sub {
		case 0x0006: // challenge request
			key := ""
			if u := s.resolve(sn); u != nil {
				if key, err = s.B.Store.OSCARAuthKey(u.ID); err != nil {
					s.B.Log.Printf("oscar: the auth key of %s: %v", u.Name, err)
				}
			}
			if key == "" {
				key = newKey() // an unknown name is challenged too; its login fails
			}
			var b buf
			b.str16(key)
			fc.snac(0x17, 0x0007, 0, h.req, b)
		case 0x0002: // login
			hash, _ := t.get(tlvPassHash)
			var u *store.User
			if pw, ok := t.get(tlvPlainPass); ok {
				u, err = s.passwordLogin(host, sn, string(pw))
			} else {
				u, err = s.digestLogin(host, sn, hash, t.has(tlvStrongHash))
			}
			fc.snac(0x17, 0x0003, 0, h.req, s.loginResult(fc, host, sn, t.str(tlvClientID), u, err).bytes())
			return
		case 0x0004: // new account: not here (the BBS makes them)
			var b buf
			b.u16(errBadName)
			fc.snac(0x17, 0x0001, 0, h.req, b)
		default:
			var b buf
			b.u16(0x0001)
			fc.snac(0x17, 0x0001, 0, h.req, b)
		}
	}
}

// digestLogin checks a BUCP digest against the account's.
func (s *Server) digestLogin(host, sn string, hash []byte, strong bool) (*store.User, error) {
	name := sn
	target := s.resolve(sn)
	if target != nil {
		name = target.Name
	}
	var stale bool
	u, err := s.B.AuthenticateBy("oscar", "ip:"+host, name, func() (*store.User, error) {
		if target == nil {
			return nil, store.ErrNotFound
		}
		k, ok, err := s.B.Store.OSCARKeys(target.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			stale = true
			return nil, store.ErrNotFound
		}
		want := k.Weak
		if strong {
			want = k.Strong
		}
		if subtle.ConstantTimeCompare(want, hash) != 1 &&
			subtle.ConstantTimeCompare(k.Weak, hash) != 1 && subtle.ConstantTimeCompare(k.Strong, hash) != 1 {
			return nil, store.ErrNotFound
		}
		return target, nil
	})
	if stale {
		s.B.Log.Printf("oscar: %s: %s has no AIM password digest yet: log in once by another way "+
			"(telnet, IRC, the web) or set the password again", host, name)
		return nil, errNoKeys
	}
	if err != nil {
		if target == nil && errors.Is(err, bbs.ErrBadLogin) {
			return nil, errNoUser
		}
		return nil, err
	}
	return u, s.may(u)
}

// newKey is a random challenge for a name with no account.
func newKey() string {
	b := make([]byte, 5)
	rand.Read(b)
	return hex.EncodeToString(b)
}
