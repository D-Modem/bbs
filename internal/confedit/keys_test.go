package confedit

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	bbssec "github.com/Neo-Desktop/bbs-sec"
)

// TestEveryKeyKnown: each key the loader reads is in the sample (and so
// has help in the editor).
func TestEveryKeyKnown(t *testing.T) {
	c := NewCatalog(bbssec.Sample)
	known := map[string]bool{}
	for _, k := range c.Kinds {
		for _, x := range k.Keys {
			known[x.Name] = true
		}
	}
	re := regexp.MustCompile(`\.(?:String|Bool|Int|Seconds|Size|Has)\("([a-z0-9_]+)"|\{"([a-z0-9_]+)", &`)
	files, _ := filepath.Glob("../server/*.go")
	var missing []string
	for _, f := range files {
		if filepath.Ext(f) != ".go" || len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			k := m[1] + m[2]
			if !known[k] {
				missing = append(missing, filepath.Base(f)+": "+k)
				known[k] = true
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("not in bbs.conf.sample: %s", m)
	}
}
