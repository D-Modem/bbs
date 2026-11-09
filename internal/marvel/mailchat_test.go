package marvel

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// static reads a reply's static dwords and words up to its end-of-static
// tag, and gives what follows it.
func static(t *testing.T, r []byte) (vals []uint32, rest []byte) {
	t.Helper()
	for len(r) > 0 {
		switch r[0] {
		case tagDword:
			vals = append(vals, binary.LittleEndian.Uint32(r[1:]))
			r = r[5:]
		case tagWord:
			vals = append(vals, uint32(binary.LittleEndian.Uint16(r[1:])))
			r = r[3:]
		case tagEndStatic:
			return vals, r[1:]
		default:
			t.Fatalf("reply: tag %#x in %x", r[0], r)
		}
	}
	t.Fatalf("reply without an end of static")
	return
}

// dynamic is a reply's single dynamic value.
func dynamic(t *testing.T, r []byte) []byte {
	t.Helper()
	if len(r) == 0 || r[0] != tagComplete {
		t.Fatalf("no dynamic value: %x", r)
	}
	return r[1:]
}

func signedOn(t *testing.T, srv *Server, name, pw string) *client {
	t.Helper()
	c := dial(t, srv)
	c.open(1, "LOGSRV")
	if r := c.login(name, pw); r != loginOK {
		t.Fatalf("sign-on %s: %#x", name, r)
	}
	return c
}

func TestMail(t *testing.T) {
	srv, st := newServer(t)
	alice, err := st.UserByName("alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser("bob", "password2", "There", false)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.SendMail(&store.Message{FromID: bob.ID, From: "bob", ToID: alice.ID, To: "alice",
		Subject: "Hi", Body: "line one\nline two"})
	if err != nil {
		t.Fatal(err)
	}

	c := signedOn(t, srv, "alice", "password1")
	c.open(3, "MOSRXP")

	ci, rest := static(t, c.call(3, mailIface, mxConnInfo))
	if ci[0] != 0 || len(rest) != 3+0xB8 || !bytes.Equal(rest[3+4:3+20], providerUID) {
		t.Fatalf("conninfo: %x", rest)
	}
	if v, _ := static(t, c.call(3, mailIface, mxOpenInbox)); v[0] != 0 || v[1] != 1 {
		t.Fatalf("open inbox: %v", v)
	}

	v, rest := static(t, c.call(3, mailIface, mxHeaders))
	hdr := dynamic(t, rest)
	if v[0] != 0 || v[1] != 1 || !bytes.Equal(hdr[:12], entryID(id)) {
		t.Fatalf("headers: %v %x", v, hdr)
	}
	r := &mapiReader{b: hdr[12:]}
	hp, err := r.props()
	if err != nil || str(hp, tagSubject) != "Hi" || str(hp, tagSentReprName) != "bob" || r.pos != len(r.b) {
		t.Fatalf("header props: %v %v", hp, err)
	}
	if dwordOf(hp, tagMessageFlags, 0)&msgFlagRead != 0 {
		t.Error("unread mail flagged read")
	}

	_, rest = static(t, c.call(3, mailIface, mxGetMessage, vparam(entryID(id))))
	r = &mapiReader{b: dynamic(t, rest)}
	head, _ := r.props()
	if n, _ := r.u32(); n != 1 {
		t.Fatalf("recipients: %d", n)
	}
	to, _ := r.props()
	body, err := r.props()
	if err != nil || str(head, tagSubject) != "Hi" || str(to, tagEmailAddress) != "alice" ||
		str(body, tagBody) != "line one\r\nline two" || str(body, tagSenderEmail) != "bob" {
		t.Fatalf("message: %v %v %v %v", head, to, body, err)
	}
	if na, _ := r.u32(); na != 0 || r.pos != len(r.b) {
		t.Fatalf("attachments %d, %d bytes left", na, len(r.b)-r.pos)
	}
	if m, _ := st.Mail(alice.ID, id); !m.Read {
		t.Error("mail read but not marked")
	}

	// the whole-message download hands each one out once, then ends
	_, rest = static(t, c.call(3, mailIface, mxGetFirst))
	if !bytes.Equal(dynamic(t, rest)[:12], entryID(id)) {
		t.Fatal("first message")
	}
	if v, _ := static(t, c.call(3, mailIface, mxGetFirst)); v[0] != mapiNotFound {
		t.Fatalf("after the last: %#x", v[0])
	}

	// a message sent: to bob, and to someone there is no such user as
	msg := propList([]mprop{{tagSubject, "Re: Hi"}, {tagMessageClass, "IPM.Note"}})
	msg = binary.LittleEndian.AppendUint32(msg, 2)
	msg = append(msg, propList([]mprop{{tagDisplayName, "bob"}, {tagAddrType, "MSN"}, {tagEmailAddress, "bob"}})...)
	msg = append(msg, propList([]mprop{{tagDisplayName, "carol"}, {tagAddrType, "MSN"}, {tagEmailAddress, "carol"}})...)
	msg = append(msg, propList([]mprop{{tagBody, "Hello back\r\nBye"}})...)
	msg = binary.LittleEndian.AppendUint32(msg, 0)
	if v, _ := static(t, c.call(3, mailIface, mxInitSend)); v[0] != 0 {
		t.Fatalf("init send: %#x", v[0])
	}
	if v, _ := static(t, c.call(3, mailIface, mxSendBlock, dparam(0), dparam(1), vparam(msg))); v[0] != 0 {
		t.Fatalf("send: %#x", v[0])
	}
	box, _ := st.Inbox(bob.ID)
	if len(box) != 1 || box[0].Subject != "Re: Hi" || box[0].From != "alice" || box[0].Body != "Hello back\nBye" {
		t.Fatalf("bob's mail: %+v", box)
	}
	box, _ = st.Inbox(alice.ID)
	var note *store.Message
	for _, m := range box {
		if m.From == "Postmaster" {
			note = m
		}
	}
	if note == nil || !strings.Contains(note.Body, "carol") {
		t.Fatalf("no note of the one not sent: %+v", box)
	}

	// deleted
	c.call(3, mailIface, mxDelete, dparam(1), vparam(entryID(id)))
	box, _ = st.Inbox(alice.ID)
	for _, m := range box {
		if m.ID == id {
			t.Fatal("deleted mail still in the inbox")
		}
	}
}

