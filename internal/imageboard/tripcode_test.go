package imageboard

import "testing"

// crypt(3) as glibc (libxcrypt) has it, by perl's crypt.
var cryptVectors = []struct{ key, salt, want string }{
	{"\x74\x65\x61", "H.", "H.br/3AFayuA6"},
	{"\x74\x65\x61", "ea", "eauWokonZwxw2"},
	{"\x74\x65\x61", "./", "./YzBHeSrdtok"},
	{"\x74\x65\x61", "zz", "zzSuWEzr.werg"},
	{"\x74\x65\x61", "A0", "A0d7wrn7NvNIE"},
	{"\x66\x61\x67\x67\x6f\x74", "H.", "H.fGD9xAZQOKg"},
	{"\x66\x61\x67\x67\x6f\x74", "ea", "eahFxG76h7zl."},
	{"\x66\x61\x67\x67\x6f\x74", "./", ".//NypE/U/qWA"},
	{"\x66\x61\x67\x67\x6f\x74", "zz", "zz/gpilhVyi/2"},
	{"\x66\x61\x67\x67\x6f\x74", "A0", "A0zab8cs93TZs"},
	{"", "H.", "H.2jPpg5.obl6"},
	{"", "ea", "eaIv9eteVyQUM"},
	{"", "./", "./Una9Fi.seRo"},
	{"", "zz", "zz6dpSdr.LHZw"},
	{"", "A0", "A0BPVpWCkcv5o"},
	{"\x61", "H.", "H.6ZnBI2EKkq."},
	{"\x61", "ea", "eaTFN5ysU0JYI"},
	{"\x61", "./", "./xT2u5QYaHcU"},
	{"\x61", "zz", "zzJZ5PtvFqi9o"},
	{"\x61", "A0", "A0gq9shZXLDtQ"},
	{"\x70\x61\x73\x73\x77\x6f\x72\x64", "H.", "H.uSfq3SPdV.c"},
	{"\x70\x61\x73\x73\x77\x6f\x72\x64", "ea", "eaL0dOhJ7ObMw"},
	{"\x70\x61\x73\x73\x77\x6f\x72\x64", "./", "./xZjzHv5vzVE"},
	{"\x70\x61\x73\x73\x77\x6f\x72\x64", "zz", "zzXUHfURnGg8I"},
	{"\x70\x61\x73\x73\x77\x6f\x72\x64", "A0", "A0qxUqZSv1XUU"},
	{"\x31\x32\x33\x34\x35\x36\x37\x38", "H.", "H.pRvhij78pUE"},
	{"\x31\x32\x33\x34\x35\x36\x37\x38", "ea", "eaQvwmUPGFy6k"},
	{"\x31\x32\x33\x34\x35\x36\x37\x38", "./", "./qw5JW./79Vg"},
	{"\x31\x32\x33\x34\x35\x36\x37\x38", "zz", "zzRtj6pNdfpLE"},
	{"\x31\x32\x33\x34\x35\x36\x37\x38", "A0", "A0B5JRmm6jDDE"},
	{"\x6c\x6f\x6e\x67\x65\x72\x74\x68\x61\x6e\x38\x63\x68\x61\x72\x73", "H.", "H.yY7BHPVkmxI"},
	{"\x6c\x6f\x6e\x67\x65\x72\x74\x68\x61\x6e\x38\x63\x68\x61\x72\x73", "ea", "eaq9SQqclHErg"},
	{"\x6c\x6f\x6e\x67\x65\x72\x74\x68\x61\x6e\x38\x63\x68\x61\x72\x73", "./", "./haKoGjqSo/Y"},
	{"\x6c\x6f\x6e\x67\x65\x72\x74\x68\x61\x6e\x38\x63\x68\x61\x72\x73", "zz", "zz6XwEAsbAWcE"},
	{"\x6c\x6f\x6e\x67\x65\x72\x74\x68\x61\x6e\x38\x63\x68\x61\x72\x73", "A0", "A0bfkiZ6ERrOA"},
	{"\x82\xa0\x78", "H.", "H.Z7RiE95NgPU"},
	{"\x82\xa0\x78", "ea", "eazOqpcN5sPEg"},
	{"\x82\xa0\x78", "./", "./XaC86oTFO8U"},
	{"\x82\xa0\x78", "zz", "zzgck742pixEQ"},
	{"\x82\xa0\x78", "A0", "A0Mc8wN7bP4f."},
	{"\x61\x26\x61\x6d\x70\x3b\x62", "H.", "H.O5sU2MVxp2E"},
	{"\x61\x26\x61\x6d\x70\x3b\x62", "ea", "eaCqRypc1nvlQ"},
	{"\x61\x26\x61\x6d\x70\x3b\x62", "./", "./N5UdDJOu1vE"},
	{"\x61\x26\x61\x6d\x70\x3b\x62", "zz", "zzHB7PLIiZFIM"},
	{"\x61\x26\x61\x6d\x70\x3b\x62", "A0", "A0LaAfTVberTc"},
}

// TestDESCrypt: desCrypt is crypt(3)'s, by its vectors.
func TestDESCrypt(t *testing.T) {
	for _, v := range cryptVectors {
		if got := desCrypt([]byte(v.key), v.salt); got != v.want {
			t.Errorf("crypt(%q, %q) = %q, want %q", v.key, v.salt, got, v.want)
		}
	}
}

// TestTripcode: names split from their tripcodes, plain and secure (and
// both), with keys escaped as HTML and in Shift JIS before hashing.
func TestTripcode(t *testing.T) {
	for typed, want := range map[string][2]string{
		"Anon#tea":   {"Anon", "!WokonZwxw2"},
		"#faggot":    {"", "!Ep8pui8Vw2"},
		"Bob":        {"Bob", ""},
		" Bob # ":    {"Bob", "!" + tripcode(" ")},
		"Al##secret": {"Al", "!!" + secureTrip("secret", []byte("k"))},
		"Al#tea##s":  {"Al", "!WokonZwxw2!!" + secureTrip("s", []byte("k"))},
	} {
		name, trip := splitName(typed, []byte("k"))
		if name != want[0] || trip != want[1] {
			t.Errorf("%q: %q %q, want %q", typed, name, trip, want)
		}
	}
	// escaped as HTML, and in Shift JIS, before it is hashed
	if got, want := tripcode("a<b"), desCrypt([]byte("a&lt;b"), ".l")[3:]; got != want {
		t.Errorf("escaped: %q, want %q", got, want)
	}
	if got, want := tripcode("あ"), desCrypt([]byte{0x82, 0xa0}, ".H")[3:]; got != want {
		t.Errorf("Shift JIS: %q, want %q", got, want)
	}
}
