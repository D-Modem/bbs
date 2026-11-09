package marvel

import (
	"bytes"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	ieee "hash/crc32"
	"unicode/utf16"

	"github.com/D-Modem/bbs/internal/store"
	"golang.org/x/crypto/md4"
)

// The 1996 client (Windows 95 OSR2, MSN 1.3) signs in with NTLM, on two
// LOGSRV methods: a NEGOTIATE answered with a CHALLENGE, then an
// AUTHENTICATE answered with the result.  The client answers the
// challenge with NTLMv1: DES keyed with the password's LM hash (the NT
// response it leaves empty), which the store keeps for it.  The context
// the sign-in leaves then seals the account methods' data: the password
// change and the billing pages.
//
// Each token rides a 0x04 value behind a header, [type u16][length u16],
// in a 256-byte slot.

const (
	ntlmTypeNegotiate    = 1
	ntlmTypeChallenge    = 2
	ntlmTypeAuthenticate = 3
	ntlmAccepted         = 4 // the sign-in reply's word: the context stands
	ntlmRefused          = 0
	ntlmSlot             = 256

	ntlmUnicode    = 0x00000001
	ntlmLMKey      = 0x00000080
	ntlmNonNTKey   = 0x00400000
	ntlmESS        = 0x00080000 // extended session security
	ntlm56         = 0x80000000
	sealTokenLen   = 16
	ntlmChallengeN = 8
)

var ntlmSig = []byte("NTLMSSP\x00")

// ntlmToken is the token in a method's 0x04 value.
func ntlmToken(ps []param, want uint32) []byte {
	for _, p := range ps {
		if p.Tag != 0x04 || len(p.Data) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint16(p.Data[2:]))
		t := p.Data[4:]
		if n < len(t) {
			t = t[:n]
		}
		if len(t) >= 12 && bytes.Equal(t[:8], ntlmSig) && binary.LittleEndian.Uint32(t[8:]) == want {
			return t
		}
	}
	return nil
}

func wrapToken(typ uint16, t []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, typ)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(t)))
	b = append(b, t...)
	if len(b) < 4+ntlmSlot {
		b = append(b, make([]byte, 4+ntlmSlot-len(b))...)
	}
	return b
}

// challengeToken is a CHALLENGE that gives the client back the flags it
// asked for: no target name, no target information.
func challengeToken(flags uint32, challenge []byte) []byte {
	b := append([]byte(nil), ntlmSig...)
	b = binary.LittleEndian.AppendUint32(b, ntlmTypeChallenge)
	b = append(b, 0, 0, 0, 0, 48, 0, 0, 0) // target name: none
	b = binary.LittleEndian.AppendUint32(b, flags)
	b = append(b, challenge...)
	b = append(b, make([]byte, 8)...)         // reserved
	return append(b, 0, 0, 0, 0, 48, 0, 0, 0) // target information: none
}

type ntlmAuth struct {
	flags          uint32
	user, domain   string
	lmResp, ntResp []byte
}

