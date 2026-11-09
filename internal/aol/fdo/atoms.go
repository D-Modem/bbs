package fdo

import (
	"strings"
	"sync"
)

// Type is an atom's argument type, as ADA.BIN gives it.  It decides how the
// compiler reads the atom's argument and how the decompiler shows it.
type Type uint8

const (
	TypeRaw       Type = 0x01 // bytes, shown as hex
	TypeDword     Type = 0x02 // a number, big-endian in as few bytes as it needs
	TypeString    Type = 0x03 // text
	TypeBool      Type = 0x04 // yes / no
	TypeGID       Type = 0x05 // a global id: 32-105, 1-0-1329
	TypeStream    Type = 0x06 // a nested atom stream (the act_ atoms' actions)
	TypeObject    Type = 0x07 // an object type and title: <trigger, "OK">
	TypeOrient    Type = 0x08 // an orientation code: vff, hef...
	TypeToken     Type = 0x09 // a two-character token, then data: <"pE">
	TypeCriterion Type = 0x0A // an action criterion: select, close, 1001...
	TypeAlert     Type = 0x0B // an alert type and its text: <info, "Done.">
	TypeAtom      Type = 0x0C // the name of another atom (two bytes)
	TypeByteList  Type = 0x0D // numbers, a byte each (the if_ atoms)
	TypeIgnore    Type = 0x0E // compiler-internal (the large-atom atoms)
	TypeVarNumber Type = 0x0F // a variable letter and a number: <A, 5>
	TypeVarString Type = 0x10 // a variable letter and a string: <A, "x">
	TypeVar       Type = 0x11 // a variable letter, A-Z
	TypeList      Type = 0x12 // several values: mat_size <25, 1, 32>
	TypeEnum      Type = 0x13 // a named value or values or'd: center_center
	TypeType14    Type = 0x14 // (de_typed_data, mat_spacing...)
	TypeType15    Type = 0x15 // (mip_folder_id...)
)

// Flags are an atom's layout flags: where the decompiler indents.
type Flags uint8

const (
	FlagIndent  Flags = 1 << iota // the atoms after this one are indented
	FlagOutdent                   // this atom and those after it are outdented
	FlagEOS                       // this atom ends a stream
)

// Atom is one entry of the atom table.
type Atom struct {
	Proto uint8
	Num   uint8
	Name  string
	Type  Type
	Flags Flags
}

// Protocol numbers (the FDO91 manual's "protocols"; NINA's atoms table for
// those the manual doesn't cover).
const (
	ProtoUNI     = 0x00 // universal: stream control
	ProtoMAN     = 0x01 // display manager
	ProtoACT     = 0x02 // actions
	ProtoDE      = 0x03 // data extraction
	ProtoBUF     = 0x04 // buffer
	ProtoIDB     = 0x05 // local database
	ProtoXFER    = 0x07 // file transfer
	ProtoFM      = 0x08 // file manager
	ProtoLM      = 0x09 // list manager
	ProtoCM      = 0x0A // code manager
	ProtoCHAT    = 0x0B
	ProtoVAR     = 0x0C // variables
	ProtoASYNC   = 0x0D
	ProtoSM      = 0x0E // shorthand
	ProtoIF      = 0x0F // conditionals
	ProtoMAT     = 0x10 // object attributes
	ProtoMIP     = 0x11 // mail
	ProtoMMI     = 0x14 // multimedia
	ProtoIMGXFER = 0x15
	ProtoIMAGE   = 0x16
	ProtoCHART   = 0x17
	ProtoMORG    = 0x18
	ProtoRICH    = 0x19 // rich text
	ProtoEXAPI   = 0x1A
	ProtoDOD     = 0x1B // data on demand
	ProtoRADIO   = 0x1C
	ProtoPICTALK = 0x1D
	ProtoIRC     = 0x1E
	ProtoDOC     = 0x1F
	ProtoCCL     = 0x22
	ProtoP3      = 0x23
	ProtoAD      = 0x27
	ProtoAPP     = 0x28
	ProtoMT      = 0x2A // tool manager
	ProtoVRM     = 0x2F // Visual Rainman
	ProtoWWW     = 0x30
	ProtoJAVA    = 0x31
	ProtoHFS     = 0x33 // host forms server
	ProtoBLANK   = 0x34
	ProtoVID     = 0x35
	ProtoACTIVEX = 0x36
	ProtoGALLERY = 0x38
	ProtoDICE    = 0x39
	ProtoSPELL   = 0x3D
	ProtoADP     = 0x42
	ProtoBUDDY   = 0x49
	ProtoHTMLVW  = 0x4B
)

