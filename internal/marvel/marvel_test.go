package marvel

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Packets as the real client and server exchanged them (from the wire
// captures in the marvel-msn notes).
func TestPacketVectors(t *testing.T) {
	params := []byte{0xFF, 0xFF, ctrlParams}
	for _, v := range []uint32{1024, 1024, 16, 1, 600} {
		params = binary.LittleEndian.AppendUint32(params, v)
	}
	got := framePackets(params, reassemblyIndex(params), 0, 0)
	want := unhex(t, "80 80 e0 17 00 ff ff 03 00 04 00 00 00 04 00 00 1b 32 00 00 00 01 00 00 00 58 02 00 00 38 c9 9a 7e 0d")
	if len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Errorf("transport params:\n got %x\nwant %x", got, want)
	}

	open := []byte{3, 0, 1, 0, 3, 0, 0, 0}
	got = framePackets(open, reassemblyIndex(open), 2, 3)
	want = unhex(t, "82 83 e3 08 00 03 00 01 00 03 00 00 00 b6 d3 09 2d 0d")
	if len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Errorf("pipe open:\n got %x\nwant %x", got, want)
	}

	// the client's "connected" control frame
	raw := unhex(t, "80 80 e0 03 00 ff ff 04 fc 03 18 a0")
	p, err := parsePacket(raw)
	if err != nil || p.kind != 'D' || p.seq != 0 || p.ack != 0 {
		t.Fatalf("parse: %+v %v", p, err)
	}
	var r reassembler
	msgs := r.packet(p.stuffed)
	if len(msgs) != 1 || !bytes.Equal(msgs[0], []byte{0xFF, 0xFF, 4}) {
		t.Errorf("control frame: %x", msgs)
	}
	raw[9] ^= 1
	if _, err := parsePacket(raw); err != errCRC {
		t.Errorf("a bad CRC: %v", err)
	}

	if got := straightRecord([]byte{0xFF, 0xFF, 4}); !bytes.Equal(got, unhex(t, "06 00 00 ff ff 04")) {
		t.Errorf("straight record: %x", got)
	}
}

func TestStuffing(t *testing.T) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	st := stuff(all)
	for _, b := range st {
		if b == terminator {
			t.Fatal("a CR left in a stuffed payload")
		}
	}
	if !bytes.Equal(unstuff(st), all) {
		t.Error("stuffing doesn't round-trip")
	}
	for i := 0x80; i < 256; i++ {
		if unheaderByte(headerByte(byte(i))) != byte(i) {
			t.Errorf("header byte %#x", i)
		}
	}
}

// A message too long for one packet goes in several, and comes back whole,
// even with an escape split between two packets.
func TestReassembly(t *testing.T) {
	msg := []byte{5, 0}
	for i := 0; len(msg) < 5000; i++ {
		msg = append(msg, byte(i*7), 0x1B, 0x0D)
	}
	pkts := framePackets(msg, reassemblyIndex(msg), 10, 0)
	if len(pkts) < 5 {
		t.Fatalf("%d packets", len(pkts))
	}
	var r reassembler
	var got [][]byte
	for i, raw := range pkts {
		if len(raw) > maxPacket {
			t.Errorf("packet %d: %d bytes", i, len(raw))
		}
		p, err := parsePacket(raw[:len(raw)-1])
		if err != nil {
			t.Fatal(err)
		}
		if p.seq != byte(10+i) {
			t.Errorf("packet %d: seq %d", i, p.seq)
		}
		got = append(got, r.packet(p.stuffed)...)
	}
	if len(got) != 1 || !bytes.Equal(got[0], msg) {
		t.Fatalf("reassembled %d messages", len(got))
	}

	// a split escape: the client's frames, as it might cut them
	body := append([]byte{7, 0}, bytes.Repeat([]byte{0x0D}, 20)...)
	frame := append([]byte{frameHeader(7, true), byte(len(body)), 0}, body...)
	st := stuff(frame)
	cut := bytes.IndexByte(st, escape) + 1 // just after an escape
	first := st[:cut]
	second := append([]byte{headerByte(frameAlways | frameContinue | 7)}, st[cut:]...)
	r = reassembler{}
	if m := r.packet(first); m != nil {
		t.Fatalf("early: %x", m)
	}
	got = r.packet(second)
	if len(got) != 1 || !bytes.Equal(got[0], body) {
		t.Fatalf("split escape: %x", got)
	}
}

