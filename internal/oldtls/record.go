package oldtls

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"hash"
)

// suite is a cipher suite: RSA key exchange, then a block cipher in CBC or
// RC4, with an HMAC.
type suite struct {
	id            uint16
	keyLen, ivLen int // ivLen 0: RC4
	macLen        int
	block         func(key []byte) (cipher.Block, error)
	hash          func() hash.Hash
}

// suites are those served, the best first.
var suites = []suite{
	{0x002f, 16, 16, 20, aes.NewCipher, sha1.New},         // TLS_RSA_WITH_AES_128_CBC_SHA
	{0x0035, 32, 16, 20, aes.NewCipher, sha1.New},         // TLS_RSA_WITH_AES_256_CBC_SHA
	{0x000a, 24, 8, 20, des.NewTripleDESCipher, sha1.New}, // TLS_RSA_WITH_3DES_EDE_CBC_SHA
	{0x0005, 16, 0, 20, nil, sha1.New},                    // TLS_RSA_WITH_RC4_128_SHA
	{0x0004, 16, 0, 16, nil, md5.New},                     // TLS_RSA_WITH_RC4_128_MD5
}

// half is one direction of the record layer: none (mac nil) until the
// ChangeCipherSpec, then a cipher and MAC.
type half struct {
	seq    uint64
	mac    hash.Hash
	block  cipher.BlockMode // CBC; its IV chains from record to record, as TLS 1.0's does
	stream cipher.Stream    // or RC4
}

// newHalf is a direction's protection from its keys (enc: the sending
// side).
func (s *suite) newHalf(mac, key, iv []byte, enc bool) half {
	h := half{mac: hmac.New(s.hash, mac)}
	if s.ivLen == 0 {
		r, _ := rc4.NewCipher(key)
		h.stream = r
		return h
	}
	b, _ := s.block(key)
	if enc {
		h.block = cipher.NewCBCEncrypter(b, iv)
	} else {
		h.block = cipher.NewCBCDecrypter(b, iv)
	}
	return h
}

func (h *half) macOf(typ uint8, data []byte) []byte {
	var hdr [13]byte
	binary.BigEndian.PutUint64(hdr[:], h.seq)
	hdr[8] = typ
	binary.BigEndian.PutUint16(hdr[9:], version10)
	binary.BigEndian.PutUint16(hdr[11:], uint16(len(data)))
	h.mac.Reset()
	h.mac.Write(hdr[:])
	h.mac.Write(data)
	return h.mac.Sum(nil)
}

// seal is a record of plain (at most maxPlain bytes).
func (h *half) seal(typ uint8, plain []byte) []byte {
	out := make([]byte, 5, 5+len(plain)+64)
	out[0] = typ
	binary.BigEndian.PutUint16(out[1:], version10)
	if h.mac == nil {
		out = append(out, plain...)
	} else {
		body := append(append(make([]byte, 0, len(plain)+64), plain...), h.macOf(typ, plain)...)
		h.seq++
		if h.block != nil {
			bs := h.block.BlockSize()
			n := (bs - (len(body)+1)%bs) % bs
			for range n + 1 {
				body = append(body, byte(n))
			}
			h.block.CryptBlocks(body, body)
		} else {
			h.stream.XORKeyStream(body, body)
		}
		out = append(out, body...)
	}
	binary.BigEndian.PutUint16(out[3:], uint16(len(out)-5))
	return out
}

// open is a record's plaintext, in place; ok false: its MAC or padding is
// wrong.  A bad padding is still MACed, so the two fail alike.
func (h *half) open(typ uint8, body []byte) (plain []byte, ok bool) {
	if h.mac == nil {
		return body, true
	}
	ms := h.mac.Size()
	defer func() { h.seq++ }()
	if h.stream != nil {
		h.stream.XORKeyStream(body, body)
		if len(body) < ms {
			return nil, false
		}
		n := len(body) - ms
		return body[:n], hmac.Equal(h.macOf(typ, body[:n]), body[n:])
	}
	bs := h.block.BlockSize()
	if len(body)%bs != 0 || len(body) < ms+1 {
		return nil, false
	}
	h.block.CryptBlocks(body, body)
	pad := int(body[len(body)-1])
	good := true
	if pad+1+ms > len(body) {
		good, pad = false, 0
	}
	for _, b := range body[len(body)-1-pad : len(body)-1] {
		if b != byte(pad) {
			good = false
		}
	}
	n := len(body) - 1 - pad - ms
	macOK := hmac.Equal(h.macOf(typ, body[:n]), body[n:n+ms])
	return body[:n], macOK && good
}

// prf10 is TLS 1.0's PRF: P_MD5 over the secret's first half, XORed with
// P_SHA1 over its second.
func prf10(secret []byte, label string, seed []byte, n int) []byte {
	ls := append([]byte(label), seed...)
	s1, s2 := secret[:(len(secret)+1)/2], secret[len(secret)/2:]
	out, b := make([]byte, n), make([]byte, n)
	pHash(out, s1, ls, md5.New)
	pHash(b, s2, ls, sha1.New)
	for i := range out {
		out[i] ^= b[i]
	}
	return out
}

func pHash(out, secret, seed []byte, h func() hash.Hash) {
	m := hmac.New(h, secret)
	m.Write(seed)
	a := m.Sum(nil)
	for j := 0; j < len(out); {
		m.Reset()
		m.Write(a)
		m.Write(seed)
		j += copy(out[j:], m.Sum(nil))
		m.Reset()
		m.Write(a)
		a = m.Sum(nil)
	}
}

// keys are a session's client and server protection, from its master
// secret and randoms.
func (s *suite) keys(ms, cr, sr []byte) (client, server struct{ mac, key, iv []byte }) {
	kb := prf10(ms, "key expansion", append(append([]byte{}, sr...), cr...), 2*(s.macLen+s.keyLen+s.ivLen))
	take := func(n int) []byte { v := kb[:n]; kb = kb[n:]; return v }
	client.mac, server.mac = take(s.macLen), take(s.macLen)
	client.key, server.key = take(s.keyLen), take(s.keyLen)
	client.iv, server.iv = take(s.ivLen), take(s.ivLen)
	return
}
