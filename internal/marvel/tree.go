package marvel

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// The directory MSN's Explorer windows browse, built from the BBS's
// message areas each time it is asked for, as the caller may see them:
//
//	0:0        Worldwide Categories (the client names it)  → Categories
//	1:0        MSN Central                                  → Member Assistance
//	1:0x10     Categories: a folder for each area group
//	1:0x11     Member Assistance: MSN Today
//	4:0        MSN Today: the boards with the newest posts
//	1:group    a group's boards
//	0:area     a board (the BBS service answers inside it)
//	1:0x12     Chat: the chat rooms (in Categories and MSN Central)
//	id:CHAT    a room (the chat window opens it)
//	1:0x13     Files: a folder for each file area (in Categories and MSN Central)
//	1:libBase+area  a file area's files
//	id:FILE    a file, Download-and-Run (FTM sends it)
//
// A node's id is the 8 bytes the client sends: two dwords, the first the
// service's index (or 0 for a board), the second the node's own.

type nodeKey struct{ a, b uint32 }

func (k nodeKey) bytes() []byte {
	return binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, k.a), k.b)
}

func keyOf(b []byte) (nodeKey, bool) {
	if len(b) < 8 {
		return nodeKey{}, false
	}
	return nodeKey{binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint32(b[4:])}, true
}

var (
	keyWorldwide = nodeKey{0, 0}
	keyCentral   = nodeKey{1, 0}
	keyToday     = nodeKey{4, 0}
	keyCats      = nodeKey{1, 0x10}
	keyAssist    = nodeKey{1, 0x11}
	// what an unknown id is answered with: an empty folder of its own (an
	// empty answer, or one that is another node, confuses the client)
	keyNowhere = nodeKey{0xFFFFFFFF, 0xFFFFFFFF}
)

const groupBase = 0x100000 // 1:groupBase+n is the n'th group's folder

// app ids: what program the client runs for a node
const (
	appDirectory = 1
	appBBS       = 2
	appDownload  = 7 // Download and Run: FTM fetches the file, and the client opens it
)

// todayBoards is how many boards MSN Today lists.
const todayBoards = 10

type node struct {
	key      nodeKey
	name     string
	desc     string
	goWord   string
	app      uint32
	board    *store.Area // a board's area
	file     *store.File
	typ      string // the Type column ("": Folder)
	room     bool
	people   int // in a room
	modified time.Time
	gen      uint32
}

type tree struct {
	find     bool // answering a Find result's rows
	nodes    map[nodeKey]*node
	children map[nodeKey][]nodeKey
	parents  map[nodeKey][]nodeKey
}

func (t *tree) add(parent nodeKey, n *node) {
	if _, ok := t.nodes[n.key]; !ok {
		t.nodes[n.key] = n
	}
	t.children[parent] = append(t.children[parent], n.key)
	t.parents[n.key] = append(t.parents[n.key], parent)
}

// get is the node with key k, or the empty stand-in.
func (t *tree) get(k nodeKey) *node {
	if n := t.nodes[k]; n != nil {
		return n
	}
	return t.nodes[keyNowhere]
}

func (t *tree) kids(k nodeKey) []*node {
	var out []*node
	for _, c := range t.children[k] {
		out = append(out, t.nodes[c])
	}
	return out
}

