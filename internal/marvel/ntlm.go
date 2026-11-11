package marvel

import (
	"bytes"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	ieee "hash/crc32"

	"github.com/D-Modem/bbs/internal/ntlm"
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
	ntlmTypeNegotiate    = ntlm.TypeNegotiate
	ntlmTypeChallenge    = ntlm.TypeChallenge
	ntlmTypeAuthenticate = ntlm.TypeAuthenticate
	ntlmAccepted         = 4 // the sign-in reply's word: the context stands
	ntlmRefused          = 0
	ntlmSlot             = 256

	sealTokenLen = 16
)

var ntlmSig = ntlm.Signature

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

// sealer is the sealing an accepted sign-in leaves its pipe: an RC4
// stream each way from one key, each message's data and then its token
// [1][RandomPad][CRC-32 of the data][sequence], the last 12 bytes
// encrypted from the same stream.
type sealer struct {
	out, in       *rc4.Cipher
	outSeq, inSeq uint32
}

func newSealer(flags uint32, key []byte) (*sealer, error) {
	if flags&ntlm.FlagESS != 0 {
		return nil, errors.New("extended session security's sealing isn't served")
	}
	var k []byte
	if flags&ntlm.Flag56 != 0 {
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