func TestVLI(t *testing.T) {
	for _, v := range []uint32{0, 1, 0x3E, 0x3F, 0x100, 0x3FFE, 0x3FFF, 0x123456} {
		b := putVLI(nil, v)
		got, n := getVLI(b)
		if got != v || n != len(b) {
			t.Errorf("%#x: %x → %#x (%d)", v, b, got, n)
		}
	}
	if b := putVLI(nil, 0x3F); len(b) != 2 {
		t.Errorf("0x3F takes two bytes, not %x", b)
	}
}

func TestRetransmission(t *testing.T) {
	rto = 50 * time.Millisecond
	defer func() { rto = 6 * time.Second }()
	a, b := net.Pipe()
	defer a.Close()
	l := newSelectLink(a, nil, t.Logf)
	go l.send([]byte{1, 0, 9, 9})
	seen := 0
	buf := make([]byte, 64)
	b.SetReadDeadline(time.Now().Add(2 * time.Second))
	for seen < 2 {
		n, err := b.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		seen += bytes.Count(buf[:n], []byte{terminator})
	}
	l.mu.Lock()
	l.acked(1)
	left := len(l.unacked)
	l.mu.Unlock()
	if left != 0 {
		t.Errorf("%d unacknowledged after the ACK", left)
	}
	l.close()
}

func TestRTF(t *testing.T) {
	// [MS-OXRTFCP]'s example
	comp := unhex(t, "2d0000002b0000004c5a4675f1c5c7a703000a0072637067313235423 20af320"+
		"68656c090020627705b06c647d0a800fa0")
	raw, err := unlzfu(comp)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\\rtf1\\ansi\\ansicpg1252\\pard hello world}\r\n"; string(raw) != want {
		t.Errorf("decompressed %q", raw)
	}
	if len(lzfuDict) != 207 {
		t.Errorf("dictionary: %d bytes", len(lzfuDict))
	}
	doc := `{\rtf1\ansi{\fonttbl{\f0\fswiss Arial;}}{\*\generator x;}\pard\f0 Caf\'e9 is \b open\b0\par Line two\tab end\u8364?}`
	if got := rtfText([]byte(doc)); got != "Café is open\nLine two\tend€" {
		t.Errorf("text %q", got)
	}
}

func TestIcons(t *testing.T) {
	ico := shabby(iconFolder)
	if len(ico) != 22+40+64+32*16+32*4 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		t.Errorf("icon: %d bytes", len(ico))
	}
	bmp := shabby(shabbyBanner)
	if string(bmp[:2]) != "BM" || int(binary.LittleEndian.Uint32(bmp[2:])) != len(bmp) {
		t.Errorf("bitmap: %d bytes", len(bmp))
	}
	if shabby(0) != nil {
		t.Error("an icon for id 0")
	}
}

// a scripted client over TCP

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type client struct {
	t   *testing.T
	c   net.Conn
	buf []byte
	q   [][]byte
	id  uint32
}

func newServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.CreateUser("alice", "password1", "Here", false); err != nil {
		t.Fatal(err)
	}
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test BBS", 0
	logs := &syncBuf{}
	b := bbs.New(s, st, log.New(logs, "", 0))
	srv, err := New(b, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("log:\n" + logs.String())
		}
	})
	return srv, st
}

func dial(t *testing.T, srv *Server) *client {
	a, b := net.Pipe()
	go srv.handleTCP(b)
	t.Cleanup(func() { a.Close() })
	c := &client{t: t, c: a}
	if m := c.recv(); m[2] != ctrlParams {
		t.Fatalf("first: %x", m)
	}
	return c
}

