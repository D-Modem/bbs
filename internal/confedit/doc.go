// Package confedit edits bbs.conf as a sysop would by hand: a key's line
// changed where it is, a new one put by the sample's commented example of
// it, and every comment kept.  Its Catalog is what each key does, read
// from bbs.conf.sample, and whether it takes effect while the BBS runs.
package confedit

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/D-Modem/bbs/internal/config"
)

// Doc is a configuration file, line by line.
type Doc struct {
	lines []string
}

// Section names a [KIND NAME] section ("" Name: none).
type Section struct {
	Kind, Name string
}

// String is the section's header: [KIND] or [KIND NAME].
func (s Section) String() string {
	if s.Name == "" {
		return "[" + s.Kind + "]"
	}
	return "[" + s.Kind + " " + s.Name + "]"
}

// Parse reads a file, which must be one the BBS can parse.
func Parse(data []byte) (*Doc, error) {
	if _, err := config.Parse(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	d := &Doc{}
	if s != "" {
		d.lines = strings.Split(s, "\n")
	}
	return d, nil
}

// Bytes is the file as it now is.
func (d *Doc) Bytes() []byte {
	if len(d.lines) == 0 {
		return nil
	}
	return []byte(strings.Join(d.lines, "\n") + "\n")
}

var (
	headerRE    = regexp.MustCompile(`^\[\s*([A-Za-z0-9_-]+)(?:\s+(\S+))?\s*\]$`)
	commentedRE = regexp.MustCompile(`^[;#]\[\s*([A-Za-z0-9_-]+)(?:\s+(\S+))?\s*\]$`)
	exampleRE   = regexp.MustCompile(`^[;#]([A-Za-z0-9_]+)\s*=`)
)

// header is the section a line opens, if it does.
func header(line string) (Section, bool) {
	m := headerRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Section{}, false
	}
	return Section{strings.ToLower(m[1]), m[2]}, true
}

// commentedHeader is a section's header commented out (;[http]).
func commentedHeader(line string) (Section, bool) {
	m := commentedRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Section{}, false
	}
	return Section{strings.ToLower(m[1]), m[2]}, true
}

// keyLine is the key and raw value a line sets, if it does.
func keyLine(line string) (key, raw string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == ';' || t[0] == '#' || t[0] == '[' {
		return "", "", false
	}
	k, v, ok := strings.Cut(t, "=")
	if !ok {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v), true
}

