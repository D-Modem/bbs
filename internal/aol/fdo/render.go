package fdo

import (
	"bytes"
	"sort"
)

// candidates are the forms the decompiler tries for an atom's data, best
// first; the first that compiles back to the data wins, and hex is the
// last resort.
func candidates(a *Atom, d []byte) [][]Value {
	var out [][]Value
	add := func(v ...Value) { out = append(out, v) }
	word := func(s string) Value { return Value{Kind: KindWord, Text: s} }
	num := func(n int64) Value { return Value{Kind: KindNum, Num: n} }
	str := func(b []byte) Value { return Value{Kind: KindString, Bytes: b} }

	if a.Type != TypeObject && a.Type != TypeAlert && a.Name != "mat_title_pos" && a.Name != "mat_text_on_picture_pos" {
		if v, ok := enumRender(a, 0, d); ok {
			add(v)
		}
	}
	switch a.Name {
	case "man_get_display_characteristics":
		if len(d) >= 2 {
			if v, ok := enumRender(a, 0, d[:1]); ok {
				add(v, num(beNumber(d[1:])))
			}
		}
	case "mat_auto_complete":
		var vals []Value
		for _, c := range d {
			vals = append(vals, word(map[byte]string{0: "web_list", 1: "address_list", 2: "other_list"}[c]))
		}
		if len(d) == 2 && d[1] == 1 {
			vals[1] = word("std_sort_search")
		}
		add(vals...)
	case "mat_title_pos":
		if len(d) == 1 && d[0]&3 != 3 {
			// always where and how it's justified: <below | center>
			p := []Value{
				word([]string{"left", "above", "right", "below"}[d[0]>>6]),
				word([]string{"center", "top_or_left", "bottom_or_right"}[d[0]&3])}
			if r := d[0] & 0x3C; r != 0 {
				p = append(p, num(int64(r)))
			}
			add(Value{Kind: KindPipe, Parts: p})
		}
	case "mat_text_on_picture_pos":
		if len(d) == 1 {
			art, title := map[byte]string{0: "art_middle_center", 0x40: "art_middle_left", 0x50: "art_middle_right"}[d[0]&0xF0],
				map[byte]string{0: "title_middle_center", 2: "title_upper_center", 4: "title_middle_left",
					5: "title_middle_right", 7: "title_lower_center"}[d[0]&0x0F]
			if art != "" && title != "" {
				add(Value{Kind: KindPipe, Parts: []Value{word(art), word(title)}})
			}
		}
	case "mat_size":
		switch len(d) {
		case 2:
			add(num(int64(d[0])), num(int64(d[1])))
		case 4:
			add(num(int64(d[0])), num(int64(d[1])), num(beNumber(d[2:])))
		}
	case "hfs_attr_field_mapping", "hfs_attr_variable_mapping", "hfs_attr_dispatch_mapping":
		if len(d) == 8 {
			add(num(beNumber(d[:4])), num(beNumber(d[4:])))
		}
	case "hfs_attr_checkbox_mapping":
		if len(d) >= 4 {
			add(num(beNumber(d[:4])), str(d[4:]))
		}
	}
	if es := atomEnums[a.Name]; len(es) > 1 && len(d) <= len(es) {
		// one byte a place: mat_font_sis <arial, 10, bold>
		var vals []Value
		for i := range d {
			if v, ok := enumRender(a, i, d[i:i+1]); ok {
				vals = append(vals, v)
			} else {
				vals = append(vals, num(int64(d[i])))
			}
		}
		add(vals...)
	}
	switch a.Type {
	case TypeStream:
		if frames, err := Decode(d); err == nil && bytes.Equal(Encode(frames), d) {
			add(Value{Kind: KindStream, streamFrames: frames})
		}
	case TypeDword, TypeCriterion:
		add(num(beNumber(d)))
	case TypeString:
		if textual(d) {
			add(str(d))
		}
	case TypeBool:
		if len(d) == 1 && d[0] <= 1 {
			add(word(map[byte]string{0: "no", 1: "yes"}[d[0]]))
		}
	case TypeGID:
		if g := gidValue(d); g != nil {
			add(*g)
		}
		add(num(beNumber(d)))
	case TypeAtom, TypeList:
		if len(d) == 2 {
			if r, ok := LookupNum(d[0], d[1]); ok {
				add(word(r.Name))
			}
		}
		if a.Type == TypeList && len(d) > 1 && textual(d[1:]) {
			add(num(int64(d[0])), str(d[1:]))
		}
		if len(d) > 2 {
			if r, ok := LookupNum(d[0], d[1]); ok {
				add(word(r.Name), Value{Kind: KindHex, Bytes: d[2:]})
			}
		}
	case TypeObject, TypeAlert:
		if len(d) >= 1 {
			if v, ok := enumRender(a, 0, d[:1]); ok && v.Kind == KindWord {
				add(v, str(d[1:]))
			}
			add(Value{Kind: KindHex, Bytes: d[:1]}, str(d[1:]))
		}
	case TypeVar:
		var vals []Value
		for _, c := range d {
			if c >= 26 {
				vals = nil
				break
			}
			vals = append(vals, word(string(rune('A'+c))))
		}
		if vals != nil {
			add(vals...)
		}
	case TypeVarNumber:
		if len(d) == 1 && d[0] < 26 {
			add(word(string(rune('A' + d[0]))))
		}
		if len(d) >= 2 && d[0] < 26 {
			add(word(string(rune('A'+d[0]))), num(beNumber(d[1:])))
		}
	case TypeVarString:
		if len(d) >= 1 && d[0] < 26 {
			add(word(string(rune('A'+d[0]))), str(d[1:]))
		}
	case TypeByteList:
		var vals []Value
		for _, c := range d {
			vals = append(vals, num(int64(c)))
		}
		add(vals...)
	case TypeToken:
		// a token, then (usually) a global id or a number
		if len(d) > 2 && printable(d[:2]) {
			if len(d) == 3 && d[2] < 10 {
				add(str(d[:2]), num(int64(d[2])))
			}
			if g := tokenGID(a, d[2:]); g != nil {
				add(str(d[:2]), *g)
			}
		}
		if len(d) >= 2 && printable(d) {
			add(str(d))
		} else if len(d) > 2 && printable(d[:2]) {
			add(str(d[:2]), Value{Kind: KindHex, Bytes: d[2:]})
		}
	}
	if len(d) >= 1 && len(d) <= 4 && a.Type != TypeRaw && a.Type != TypeString {
		add(num(beNumber(d)))
	}
	return out
}

