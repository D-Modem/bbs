package marvel

import (
	"strings"
)

// DIRSRV is the directory: the client's Explorer windows (MSN Central,
// Categories, a folder's contents, Go words, icons) read it.
type dirsrv struct{ c *conn }

// interface bytes: the tree reads, and the tree edits
const (
	ifaceTree = 0x03
	ifaceEdit = 0x04
)

// tree read methods
const (
	treeGetProperties = 0x00
	treeGetParents    = 0x01
	treeGetChildren   = 0x02
	treeGoWord        = 0x03
	treeGetShabby     = 0x04
	treeEnumShn       = 0x05
)

// tree edit methods
const (
	editAddNode          = 0x02
	editDeleteNode       = 0x03
	editSetProperties    = 0x04
	editAddShabby        = 0x07
	editGetDataSets      = 0x0B
	editGetTicket        = 0x0C
	editInvalidateTicket = 0x0D
)

func (d *dirsrv) ifaces() []iface {
	return []iface{mosIID(0x27, ifaceTree), mosIID(0x28, ifaceEdit)}
}

// treeRequest is a tree read's parameters.
type treeRequest struct {
	nodes  []nodeKey
	props  []string
	flags  byte // GetChildren: 1 puts the node itself first
	locale []byte
}

func parseTreeRequest(ps []param) treeRequest {
	var r treeRequest
	vars := 0
	for _, p := range ps {
		switch p.Tag {
		case 0x01:
			r.flags = byte(p.Int)
		case 0x04:
			switch vars {
			case 0:
				for b := p.Data; len(b) >= 8; b = b[8:] {
					k, _ := keyOf(b)
					r.nodes = append(r.nodes, k)
				}
			case 1:
				for _, s := range strings.Split(string(p.Data), "\x00") {
					if s != "" {
						r.props = append(r.props, s)
					}
				}
			case 2:
				r.locale = p.Data
			}
			vars++
		}
	}
	if len(r.nodes) == 0 {
		r.nodes = []nodeKey{keyNowhere}
	}
	return r
}

// treeReply is the reply to a tree read: a status, the count, and the
// records as a stream.
func treeReply(records [][]byte) []byte {
	r := reply{}.dword(0).dword(uint32(len(records))).endStatic()
	r = append(r, tagStreamEnd)
	for _, rec := range records {
		r = append(r, rec...)
	}
	return r
}

func (d *dirsrv) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false // an icon upload's chunks: refused at its head
	}
	ps, _ := parseParams(h.Params)
	if h.Iface == ifaceEdit {
		return editCall(d.c, h.Method, ps, false)
	}
	switch h.Method {
	case treeGetProperties, treeGetParents, treeGetChildren:
		r := parseTreeRequest(ps)
		t := d.c.tree()
		var recs [][]byte
		switch h.Method {
		case treeGetProperties:
			for _, k := range r.nodes {
				recs = append(recs, record(t.dirProps(r.props, t.get(k), false)))
			}
		case treeGetParents:
			for _, p := range t.parents[r.nodes[0]] {
				recs = append(recs, record(t.dirProps(r.props, t.nodes[p], true)))
			}
		case treeGetChildren:
			k := r.nodes[0]
			if k == keyWorldwide && len(r.props) == 1 && r.props[0] == "q" {
				// the browse languages the Options dialog offers
				recs = append(recs, record([]prop{{ptQword, "q", append(u32(1), u32(lcidEnglishUS)...)}}))
				break
			}
			n := t.get(k)
			if r.flags == 1 {
				recs = append(recs, record(t.dirProps(r.props, n, true)))
			}
			for _, c := range t.kids(n.key) {
				recs = append(recs, record(t.dirProps(r.props, c, true)))
			}
			if len(recs) == 0 && (n.key == keyNowhere || n.key == keyToday) {
				// a node the client was sent to but this server hasn't (or
				// an MSN Today with nothing new) answers with an empty
				// folder: no rows at all leaves the client unable to go on
				recs = append(recs, record(t.dirProps(r.props, t.nodes[keyNowhere], true)))
			}
		}
		return treeReply(recs), true
	case treeGoWord:
		return goWordReply(d.c.tree(), ps), true
	case treeGetShabby:
		return shabbyReply(ps), true
	case treeEnumShn:
		var key uint32
		for _, p := range ps {
			if p.Tag == 0x01 {
				key = p.Int
			}
		}
		var ids []uint32
		if key == 0 {
			ids = []uint32{iconFolder, iconBoard}
		}
		var b []byte
		for _, id := range ids {
			b = append(b, u32(id)...)
		}
		return reply{}.dword(0).word(uint16(len(ids))).blob(b), true
	}
	d.c.logf("DIRSRV: no method %#x.%#x", h.Iface, h.Method)
	return nil, false
}

// goWordReply finds a Go word's node.
func goWordReply(t *tree, ps []param) []byte {
	var w string
	for _, p := range ps {
		if p.Tag == 0x04 {
			w = wide(p.Data)
			break
		}
	}
	if n := t.byGoWord(w); n != nil {
		return reply{}.dword(0).endStatic().bytes_(n.key.bytes())
	}
	return reply{}.dword(notFound).endStatic().bytes_(make([]byte, 8))
}

func shabbyReply(ps []param) []byte {
	var id uint32
	for _, p := range ps {
		if p.Tag == 0x03 {
			id = p.Int
			break
		}
	}
	return reply{}.dword(0).blob(shabby(id))
}

// editCall answers the tree edits: the BBS reader's delete of a message
// (on the BBS pipe), and a refusal of anything else.
func editCall(c *conn, method byte, ps []param, bbs bool) ([]byte, bool) {
	switch method {
	case editGetTicket:
		if bbs && c.signedOn() != nil {
			return reply{}.dword(0).blob(u16(2)), true
		}
		return reply{}.dword(refused).blob(nil), true
	case editInvalidateTicket:
		return reply{}.dword(0).endStatic(), true
	case editGetDataSets:
		return reply{}.dword(0).blob(record(nil)), true
	case editDeleteNode:
		st := uint32(refused)
		if bbs {
			st = c.deleteMessage(ps)
		}
		return reply{}.dword(st).dword(0).endStatic(), true
	case editAddNode:
		return reply{}.dword(refused).dword(0).endStatic().bytes_(make([]byte, 8)), true
	case editSetProperties:
		return reply{}.dword(refused).dword(0).endStatic(), true
	case editAddShabby:
		return reply{}.dword(refused).dword(0).dword(0).endStatic(), true
	}
	c.logf("tree edit: no method %#x", method)
	return nil, false
}
