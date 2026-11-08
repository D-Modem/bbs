package fdo

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The corpus tests run against AtomForge's golden files (MIT License:
// LICENSE.atomforge): FDO91 source and what Ada32.dll compiled it to.
// testdata/golden holds 93 of its 1,889 forms, chosen to have as many
// different atoms as they can, and testdata/livecapture 44 streams captured
// from a live client; FDO_CORPUS names the directory with all of them
// (atomforge-fdo-java/src/test/resources/golden), and the master list of
// 74,369 single atoms that is there too.
func corpusDir() string {
	if d := os.Getenv("FDO_CORPUS"); d != "" {
		return d
	}
	return "testdata/golden"
}

func corpusPairs(t *testing.T) []string {
	files, _ := filepath.Glob(filepath.Join(corpusDir(), "*.txt"))
	live, _ := filepath.Glob("testdata/livecapture/*.txt")
	files = append(files, live...)
	var out []string
	for _, f := range files {
		base := strings.TrimSuffix(f, ".txt")
		if _, err := os.Stat(base + ".bin"); err == nil {
			out = append(out, base)
		}
	}
	if len(out) == 0 {
		t.Skip("no corpus")
	}
	return out
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// TestCorpusCompile compiles each source and checks it against Ada32's bytes.
func TestCorpusCompile(t *testing.T) {
	bad := 0
	for _, base := range corpusPairs(t) {
		src, _ := os.ReadFile(base + ".txt")
		want, _ := os.ReadFile(base + ".bin")
		got, err := Compile(string(src))
		if err != nil {
			bad++
			if bad <= 20 {
				t.Errorf("%s: %v", filepath.Base(base), err)
			}
			continue
		}
		if !bytes.Equal(got, want) {
			bad++
			if bad <= 20 {
				i := firstDiff(got, want)
				t.Errorf("%s: differs at %d: got % x, want % x", filepath.Base(base), i,
					got[max(0, i-8):min(len(got), i+16)], want[max(0, i-8):min(len(want), i+16)])
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d files differ", bad)
	}
}

// TestCorpusRoundTrip decodes each binary, decompiles it and compiles the
// text again: the bytes must come back the same.
func TestCorpusRoundTrip(t *testing.T) {
	bad := 0
	for _, base := range corpusPairs(t) {
		want, _ := os.ReadFile(base + ".bin")
		src, err := Decompile(want)
		if err == nil {
			var got []byte
			got, err = Compile(src)
			if err == nil && !bytes.Equal(got, want) {
				i := firstDiff(got, want)
				err = fmt.Errorf("differs at %d: got % x, want % x", i,
					got[max(0, i-8):min(len(got), i+16)], want[max(0, i-8):min(len(want), i+16)])
			}
		}
		if err != nil {
			bad++
			if bad <= 20 {
				t.Errorf("%s: %v", filepath.Base(base), err)
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d files don't round-trip", bad)
	}
}

// TestCorpusDecompile counts how many decompiled files read as their
// source does (the text, not just the bytes), indentation aside.
func TestCorpusDecompile(t *testing.T) {
	same, n := 0, 0
	lines, sameLines := 0, 0
	diffs := map[string]int{}
	ex := map[string]string{}
	for _, base := range corpusPairs(t) {
		bin, _ := os.ReadFile(base + ".bin")
		want, _ := os.ReadFile(base + ".txt")
		got, err := Decompile(bin)
		if err != nil {
			continue
		}
		n++
		if trimLines(got) == trimLines(string(want)) {
			same++
		}
		g := strings.Split(strings.TrimRight(got, "\n"), "\n")
		w := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
		if len(g) != len(w) {
			continue
		}
		for i := range w {
			lines++
			g[i], w[i] = strings.TrimSpace(g[i]), strings.TrimSpace(w[i])
			if g[i] == w[i] {
				sameLines++
				continue
			}
			name := strings.Fields(w[i])[0]
			diffs[name]++
			ex[name] = fmt.Sprintf("got %q want %q", strings.TrimSpace(g[i]), strings.TrimSpace(w[i]))
		}
	}
	t.Logf("%d of %d files, %d of %d lines read as their source", same, n, sameLines, lines)
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range diffs {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v })
	for i, e := range kvs {
		if i >= 25 || !testing.Verbose() {
			break
		}
		t.Logf("%6d %s: %s", e.v, e.k, ex[e.k])
	}
}

// TestMasterAtomList compiles AtomForge's list of 74,000 single atoms,
// each as Ada32 compiled it (or refused to).
func TestMasterAtomList(t *testing.T) {
	dir := corpusDir()
	src, err := os.ReadFile(filepath.Join(dir, "master_atom_list.fdo"))
	if err != nil {
		t.Skip("no master_atom_list")
	}
	bin, _ := os.ReadFile(filepath.Join(dir, "master_atom_list.bin"))
	idx, _ := os.Open(filepath.Join(dir, "master_atom_list.idx"))
	defer idx.Close()
	lines := strings.Split(string(src), "\n")
	sc := bufio.NewScanner(idx)
	sc.Buffer(nil, 1<<20)
	bad, n, refused := 0, 0, 0
	perAtom := map[string]int{}
	ex := map[string]string{}
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "#") || l == "" {
			continue
		}
		f := strings.SplitN(l, ",", 3)
		if f[0] == "E" {
			refused++
			continue
		}
		ln, _ := strconv.Atoi(f[0])
		off, _ := strconv.Atoi(f[1])
		size, _ := strconv.Atoi(f[2])
		want := bin[off : off+size]
		line := lines[ln-1]
		n++
		got, err := Compile(line)
		if err == nil && !bytes.Equal(got, want) {
			err = fmt.Errorf("got % x, want % x", got, want)
		}
		if err != nil {
			bad++
			name := strings.Fields(line)[0]
			perAtom[name]++
			if _, ok := ex[name]; !ok {
				ex[name] = fmt.Sprintf("%.120s: %.200v", strings.TrimSpace(line), err)
			}
		}
	}
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range perAtom {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v })
	for i, e := range kvs {
		if i >= 60 {
			break
		}
		t.Errorf("%5d %s", e.v, ex[e.k])
	}
	if bad > 0 {
		t.Errorf("%d of %d atoms differ (%d refused by Ada32, skipped)", bad, n, refused)
	}
	t.Logf("%d atoms compiled (%d refused by Ada32, skipped)", n, refused)
}

// trimLines is s without its indentation, which Ada32's disassembler
// doesn't keep consistently.
func trimLines(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	for i := range l {
		l[i] = strings.TrimSpace(l[i])
	}
	return strings.Join(l, "\n")
}
