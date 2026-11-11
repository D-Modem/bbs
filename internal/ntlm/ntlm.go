// Package ntlm is the server's side of NTLM, as Microsoft's clients of the
// 1990s spoke it: MSN's own security package (msnsspc, "Sicily") is NTLM
// under another name, so MSN Classic's 1996 client, MSN 2.x's Program
// Viewer, Outlook Express and Internet News all sign in by it.  The
// client proves the password with DES over the server's challenge, keyed
// with the password's LM or NT hash, which the store keeps for it.
package ntlm

import (
	"bytes"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/md4"
	"golang.org/x/text/encoding/charmap"
)

// Message types.
const (
	TypeNegotiate    = 1
	TypeChallenge    = 2
	TypeAuthenticate = 3
)

// Flags.
const (
	FlagUnicode       = 0x00000001
	FlagOEM           = 0x00000002
	FlagRequestTarget = 0x00000004
	FlagSign          = 0x00000010
	FlagSeal          = 0x00000020
	FlagLMKey         = 0x00000080
	FlagNTLM          = 0x00000200
	FlagAlwaysSign    = 0x00008000
	FlagTargetDomain  = 0x00010000
	FlagESS           = 0x00080000 // extended session security
	FlagNonNTKey      = 0x00400000
	FlagTargetInfo    = 0x00800000
	Flag128           = 0x20000000
	FlagKeyExchange   = 0x40000000
	Flag56            = 0x80000000
)

// Signature begins every message.
var Signature = []byte("NTLMSSP\x00")

// Type is a message's type, or 0 if it isn't one.
func Type(t []byte) uint32 {
	if len(t) < 12 || !bytes.Equal(t[:8], Signature) {
		return 0
	}
	return binary.LittleEndian.Uint32(t[8:])
}

// LMHash is a password's LAN Manager hash: its first 14 characters in
// capitals, as two DES keys over "KGS!@#$%".
func LMHash(pw string) []byte {
	var k [14]byte
	b, err := charmap.Windows1252.NewEncoder().Bytes([]byte(strings.ToUpper(pw)))
	if err != nil {
		b = []byte(strings.ToUpper(pw))
	}
	copy(k[:], b)
	out := make([]byte, 16)
	for i := range 2 {
		c, _ := des.NewCipher(DESKey(k[i*7 : i*7+7]))
		c.Encrypt(out[i*8:], []byte("KGS!@#$%"))
	}
	return out
}

// NTHash is a password's NT hash: MD4 of it in UTF-16LE.
func NTHash(pw string) []byte {
	h := md4.New()
	h.Write(utf16le(pw))
	return h.Sum(nil)
}

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// DESKey spreads 7 bytes over a DES key's 8, a parity bit in each.
func DESKey(k []byte) []byte {
	var v uint64
	for _, b := range k[:7] {
		v = v<<8 | uint64(b)
	}
	out := make([]byte, 8)
	for i := range 8 {
		out[i] = byte(v>>(56-7*(i+1))) << 1
	}
	return out
}

// DESL is NTLMv1's response: the challenge under each third of the hash
// (padded to 21 bytes).
func DESL(k, c []byte) []byte {
	key := append(append([]byte(nil), k...), make([]byte, 21-len(k))...)
	out := make([]byte, 24)
	for i := range 3 {
		b, _ := des.NewCipher(DESKey(key[i*7 : i*7+7]))
		b.Encrypt(out[i*8:], c)
	}
	return out
}

func desEncrypt(k7, c []byte) []byte {
	b, _ := des.NewCipher(DESKey(k7))
	out := make([]byte, 8)
	b.Encrypt(out, c)
	return out
}

// NewChallenge is a server challenge.
func NewChallenge() []byte {
	c := make([]byte, 8)
	rand.Read(c)
	return c
}

// NegotiateFlags are a NEGOTIATE's flags.
func NegotiateFlags(t []byte) (uint32, error) {
	if Type(t) != TypeNegotiate || len(t) < 16 {
		return 0, errors.New("not an NTLM NEGOTIATE")
	}
	return binary.LittleEndian.Uint32(t[12:]), nil
}