func TestAddressBook(t *testing.T) {
	srv, st := newServer(t)
	if _, err := st.CreateUser("bob", "password2", "There", false); err != nil {
		t.Fatal(err)
	}
	c := signedOn(t, srv, "alice", "password1")
	c.open(3, "MOSABP")

	v, rest := static(t, c.call(3, abIface, abContainers))
	rec := dynamic(t, rest)
	if v[1] != 1 || len(rec) != 0xDC || unansi(bytes.TrimRight(rec[8:0x48], "\x00")) != "Test BBS members" ||
		binary.LittleEndian.Uint32(rec[0xD0:]) != 2 {
		t.Fatalf("containers: %v %x", v, rec)
	}

	tags := []uint32{tagDisplayName, tagAddrType, tagCity, tagObjectType}
	var tb []byte
	for _, x := range tags {
		tb = binary.LittleEndian.AppendUint32(tb, x)
	}
	_, rest = static(t, c.call(3, abIface, abUserDetails, vparam([]byte("bob\x00")), dparam(uint32(len(tags))), vparam(tb)))
	want := abRow(t, dynamic(t, rest))
	if strings.Join(want, "|") != "bob|MSN|There|6" {
		t.Fatalf("details: %q", want)
	}
	if v, _ := static(t, c.call(3, abIface, abUserDetails, vparam([]byte("nobody\x00")), dparam(1), vparam(tb[:4]))); v[0] != mapiNotFound {
		t.Fatalf("no such member: %#x", v[0])
	}

	v, rest = static(t, c.call(3, abIface, abQueryRows, vparam([]byte{0}), dparam(0), dparam(0), dparam(50), vparam(tb[:4])))
	if len(v) != 5 || v[3] != 2 {
		t.Fatalf("rows: %v", v)
	}
	z := dynamic(t, rest)
	if string(z[:2]) != "CK" {
		t.Fatalf("rows not compressed: %x", z)
	}
	rows, err := io.ReadAll(flate.NewReader(bytes.NewReader(z[2:])))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(rows) > 0 {
		n := int(binary.LittleEndian.Uint32(rows[4:]))
		got = append(got, string(rows[8:8+n]))
		rows = rows[8+n:]
	}
	if strings.Join(got, ",") != "alice,bob" {
		t.Fatalf("members: %q", got)
	}
}

// abRow reads a row of string and dword values (a dword if four bytes
// don't read as a length).
func abRow(t *testing.T, b []byte) []string {
	t.Helper()
	n := int(binary.LittleEndian.Uint32(b))
	b = b[4:]
	var out []string
	for i := 0; i < n; i++ {
		l := int(binary.LittleEndian.Uint32(b))
		if i == n-1 && len(b) == 4 {
			out = append(out, itoa(int64(l)))
			b = b[4:]
			continue
		}
		out = append(out, string(b[4:4+l]))
		b = b[4+l:]
	}
	if len(b) != 0 {
		t.Fatalf("%d bytes after the row", len(b))
	}
	return out
}

// event is a chat event pushed on the join call.
type chatEvent struct {
	kind    uint16
	from    uint32
	payload []byte
}