// buildTree is the directory for the areas given (those the caller may
// read), under the BBS's name.
func buildTree(system string, areas []*store.Area, rooms []roomEntry, libs []library) *tree {
	t := &tree{nodes: map[nodeKey]*node{}, children: map[nodeKey][]nodeKey{}, parents: map[nodeKey][]nodeKey{}}
	folder := func(k nodeKey, name, desc, goWord string) *node {
		return &node{key: k, name: name, desc: desc, goWord: goWord, app: appDirectory}
	}
	t.nodes[keyNowhere] = folder(keyNowhere, "", "", "")
	t.nodes[keyWorldwide] = folder(keyWorldwide, "Categories", "", "")
	t.nodes[keyCentral] = folder(keyCentral, system, "", "central")
	t.add(keyWorldwide, folder(keyCats, "Categories", "The message areas of "+system, "categories"))
	t.add(keyCentral, folder(keyAssist, "Member Assistance", "", "assistance"))
	t.add(keyAssist, folder(keyToday, "MSN Today", "The boards with the newest messages", "today"))

	if len(rooms) > 0 {
		chat := folder(keyChat, "Chat", "Talk with the people on "+system+" now", "chat")
		t.add(keyCentral, chat)
		t.add(keyCats, chat)
		for _, r := range rooms {
			desc := r.topic
			if desc == "" {
				desc = "A chat room"
			}
			rn := &node{key: nodeKey{r.id, roomTag}, name: r.name, desc: desc, goWord: "chat " + strings.ToLower(r.name),
				app: appChat, room: true, people: r.people, gen: uint32(r.people)*2 + 1, typ: "Chat Room"}
			if r.name == bbs.MainRoom {
				rn.goWord = "chat main"
			}
			t.add(keyChat, rn)
		}
	}

	if len(libs) > 0 {
		files := folder(keyFiles, "Files", "The file libraries of "+system, "files")
		t.add(keyCentral, files)
		t.add(keyCats, files)
		for _, l := range libs {
			lk := nodeKey{1, libBase + uint32(l.area.ID)}
			lib := folder(lk, l.area.Name, l.area.Description, strings.ToLower(l.area.Name))
			lib.typ = "File Library"
			t.add(keyFiles, lib)
			for _, f := range l.files {
				t.add(lk, fileNode(f))
			}
		}
	}

	var groups []string
	byGroup := map[string][]*store.Area{}
	for _, a := range areas {
		g := a.Group
		if g == "" {
			g = "General"
		}
		if _, ok := byGroup[g]; !ok {
			groups = append(groups, g)
		}
		byGroup[g] = append(byGroup[g], a)
	}
	for i, g := range groups {
		gk := nodeKey{1, groupBase + uint32(i)}
		t.add(keyCats, folder(gk, g, "", strings.ToLower(g)))
		for _, a := range byGroup[g] {
			t.add(gk, boardNode(a))
		}
	}
	recent := append([]*store.Area(nil), areas...)
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].LastID > recent[j].LastID })
	for i, a := range recent {
		if i == todayBoards || a.Messages == 0 {
			break
		}
		t.add(keyToday, t.nodes[nodeKey{0, uint32(a.ID)}])
	}
	t.stamp(keyWorldwide, map[nodeKey]bool{})
	t.stamp(keyCentral, map[nodeKey]bool{})
	t.nodes[keyNowhere].gen = 1
	return t
}

func boardNode(a *store.Area) *node {
	return &node{key: nodeKey{0, uint32(a.ID)}, name: a.Name, desc: a.Description,
		goWord: strings.ToLower(a.Name), app: appBBS, board: a, gen: boardGen(a), typ: "Bulletin Board Folder"}
}

func fileNode(f *store.File) *node {
	return &node{key: nodeKey{uint32(f.ID), fileTag}, name: f.Name, desc: f.Description, app: appDownload,
		file: f, modified: f.Created, gen: uint32(f.Downloads)*2 + 1, typ: "File"}
}

// boardGen changes when a board's messages do.
func boardGen(a *store.Area) uint32 {
	return uint32(a.LastID)*31 + uint32(a.Messages) + 1
}

// stamp gives each folder a generation from its children's: the client
// refreshes a view only when the number changes.
func (t *tree) stamp(k nodeKey, seen map[nodeKey]bool) uint32 {
	n := t.nodes[k]
	if n.board != nil || n.room || n.file != nil || seen[k] {
		return n.gen
	}
	seen[k] = true
	h := fnv.New32a()
	for _, c := range t.children[k] {
		var b [12]byte
		binary.LittleEndian.PutUint32(b[:], c.a)
		binary.LittleEndian.PutUint32(b[4:], c.b)
		binary.LittleEndian.PutUint32(b[8:], t.stamp(c, seen))
		h.Write(b[:])
	}
	h.Write([]byte(n.name))
	n.gen = h.Sum32() | 1
	return n.gen
}

// byGoWord finds a node by its go word.
func (t *tree) byGoWord(w string) *node {
	w = strings.ToLower(strings.TrimSpace(w))
	if w == "" {
		return nil
	}
	keys := make([]nodeKey, 0, len(t.nodes))
	for k := range t.nodes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].a < keys[j].a || keys[i].a == keys[j].a && keys[i].b < keys[j].b
	})
	for _, k := range keys {
		if n := t.nodes[k]; n.goWord == w {
			return n
		}
	}
	return nil
}