// Answer is the flags a server gives back to a client's: the client's
// character set, NTLM, and what it asked for of extended session
// security, signing and key strength.
func Answer(asked uint32, target string) uint32 {
	f := uint32(FlagNTLM | FlagAlwaysSign)
	if asked&FlagUnicode != 0 {
		f |= FlagUnicode
	} else {
		f |= FlagOEM
	}
	f |= asked & (FlagESS | FlagSign | FlagSeal | Flag128 | Flag56 | FlagRequestTarget)
	if f&FlagESS == 0 {
		f |= asked & FlagLMKey
	}
	if target != "" && asked&FlagRequestTarget != 0 {
		f |= FlagTargetDomain | FlagTargetInfo
	}
	return f
}

// ChallengeMessage is a CHALLENGE with flags: with the target's name and
// target information when target is given, else with neither (as MSN
// Classic's client expects).
func ChallengeMessage(flags uint32, challenge []byte, target string) []byte {
	var name, info []byte
	if target != "" && flags&FlagTargetInfo != 0 {
		name = []byte(strings.ToUpper(target))
		if flags&FlagUnicode != 0 {
			name = utf16le(strings.ToUpper(target))
		}
		av := func(id uint16, v []byte) []byte {
			b := binary.LittleEndian.AppendUint16(nil, id)
			b = binary.LittleEndian.AppendUint16(b, uint16(len(v)))
			return append(b, v...)
		}
		u := utf16le(strings.ToUpper(target))
		info = append(append(av(2, u), av(1, u)...), 0, 0, 0, 0) // domain, server, end
	}
	const head = 48
	field := func(b []byte, v []byte, off int) []byte {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(v)))
		b = binary.LittleEndian.AppendUint16(b, uint16(len(v)))
		return binary.LittleEndian.AppendUint32(b, uint32(off))
	}
	b := append([]byte(nil), Signature...)
	b = binary.LittleEndian.AppendUint32(b, TypeChallenge)
	b = field(b, name, head)
	b = binary.LittleEndian.AppendUint32(b, flags)
	b = append(b, challenge...)
	b = append(b, make([]byte, 8)...) // reserved
	b = field(b, info, head+len(name))
	return append(append(b, name...), info...)
}

// Auth is an AUTHENTICATE read.
type Auth struct {
	Flags                     uint32
	User, Domain, Workstation string
	LM, NT                    []byte
}

// ParseAuthenticate reads an AUTHENTICATE.  MSN Classic's 1996 client's
// header stops after the workstation's field (52 bytes, with no flags of
// its own), so the flags come from the message only when its data starts
// past the long header; else they are those negotiated.
func ParseAuthenticate(t []byte, negotiated uint32) (*Auth, error) {
	if Type(t) != TypeAuthenticate || len(t) < 52 {
		return nil, errors.New("not an NTLM AUTHENTICATE")
	}
	var fields [5][]byte
	start := len(t)
	for i := range fields {
		at := 12 + 8*i
		n, off := int(binary.LittleEndian.Uint16(t[at:])), int(binary.LittleEndian.Uint32(t[at+4:]))
		if n == 0 {
			continue
		}
		if off < 0 || off+n > len(t) {
			return nil, errors.New("an AUTHENTICATE's field outside it")
		}
		fields[i] = t[off : off+n]
		start = min(start, off)
	}
	a := &Auth{Flags: negotiated, LM: fields[0], NT: fields[1]}
	if start >= 64 {
		a.Flags = binary.LittleEndian.Uint32(t[60:])
	}
	text := func(b []byte) string {
		if a.Flags&FlagUnicode != 0 {
			u := make([]uint16, len(b)/2)
			for i := range u {
				u[i] = binary.LittleEndian.Uint16(b[2*i:])
			}
			return string(utf16.Decode(u))
		}
		s, err := charmap.Windows1252.NewDecoder().Bytes(b)
		if err != nil {
			return string(b)
		}
		return string(s)
	}
	a.Domain, a.User, a.Workstation = text(fields[2]), text(fields[3]), text(fields[4])
	return a, nil
}

// Anonymous reports whether the AUTHENTICATE names no one.
func (a *Auth) Anonymous() bool { return a.User == "" && len(a.NT) == 0 && len(a.LM) <= 1 }

func (a *Auth) ess() bool {
	return a.Flags&FlagESS != 0 && len(a.LM) == 24 && len(a.NT) == 24 && bytes.Equal(a.LM[8:], make([]byte, 16))
}