// textual is whether Ada32's disassembler writes b as a string: no
// control characters but tab, CR and LF.
func textual(b []byte) bool {
	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\r' && c != '\n' {
			return false
		}
	}
	return true
}

// gid3 are the token atoms whose global ids Ada32 writes in three parts,
// 0-36-7232 where the others write 36-7232.
var gid3 = map[string]bool{"sm_m_send_token_arg": true, "de_ez_send_form": true, "sm_send_token_arg": true}

// tokenGID shows the bytes after a token as a global id.
func tokenGID(a *Atom, d []byte) *Value {
	switch {
	case len(d) == 1:
		return &Value{Kind: KindGID, GID: []int64{0, 0, beNumber(d)}}
	case len(d) == 2 && gid3[a.Name]:
		return &Value{Kind: KindGID, GID: []int64{0, 0, beNumber(d)}}
	case len(d) == 2:
		return &Value{Kind: KindGID, GID: []int64{0, beNumber(d)}}
	case len(d) == 3 && gid3[a.Name]:
		return &Value{Kind: KindGID, GID: []int64{0, int64(d[0]), beNumber(d[1:])}}
	case len(d) == 3 || len(d) == 4:
		return gidValue(d)
	}
	return nil
}

func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c >= 0x7F {
			return false
		}
	}
	return true
}

// enumRender shows d as the i'th place's named value, or flags or'd.
func enumRender(a *Atom, i int, d []byte) (Value, bool) {
	if a.Type == TypeOrient && i == 0 {
		if len(d) == 1 {
			if w, ok := orientName(d[0]); ok {
				return Value{Kind: KindWord, Text: w}, true
			}
		}
		return Value{}, false
	}
	e := enumAt(a, i)
	if e == nil || len(d) != e.width {
		return Value{}, false
	}
	v := uint32(beNumber(d))
	if w, ok := e.name(v); ok {
		return Value{Kind: KindWord, Text: w}, true
	}
	if !e.flags {
		return Value{}, false
	}
	// flags: the biggest named values that fit, then what's left as a number
	vals := append([]namedValue(nil), e.vals...)
	sort.SliceStable(vals, func(i, j int) bool { return vals[i].val > vals[j].val })
	var named []namedValue
	rest := v
	for _, f := range vals {
		if f.val != 0 && f.name[0] != '~' && rest&f.val == f.val {
			named = append(named, f)
			rest &^= f.val
		}
	}
	if !e.desc {
		sort.SliceStable(named, func(i, j int) bool { return named[i].val < named[j].val })
	}
	var parts []Value
	for _, f := range named {
		parts = append(parts, Value{Kind: KindWord, Text: f.name})
	}
	if rest != 0 {
		parts = append(parts, Value{Kind: KindNum, Num: int64(rest)})
	}
	if len(parts) < 2 {
		return Value{}, false
	}
	return Value{Kind: KindPipe, Parts: parts}, true
}

func beNumber(d []byte) int64 {
	var n int64
	for _, c := range d {
		n = n<<8 | int64(c)
	}
	return n
}

// gidValue reads 3 bytes as a-b and 4 as a-b-c.
func gidValue(d []byte) *Value {
	switch len(d) {
	case 3:
		return &Value{Kind: KindGID, GID: []int64{int64(d[0]), int64(d[1])<<8 | int64(d[2])}}
	case 4:
		return &Value{Kind: KindGID, GID: []int64{int64(d[0]), int64(d[1]), int64(d[2])<<8 | int64(d[3])}}
	}
	return nil
}