// protoNames are the protocols' atom-name prefixes.
var protoNames = map[uint8]string{
	ProtoUNI: "uni", ProtoMAN: "man", ProtoACT: "act", ProtoDE: "de",
	ProtoBUF: "buf", ProtoIDB: "idb", ProtoXFER: "xfer", ProtoFM: "fm",
	ProtoLM: "lm", ProtoCM: "cm", ProtoCHAT: "chat", ProtoVAR: "var",
	ProtoASYNC: "async", ProtoSM: "sm", ProtoIF: "if", ProtoMAT: "mat",
	ProtoMIP: "mip", ProtoMMI: "mmi", ProtoIMGXFER: "imgxfer",
	ProtoIMAGE: "image", ProtoCHART: "chart", ProtoMORG: "morg",
	ProtoRICH: "rich", ProtoEXAPI: "exapi", ProtoDOD: "dod",
	ProtoRADIO: "radio", ProtoPICTALK: "pictalk", ProtoIRC: "irc",
	ProtoDOC: "doc", ProtoCCL: "ccl", ProtoP3: "p3", ProtoAD: "ad",
	ProtoAPP: "app", ProtoMT: "mt", ProtoVRM: "vrm", ProtoWWW: "www",
	ProtoJAVA: "java", ProtoHFS: "hfs", ProtoBLANK: "blank",
	ProtoVID: "vid", ProtoACTIVEX: "activex", ProtoGALLERY: "gallery",
	ProtoDICE: "dice", ProtoSPELL: "spell", ProtoADP: "adp",
	ProtoBUDDY: "buddy", ProtoHTMLVW: "htmlview",
}

// ProtocolName is a protocol's prefix ("man" for 1), or "" if it has none.
func ProtocolName(proto uint8) string { return protoNames[proto] }

var (
	tableOnce sync.Once
	byName    map[string]*Atom
	byNum     map[uint16]*Atom
)

// moreAtoms are atoms of AOL 4.0's protocols that the ADA.BIN dump lacks,
// with their numbers as Ada32 compiled them in AtomForge's corpus.
var moreAtoms = []Atom{
	{ProtoGALLERY, 6, "gallery_open", TypeRaw, 0},
	{ProtoGALLERY, 7, "gallery_send_img_to_mail", TypeRaw, 0},
	{ProtoDICE, 0, "dice_set_context_tool", TypeDword, 0},
	{ProtoDICE, 2, "dice_end_context", TypeRaw, 0},
	{ProtoDICE, 11, "dice_disconnect", TypeRaw, 0},
	{ProtoDICE, 19, "dice_send_message", TypeDword, 0},
	{ProtoDICE, 39, "dice_access_search_form", TypeRaw, 0},
	{ProtoDICE, 41, "dice_disp_connect_summary", TypeRaw, 0},
	{ProtoSPELL, 0, "spell_rules_dlg", TypeRaw, 0},
	{ProtoBUDDY, 0, "buddy_action_command", TypeRaw, 0},
	{ProtoHTMLVW, 0, "htmlview_manage", TypeRaw, 0},
}

func loadTable() {
	byName = make(map[string]*Atom, len(atomTable)+len(moreAtoms))
	byNum = make(map[uint16]*Atom, len(atomTable)+len(moreAtoms))
	add := func(a *Atom) {
		byName[a.Name] = a
		byNum[uint16(a.Proto)<<8|uint16(a.Num)] = a
	}
	for i := range atomTable {
		add(&atomTable[i])
	}
	for i := range moreAtoms {
		add(&moreAtoms[i])
	}
}

// Lookup finds an atom by name (case doesn't matter, and an "atom$"
// prefix, as the manual writes them, is allowed).
func Lookup(name string) (*Atom, bool) {
	tableOnce.Do(loadTable)
	name = strings.ToLower(name)
	name = strings.TrimPrefix(name, "atom$")
	a, ok := byName[name]
	return a, ok
}

// LookupNum finds an atom by protocol and number.
func LookupNum(proto, num uint8) (*Atom, bool) {
	tableOnce.Do(loadTable)
	a, ok := byNum[uint16(proto)<<8|uint16(num)]
	return a, ok
}

// Atoms is the whole table.
func Atoms() []Atom { return append(atomTable[:len(atomTable):len(atomTable)], moreAtoms...) }

// MustLookup is Lookup for names known when the program is written; it
// panics on a name that isn't in the table.
func MustLookup(name string) *Atom {
	a, ok := Lookup(name)
	if !ok {
		panic("fdo: no atom " + name)
	}
	return a
}
