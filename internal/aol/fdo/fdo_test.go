package fdo

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"testing"
)

// The sign-on form as an AOL 3.0 client sends it (token Dd, stream 0x15),
// from "The AOL Protocol" captures: screen name and password as de_data,
// numbers in four bytes where Ada32 would write one.
var signOn = []byte{
	0x00, 0x01, 0x00,
	0x01, 0x0A, 0x04, 0, 0, 0, 1,
	0x01, 0x0B, 0x04, 0, 0, 0, 1,
	0x03, 0x01, 0x0A, 'h', 'o', 'l', 'y', 't', 'a', 'b', 'l', 'e', ' ',
	0x01, 0x1D, 0x00, 0x01, 0x1D, 0x00,
	0x01, 0x0A, 0x04, 0, 0, 0, 2,
	0x03, 0x01, 0x08, '*', '*', '*', '*', '*', '*', '*', '*',
	0x01, 0x1D, 0x00,
	0x00, 0x02, 0x00,
}

func TestSignOn(t *testing.T) {
	frames, err := Decode(signOn)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range frames {
		names = append(names, f.Name())
	}
	want := "uni_start_stream man_set_context_relative man_set_context_index de_data " +
		"man_end_context man_end_context man_set_context_relative de_data man_end_context uni_end_stream"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("atoms\n got %s\nwant %s", got, want)
	}
	de := FindAll(frames, "de_data")
	if len(de) != 2 || string(de[0].Data) != "holytable " || string(de[1].Data) != "********" {
		t.Errorf("de_data: %v", de)
	}
	if !bytes.Equal(Encode(frames), signOn) {
		t.Error("Encode doesn't give the packet back")
	}
	src, err := Decompile(signOn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "man_set_context_relative <00x, 00x, 00x, 01x>") ||
		!strings.Contains(src, `de_data <"holytable ">`) {
		t.Errorf("decompiled:\n%s", src)
	}
	if b, err := Compile(src); err != nil || !bytes.Equal(b, signOn) {
		t.Errorf("recompiled: % x, %v", b, err)
	}
}

// The sign-off confirmation from the FDO91 manual's introduction.
const signOff = `
atom$uni_start_stream
atom$man_start_object <ind_group, "Exit Gateway">
atom$mat_orientation <vff>
atom$mat_position <5>
atom$man_start_object <ornament, "Are you sure you want to sign off?">
atom$man_start_sibling <org_group>
atom$mat_orientation <hef>
atom$man_start_object <trigger, "Yes">
atom$mat_bool_default <yes>
atom$man_start_sibling <trigger, "No">
atom$man_end_object
atom$man_update_display
atom$uni_wait_off
atom$uni_end_stream
`

func TestManualExample(t *testing.T) {
	b, err := Compile(signOff)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x01, 0x00, 0x01, 0x00, 0x0D, 0x01}
	want = append(want, "Exit Gateway"...)
	want = append(want, 0x10, 0x08, 0x01, 0x5B, 0x10, 0x40, 0x01, 0x05)
	if !bytes.HasPrefix(b, want) {
		t.Errorf("got % x\nwant prefix % x", b, want)
	}
	src, err := Decompile(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`man_start_object <ind_group, "Exit Gateway">`, "mat_orientation <vff>",
		"mat_position <center_center>", `man_start_sibling <org_group, "">`, "mat_bool_default <yes>"} {
		if !strings.Contains(src, s) {
			t.Errorf("decompiled source lacks %s:\n%s", s, src)
		}
	}
	if b2, err := Compile(src); err != nil || !bytes.Equal(b, b2) {
		t.Errorf("round trip: %v", err)
	}
}

func TestNestedStream(t *testing.T) {
	src := `uni_start_stream
  man_start_object <trigger, "Go">
    act_replace_select_action
        <
        uni_start_stream
        sm_send_token_raw <"pE">
        uni_end_stream
        >
  man_end_object
uni_end_stream
`
	b, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	inner := []byte{0x00, 0x01, 0x00, 0x0E, 0x10, 0x02, 'p', 'E', 0x00, 0x02, 0x00}
	if !bytes.Contains(b, append([]byte{0x02, 0x04, byte(len(inner))}, inner...)) {
		t.Errorf("nested stream not found in % x", b)
	}
	got, err := Decompile(b)
	if err != nil || got != src {
		t.Errorf("decompiled:\n%s\nwant:\n%s (%v)", got, src, err)
	}
}

