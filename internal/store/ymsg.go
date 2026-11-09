package store

import (
	"crypto/md5"
	"database/sql"
	"errors"
)

// Yahoo! Messenger's challenge login (Y!M 5.x, auth type 0) proves the
// password by two digests of it mixed with the server's challenge:
// MD5(password) and MD5(md5crypt(password, "$1$_2S43d5f$")), each in
// Yahoo's base 64.  The salt is fixed, so both are kept, made whenever the
// password is known in the clear and bound to the password hash they were
// made with, as the AIM and MSN digests are (oscar.go, msn.go).  Either
// is as good as the password to a Yahoo client: anyone who read them
// from the database could sign in by Yahoo as that user.

// YMSGKeys are an account's two Yahoo digests, in Yahoo's base 64.
type YMSGKeys struct {
	Pass  string // Y64(MD5(password))
	Crypt string // Y64(MD5(md5crypt(password, "$1$_2S43d5f$")))
}

// YMSGDigests are the digests of pw.
func YMSGDigests(pw string) YMSGKeys {
	p := md5.Sum([]byte(pw))
	c := md5.Sum([]byte(MD5Crypt(pw, "_2S43d5f")))
	return YMSGKeys{Pass: Y64(p[:]), Crypt: Y64(c[:])}
}

// y64digits are Yahoo's base 64 digits: "+/=" are "._-".
const y64digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._"

// Y64 is b in Yahoo's base 64 (padded with '-').
func Y64(b []byte) string {
	out := make([]byte, 0, (len(b)+2)/3*4)
	for ; len(b) >= 3; b = b[3:] {
		out = append(out, y64digits[b[0]>>2], y64digits[(b[0]<<4&0x30)|b[1]>>4],
			y64digits[(b[1]<<2&0x3c)|b[2]>>6], y64digits[b[2]&0x3f])
	}
	switch len(b) {
	case 1:
		out = append(out, y64digits[b[0]>>2], y64digits[b[0]<<4&0x30], '-', '-')
	case 2:
		out = append(out, y64digits[b[0]>>2], y64digits[(b[0]<<4&0x30)|b[1]>>4], y64digits[b[1]<<2&0x3c], '-')
	}
	return string(out)
}

// MD5Crypt is the FreeBSD MD5 crypt of pw with salt (at most 8
// characters): "$1$salt$hash".
func MD5Crypt(pw, salt string) string {
	if len(salt) > 8 {
		salt = salt[:8]
	}
	p, s := []byte(pw), []byte(salt)
	alt := md5.New()
	alt.Write(p)
	alt.Write(s)
	alt.Write(p)
	final := alt.Sum(nil)
	h := md5.New()
	h.Write(p)
	h.Write([]byte("$1$"))
	h.Write(s)
	for n := len(p); n > 0; n -= 16 {
		h.Write(final[:min(n, 16)])
	}
	for n := len(p); n > 0; n >>= 1 {
		if n&1 != 0 {
			h.Write([]byte{0})
		} else {
			h.Write(p[:1])
		}
	}
	final = h.Sum(nil)
	for i := range 1000 {
		r := md5.New()
		if i&1 != 0 {
			r.Write(p)
		} else {
			r.Write(final)
		}
		if i%3 != 0 {
			r.Write(s)
		}
		if i%7 != 0 {
			r.Write(p)
		}
		if i&1 != 0 {
			r.Write(final)
		} else {
			r.Write(p)
		}
		final = r.Sum(nil)
	}
	const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	out := []byte("$1$" + salt + "$")
	put := func(v uint32, n int) {
		for ; n > 0; n-- {
			out = append(out, itoa64[v&0x3f])
			v >>= 6
		}
	}
	f := final
	put(uint32(f[0])<<16|uint32(f[6])<<8|uint32(f[12]), 4)
	put(uint32(f[1])<<16|uint32(f[7])<<8|uint32(f[13]), 4)
	put(uint32(f[2])<<16|uint32(f[8])<<8|uint32(f[14]), 4)
	put(uint32(f[3])<<16|uint32(f[9])<<8|uint32(f[15]), 4)
	put(uint32(f[4])<<16|uint32(f[10])<<8|uint32(f[5]), 4)
	put(uint32(f[11]), 2)
	return string(out)
}

// SetKeepYMSG says whether to keep the digests: only with a Yahoo
// server, as they would let anyone who read the database sign in by
// Yahoo.
func (s *Store) SetKeepYMSG(on bool) { s.keepYMSG.Store(on) }

// YMSGKeys are the account's digests; ok false: none current (none kept,
// or the password changed since).
func (s *Store) YMSGKeys(id int64) (k YMSGKeys, ok bool, err error) {
	var bound, hash string
	err = s.db.QueryRow(`SELECT y.pass, y.crypt, y.bound, u.password FROM ymsg_keys y
		JOIN users u ON u.id = y.user_id WHERE y.user_id = ?`, id).Scan(&k.Pass, &k.Crypt, &bound, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return YMSGKeys{}, false, nil
	}
	if err != nil {
		return YMSGKeys{}, false, err
	}
	return k, bound == hash && k.Pass != "", nil
}

// KeepYMSGKeys makes the account's digests of pw (its password, just
// checked or set), bound to its password hash now, if the store keeps
// them (SetKeepYMSG).  Nothing is written if they are current already.
func (s *Store) KeepYMSGKeys(id int64, pw string) error {
	if !s.keepYMSG.Load() || pw == "" {
		return nil
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	k := YMSGDigests(pw)
	if old, ok, err := s.YMSGKeys(id); err == nil && ok && old == k {
		return nil
	}
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.Exec(`DELETE FROM ymsg_keys WHERE user_id = ?`, id); err != nil {
		return err
	}
	if _, err := t.Exec(`INSERT INTO ymsg_keys (user_id, pass, crypt, bound) VALUES (?, ?, ?, ?)`,
		id, k.Pass, k.Crypt, hash); err != nil {
		return err
	}
	return t.Commit()
}
