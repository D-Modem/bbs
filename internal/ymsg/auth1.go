package ymsg

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// Auth type 1 is the challenge login of Y!M 6 to 8 (YMSG 10 to 15).
//
// The client turns the challenge into 20 bytes: a 4-byte key followed by
// MD5(key, depth, table). It searches for the depth and table that give
// that MD5, then runs the key through Yahoo's transform tables that many
// times. The answers are HMAC-SHA1 of the key, keyed with the two password
// digests auth type 0 already uses.
//
// The server picks the challenge, so it always picks depth 0 and table 0.
// The transform then runs no times and the key is used as it is, so the
// server needs none of the transform tables.

const (
	chalLetters  = "qzec2tb3um1olpar8whx4dfgijknsvy5"
	chalOperands = "+|&%/*^-"
	ansAlpha1    = "FBZDWAGHrJTLMNOPpRSKUVEXYChImkwQ"
	ansAlpha2    = "F0E1D2C3B4A59687abcdefghijklmnop"
)

// newChallenge1 returns an auth type 1 challenge and the 4-byte key the
// client will find in it.
func newChallenge1() (string, [4]byte) {
	var key [4]byte
	rand.Read(key[:])
	sum := md5.Sum(append(key[:], 0, 0, 0)) // depth 0, table 0

	// The 20 bytes are ten 16-bit values, each written as three bytes:
	// 0xe0|top 4 bits, then 6 bits, then 6 bits.
	src := append(key[:], sum[:]...)
	var r [2]byte
	rand.Read(r[:])
	f := []byte{r[0]} // the client skips the first byte
	for i := 0; i < len(src); i += 2 {
		v := int(src[i])<<8 | int(src[i+1])
		f = append(f, byte(0xe0|v>>12), byte(v>>6&0x3f), byte(v&0x3f))
	}
	f = append(f, r[1]) // the client stops one short of the end

	// The client mixes each byte with the one before it:
	// f[i+1] = m[i+1] ^ m[i]*0xcd. Undo that so it gets f back.
	m := make([]byte, len(f))
	m[0] = f[0]
	for i := 1; i < len(f); i++ {
		m[i] = f[i] ^ m[i-1]*0xcd
	}

	// Each byte is a letter (top 5 bits) and an operator (low 3 bits).
	// Brackets are ignored by the client; Yahoo's challenges had them.
	var b strings.Builder
	for i, c := range m {
		if i%4 == 0 {
			b.WriteByte('(')
		}
		b.WriteByte(chalLetters[c>>3])
		b.WriteByte(chalOperands[c&7])
		if i%4 == 3 || i == len(m)-1 {
			b.WriteByte(')')
		}
	}
	return b.String(), key
}

// answer1 is one auth type 1 answer: HMAC-SHA1 of the key, keyed with a
// Y64 digest, written five characters per 16 bits.
func answer1(digest string, key [4]byte) string {
	h := hmac.New(sha1.New, []byte(digest))
	h.Write(key[:])
	d := h.Sum(nil)
	var b strings.Builder
	for i := 0; i < len(d); i += 2 {
		v := int(d[i])<<8 | int(d[i+1])
		b.WriteByte(ansAlpha1[v>>11&0x1f])
		b.WriteByte('=')
		b.WriteByte(ansAlpha2[v>>6&0x1f])
		b.WriteByte(ansAlpha2[v>>1&0x1f])
		b.WriteByte(",;"[v&1])
	}
	return b.String()
}

// challengeAnswers1 are auth type 1's two answers (keys 6 and 96).
func challengeAnswers1(k store.YMSGKeys, key [4]byte) (string, string) {
	return answer1(k.Pass, key), answer1(k.Crypt, key)
}