func TestValues(t *testing.T) {
	for _, c := range []struct {
		src  string
		want []byte
	}{
		{"mat_object_id <32-105>", []byte{0x10, 0x0C, 3, 0x20, 0x00, 0x69}},
		{"mat_art_id <1-0-1329>", []byte{0x10, 0x0E, 4, 1, 0, 0x05, 0x31}},
		{"uni_sync_skip <0>", []byte{0x00, 0x07, 1, 0}},
		{"uni_sync_skip <300>", []byte{0x00, 0x07, 2, 0x01, 0x2C}},
		{"uni_sync_skip <4000000000>", []byte{0x00, 0x07, 4, 0x7F, 0xFF, 0xFF, 0xFF}},
		{"mat_size <25, 1, 32>", []byte{0x10, 0x17, 4, 25, 1, 0, 32}},
		{"mat_title_pos <above | center>", []byte{0x10, 0x11, 1, 0x40}},
		{"mat_font_sis <arial, 10, bold | italic>", []byte{0x10, 0x0A, 3, 0, 10, 3}},
		{"var_string_set <A,\"x\\r\\n\\x80\">", []byte{0x0C, 0x05, 5, 0, 'x', '\r', '\n', 0x80}},
		{"buf_set_data_atom <de_data>", []byte{0x04, 0x12, 2, 0x03, 0x01}},
		{"async_alert <info, \"Done.\">", []byte{0x0D, 0x07, 6, 1, 'D', 'o', 'n', 'e', '.'}},
		{"buf_start_buffer <token_header | host_bound>", []byte{0x04, 0x00, 4, 0, 0, 0, 5}},
		{"the_unknown <99, 7, 01x, 02x>", []byte{0xE0 | 0x60>>2, 0x03, 7, 2, 1, 2}},
		{"www_load_url <\"x\">", nil},
	} {
		got, err := Compile(c.src)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if c.want != nil && !bytes.Equal(got, c.want) {
			t.Errorf("%s: got % x, want % x", c.src, got, c.want)
		}
		back, err := Decompile(got)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if again, err := Compile(back); err != nil || !bytes.Equal(again, got) {
			t.Errorf("%s: decompiled as %q, which compiles to % x (%v)", c.src, back, again, err)
		}
	}
}

func TestErrors(t *testing.T) {
	for _, c := range []struct{ src, msg string }{
		{"uni_start_stream\nnot_an_atom <1>", "line 2: not_an_atom: unrecognized atom"},
		{`man_append_data <"no end>`, "line 1: string not closed"},
		{"act_replace_action\n<\nuni_start_stream\n", "nested stream not closed"},
		{"mat_position <sideways>", `unknown word "sideways"`},
		{"uni_start_stream <1, >", "expected a value"},
		{"the_unknown <1>", "needs <protocol, atom, data...>"},
	} {
		_, err := Compile(c.src)
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%q: got %v, want %q", c.src, err, c.msg)
		}
	}
	var ce *CompileError
	if _, err := Compile("uni_start_stream\n\nbogus"); !errors.As(err, &ce) || ce.Line != 3 {
		t.Errorf("CompileError line: %v", err)
	}
}

// TestStyles decodes each of the eight styles.
func TestStyles(t *testing.T) {
	b := []byte{
		0x10, 0x08, 0x01, 0x5B, // full: mat_orientation <vff>
		0x30 | 0x00, 0x08, // atom: mat 8, no data (protocol carried over)
		0x80 | 0x0C, 0x02, 0xAA, 0xBB, // current: mat 12, two bytes
		0xA0 | 0x07,              // zero: mat 7 <0>
		0xC0 | 0x07,              // one: mat 7 <1>
		0x40 | 0x0D, 0x60 | 0x07, // data: async 7, value 3
		0x20 | 0x0D, 0x40 | 0x07, 'h', 'i', // length: async 7, two bytes
		0xE0 | 0x08, 0x10, 0x05, 0x01, 'x', // prefix: www (48) atom 5, full
		0xE0 | 0x08 | 0x02, 0xA0 | 0x10, 0x01, // prefix, zero: proto 48, atom 0x41
	}
	frames, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Frame{
		{16, 8, []byte{0x5B}}, {16, 8, []byte{}}, {16, 12, []byte{0xAA, 0xBB}},
		{16, 7, []byte{0}}, {16, 7, []byte{1}}, {13, 7, []byte{3}},
		{13, 7, []byte("hi")}, {48, 5, []byte("x")}, {48, 0x41, []byte{0}},
	}
	if len(frames) != len(want) {
		t.Fatalf("got %d frames: %v", len(frames), frames)
	}
	for i := range want {
		if frames[i].Proto != want[i].Proto || frames[i].Num != want[i].Num || !bytes.Equal(frames[i].Data, want[i].Data) {
			t.Errorf("frame %d: got %v, want %v", i, frames[i], want[i])
		}
	}
	if _, err := Decode([]byte{0x10, 0x08, 0x05, 1}); !errors.Is(err, ErrShort) {
		t.Errorf("short atom: %v", err)
	}
}

