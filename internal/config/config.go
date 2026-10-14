// Package config reads the BBS configuration: an INI file in the style of
// modemmenu.conf.
//
//	[general]
//	name = D-Modem BBS
//
//	[listen telnet]
//	type = telnet
//	address = :2300
//
// Comments (; or #) go on lines of their own.  Values may be "quoted" to
// keep spaces at their ends; in quoted values \r \n \t \e \\ \" and \xHH
// are those bytes.
package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Section is one [section] of the file.
type Section struct {
	Kind string // "general", "listen", ...
	Name string // the second word of the header, if any
	Line int
	keys map[string]string
	used map[string]bool
	ord  []string
}

// File is a parsed configuration file.
type File struct {
	Path     string
	Sections []*Section
}

// Load reads and parses the file at path.
func Load(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cf, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cf.Path = path
	return cf, nil
}

// Parse parses a configuration from r.
func Parse(r io.Reader) (*File, error) {
	cf := &File{}
	var cur *Section
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: bad section header", n)
			}
			words := strings.Fields(line[1 : len(line)-1])
			if len(words) == 0 || len(words) > 2 {
				return nil, fmt.Errorf("line %d: bad section header", n)
			}
			cur = &Section{Kind: strings.ToLower(words[0]), Line: n,
				keys: map[string]string{}, used: map[string]bool{}}
			if len(words) == 2 {
				cur.Name = words[1]
			}
			cf.Sections = append(cf.Sections, cur)
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("line %d: a value outside of any section", n)
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: no = in %q", n, line)
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v, err := unquote(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		if _, dup := cur.keys[k]; dup {
			return nil, fmt.Errorf("line %d: %s given twice", n, k)
		}
		cur.keys[k] = v
		cur.ord = append(cur.ord, k)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return cf, nil
}

func unquote(v string) (string, error) {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return v, nil
	}
	v = v[1 : len(v)-1]
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\\' || i+1 == len(v) {
			b.WriteByte(c)
			continue
		}
		i++
		switch v[i] {
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		case '\\', '"':
			b.WriteByte(v[i])
		case 'x':
			if i+2 >= len(v) {
				return "", fmt.Errorf("short \\x escape")
			}
			x, err := strconv.ParseUint(v[i+1:i+3], 16, 8)
			if err != nil {
				return "", fmt.Errorf("bad \\x escape")
			}
			b.WriteByte(byte(x))
			i += 2
		default:
			b.WriteByte('\\')
			b.WriteByte(v[i])
		}
	}
	return b.String(), nil
}

// Get finds the first section of kind (and name, if not empty).
func (cf *File) Get(kind, name string) *Section {
	for _, s := range cf.Sections {
		if s.Kind == kind && (name == "" || s.Name == name) {
			return s
		}
	}
	return nil
}

// All returns the sections of kind, in file order.
func (cf *File) All(kind string) []*Section {
	var out []*Section
	for _, s := range cf.Sections {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// Unused lists the keys nothing asked for: misspelt options.
func (cf *File) Unused() []string {
	var out []string
	for _, s := range cf.Sections {
		for _, k := range s.ord {
			if !s.used[k] {
				where := "[" + s.Kind
				if s.Name != "" {
					where += " " + s.Name
				}
				out = append(out, fmt.Sprintf("%s] %s (section at line %d)", where, k, s.Line))
			}
		}
	}
	return out
}

// Has reports whether key is set.
func (s *Section) Has(key string) bool {
	if s == nil {
		return false
	}
	_, ok := s.keys[key]
	return ok
}

// String returns key's value, or def.
func (s *Section) String(key, def string) string {
	if s == nil {
		return def
	}
	v, ok := s.keys[key]
	s.used[key] = true
	if !ok {
		return def
	}
	return v
}

// Bool returns key as yes/no, true/false, on/off or 1/0.
func (s *Section) Bool(key string, def bool) (bool, error) {
	v := strings.ToLower(s.String(key, ""))
	switch v {
	case "":
		return def, nil
	case "yes", "true", "on", "1":
		return true, nil
	case "no", "false", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("%s: %q is not yes or no", key, v)
}

// Int returns key as an integer in [min, max].
func (s *Section) Int(key string, def, min, max int) (int, error) {
	v := s.String(key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 0, 64)
	if err != nil || n < int64(min) || n > int64(max) {
		return 0, fmt.Errorf("%s: %q is not a number from %d to %d", key, v, min, max)
	}
	return int(n), nil
}

// Seconds returns key, a number of seconds, as a duration.
func (s *Section) Seconds(key string, def, min, max int) (time.Duration, error) {
	n, err := s.Int(key, def, min, max)
	return time.Duration(n) * time.Second, err
}

// Keys lists the section's keys in the order they came.
func (s *Section) Keys() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.ord...)
}

// Unquote is a value as the file has it read: "quoted", with escapes, or
// as it is.
func Unquote(v string) (string, error) { return unquote(v) }
