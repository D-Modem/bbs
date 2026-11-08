package fdo

import (
	"fmt"
	"strings"
)

// CompileError is an atom the compiler couldn't encode.
type CompileError struct {
	Line int
	Atom string
	Msg  string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("fdo: line %d: %s: %s", e.Line, e.Atom, e.Msg)
}

// Compile compiles FDO91 source to an atom stream, byte for byte as AOL's
// Ada32.dll does.
func Compile(src string) ([]byte, error) {
	frames, err := CompileFrames(src)
	if err != nil {
		return nil, err
	}
	return Encode(frames), nil
}

// MustCompile is Compile for source known when the program is written.
func MustCompile(src string) []byte {
	b, err := Compile(src)
	if err != nil {
		panic(err)
	}
	return b
}

// CompileFrames compiles source to its atoms, not yet encoded.
func CompileFrames(src string) ([]Frame, error) {
	nodes, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return NodesFrames(nodes)
}

// NodesFrames compiles parsed atoms.
func NodesFrames(nodes []*Node) ([]Frame, error) {
	out := make([]Frame, 0, len(nodes))
	for _, n := range nodes {
		f, err := nodeFrame(n)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func nodeFrame(n *Node) (Frame, error) {
	fail := func(format string, a ...any) (Frame, error) {
		return Frame{}, &CompileError{n.Line, n.Name, fmt.Sprintf(format, a...)}
	}
	if n.Name == "the_unknown" {
		args := n.Args
		if len(args) < 2 || args[0].Kind != KindNum || args[1].Kind != KindNum ||
			args[0].Num < 0 || args[0].Num > 127 || args[1].Num < 0 || args[1].Num > 255 {
			return fail("needs <protocol, atom, data...>")
		}
		var d []byte
		for _, v := range args[2:] {
			b, err := rawValue(v)
			if err != nil {
				return fail("%v", err)
			}
			d = append(d, b...)
		}
		return Frame{uint8(args[0].Num), uint8(args[1].Num), nonNil(d)}, nil
	}
	a, ok := Lookup(n.Name)
	if !ok {
		return fail("unrecognized atom")
	}
	d, err := encodeArgs(a, n.Args)
	if err != nil {
		return fail("%v", err)
	}
	if len(d) > 0xFFFF {
		return fail("argument is %d bytes; the most is 65535", len(d))
	}
	return Frame{a.Proto, a.Num, nonNil(d)}, nil
}

func nonNil(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// rawValue is a the_unknown byte: hex, a number (one byte) or a string.
func rawValue(v Value) ([]byte, error) {
	switch v.Kind {
	case KindHex, KindString:
		return v.Bytes, nil
	case KindNum:
		return []byte{byte(v.Num)}, nil
	}
	return nil, fmt.Errorf("the_unknown data must be hex, numbers or strings")
}

// number is n big-endian in as few bytes as it needs (at least one).
func number(n int64) []byte {
	u := uint32(n)
	if n < 0 {
		u = uint32(int32(n))
	}
	switch {
	case u <= 0xFF:
		return []byte{byte(u)}
	case u <= 0xFFFF:
		return []byte{byte(u >> 8), byte(u)}
	case u <= 0xFFFFFF:
		return []byte{byte(u >> 16), byte(u >> 8), byte(u)}
	}
	return []byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
}

// fixed is n big-endian in w bytes.
func fixed(n int64, w int) []byte {
	b := make([]byte, w)
	for i := w - 1; i >= 0; i-- {
		b[i] = byte(n)
		n >>= 8
	}
	return b
}

// gidNumber is a global id as a number: a-b is a<<16|b, a-b-c a<<24|b<<16|c.
func gidNumber(g []int64) int64 {
	if len(g) == 2 {
		return g[0]<<16 | g[1]&0xFFFF
	}
	return g[0]<<24 | (g[1]&0xFF)<<16 | g[2]&0xFFFF
}

// encodeArgs encodes an atom's argument.
func encodeArgs(a *Atom, args []Value) ([]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	var out []byte
	for i, v := range args {
		b, err := encodeValue(a, i, v)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

// encodeValue encodes the i'th value of an atom's argument.
func encodeValue(a *Atom, i int, v Value) ([]byte, error) {
	switch v.Kind {
	case KindHex, KindString:
		return v.Bytes, nil
	case KindNum:
		if w := numWidth(a, i); w > 0 {
			return fixed(v.Num, w), nil
		}
		n := v.Num
		if n > 0x7FFFFFFF {
			n = 0x7FFFFFFF
		}
		return number(n), nil
	case KindGID:
		if len(v.GID) == 2 && v.GID[0] == 0 && isTokenType(a) {
			return fixed(v.GID[1], 2), nil
		}
		return number(gidNumber(v.GID)), nil
	case KindWord:
		return encodeWord(a, i, v.Text)
	case KindPipe:
		var or uint64
		width := 0
		for _, p := range v.Parts {
			var b []byte
			var err error
			switch p.Kind {
			case KindWord:
				b, err = encodeWord(a, i, p.Text)
			case KindNum, KindHex:
				b, err = encodeValue(a, i, p)
			default:
				err = fmt.Errorf("only words and numbers can be or'd")
			}
			if err != nil {
				return nil, err
			}
			var n uint64
			for _, c := range b {
				n = n<<8 | uint64(c)
			}
			or |= n
			width = max(width, len(b))
		}
		if w := pipeWidth(a); w > 0 {
			width = w
		}
		return fixed(int64(or), width), nil
	case KindStream:
		frames, err := NodesFrames(v.Stream)
		if err != nil {
			return nil, err
		}
		return Encode(frames), nil
	}
	return nil, fmt.Errorf("bad value")
}

func isTokenType(a *Atom) bool { return a.Type == TypeToken }

// encodeWord encodes a word: one of the atom's named values, yes or no, a
// variable letter, or an atom's name.
func encodeWord(a *Atom, i int, w string) ([]byte, error) {
	lw := strings.ToLower(w)
	if b, ok := enumValue(a, i, lw); ok {
		return b, nil
	}
	switch lw {
	case "yes", "true":
		return []byte{1}, nil
	case "no", "false":
		return []byte{0}, nil
	}
	if len(w) == 1 && w[0] >= 'A' && w[0] <= 'Z' {
		return []byte{w[0] - 'A'}, nil
	}
	if r, ok := Lookup(lw); ok {
		return []byte{r.Proto, r.Num}, nil
	}
	return nil, fmt.Errorf("unknown word %q", w)
}