func randomFrames(r *rand.Rand, n int) []Frame {
	var out []Frame
	for i := 0; i < n; i++ {
		f := Frame{Proto: uint8(r.Intn(128)), Num: uint8(r.Intn(256))}
		if r.Intn(3) == 0 {
			f.Proto = uint8(r.Intn(3)) // runs of one protocol, for the compact styles
		}
		if f.Proto == ProtoUNI && f.Num >= atomStartLarge && f.Num <= atomEndLarge {
			f.Num = 1 // not the large-atom atoms
		}
		switch r.Intn(5) {
		case 0:
			f.Data = []byte{}
		case 1:
			f.Data = []byte{byte(r.Intn(9))}
		case 2:
			f.Data = make([]byte, 1+r.Intn(200))
		default:
			f.Data = make([]byte, r.Intn(8))
		}
		r.Read(f.Data)
		out = append(out, f)
	}
	return out
}

func sameFrames(a, b []Frame) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Proto != b[i].Proto || a[i].Num != b[i].Num || !bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}

func TestEncodeDecode(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		frames := randomFrames(r, 30)
		for name, enc := range map[string]func([]Frame) []byte{"full": Encode, "compact": EncodeCompact} {
			got, err := Decode(enc(frames))
			if err != nil || !sameFrames(got, frames) {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if c, f := len(EncodeCompact(frames)), len(Encode(frames)); c > f {
			t.Errorf("compact %d bytes, full %d", c, f)
		}
	}
}

func TestLargeAtoms(t *testing.T) {
	big := make([]byte, 1000)
	for i := range big {
		big[i] = byte(i)
	}
	frames := []Frame{F("uni_start_stream", 0), F("man_append_data", big...), F("man_update_display"), F("uni_end_stream", 0)}
	packets := Packets(frames, 115)
	var d Decoder
	var got []Frame
	for _, p := range packets {
		if len(p) > 115 {
			t.Errorf("packet of %d bytes", len(p))
		}
		f, err := d.Feed(p)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, f...)
	}
	if !sameFrames(got, frames) {
		t.Errorf("got %v", got)
	}
	// the start says which atom and how long
	if p := packets[1]; !bytes.Equal(p, []byte{0x00, 0x04, 0x04, 0x01, 0x14, 0x03, 0xE8}) {
		t.Errorf("start of the large atom: % x", p)
	}
	// and an atom over MaxData goes as one in Encode too
	huge := Frame{1, 4, make([]byte, MaxData+10)}
	if got, err := Decode(Encode([]Frame{huge})); err != nil || !sameFrames(got, []Frame{huge}) {
		t.Errorf("huge atom: %v", err)
	}
	if _, err := Decode([]byte{0x00, 0x05, 0x01, 'x'}); !errors.Is(err, ErrLargeAtom) {
		t.Errorf("segment without a start: %v", err)
	}
}

func TestOrientation(t *testing.T) {
	n := 0
	for _, d := range "hv" {
		for _, x := range orientAcross {
			for _, y := range orientAlong {
				w := string([]rune{d, x, y})
				v, ok := orientValue(w)
				if !ok {
					t.Fatalf("%s: no value", w)
				}
				if back, _ := orientName(v); back != w {
					t.Errorf("%s: %#x reads %s", w, v, back)
				}
				n++
			}
		}
	}
	if v, _ := orientValue("vff"); v != 0x5B {
		t.Errorf("vff is %#x", v)
	}
	if v, _ := orientValue("hec"); v != 0x20 {
		t.Errorf("hec is %#x", v)
	}
}

func TestLatin1(t *testing.T) {
	s := "Café “quoted” – 50€ 日本"
	b := Latin1(s)
	want := []byte("Caf\xe9 \x93quoted\x94 \x96 50\x80 ??")
	if !bytes.Equal(b, want) {
		t.Errorf("Latin1: % x", b)
	}
	if got := FromLatin1(b); got != "Café “quoted” – 50€ ??" {
		t.Errorf("FromLatin1: %q", got)
	}
	src := "man_append_data <" + Quote(`say "hi" \ `+s) + ">"
	got, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := Decode(got); !bytes.Equal(f[0].Data, append([]byte(`say "hi" \ `), want...)) {
		t.Errorf("quoted: % x", f[0].Data)
	}
}

func TestTable(t *testing.T) {
	if n := len(Atoms()); n < 1600 {
		t.Errorf("%d atoms", n)
	}
	seen := map[uint16]string{}
	for _, a := range Atoms() {
		k := uint16(a.Proto)<<8 | uint16(a.Num)
		if s, dup := seen[k]; dup {
			t.Errorf("%s and %s are both %d:%d", s, a.Name, a.Proto, a.Num)
		}
		seen[k] = a.Name
		if p := ProtocolName(a.Proto); p == "" || !strings.HasPrefix(a.Name, p+"_") {
			t.Errorf("%s is protocol %d (%q)", a.Name, a.Proto, p)
		}
	}
	if a, ok := Lookup("ATOM$MAN_START_OBJECT"); !ok || a.Proto != ProtoMAN || a.Num != 0 {
		t.Errorf("Lookup: %v", a)
	}
}
