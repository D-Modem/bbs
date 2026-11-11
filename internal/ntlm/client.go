package ntlm

import "encoding/binary"

// ClientNegotiate is a client's NEGOTIATE with flags (for tests).
func ClientNegotiate(flags uint32) []byte {
	b := append([]byte(nil), Signature...)
	b = binary.LittleEndian.AppendUint32(b, TypeNegotiate)
	b = binary.LittleEndian.AppendUint32(b, flags)
	return append(b, make([]byte, 16)...)
}

// ClientAuthenticate is a client's AUTHENTICATE (the long header), in
// Unicode (for tests).
func ClientAuthenticate(flags uint32, domain, user string, lm, nt []byte) []byte {
	payload := [][]byte{lm, nt, utf16le(domain), utf16le(user), utf16le("WS")}
	b := append([]byte(nil), Signature...)
	b = binary.LittleEndian.AppendUint32(b, TypeAuthenticate)
	off := 64
	for _, p := range payload {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(p)))
		b = binary.LittleEndian.AppendUint16(b, uint16(len(p)))
		b = binary.LittleEndian.AppendUint32(b, uint32(off))
		off += len(p)
	}
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0) // session key: none
	b = binary.LittleEndian.AppendUint32(b, flags)
	for _, p := range payload {
		b = append(b, p...)
	}
	return b
}
