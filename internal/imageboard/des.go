package imageboard

// The traditional Unix crypt(3): DES, its E expansion perturbed by a
// 12-bit salt, run 25 times over a block of zeros.  It is what 2channel's
// tripcodes are made with, and so every image board's since: the same
// password gives the same tripcode on all of them.  Its tables are DES's
// (FIPS 46-3), numbered from 1 as the standard numbers them.

var desIP = [64]byte{
	58, 50, 42, 34, 26, 18, 10, 2, 60, 52, 44, 36, 28, 20, 12, 4,
	62, 54, 46, 38, 30, 22, 14, 6, 64, 56, 48, 40, 32, 24, 16, 8,
	57, 49, 41, 33, 25, 17, 9, 1, 59, 51, 43, 35, 27, 19, 11, 3,
	61, 53, 45, 37, 29, 21, 13, 5, 63, 55, 47, 39, 31, 23, 15, 7,
}

var desFP = [64]byte{
	40, 8, 48, 16, 56, 24, 64, 32, 39, 7, 47, 15, 55, 23, 63, 31,
	38, 6, 46, 14, 54, 22, 62, 30, 37, 5, 45, 13, 53, 21, 61, 29,
	36, 4, 44, 12, 52, 20, 60, 28, 35, 3, 43, 11, 51, 19, 59, 27,
	34, 2, 42, 10, 50, 18, 58, 26, 33, 1, 41, 9, 49, 17, 57, 25,
}

var desE = [48]byte{
	32, 1, 2, 3, 4, 5, 4, 5, 6, 7, 8, 9,
	8, 9, 10, 11, 12, 13, 12, 13, 14, 15, 16, 17,
	16, 17, 18, 19, 20, 21, 20, 21, 22, 23, 24, 25,
	24, 25, 26, 27, 28, 29, 28, 29, 30, 31, 32, 1,
}

var desP = [32]byte{
	16, 7, 20, 21, 29, 12, 28, 17, 1, 15, 23, 26, 5, 18, 31, 10,
	2, 8, 24, 14, 32, 27, 3, 9, 19, 13, 30, 6, 22, 11, 4, 25,
}

var desPC1 = [56]byte{
	57, 49, 41, 33, 25, 17, 9, 1, 58, 50, 42, 34, 26, 18,
	10, 2, 59, 51, 43, 35, 27, 19, 11, 3, 60, 52, 44, 36,
	63, 55, 47, 39, 31, 23, 15, 7, 62, 54, 46, 38, 30, 22,
	14, 6, 61, 53, 45, 37, 29, 21, 13, 5, 28, 20, 12, 4,
}

var desPC2 = [48]byte{
	14, 17, 11, 24, 1, 5, 3, 28, 15, 6, 21, 10,
	23, 19, 12, 4, 26, 8, 16, 7, 27, 20, 13, 2,
	41, 52, 31, 37, 47, 55, 30, 40, 51, 45, 33, 48,
	44, 49, 39, 56, 34, 53, 46, 42, 50, 36, 29, 32,
}

var desShifts = [16]byte{1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1}

var desS = [8][64]byte{
	{14, 4, 13, 1, 2, 15, 11, 8, 3, 10, 6, 12, 5, 9, 0, 7,
		0, 15, 7, 4, 14, 2, 13, 1, 10, 6, 12, 11, 9, 5, 3, 8,
		4, 1, 14, 8, 13, 6, 2, 11, 15, 12, 9, 7, 3, 10, 5, 0,
		15, 12, 8, 2, 4, 9, 1, 7, 5, 11, 3, 14, 10, 0, 6, 13},
	{15, 1, 8, 14, 6, 11, 3, 4, 9, 7, 2, 13, 12, 0, 5, 10,
		3, 13, 4, 7, 15, 2, 8, 14, 12, 0, 1, 10, 6, 9, 11, 5,
		0, 14, 7, 11, 10, 4, 13, 1, 5, 8, 12, 6, 9, 3, 2, 15,
		13, 8, 10, 1, 3, 15, 4, 2, 11, 6, 7, 12, 0, 5, 14, 9},
	{10, 0, 9, 14, 6, 3, 15, 5, 1, 13, 12, 7, 11, 4, 2, 8,
		13, 7, 0, 9, 3, 4, 6, 10, 2, 8, 5, 14, 12, 11, 15, 1,
		13, 6, 4, 9, 8, 15, 3, 0, 11, 1, 2, 12, 5, 10, 14, 7,
		1, 10, 13, 0, 6, 9, 8, 7, 4, 15, 14, 3, 11, 5, 2, 12},
	{7, 13, 14, 3, 0, 6, 9, 10, 1, 2, 8, 5, 11, 12, 4, 15,
		13, 8, 11, 5, 6, 15, 0, 3, 4, 7, 2, 12, 1, 10, 14, 9,
		10, 6, 9, 0, 12, 11, 7, 13, 15, 1, 3, 14, 5, 2, 8, 4,
		3, 15, 0, 6, 10, 1, 13, 8, 9, 4, 5, 11, 12, 7, 2, 14},
	{2, 12, 4, 1, 7, 10, 11, 6, 8, 5, 3, 15, 13, 0, 14, 9,
		14, 11, 2, 12, 4, 7, 13, 1, 5, 0, 15, 10, 3, 9, 8, 6,
		4, 2, 1, 11, 10, 13, 7, 8, 15, 9, 12, 5, 6, 3, 0, 14,
		11, 8, 12, 7, 1, 14, 2, 13, 6, 15, 0, 9, 10, 4, 5, 3},
	{12, 1, 10, 15, 9, 2, 6, 8, 0, 13, 3, 4, 14, 7, 5, 11,
		10, 15, 4, 2, 7, 12, 9, 5, 6, 1, 13, 14, 0, 11, 3, 8,
		9, 14, 15, 5, 2, 8, 12, 3, 7, 0, 4, 10, 1, 13, 11, 6,
		4, 3, 2, 12, 9, 5, 15, 10, 11, 14, 1, 7, 6, 0, 8, 13},
	{4, 11, 2, 14, 15, 0, 8, 13, 3, 12, 9, 7, 5, 10, 6, 1,
		13, 0, 11, 7, 4, 9, 1, 10, 14, 3, 5, 12, 2, 15, 8, 6,
		1, 4, 11, 13, 12, 3, 7, 14, 10, 15, 6, 8, 0, 5, 9, 2,
		6, 11, 13, 8, 1, 4, 10, 7, 9, 5, 0, 15, 14, 2, 3, 12},
	{13, 2, 8, 4, 6, 15, 11, 1, 10, 9, 3, 14, 5, 0, 12, 7,
		1, 15, 13, 8, 10, 3, 7, 4, 12, 5, 6, 11, 0, 14, 9, 2,
		7, 11, 4, 1, 9, 12, 14, 2, 0, 6, 10, 13, 15, 3, 5, 8,
		2, 1, 14, 7, 4, 10, 8, 13, 15, 12, 9, 0, 3, 5, 6, 11},
}

