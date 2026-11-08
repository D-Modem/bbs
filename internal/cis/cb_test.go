package cis

import "testing"

func TestScramble(t *testing.T) {
	text := "Hello, CB! 123"
	s := scramble("xyz", text)
	if s == text || len(s) != len(text) || s[5] != ',' && s[6] != ' ' {
		t.Errorf("scrambled %q", s)
	}
	if s[6] != ' ' {
		t.Errorf("spaces are kept: %q", s)
	}
	if u := unscramble("XYZ", s); u != text {
		t.Errorf("unscrambled %q", u)
	}
	if u := unscramble("abc", s); u == text {
		t.Error("another key unscrambled it")
	}
}