// example is the key a commented example line (;key = value) sets.
func example(line string) (string, bool) {
	m := exampleRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// Sections are the file's sections, in order.
func (d *Doc) Sections() []Section {
	var out []Section
	for _, l := range d.lines {
		if s, ok := header(l); ok {
			out = append(out, s)
		}
	}
	return out
}

// Has reports whether the section is there.
func (d *Doc) Has(s Section) bool {
	_, _, ok := d.span(s)
	return ok
}

// span is the section's lines: its header at start, and up to end (the
// next section's header, or the file's end).
func (d *Doc) span(s Section) (start, end int, ok bool) {
	start = -1
	for i, l := range d.lines {
		h, isH := header(l)
		if !isH {
			continue
		}
		if start >= 0 {
			return start, i, true
		}
		if h == s {
			start = i
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, len(d.lines), true
}

// own is how far a section's own lines go: up to the next header, or to
// a commented-out section's header, or the comments before it.
func (d *Doc) own(start, end int) int {
	last := start
	for i := start + 1; i < end; i++ {
		l := d.lines[i]
		if _, ok := commentedHeader(l); ok {
			break
		}
		if _, _, ok := keyLine(l); ok {
			last = i
		} else if _, ok := example(l); ok {
			last = i
		}
	}
	return last + 1
}

// Keys are the keys the section sets, in order.
func (d *Doc) Keys(s Section) []string {
	start, end, ok := d.span(s)
	if !ok {
		return nil
	}
	var out []string
	for _, l := range d.lines[start+1 : end] {
		if k, _, ok := keyLine(l); ok {
			out = append(out, k)
		}
	}
	return out
}

// Get is a key's value, unquoted.
func (d *Doc) Get(s Section, key string) (string, bool) {
	i := d.find(s, key)
	if i < 0 {
		return "", false
	}
	_, raw, _ := keyLine(d.lines[i])
	v, err := config.Unquote(raw)
	if err != nil {
		return raw, true
	}
	return v, true
}

// find is the line of key in section s, or -1 if either is not there.
func (d *Doc) find(s Section, key string) int {
	start, end, ok := d.span(s)
	if !ok {
		return -1
	}
	for i := start + 1; i < end; i++ {
		if k, _, ok := keyLine(d.lines[i]); ok && k == key {
			return i
		}
	}
	return -1
}

// Set sets a key, making its section if it is not there.  A new key goes
// below the commented example of it, if the section has one; else below
// the section's last key.
func (d *Doc) Set(s Section, key, value string) error {
	key = strings.ToLower(strings.TrimSpace(key))
	if !validKey(key) {
		return fmt.Errorf("%q: a key is letters, digits and _", key)
	}
	if strings.ContainsAny(value, "\x00") {
		return fmt.Errorf("%s: no NUL in a value", key)
	}
	line := key + " = " + Quote(value)
	if i := d.find(s, key); i >= 0 {
		d.lines[i] = line
		return nil
	}
	if !d.Has(s) {
		if err := d.AddSection(s); err != nil {
			return err
		}
	}
	start, end, _ := d.span(s)
	own := d.own(start, end)
	at := own
	for i := start + 1; i < own; i++ {
		if k, ok := example(d.lines[i]); ok && k == key {
			at = i + 1
			// below the example's following examples of other values
			for at < own {
				if k2, ok := example(d.lines[at]); ok && k2 == key {
					at++
					continue
				}
				break
			}
			break
		}
	}
	d.insert(at, line)
	return nil
}

// validKey reports whether k is a key's name: lower-case letters, digits and
// _, at least one.
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// insert puts lines in before line at.
func (d *Doc) insert(at int, lines ...string) {
	d.lines = append(d.lines[:at], append(lines, d.lines[at:]...)...)
}

// Unset takes a key out (its default then holds); its line is kept as a
// commented example, as in the sample.
func (d *Doc) Unset(s Section, key string) bool {
	i := d.find(s, key)
	if i < 0 {
		return false
	}
	d.lines[i] = ";" + strings.TrimSpace(d.lines[i])
	return true
}

// AddSection makes a section: where the file has it commented out, it is
// uncommented there (its comments and examples kept below it); else it
// goes at the end.
func (d *Doc) AddSection(s Section) error {
	if !validKey(s.Kind) || strings.ContainsAny(s.Name, " \t[];#=") {
		return fmt.Errorf("%s: not a section's name", s)
	}
	if d.Has(s) {
		return fmt.Errorf("%s is there already", s)
	}
	for i, l := range d.lines {
		if h, ok := commentedHeader(l); ok && h == s {
			d.lines[i] = s.String()
			return nil
		}
	}
	if n := len(d.lines); n > 0 && strings.TrimSpace(d.lines[n-1]) != "" {
		d.lines = append(d.lines, "")
	}
	d.lines = append(d.lines, s.String())
	return nil
}

// RemoveSection comments a section out, header and keys, so that it can
// be had back.
func (d *Doc) RemoveSection(s Section) bool {
	start, end, ok := d.span(s)
	if !ok {
		return false
	}
	d.lines[start] = ";" + s.String()
	for i := start + 1; i < end; i++ {
		if _, _, ok := keyLine(d.lines[i]); ok {
			d.lines[i] = ";" + strings.TrimSpace(d.lines[i])
		}
	}
	return true
}

// Rename gives a named section another name.
func (d *Doc) Rename(s Section, name string) error {
	to := Section{s.Kind, name}
	if name == "" || strings.ContainsAny(name, " \t[];#=") {
		return fmt.Errorf("%q: not a section's name", name)
	}
	if d.Has(to) {
		return fmt.Errorf("%s is there already", to)
	}
	start, _, ok := d.span(s)
	if !ok {
		return fmt.Errorf("no %s", s)
	}
	d.lines[start] = to.String()
	return nil
}

// Quote is a value as the file takes it: quoted only where it must be
// (spaces at its ends, quotes around it, control characters).
func Quote(v string) string {
	need := v != strings.TrimSpace(v) ||
		len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"'
	for i := 0; i < len(v) && !need; i++ {
		need = v[i] < 0x20 || v[i] == 0x7f
	}
	if !need {
		return v
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case 0x1b:
			b.WriteString(`\e`)
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Diff is what changed between two files: sections and keys added, taken
// out or changed, section by section.
func Diff(old, new *Doc) []Change {
	var out []Change
	seen := map[Section]bool{}
	secs := append(old.Sections(), new.Sections()...)
	for _, s := range secs {
		if seen[s] {
			continue
		}
		seen[s] = true
		keys := map[string]bool{}
		var order []string
		for _, k := range append(old.Keys(s), new.Keys(s)...) {
			if !keys[k] {
				keys[k] = true
				order = append(order, k)
			}
		}
		sort.Strings(order)
		if !old.Has(s) && new.Has(s) {
			out = append(out, Change{Section: s, Op: Added})
		}
		if old.Has(s) && !new.Has(s) {
			out = append(out, Change{Section: s, Op: Removed})
		}
		for _, k := range order {
			a, inA := old.Get(s, k)
			b, inB := new.Get(s, k)
			switch {
			case inA && !inB:
				out = append(out, Change{Section: s, Key: k, Op: Removed, Old: a})
			case !inA && inB:
				out = append(out, Change{Section: s, Key: k, Op: Added, New: b})
			case a != b:
				out = append(out, Change{Section: s, Key: k, Op: Changed, Old: a, New: b})
			}
		}
	}
	return out
}

// Op is what a Change did.
type Op int

const (
	Added Op = iota
	Removed
	Changed
)

// Change is a section or key added, taken out or changed (Key "": the
// section itself).
type Change struct {
	Section  Section
	Key      string
	Op       Op
	Old, New string
}

// String is the change as the sysop reads it, secrets' values hidden.
func (c Change) String() string {
	if Secret(c.Key) {
		if c.Old != "" {
			c.Old = "********"
		}
		if c.New != "" {
			c.New = "********"
		}
	}
	if c.Key == "" {
		return map[Op]string{Added: "+", Removed: "-"}[c.Op] + c.Section.String()
	}
	switch c.Op {
	case Added:
		return fmt.Sprintf("+%s %s = %s", c.Section, c.Key, Quote(c.New))
	case Removed:
		return fmt.Sprintf("-%s %s", c.Section, c.Key)
	}
	return fmt.Sprintf("~%s %s = %s (was %s)", c.Section, c.Key, Quote(c.New), Quote(c.Old))
}

// Secret reports whether a key's value is a secret, not to be shown in
// lists and logs.
func Secret(key string) bool {
	return strings.Contains(key, "password") || key == "authcode" || key == "syscode" || key == "schemecode"
}