// parseAuthenticate reads an AUTHENTICATE.  The 1996 client's header
// stops after the workstation's field (52 bytes, with no flags of its
// own), so the flags come from the token only when its data starts past
// the long header.
func parseAuthenticate(t []byte, negotiated uint32) (*ntlmAuth, error) {
	if len(t) < 52 {
		return nil, errors.New("an AUTHENTICATE cut short")
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
	a := &ntlmAuth{flags: negotiated, lmResp: fields[0], ntResp: fields[1]}
	if start >= 64 {
		a.flags = binary.LittleEndian.Uint32(t[60:])
	}
	text := func(b []byte) string {
		if a.flags&ntlmUnicode != 0 {
			u := make([]uint16, len(b)/2)
			for i := range u {
				u[i] = binary.LittleEndian.Uint16(b[2*i:])
			}
			return string(utf16.Decode(u))
		}
		return unansi(b)
	}
	a.domain, a.user = text(fields[2]), text(fields[3])
	return a, nil
}

// desl is NTLMv1's response: the challenge under each third of the hash
// (padded to 21 bytes).
func desl(k, c []byte) []byte {
	key := append(append([]byte(nil), k...), make([]byte, 21-len(k))...)
	out := make([]byte, 24)
	for i := range 3 {
		b, _ := des.NewCipher(store.DESKey(key[i*7 : i*7+7]))
		b.Encrypt(out[i*8:], c)
	}
	return out
}

func desEncrypt(k7, c []byte) []byte {
	b, _ := des.NewCipher(store.DESKey(k7))
	out := make([]byte, 8)
	b.Encrypt(out, c)
	return out
}

// verify checks a response against the hashes: the NT response if there
// is one, else the LM.
func (a *ntlmAuth) verify(lm, nt, challenge []byte) bool {
	c := challenge
	if a.flags&ntlmESS != 0 && len(a.lmResp) >= 8 {
		h := md5.Sum(append(append([]byte(nil), challenge...), a.lmResp[:8]...))
		c = h[:8]
	}
	switch {
	case len(a.ntResp) == 24:
		return subtle.ConstantTimeCompare(a.ntResp, desl(nt, c)) == 1
	case len(a.lmResp) == 24 && a.flags&ntlmESS == 0:
		return subtle.ConstantTimeCompare(a.lmResp, desl(lm, c)) == 1
	}
	return false
}

// sessionKey is the key an accepted exchange leaves (no key exchange: the
// short AUTHENTICATE has no room for one).
func (a *ntlmAuth) sessionKey(lm, nt, challenge []byte) []byte {
	h := md4.New()
	h.Write(nt)
	base := h.Sum(nil)
	switch {
	case a.flags&ntlmESS != 0:
		m := hmac.New(md5.New, base)
		m.Write(challenge)
		m.Write(a.lmResp[:8])
		return m.Sum(nil)
	case a.flags&ntlmLMKey != 0 && len(a.lmResp) >= 8:
		k := desEncrypt(lm[:7], a.lmResp[:8])
		return append(k, desEncrypt(append([]byte{lm[7]}, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD), a.lmResp[:8])...)
	case a.flags&ntlmNonNTKey != 0:
		return append(append([]byte(nil), lm[:8]...), make([]byte, 8)...)
	}
	return base
}

// sealer is the sealing an accepted sign-in leaves its pipe: an RC4
// stream each way from one key, each message's data and then its token
// [1][RandomPad][CRC-32 of the data][sequence], the last 12 bytes
// encrypted from the same stream.
type sealer struct {
	out, in       *rc4.Cipher
	outSeq, inSeq uint32
}

func newSealer(flags uint32, key []byte) (*sealer, error) {
	if flags&ntlmESS != 0 {
		return nil, errors.New("extended session security's sealing isn't served")
	}
	var k []byte
	if flags&ntlm56 != 0 {
		k = append(append([]byte(nil), key[:7]...), 0xA0)
	} else {
		k = append(append([]byte(nil), key[:5]...), 0xE5, 0x38, 0xB0)
	}
	out, _ := rc4.NewCipher(k)
	in, _ := rc4.NewCipher(k)
	return &sealer{out: out, in: in}, nil
}

func (s *sealer) seal(data []byte) (sealed, token []byte) {
	sealed = make([]byte, len(data))
	s.out.XORKeyStream(sealed, data)
	tr := binary.LittleEndian.AppendUint32(make([]byte, 4), ieee.ChecksumIEEE(data))
	tr = binary.LittleEndian.AppendUint32(tr, s.outSeq)
	s.outSeq++
	token = binary.LittleEndian.AppendUint32(nil, 1)
	enc := make([]byte, 12)
	s.out.XORKeyStream(enc, tr)
	return sealed, append(token, enc...)
}

func (s *sealer) unseal(sealed, token []byte) ([]byte, bool) {
	if len(token) != sealTokenLen || binary.LittleEndian.Uint32(token) != 1 {
		return nil, false
	}
	data := make([]byte, len(sealed))
	s.in.XORKeyStream(data, sealed)
	tr := make([]byte, 12)
	s.in.XORKeyStream(tr, token[4:])
	seq := s.inSeq
	s.inSeq++
	return data, binary.LittleEndian.Uint32(tr[8:]) == seq && binary.LittleEndian.Uint32(tr[4:]) == ieee.ChecksumIEEE(data)
}

func newChallenge() []byte {
	c := make([]byte, ntlmChallengeN)
	rand.Read(c)
	return c
}