// Node flags (property b)
const (
	flagLeaf     = 0x01
	flagHasSub   = 0x02
	flagDelegate = 0x04
)

// The shabbies (icons) this server has: a banner bitmap for every folder,
// and two icons, which are also what the Change Icon picker offers.
const (
	shabbyBanner   = 0x05000001
	iconFolder     = 0x0599
	iconBoard      = 0x059A
	iconChat       = 0x059B
	iconFile       = 0x059C
	lcidEnglishUS  = 0x0409
	notFound       = 0x10002 // GetDeidFromGoWord: no such go word
	refused        = 0x101   // a tree edit this server won't make
	filetimeOffset = 116444736000000000
)

func filetime(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UnixNano()/100) + filetimeOffset
}

// dirProps is a directory node's properties, those named and in their
// order: a property asked for and left out makes the client drop the
// rest.  list is true for a folder view's rows (some types differ from
// the Properties dialog's).
func (t *tree) dirProps(names []string, n *node, list bool) []prop {
	out := make([]prop, 0, len(names))
	for _, name := range names {
		out = append(out, t.dirProp(name, n, list))
	}
	return out
}

func (t *tree) dirProp(name string, n *node, list bool) prop {
	str := func(typ byte, s string) prop { return prop{typ, name, sz(s)} }
	dword := func(v uint32) prop { return prop{ptDword, name, u32(v)} }
	switch name {
	case "a":
		return prop{ptBlob, name, append(u32(8), n.key.bytes()...)}
	case "b":
		var f byte
		if n.board != nil {
			f = flagDelegate
		} else if n.room || n.file != nil {
			f = flagLeaf
		} else if len(t.children[n.key]) > 0 {
			f = flagHasSub
		}
		return prop{ptByte, name, u8(f)}
	case "c":
		return dword(n.app)
	case "e":
		return str(ptAnsi, n.name)
	case "f":
		return str(ptWide, n.name)
	case "h":
		if n.board != nil {
			return dword(iconBoard)
		}
		if n.room {
			return dword(iconChat)
		}
		if n.file != nil {
			return dword(iconFile)
		}
		return dword(iconFolder)
	case "mf", "wv":
		return dword(shabbyBanner)
	case "g":
		return dword(n.gen)
	case "x":
		return dword(0) // no rights to change the directory
	case "nc":
		if n.board != nil {
			return dword(uint32(n.board.Messages))
		}
		return dword(uint32(len(t.children[n.key])))
	case "np":
		return dword(uint32(max(1, len(t.parents[n.key]))))
	case "p":
		if n.board != nil {
			return dword(uint32(n.board.Messages))
		}
		if n.room {
			return dword(uint32(n.people))
		}
		if n.file != nil {
			return dword(uint32(min(n.file.Size, 1<<32-1)))
		}
		return dword(0)
	case "l":
		if n.board != nil {
			return prop{ptQword2, name, n.key.bytes()}
		}
		return dword(0)
	case "i":
		if n.board != nil {
			return prop{ptWord, name, u16(0)}
		}
		return dword(0)
	case "tp":
		typ := n.typ
		if typ == "" {
			typ = "Folder"
		}
		if list {
			return str(ptAnsi, typ)
		}
		return str(ptWide, typ)
	case "w":
		if !list {
			s := ""
			if !n.modified.IsZero() {
				s = n.modified.Local().Format("January 2, 2006 3:04 PM")
			}
			return str(ptWide, s)
		}
		if ft := filetime(n.modified); ft != 0 {
			return prop{ptQword2, name, u64(ft)}
		}
		if t.find {
			// a Find result's row must have a date (an empty one shows as 1601)
			return prop{ptQword2, name, u64(filetime(time.Now()))}
		}
		return str(ptAnsi, "")
	case "q":
		return prop{ptQword, name, append(u32(1), u32(lcidEnglishUS)...)}
	case "j":
		return str(ptWide, n.desc)
	case "k":
		return str(ptWide, n.goWord)
	case "ca", "r", "s", "t", "u", "n", "on", "v":
		return str(ptWide, "")
	case "fn":
		if n.file != nil {
			return str(ptAnsi, n.file.Name) // the local file's name
		}
		return str(ptAnsi, "")
	case "zc":
		return dword(0) // sent as it is: no MOS2 compression
	case "bbix":
		return prop{ptBlob, name, u32(0)}
	}
	return dword(0)
}