// Verify checks the responses against the password's hashes: NTLMv2 if
// the NT response is longer than v1's, else NTLMv1 (with extended session
// security if it's that), by the NT response if there is one, else the LM.
func (a *Auth) Verify(lm, nt, challenge []byte) bool {
	eq := func(x, y []byte) bool { return subtle.ConstantTimeCompare(x, y) == 1 }
	switch {
	case len(a.NT) > 24:
		for _, k := range a.v2Keys(nt) {
			m := hmac.New(md5.New, k)
			m.Write(challenge)
			m.Write(a.NT[16:])
			if eq(m.Sum(nil), a.NT[:16]) {
				return true
			}
		}
		return false
	case a.ess():
		h := md5.Sum(append(append([]byte(nil), challenge...), a.LM[:8]...))
		return eq(a.NT, DESL(nt, h[:8]))
	case len(a.NT) == 24:
		return eq(a.NT, DESL(nt, challenge))
	case len(a.LM) == 24:
		if eq(a.LM, DESL(lm, challenge)) {
			return true
		}
		for _, k := range a.v2Keys(nt) { // LMv2 alone
			m := hmac.New(md5.New, k)
			m.Write(challenge)
			m.Write(a.LM[16:])
			if eq(m.Sum(nil), a.LM[:16]) {
				return true
			}
		}
	}
	return false
}

// v2Keys are NTOWFv2's for the name and domain as sent, and with no
// domain (clients differ on which they use).
func (a *Auth) v2Keys(nt []byte) [][]byte {
	var out [][]byte
	for _, d := range []string{a.Domain, ""} {
		m := hmac.New(md5.New, nt)
		m.Write(utf16le(strings.ToUpper(a.User) + d))
		out = append(out, m.Sum(nil))
		if a.Domain == "" {
			break
		}
	}
	return out
}

// SessionKey is the NTLMv1 key an accepted exchange leaves, with no key
// exchange (MSN Classic's short AUTHENTICATE has no room for one).
func (a *Auth) SessionKey(lm, nt, challenge []byte) []byte {
	h := md4.New()
	h.Write(nt)
	base := h.Sum(nil)
	switch {
	case a.Flags&FlagESS != 0 && len(a.LM) >= 8:
		m := hmac.New(md5.New, base)
		m.Write(challenge)
		m.Write(a.LM[:8])
		return m.Sum(nil)
	case a.Flags&FlagLMKey != 0 && len(a.LM) >= 8:
		k := desEncrypt(lm[:7], a.LM[:8])
		return append(k, desEncrypt(append([]byte{lm[7]}, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD), a.LM[:8])...)
	case a.Flags&FlagNonNTKey != 0:
		return append(append([]byte(nil), lm[:8]...), make([]byte, 8)...)
	}
	return base
}

// Exchange is one sign-in's server side: a NEGOTIATE in, a CHALLENGE out,
// then the AUTHENTICATE.
type Exchange struct {
	Target    string // the CHALLENGE's domain, if any
	flags     uint32
	challenge []byte
}

// Challenge answers a NEGOTIATE.
func (x *Exchange) Challenge(negotiate []byte) ([]byte, error) {
	asked, err := NegotiateFlags(negotiate)
	if err != nil {
		return nil, err
	}
	x.flags = Answer(asked, x.Target)
	x.challenge = NewChallenge()
	return ChallengeMessage(x.flags, x.challenge, x.Target), nil
}

// Authenticate reads the AUTHENTICATE that answers the challenge, and
// gives the challenge it answers.  There is one answer to each.
func (x *Exchange) Authenticate(t []byte) (*Auth, []byte, error) {
	if x.challenge == nil {
		return nil, nil, errors.New("an AUTHENTICATE before a CHALLENGE")
	}
	c := x.challenge
	x.challenge = nil
	a, err := ParseAuthenticate(t, x.flags)
	return a, c, err
}

// SASL is the exchange as a SASL mechanism's server (NTLM, or MSN by
// MSN's name for it): Next takes the NEGOTIATE (asking for it if the
// client began with nothing), then the AUTHENTICATE, which Login checks.
type SASL struct {
	Exchange
	Login func(a *Auth, challenge []byte) error
	step  int
}

// ErrFailed is a sign-in that isn't accepted.
var ErrFailed = errors.New("NTLM: not accepted")

func (s *SASL) Next(response []byte) (challenge []byte, done bool, err error) {
	switch s.step {
	case 0:
		if len(response) == 0 {
			return nil, false, nil
		}
		s.step = 1
		c, err := s.Challenge(response)
		if err != nil {
			return nil, true, err
		}
		return c, false, nil
	case 1:
		s.step = 2
		a, c, err := s.Authenticate(response)
		if err != nil {
			return nil, true, err
		}
		return nil, true, s.Login(a, c)
	}
	return nil, true, ErrFailed
}
