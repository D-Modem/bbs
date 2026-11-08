package cis

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	mrand "math/rand/v2"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// CompuServe's Secure Login Protocol, as WinCIM 3.0.2 does it (recovered
// from CCTN240C.DLL by 24high/CompuServe-Emulator, SECURE-LOGIN.md, with
// its test vectors):
//
//	H→C  "RA:" CR LF HostChallenge "." CR LF
//	C→H  "RB:" CR LF MicroChallenge "." CR LF
//	C→H  "UR:" CR LF UserResponse "." CR LF
//	H→C  "HR:" CR LF HostResponse "." CR LF    (or "No.")
//
// Each value is Base64 of its bytes and a checksum byte, in lines of 76
// split by a CR.  K = MD5(UTF-16BE(lowercase password)) (store.CISKeyOf),
// Z 48 zero bytes:
//
//	UserResponse = MD5(K Z H M K)
//	HostResponse = MD5(K Z M H K)

// slpSum is the checksum: a 16-bit ones'-complement sum of little-endian
// words, folded to 8 bits and inverted.
func slpSum(data []byte) byte {
	var sum uint32
	i := 0
	for ; len(data)-i > 1; i += 2 {
		sum += uint32(data[i]) | uint32(data[i+1])<<8
	}
	if i < len(data) {
		sum += uint32(data[i])
	}
	for sum&0xFFFF0000 != 0 {
		sum = sum>>16 + sum&0xFFFF
	}
	for sum&0xFFFFFF00 != 0 {
		sum = sum&0xFF + sum>>8
	}
	return ^byte(sum)
}

// slpEncode is data and its checksum in Base64, CR every 76.
func slpEncode(data []byte) string {
	s := base64.StdEncoding.EncodeToString(append(append([]byte{}, data...), slpSum(data)))
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

var errSLP = errors.New("cis: bad Secure Login value")

// slpDecode is a value's bytes, its checksum checked.  White space is
// skipped, and "." ends it.
func slpDecode(s string) ([]byte, error) {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\r' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if len(s) == 0 || len(s) > 1000 || len(s)%4 != 0 {
		return nil, errSLP
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) < 2 || slpSum(b[:len(b)-1]) != b[len(b)-1] {
		return nil, errSLP
	}
	return b[:len(b)-1], nil
}

func slpDigest(parts ...[]byte) []byte {
	h := md5.New()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

var slpZ = make([]byte, 48)

// slpUser is the UserResponse the client must send.
func slpUser(k, host, micro []byte) []byte { return slpDigest(k, slpZ, host, micro, k) }

// slpHost is the HostResponse that proves the host knows K.
func slpHost(k, host, micro []byte) []byte { return slpDigest(k, slpZ, micro, host, k) }

// secureLogin is the Secure Login of name, through the BBS's login (its
// throttling and delays).  The account needs its key kept: a password
// login, or a password set, while a CompuServe listener ran.
func (s *session) secureLogin(name string) (*store.User, error) {
	return s.node.LoginWith(name, "CompuServe Secure Login", func() (*store.User, error) {
		host := make([]byte, 64+mrand.IntN(0x201-64))
		rand.Read(host)
		s.out("RA:\n" + slpEncode(host) + ".\n")
		micro, user, err := s.readSecure()
		if err != nil {
			if errors.Is(err, errSLP) {
				return nil, store.ErrNotFound
			}
			return nil, err
		}
		u, err := s.b.Store.UserByName(name)
		if err != nil {
			return nil, err // not found: a failure
		}
		k, ok, err := s.b.Store.CISKey(u.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			s.b.Log.Printf("node %d: CompuServe: no Secure Login key kept for %s: they log on once with their password (CIM: DisableSecureLogin=ON)", s.node.Num, u.Name)
			return nil, store.ErrNotFound
		}
		if subtle.ConstantTimeCompare(user, slpUser(k, host, micro)) != 1 {
			return nil, store.ErrNotFound
		}
		s.out("HR:\n" + slpEncode(slpHost(k, host, micro)) + ".\n")
		return u, nil
	})
}

// readSecure reads the client's RB: and UR: blocks; an ETX instead is its
// script giving up (errSLP).
func (s *session) readSecure() (micro, user []byte, err error) {
	var b []byte
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		c, err := s.getc(time.Until(deadline))
		if err != nil {
			if errors.Is(err, errIdle) {
				break
			}
			return nil, nil, err
		}
		if c == 3 {
			return nil, nil, errSLP
		}
		b = append(b, c)
		if len(b) > 4096 {
			return nil, nil, errSLP
		}
		t := string(b)
		rb := strings.Index(t, "RB:")
		if rb < 0 {
			continue
		}
		d1 := strings.IndexByte(t[rb:], '.')
		if d1 < 0 {
			continue
		}
		ur := strings.Index(t[rb+d1:], "UR:")
		if ur < 0 {
			continue
		}
		ur += rb + d1
		d2 := strings.IndexByte(t[ur:], '.')
		if d2 < 0 {
			continue
		}
		s.cr = false
		if micro, err = slpDecode(t[rb+3 : rb+d1]); err != nil {
			return nil, nil, err
		}
		if user, err = slpDecode(t[ur+3 : ur+d2]); err != nil {
			return nil, nil, err
		}
		return micro, user, nil
	}
	return nil, nil, errSLP
}

// SecureAnswer is the client's side of the Secure Login, for tests: from
// the host's RA: value, the RB: and UR: values a CIM sends, and a check of
// the host's HR: value.
func SecureAnswer(password, ra string) (rb, ur string, hostOK func(hr string) bool, err error) {
	host, err := slpDecode(ra)
	if err != nil {
		return "", "", nil, err
	}
	micro := make([]byte, 64+mrand.IntN(0x201-64))
	rand.Read(micro)
	k := store.CISKeyOf(password)
	want := slpHost(k, host, micro)
	return slpEncode(micro), slpEncode(slpUser(k, host, micro)), func(hr string) bool {
		got, err := slpDecode(hr)
		return err == nil && subtle.ConstantTimeCompare(got, want) == 1
	}, nil
}
