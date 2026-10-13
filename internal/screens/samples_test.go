package screens

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSamples: every tag in the sample screens and in bbs.conf.sample's
// menus and strings is one (a typo would show its braces).
func TestSamples(t *testing.T) {
	var texts []string
	for _, pat := range []string{"*.asc", "*.3270"} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "screens.sample", pat))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			texts = append(texts, string(b))
		}
	}
	conf, err := os.ReadFile(filepath.Join("..", "..", "bbs.conf.sample"))
	if err != nil {
		t.Fatal(err)
	}
	// the menus' and the strings' lines (other settings have {placeholders})
	c := string(conf)
	from, to := strings.Index(c, ";[menu main]"), strings.Index(c, ";welcome = ")
	if from < 0 || to < from {
		t.Fatal("no sample menus and strings in bbs.conf.sample")
	}
	setting := regexp.MustCompile(`^;[a-z_]+ = `)
	for _, l := range strings.Split(c[from:to], "\n") {
		if setting.MatchString(l) && strings.Contains(l, "{") {
			texts = append(texts, l)
		}
	}
	if len(texts) < 10 {
		t.Fatalf("only %d texts found", len(texts))
	}
	any := func(string) (string, bool) { return "x", true }
	for _, s := range texts {
		if left := String(s, any); strings.ContainsAny(left, "{}") {
			t.Errorf("an unknown tag in %q: %q", s, left)
		}
	}
}