func (c *client) send(msg []byte) {
	c.t.Helper()
	c.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.c.Write(straightRecord(msg)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) recv() []byte {
	c.t.Helper()
	for len(c.q) == 0 {
		var b [8192]byte
		c.c.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := c.c.Read(b[:])
		if err != nil {
			c.t.Fatal(err)
		}
		c.buf = append(c.buf, b[:n]...)
		var msgs [][]byte
		msgs, c.buf, err = takeStraight(c.buf)
		if err != nil {
			c.t.Fatal(err)
		}
		c.q = append(c.q, msgs...)
	}
	m := c.q[0]
	c.q = c.q[1:]
	return m
}

// open opens a pipe to svc and checks its discovery block.
func (c *client) open(pipe uint16, svc string) {
	c.t.Helper()
	msg := []byte{0, 0, 0, 0, byte(pipe), 0}
	msg = append(append(msg, svc...), 0, 'U', 0, 1, 0, 0, 0)
	c.send(msg)
	if r := c.recv(); !bytes.Equal(r, []byte{byte(pipe), 0, 1, 0, byte(pipe), 0, 0, 0}) {
		c.t.Fatalf("open %s: %x", svc, r)
	}
	d := c.recv()
	if binary.LittleEndian.Uint16(d) != pipe || len(d) < 5+17 || (len(d)-5)%17 != 0 {
		c.t.Fatalf("discovery %s: %x", svc, d)
	}
}

// call makes a call and returns the reply's parameters.
func (c *client) call(pipe uint16, iface, method byte, params ...[]byte) []byte {
	c.t.Helper()
	c.id++
	h := hostBlock{Iface: iface, Method: method, ID: c.id, Params: bytes.Join(params, nil)}
	c.send(append(binary.LittleEndian.AppendUint16(nil, pipe), h.bytes()...))
	r := c.recv()
	got, err := parseHostBlock(r[2:])
	if err != nil || binary.LittleEndian.Uint16(r) != pipe || got.Iface != iface || got.Method != method || got.ID != c.id {
		c.t.Fatalf("reply to %#x.%#x: %x", iface, method, r)
	}
	return got.Params
}

func vparam(b []byte) []byte {
	if len(b) < 0x80 {
		return append([]byte{0x04, 0x80 | byte(len(b))}, b...)
	}
	return append([]byte{0x04, byte(len(b) >> 8), byte(len(b))}, b...)
}

func dparam(v uint32) []byte { return binary.LittleEndian.AppendUint32([]byte{0x03}, v) }

func (c *client) login(name, pw string) uint32 {
	c.t.Helper()
	blob := make([]byte, blobLen)
	copy(blob[blobMemberAt:], name)
	copy(blob[blobPassAt:], pw)
	r := c.call(1, logsrvIface, logLogin, vparam(blob), []byte{0x83, 0x83, 0x83, 0x83, 0x83, 0x83, 0x83, 0x84})
	if len(r) < 5 || r[0] != tagDword {
		c.t.Fatalf("login reply %x", r)
	}
	return binary.LittleEndian.Uint32(r[1:])
}

// treeCall is a tree read: the records' properties, by name.
func (c *client) treeCall(pipe uint16, method byte, k nodeKey, flags byte, props ...string) []map[string][]byte {
	c.t.Helper()
	group := strings.Join(props, "\x00") + "\x00"
	r := c.call(pipe, ifaceTree, method, vparam(k.bytes()), []byte{0x01, flags}, dparam(1), dparam(uint32(len(props))),
		vparam([]byte(group)), vparam([]byte{0, 0, 0, 0}), []byte{0x83, 0x83, 0x85})
	if len(r) < 12 || r[0] != tagDword || binary.LittleEndian.Uint32(r[1:]) != 0 || r[10] != tagEndStatic || r[11] != tagStreamEnd {
		c.t.Fatalf("tree reply %x", r)
	}
	n := int(binary.LittleEndian.Uint32(r[6:]))
	return readRecords(c.t, r[12:], n, props)
}

func readRecords(t *testing.T, b []byte, n int, want []string) []map[string][]byte {
	t.Helper()
	var out []map[string][]byte
	for i := 0; i < n; i++ {
		size := int(binary.LittleEndian.Uint32(b))
		count := int(binary.LittleEndian.Uint16(b[4:]))
		rec, p := b[6:size], map[string][]byte{}
		b = b[size:]
		if count != len(want) {
			t.Fatalf("record %d: %d properties, not %d", i, count, len(want))
		}
		for j := 0; j < count; j++ {
			typ := rec[0]
			end := bytes.IndexByte(rec, 0)
			name := string(rec[1:end])
			rec = rec[end+1:]
			if name != want[j] {
				t.Fatalf("record %d property %d: %q, not %q", i, j, name, want[j])
			}
			var v []byte
			switch typ {
			case ptByte:
				v, rec = rec[:1], rec[1:]
			case ptWord:
				v, rec = rec[:2], rec[2:]
			case ptDword:
				v, rec = rec[:4], rec[4:]
			case ptQword, ptQword2:
				v, rec = rec[:8], rec[8:]
			case ptAnsi, ptWide:
				e := bytes.IndexByte(rec, 0)
				v, rec = rec[1:e], rec[e+1:]
			case ptBlob, ptDwords:
				l := int(binary.LittleEndian.Uint32(rec))
				if typ == ptDwords {
					l *= 4
				}
				v, rec = rec[4:4+l], rec[4+l:]
			default:
				t.Fatalf("property %q: type %#x", name, typ)
			}
			p[name] = v
		}
		out = append(out, p)
	}
	if len(b) != 0 {
		t.Fatalf("%d bytes after the records", len(b))
	}
	return out
}

func names(recs []map[string][]byte) []string {
	var out []string
	for _, r := range recs {
		out = append(out, string(r["e"]))
	}
	return out
}

func TestSignOn(t *testing.T) {
	srv, _ := newServer(t)
	c := dial(t, srv)
	c.open(1, "LOGSRV")
	if r := c.login("alice", "wrong"); r != loginBadPassword {
		t.Errorf("a wrong password: %#x", r)
	}
	if r := c.login("nobody", "x"); r != loginNoMember {
		t.Errorf("no such member: %#x", r)
	}
	if r := c.login("alice", "password1"); r != loginOK {
		t.Fatalf("sign-on: %#x", r)
	}
	if srv.Clients() != 1 {
		t.Errorf("%d clients", srv.Clients())
	}
	r := c.call(1, logsrvIface, logBilling, []byte{0x84})
	if len(r) != 3+0x41C || !bytes.HasPrefix(r[3+8+0x3B:], []byte("alice\x00")) {
		t.Errorf("billing record: %d bytes", len(r))
	}
	// the client signs out: its pipes close, and it is hung up on
	c.send([]byte{1, 0, pipeClose})
	c.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.c.Read(make([]byte, 1)); err == nil {
		t.Error("still connected after the last pipe closed")
	}
}

func TestDirectoryAndBoards(t *testing.T) {
	srv, st := newServer(t)
	areas, _ := st.Areas()
	for _, a := range areas {
		st.DeleteArea(a.ID)
	}
	gen, err := st.CreateArea("Chatter", "Talk about anything", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateArea("Retro", "Old machines", false); err != nil {
		t.Fatal(err)
	}
	first, err := st.Post(&store.Message{AreaID: gen.ID, From: "Bob", To: "All", Subject: "Hello", Body: "First post\nsecond line"})
	if err != nil {
		t.Fatal(err)
	}

	c := dial(t, srv)
	c.open(1, "LOGSRV")
	if r := c.login("alice", "password1"); r != loginOK {
		t.Fatalf("sign-on: %#x", r)
	}
	c.open(2, "DIRSRV")
	nav := []string{"a", "c", "h", "b", "e", "g", "x", "mf", "wv", "tp", "p", "w", "l", "i"}
	central := c.treeCall(2, treeGetProperties, keyCentral, 0, "a", "e")
	if names(central)[0] != "Test BBS" {
		t.Errorf("MSN Central: %q", names(central))
	}
	cats := c.treeCall(2, treeGetChildren, keyCats, 0, nav...)
	if got := names(cats); strings.Join(got, ",") != "Chat,General" {
		t.Fatalf("categories: %q", got)
	}
	gk, _ := keyOf(cats[1]["a"])
	brds := c.treeCall(2, treeGetChildren, gk, 0, nav...)
	if got := names(brds); strings.Join(got, ",") != "Chatter,Retro" {
		t.Fatalf("boards: %q", got)
	}
	board := brds[0]
	if board["b"][0] != flagDelegate || binary.LittleEndian.Uint32(board["c"]) != appBBS ||
		!bytes.Equal(board["l"], nodeKey{0, uint32(gen.ID)}.bytes()) {
		t.Errorf("board row: %v", board)
	}
	today := c.treeCall(2, treeGetChildren, keyToday, 0, "e")
	if got := names(today); len(got) != 1 || got[0] != "Chatter" {
		t.Errorf("MSN Today: %q", got)
	}
	// go words
	gw := c.call(2, ifaceTree, treeGoWord, vparam(utf16z("retro")), vparam(append(u32(1), u32(0x409)...)), []byte{0x83, 0x84})
	if !bytes.Equal(gw[:6], []byte{tagDword, 0, 0, 0, 0, tagEndStatic}) {
		t.Errorf("go word: %x", gw)
	}
	gw = c.call(2, ifaceTree, treeGoWord, vparam(utf16z("nowhere")), []byte{0x83, 0x84})
	if binary.LittleEndian.Uint32(gw[1:]) != notFound {
		t.Errorf("no go word: %x", gw)
	}
	icon := c.call(2, ifaceTree, treeGetShabby, dparam(iconBoard), []byte{0x83, 0x85})
	if !bytes.Equal(icon[5:7], []byte{tagEndStatic, tagComplete}) || !bytes.Equal(icon[7:], shabby(iconBoard)) {
		t.Errorf("icon reply: %x", icon[:8])
	}
	// an id nobody has is an empty folder
	if got := c.treeCall(2, treeGetChildren, nodeKey{7, 7}, 0, "e"); len(got) != 1 {
		t.Errorf("unknown: %d", len(got))
	}

	// the board, in the reader
	c.open(3, "BBS")
	bk := nodeKey{0, uint32(gen.ID)}
	props := []string{"a", "b", "e", "_a", "p", "_D", "_P", "_t", "_F", "q"}
	msgs := c.treeCall(3, treeGetChildren, bk, 1, props...)
	if len(msgs) != 2 || string(msgs[0]["e"]) != "Chatter" || string(msgs[1]["e"]) != "Hello" ||
		string(msgs[1]["_a"]) != "Bob" || msgs[1]["b"][0] != flagLeaf {
		t.Fatalf("board children: %v", msgs)
	}
	if f := binary.LittleEndian.Uint16(msgs[1]["_F"]); f&boardNoChildren == 0 || f&boardMSN == 0 {
		t.Errorf("message flags %#x", f)
	}
	mk := nodeKey{uint32(first), uint32(gen.ID)}
	art := c.call(3, ifaceMessage, msgArticle, vparam(mk.bytes()), []byte{0x83, 0x85})
	if !bytes.HasPrefix(art, []byte{tagDword, 0, 0, 0, 0, tagEndStatic, tagComplete}) {
		t.Fatalf("article reply %x", art[:8])
	}
	head, body, ok := strings.Cut(string(art[7:]), "\n\n")
	if !ok || !strings.Contains(head, "Subject: Hello\n") || !strings.Contains(head, "Newsgroups: Chatter\n") ||
		body != "First post\r\nsecond line" {
		t.Errorf("article %q", art[7:])
	}

	// a reply, uploaded in two pieces
	post := []byte("X-MOS-To: Chatter\nX-MOS-Parent: " + itoa(first) + "\nSubject: RE: Hello\nX-MOS-Format: TEXT\n" +
		"X-MOS-Size: 9\n\nHi\r\nthere")
	r := c.call(3, ifaceMessage, msgPostStart, dparam(uint32(len(post))), vparam(nodeKey{uint32(first), uint32(gen.ID)}.bytes()),
		[]byte{0x02, 1, 0}, vparam(nil), []byte{0x02, 0, 0}, vparam(post[:20]), dparam(20), []byte{0x81})
	if len(r) != 3 || r[0] != tagByte || r[1] == 0 {
		t.Fatalf("post start %x", r)
	}
	r = c.call(3, ifaceMessage, msgPostCommit, []byte{0x01, r[1]}, vparam(post[20:]), dparam(uint32(len(post)-20)), []byte{0x81})
	if !bytes.Equal(r, []byte{tagByte, 1, tagEndStatic}) {
		t.Fatalf("post commit %x", r)
	}
	got, _ := st.Newest(gen.ID, 1)
	if len(got) != 1 || got[0].Subject != "RE: Hello" || got[0].Body != "Hi\nthere" || got[0].ReplyTo != first ||
		got[0].From != "alice" || got[0].To != "Bob" {
		t.Fatalf("posted %+v", got[0])
	}
	msgs = c.treeCall(3, treeGetChildren, bk, 0, props...)
	if len(msgs) != 2 || binary.LittleEndian.Uint32(msgs[1]["_P"]) != uint32(first) {
		t.Errorf("after the post: %v", msgs)
	}

	// deleting: alice's own, and not Bob's
	tk := c.call(3, ifaceEdit, editGetTicket, []byte{0x83, 0x85})
	if !bytes.Equal(tk, []byte{tagDword, 0, 0, 0, 0, tagEndStatic, tagComplete, 2, 0}) {
		t.Fatalf("ticket %x", tk)
	}
	del := func(id int64) uint32 {
		r := c.call(3, ifaceEdit, editDeleteNode, vparam([]byte{2, 0}), vparam(nodeKey{uint32(id), uint32(gen.ID)}.bytes()), []byte{0x83, 0x83})
		return binary.LittleEndian.Uint32(r[1:])
	}
	if st := del(first); st != refused {
		t.Errorf("deleting Bob's: %#x", st)
	}
	if st := del(got[0].ID); st != 0 {
		t.Errorf("deleting her own: %#x", st)
	}
	if _, err := st.Message(got[0].ID); err == nil {
		t.Error("still there")
	}
}

func utf16z(s string) []byte {
	var b []byte
	for _, r := range s {
		b = binary.LittleEndian.AppendUint16(b, uint16(r))
	}
	return append(b, 0, 0)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// A modem caller: the CR, "COM", then the serial link.
func TestModemCall(t *testing.T) {
	srv, _ := newServer(t)
	n, err := srv.B.Enter(&bbs.Caller{Via: "modem", Remote: "555-1212", Source: "phone:5551212"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Leave()
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan error, 1)
	go func() { done <- srv.Call(n, b) }()
	a.SetDeadline(time.Now().Add(10 * time.Second))
	a.Write([]byte("\r"))
	rd := bufio.NewReader(a)
	packet := func() packet {
		t.Helper()
		for {
			raw, err := rd.ReadBytes(terminator)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) == 1 {
				continue
			}
			p, err := parsePacket(raw[:len(raw)-1])
			if err != nil {
				t.Fatalf("%x: %v", raw, err)
			}
			if p.kind == 'D' {
				return p
			}
		}
	}
	if line, _ := rd.ReadString(terminator); line != "COM\r" {
		t.Fatalf("answered %q", line)
	}
	var r reassembler
	if p := packet(); p.seq != 0 || r.packet(p.stuffed)[0][2] != ctrlParams {
		t.Fatalf("first packet %+v", p)
	}
	open := append([]byte{0, 0, 0, 0, 1, 0}, "LOGSRV\x00U\x00\x01\x00\x00\x00"...)
	for _, p := range framePackets(open, 0, 0, 1) {
		a.Write(p)
	}
	var got [][]byte
	for len(got) < 2 {
		p := packet()
		got = append(got, r.packet(p.stuffed)...)
		go a.Write(ackPacket(p.seq + 1)) // (a pipe has no buffer, as a line has)
	}
	if !bytes.Equal(got[0], []byte{1, 0, 1, 0, 1, 0, 0, 0}) || len(got[1]) != 5+17 {
		t.Fatalf("pipe open: %x", got)
	}
	// signing out hangs up
	for _, p := range framePackets([]byte{1, 0, pipeClose}, 1, 1, 3) {
		a.Write(p)
	}
	go io.Copy(io.Discard, rd)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("still connected")
	}
}

type lineBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lineBuf) Read([]byte) (int, error) { return 0, io.EOF }
func (l *lineBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lineBuf) Close() error { return nil }
func (l *lineBuf) packets() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return bytes.Count(l.b.Bytes(), []byte{terminator})
}

// No more than the window is sent before an ACK; each ACK lets more go.
func TestWindow(t *testing.T) {
	line := &lineBuf{}
	l := newSelectLink(line, nil, t.Logf)
	defer l.close()
	msg := append([]byte{2, 0}, bytes.Repeat([]byte{'x'}, 30*1000)...)
	if err := l.send(msg); err != nil {
		t.Fatal(err)
	}
	if n := line.packets(); n != window {
		t.Fatalf("%d packets sent at once", n)
	}
	l.mu.Lock()
	l.acked(5)
	l.mu.Unlock()
	if n := line.packets(); n != window+5 {
		t.Errorf("%d packets after 5 were acknowledged", n)
	}
}