const cryptChars = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// cryptValue is a salt character's value (an unknown one counts as '.').
func cryptValue(c byte) uint {
	switch {
	case c >= 'a' && c <= 'z':
		return uint(c-'a') + 38
	case c >= 'A' && c <= 'Z':
		return uint(c-'A') + 12
	case c >= '.' && c <= '9':
		return uint(c - '.')
	}
	return 0
}

// desCrypt is crypt(3) of a key (its first 8 bytes) with a 2-character
// salt: the salt, then 11 characters.
func desCrypt(key []byte, salt string) string {
	if len(salt) < 2 {
		salt += "..."[:2-len(salt)]
	}
	var kb [64]byte
	for i := 0; i < 8 && i < len(key); i++ {
		c := key[i] << 1
		for j := 0; j < 8; j++ {
			kb[i*8+j] = c >> (7 - j) & 1
		}
	}
	// the 16 subkeys
	var cd [56]byte
	for i, p := range desPC1 {
		cd[i] = kb[p-1]
	}
	var subkeys [16][48]byte
	for r := 0; r < 16; r++ {
		for n := byte(0); n < desShifts[r]; n++ {
			c0, d0 := cd[0], cd[28]
			copy(cd[0:27], cd[1:28])
			copy(cd[28:55], cd[29:56])
			cd[27], cd[55] = c0, d0
		}
		for i, p := range desPC2 {
			subkeys[r][i] = cd[p-1]
		}
	}
	sv := cryptValue(salt[0]) | cryptValue(salt[1])<<6
	var block [64]byte
	for it := 0; it < 25; it++ {
		var lr [64]byte
		for i, p := range desIP {
			lr[i] = block[p-1]
		}
		l, r := lr[:32], lr[32:]
		for round := 0; round < 16; round++ {
			var e [48]byte
			for i, p := range desE {
				e[i] = r[p-1]
			}
			for i := 0; i < 12; i++ {
				if sv>>i&1 == 1 {
					e[i], e[i+24] = e[i+24], e[i]
				}
			}
			for i := range e {
				e[i] ^= subkeys[round][i]
			}
			var sOut [32]byte
			for b := 0; b < 8; b++ {
				x := e[b*6 : b*6+6]
				row := x[0]<<1 | x[5]
				col := x[1]<<3 | x[2]<<2 | x[3]<<1 | x[4]
				v := desS[b][row*16+col]
				for j := 0; j < 4; j++ {
					sOut[b*4+j] = v >> (3 - j) & 1
				}
			}
			var nr [32]byte
			for i, p := range desP {
				nr[i] = l[i] ^ sOut[p-1]
			}
			copy(l, r)
			copy(r, nr[:])
		}
		var pre [64]byte
		copy(pre[:32], r)
		copy(pre[32:], l)
		for i, p := range desFP {
			block[i] = pre[p-1]
		}
	}
	out := []byte{salt[0], salt[1]}
	for i := 0; i < 66; i += 6 {
		v := 0
		for j := 0; j < 6; j++ {
			v <<= 1
			if i+j < 64 {
				v |= int(block[i+j])
			}
		}
		out = append(out, cryptChars[v])
	}
	return string(out)
}
