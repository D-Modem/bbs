package ymsg

import (
	"crypto/md5"
	"crypto/sha1"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/store"
)

// answers1 is the client side of auth type 1, step by step as libyahoo2's
// yahoo_process_auth_0x0b does it. It has no transform tables, so it fails
// the test unless the challenge needs none (depth 0).
func answers1(t *testing.T, pw, seed string) (string, string) {
	t.Helper()
	key := chalKey(t, seed)
	p := md5.Sum([]byte(pw))
	c := md5.Sum([]byte(store.MD5Crypt(pw, "_2S43d5f")))
	return libAnswer(y64(p[:]), key), libAnswer(y64(c[:]), key)
}

// chalKey reads a challenge as the client does: the 4-byte key, checking
// the depth is 0 and the table under 2 (above that, libyahoo2 changes the
// SHA-1 length, which Yahoo's client may not).
func chalKey(t *testing.T, seed string) [4]byte {
	t.Helper()
	// phase 1: letters set the high bits, operators add a byte
	var magic []int
	work := 0
	for i := 0; i < len(seed); i++ {
		ch := seed[i]
		switch {
		case ch == '(' || ch == ')':
		case ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9':
			n := strings.IndexByte(chalLetters, ch)
			if n < 0 {
				t.Fatalf("challenge %q: %q would hang the client", seed, ch)
			}
			work = n << 3
		default:
			n := strings.IndexByte(chalOperands, ch)
			if n < 0 {
				t.Fatalf("challenge %q: %q would hang the client", seed, ch)
			}
			if len(magic) < 64 {
				magic = append(magic, work|n)
			}
		}
	}
	// phase 2
	for i := len(magic) - 2; i >= 0; i-- {
		b1 := byte(magic[i])
		b1 *= 0xcd
		b1 ^= byte(magic[i+1])
		magic[i+1] = int(b1)
	}
	// phase 3
	var src []byte
	n := 1
	for len(src) < 20 {
		cl := magic[n]
		n++
		if n >= len(magic) {
			t.Fatalf("challenge %q: too short", seed)
		}
		var bl int
		if cl > 0x7f {
			if cl < 0xe0 {
				bl = (cl & 0x1f) << 6
			} else {
				bl = magic[n]
				n++
				bl = ((bl & 0x3f) + (cl&0x0f)<<6) << 6
			}
			cl = magic[n]
			n++
			bl = (cl & 0x3f) + bl
		} else {
			bl = cl
		}
		src = append(src, byte(bl>>8), byte(bl))
	}
	var key [4]byte
	copy(key[:], src)
	for i := 0; i < 0xffff; i++ {
		for j := 0; j < 5; j++ {
			sum := md5.Sum([]byte{key[0], key[1], key[2], key[3], byte(i), byte(i >> 8), byte(j)})
			if string(sum[:]) == string(src[4:]) {
				if i != 0 || j >= 2 {
					t.Fatalf("challenge %q: depth %d, table %d", seed, i, j)
				}
				return key
			}
		}
		if i > 4 {
			break // it would be at depth 0
		}
	}
	t.Fatalf("challenge %q: no depth and table fit", seed)
	return key
}

// libAnswer is one answer: SHA-1 twice with the padded digest, written as
// libyahoo2 writes it.
func libAnswer(digest string, key [4]byte) string {
	var x1, x2 [64]byte
	for i := range x1 {
		x1[i], x2[i] = 0x36, 0x5c
	}
	for i := 0; i < len(digest); i++ {
		x1[i] = digest[i] ^ 0x36
		x2[i] = digest[i] ^ 0x5c
	}
	d1 := sha1.Sum(append(x1[:], key[:]...))
	d2 := sha1.Sum(append(x2[:], d1[:]...))
	var out string
	for i := 0; i < 20; i += 2 {
		v := int(d2[i])<<8 + int(d2[i+1])
		out += string(ansAlpha1[(v>>0x0b)&0x1f]) + "=" + string(ansAlpha2[(v>>0x06)&0x1f]) +
			string(ansAlpha2[(v>>0x01)&0x1f]) + string(",;"[v&1])
	}
	return out
}

func TestChallenge1(t *testing.T) {
	keys := store.YMSGDigests("password1")
	for range 500 {
		seed, key := newChallenge1()
		if got := chalKey(t, seed); got != key {
			t.Fatalf("challenge %q: key %x, want %x", seed, got, key)
		}
		w6, w96 := challengeAnswers1(keys, key)
		g6, g96 := answers1(t, "password1", seed)
		if w6 != g6 || w96 != g96 {
			t.Fatalf("answers: %q %q, client %q %q", w6, w96, g6, g96)
		}
		if len(g6) != 50 {
			t.Fatalf("answer %q", g6)
		}
	}
}

func TestLoginTypes(t *testing.T) {
	e := start(t)
	for _, v := range []uint16{9, 11, 13, 15} {
		if code, ok := dial(t, e, v).login("alice", "password1"); !ok {
			t.Errorf("YMSG %d: refused (%s)", v, code)
		}
		if code, ok := dial(t, e, v).login("alice", "wrong"); ok || code != "13" {
			t.Errorf("YMSG %d, wrong password: %q %v", v, code, ok)
		}
	}
	e = startWith(t, func(s *Settings) { s.OldLogin = true })
	if code, ok := dial(t, e, 13).login("bob", "password1"); !ok {
		t.Errorf("old_login: refused (%s)", code)
	}
}