// nextEvent reads chat events until one of the kind comes.
func (c *client) nextEvent(pipe uint16, kind uint16) chatEvent {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := c.recv()
		h, err := parseHostBlock(r[2:])
		if err != nil || binary.LittleEndian.Uint16(r) != pipe || h.Method != chatJoin || h.Params[0] != tagStreamEnd {
			c.t.Fatalf("not an event: %x", r)
		}
		p := h.Params[1:]
		e := chatEvent{binary.LittleEndian.Uint16(p), binary.LittleEndian.Uint32(p[4:]), p[8:]}
		if e.kind == kind {
			return e
		}
	}
	c.t.Fatalf("no event %d", kind)
	return chatEvent{}
}

// roster is the names in a roster's (or a joined event's) records.
func roster(b []byte) map[string]uint32 {
	out := map[string]uint32{}
	for len(b) >= 14 {
		id := binary.LittleEndian.Uint32(b)
		n := int(binary.LittleEndian.Uint32(b[6:]))
		out[string(b[14:14+n])] = id
		b = b[14+n:]
	}
	return out
}

// joinChat finds the main room in the directory, locates it and joins it.
func joinChat(t *testing.T, c *client) (me uint32) {
	t.Helper()
	c.open(2, "DIRSRV")
	rooms := c.treeCall(2, treeGetChildren, keyChat, 0, "a", "e", "c", "b", "tp")
	if len(rooms) == 0 || string(rooms[0]["e"]) != "main" || binary.LittleEndian.Uint32(rooms[0]["c"]) != appChat ||
		rooms[0]["b"][0] != flagLeaf || string(rooms[0]["tp"]) != "Chat Room" {
		t.Fatalf("rooms: %v", rooms)
	}
	k, _ := keyOf(rooms[0]["a"])
	c.open(4, "CONFLOC")
	if v, _ := static(t, c.call(4, conflocIface, conflocLocate, dparam(k.a))); v[0] != k.a || v[1] != locateFound {
		t.Fatalf("locate: %v", v)
	}
	if v, _ := static(t, c.call(4, conflocIface, conflocLocate, dparam(7))); v[1] != locateNotFound {
		t.Fatalf("locate nowhere: %v", v)
	}
	c.open(5, "CONFSRV")
	r := c.call(5, 0x01, chatJoin, dparam(k.a), []byte{0x85})
	if len(r) < 2+20 || r[0] != tagEndStatic || r[1] != tagStreamEnd || binary.LittleEndian.Uint16(r[2:]) != chatJoined {
		t.Fatalf("join: %x", r)
	}
	rec := r[2:]
	me = binary.LittleEndian.Uint32(rec[4:])
	n := int(binary.LittleEndian.Uint32(rec[14:]))
	if string(bytes.ReplaceAll(rec[18:18+2*n-2], []byte{0}, nil)) != "#main" {
		t.Fatalf("join record: %x", rec)
	}
	return me
}

func say(c *client, me uint32, text string) {
	rec := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, evText), me)
	r := c.call(5, 0x01, chatSend, vparam(append(rec, utf16z(text)...)))
	if !bytes.Equal(r, []byte{tagEndStatic}) {
		c.t.Fatalf("send: %x", r)
	}
}

func TestChat(t *testing.T) {
	srv, st := newServer(t)
	if _, err := st.CreateUser("bob", "password2", "There", false); err != nil {
		t.Fatal(err)
	}
	a := signedOn(t, srv, "alice", "password1")
	am := joinChat(t, a)
	if got := roster(a.nextEvent(5, evList).payload); len(got) != 1 || got["alice"] != am {
		t.Fatalf("alice's roster: %v", got)
	}

	b := signedOn(t, srv, "bob", "password2")
	bm := joinChat(t, b)
	if got := roster(b.nextEvent(5, evList).payload); len(got) != 2 || got["alice"] != am || got["bob"] != bm {
		t.Fatalf("bob's roster: %v", got)
	}
	if e := a.nextEvent(5, evJoined); e.from != bm || roster(e.payload)["bob"] != bm {
		t.Fatalf("bob joined: %+v", e)
	}

	say(a, am, "hello there")
	for _, c := range []*client{b, a} { // the sender's copy comes back too
		if e := c.nextEvent(5, evText); e.from != am || !bytes.Equal(e.payload, utf16z("hello there")) {
			t.Fatalf("text: %+v", e)
		}
	}
	say(b, bm, "/me waves")
	if e := a.nextEvent(5, evText); e.from != bm || !bytes.Equal(e.payload, utf16z("* waves")) {
		t.Fatalf("emote: %+v", e)
	}

	b.c.Close()
	if e := a.nextEvent(5, evLeft); e.from != bm {
		t.Fatalf("bob left: %+v", e)
	}
}
