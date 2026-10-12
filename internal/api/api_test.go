package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Neo-Desktop/bbs-sec/internal/api/pb"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/guard"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

type site struct {
	t    *testing.T
	b    *bbs.BBS
	s    *Server
	ts   *httptest.Server
	logs *bytes.Buffer
}

func setup(t *testing.T, set Settings) *site {
	t.Helper()
	store.BcryptCost = 4
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logs := &bytes.Buffer{}
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test BBS", 0
	b := bbs.New(s, st, log.New(logs, "", 0))
	st.CreateUser("alice", "alicepass1", "Here", false)
	st.CreateUser("bob", "bobpass123", "", false)
	st.CreateUser("sysop", "sysoppass1", "", true)
	st.CreateArea("General", "Talk", false)
	st.CreateArea("News", "From the sysop", true)
	st.CreateFileArea("Uploads", "Files", false)
	if b.Files, err = files.Open(st, filepath.Join(dir, "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	if set.SessionTTL == 0 {
		set.SessionTTL = time.Hour
	}
	set.Anonymous = true
	srv := &Server{B: b, S: set}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &site{t: t, b: b, s: srv, ts: ts, logs: logs}
}

// do makes a request: its status and JSON body.
func (s *site) do(method, path, token string, body any) (int, map[string]any, []any) {
	s.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, s.ts.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	var a []any
	if json.Unmarshal(raw, &m) != nil {
		json.Unmarshal(raw, &a)
	}
	return resp.StatusCode, m, a
}

func (s *site) login(name, pw string) string {
	s.t.Helper()
	code, m, _ := s.do("POST", "/api/v1/login", "", map[string]any{"name": name, "password": pw})
	if code != 200 {
		s.t.Fatalf("login %s: %d %v", name, code, m)
	}
	return m["token"].(string)
}

// key makes an API key of the scopes for a user.
func (s *site) key(name, scopes string) string {
	u, _ := s.b.Store.UserByName(name)
	secret, hash := NewToken()
	s.b.Store.AddToken(&store.Token{UserID: u.ID, Name: "test", Hash: hash, Scopes: scopes})
	return secret
}

func TestREST(t *testing.T) {
	s := setup(t, Settings{})
	if code, m, _ := s.do("GET", "/api/v1/system", "", nil); code != 200 || m["name"] != "Test BBS" || m["users"] != 3.0 {
		t.Fatalf("system: %d %v", code, m)
	}
	if code, _, _ := s.do("POST", "/api/v1/login", "", map[string]any{"name": "alice", "password": "wrong"}); code != 401 {
		t.Errorf("wrong password: %d", code)
	}
	alice := s.login("alice", "alicepass1")
	bob := s.login("bob", "bobpass123")
	if code, m, _ := s.do("GET", "/api/v1/me", alice, nil); code != 200 || m["user"].(map[string]any)["name"] != "alice" {
		t.Fatalf("me: %d %v", code, m)
	}
	for _, c := range []struct {
		token string
		want  int
	}{{"", 401}, {"bbs_nope", 401}, {"Basic x", 401}} {
		if code, _, _ := s.do("GET", "/api/v1/me", c.token, nil); code != c.want {
			t.Errorf("me with %q: %d", c.token, code)
		}
	}

	// areas and messages
	_, _, areas := s.do("GET", "/api/v1/areas", alice, nil)
	if len(areas) != 2 || areas[1].(map[string]any)["can_post"] != false {
		t.Fatalf("areas %v", areas)
	}
	code, m, _ := s.do("POST", "/api/v1/areas/1/messages", alice, map[string]any{"subject": "Hi \x1b[2J", "body": "line one\r\nline two"})
	if code != 201 || m["subject"] != "Hi [2J" || m["body"] != "line one\nline two" || m["from"] != "alice" {
		t.Fatalf("post: %d %v", code, m)
	}
	if code, m, _ := s.do("POST", "/api/v1/areas/2/messages", alice, map[string]any{"subject": "x", "body": "y"}); code != 403 {
		t.Errorf("post to a sysop area: %d %v", code, m)
	}
	if code, _, _ := s.do("POST", "/api/v1/areas/1/messages", alice, map[string]any{"subject": "x", "body": "y", "extra": 1}); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
	if code, _, _ := s.do("POST", "/api/v1/areas/1/messages", alice, strings.Repeat("x", 70<<10)); code != 413 && code != 400 {
		t.Errorf("too big: %d", code)
	}
	s.do("POST", "/api/v1/areas/1/messages", bob, map[string]any{"subject": "Re: Hi", "body": "hello", "reply_to": 1})
	code, m, _ = s.do("GET", "/api/v1/areas/1/messages?limit=1", alice, nil)
	if code != 200 || len(m["messages"].([]any)) != 1 || m["next"] != "/api/v1/areas/1/messages?after=1&limit=1" {
		t.Fatalf("page: %v", m)
	}
	if code, m, _ = s.do("GET", "/api/v1/messages/2", alice, nil); m["reply_to"] != 1.0 {
		t.Errorf("message: %v", m)
	}
	if code, m, _ = s.do("PUT", "/api/v1/areas/1/lastread", alice, map[string]any{"id": 1}); m["unread"] != 1.0 {
		t.Errorf("lastread: %d %v", code, m)
	}

	// mail
	if code, m, _ = s.do("POST", "/api/v1/mail", alice, map[string]any{"to": "bob", "subject": "Psst", "body": "for you"}); code != 202 {
		t.Fatalf("mail: %d %v", code, m)
	}
	if code, m, _ = s.do("POST", "/api/v1/mail", alice, map[string]any{"to": "nobody", "subject": "x", "body": "y"}); code != 400 {
		t.Errorf("mail to nobody: %d", code)
	}
	_, _, in := s.do("GET", "/api/v1/mail", bob, nil)
	if len(in) != 1 || in[0].(map[string]any)["read"] != false {
		t.Fatalf("inbox %v", in)
	}
	if _, m, _ = s.do("GET", "/api/v1/mail/1", bob, nil); m["body"] != "for you" || m["read"] != true {
		t.Errorf("mail: %v", m)
	}
	if code, _, _ = s.do("GET", "/api/v1/mail/1", alice, nil); code != 404 {
		t.Errorf("another's mail: %d", code)
	}
	if code, _, _ = s.do("DELETE", "/api/v1/mail/1", bob, nil); code != 204 {
		t.Errorf("delete: %d", code)
	}

	// scopes
	readOnly := s.key("alice", "read")
	if code, m, _ = s.do("POST", "/api/v1/areas/1/messages", readOnly, map[string]any{"subject": "x", "body": "y"}); code != 403 {
		t.Errorf("read-only post: %d %v", code, m)
	}
	if code, _, _ = s.do("GET", "/api/v1/mail", readOnly, nil); code != 403 {
		t.Errorf("read-only mail: %d", code)
	}
	// a key that says sysop for a non-sysop has no sysop
	fake := s.key("alice", "read sysop")
	if code, _, _ = s.do("GET", "/api/v1/admin/pages", fake, nil); code != 403 {
		t.Errorf("non-sysop with a sysop key: %d", code)
	}

	// files
	code, m, _ = s.do("POST", "/api/v1/fileareas/1/files?name=hello.txt&description=Greeting", alice, []byte("hello, world\n"))
	if code != 201 || m["size"] != 13.0 {
		t.Fatalf("upload: %d %v", code, m)
	}
	for q, want := range map[string]int{"../x": 400, "hello.txt": 409} {
		if code, _, _ = s.do("POST", "/api/v1/fileareas/1/files?name="+q, alice, []byte("x")); code != want {
			t.Errorf("upload %s: %d", q, code)
		}
	}
	if code, _, _ = s.do("POST", "/api/v1/fileareas/1/files?name=big.bin", alice, make([]byte, 2<<20)); code != 413 {
		t.Errorf("too big upload: %d", code)
	}
	req, _ := http.NewRequest("GET", s.ts.URL+"/api/v1/files/1/content", nil)
	req.Header.Set("Authorization", "Bearer "+alice)
	req.Header.Set("Range", "bytes=7-")
	resp, _ := http.DefaultClient.Do(req)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(got) != "world\n" {
		t.Errorf("range download: %d %q", resp.StatusCode, got)
	}
	// an archive: its FILE_ID.DIZ, its listing; finding files
	var zb bytes.Buffer
	z := zip.NewWriter(&zb)
	zw, _ := z.Create("FILE_ID.DIZ")
	zw.Write([]byte("Zork I\nThe Great Underground Empire"))
	z.Close()
	if code, m, _ = s.do("POST", "/api/v1/fileareas/1/files?name=zork.zip", alice, zb.Bytes()); code != 201 ||
		m["description"] != "Zork I" || m["long_description"] != "Zork I\nThe Great Underground Empire" {
		t.Fatalf("zip upload: %d %v", code, m)
	}
	zid := int(m["id"].(float64))
	if code, _, l := s.do("GET", fmt.Sprintf("/api/v1/files/%d/listing", zid), alice, nil); code != 200 || len(l) != 1 {
		t.Errorf("listing: %d %v", code, l)
	}
	if code, _, _ := s.do("GET", "/api/v1/files/1/listing", alice, nil); code != 422 {
		t.Errorf("listing a text file: %d", code)
	}
	if code, _, l := s.do("GET", "/api/v1/files?q=underground", alice, nil); code != 200 || len(l) != 1 {
		t.Errorf("find: %d %v", code, l)
	}
	if code, _, l := s.do("GET", "/api/v1/files?since=2000-01-01T00:00:00Z", alice, nil); code != 200 || len(l) != 2 {
		t.Errorf("since: %d %v", code, l)
	}
	for _, q := range []string{"", "?q=z", "?since=yesterday"} {
		if code, _, _ := s.do("GET", "/api/v1/files"+q, alice, nil); code != 400 {
			t.Errorf("files%s: %d", q, code)
		}
	}

	// sysop
	sys := s.login("sysop", "sysoppass1")
	if code, _, _ = s.do("POST", "/api/v1/admin/users", alice, map[string]any{"name": "carol", "password": "carolpass1"}); code != 403 {
		t.Errorf("alice adds a user: %d", code)
	}
	if code, m, _ = s.do("POST", "/api/v1/admin/users", sys, map[string]any{"name": "carol", "password": "carolpass1"}); code != 201 {
		t.Fatalf("add user: %d %v", code, m)
	}
	carol := s.login("carol", "carolpass1")
	if code, m, _ = s.do("PATCH", "/api/v1/admin/users/carol", sys, map[string]any{"locked": true}); code != 200 || m["locked"] != true {
		t.Fatalf("lock: %d %v", code, m)
	}
	if code, _, _ = s.do("GET", "/api/v1/me", carol, nil); code != 401 {
		t.Errorf("a locked user's token: %d", code)
	}
	if code, m, _ = s.do("PATCH", "/api/v1/admin/areas/1", sys, map[string]any{"newsgroup": "Bad Name"}); code != 400 {
		t.Errorf("bad newsgroup: %d %v", code, m)
	}
	if code, m, _ = s.do("PATCH", "/api/v1/admin/areas/1", sys, map[string]any{"newsgroup": "bbs.general", "echo_tag": "test_echo"}); code != 200 || m["echo_tag"] != "TEST_ECHO" {
		t.Errorf("area: %d %v", code, m)
	}

	// tokens and logout
	_, _, toks := s.do("GET", "/api/v1/me/tokens", alice, nil)
	if len(toks) != 3 {
		t.Errorf("alice's tokens: %v", toks)
	}
	if code, _, _ = s.do("POST", "/api/v1/logout", alice, nil); code != 204 {
		t.Errorf("logout: %d", code)
	}
	if code, _, _ = s.do("GET", "/api/v1/me", alice, nil); code != 401 {
		t.Errorf("after logout: %d", code)
	}
	if strings.Contains(s.logs.String(), "bbs_") {
		t.Error("a token in the log")
	}
}

func TestRateAndOpenAPI(t *testing.T) {
	s := setup(t, Settings{Rate: 1, Burst: 3})
	n429 := 0
	for i := 0; i < 6; i++ {
		if code, _, _ := s.do("GET", "/api/v1/system", "", nil); code == 429 {
			n429++
		}
	}
	if n429 < 2 {
		t.Errorf("%d of 6 limited", n429)
	}
	// every route is in the document, and only they
	doc := map[string]bool{}
	for _, o := range ops {
		doc[o.method+" "+o.path] = true
	}
	for p := range s.s.routes() {
		if !doc[p] {
			t.Errorf("%s is not in the OpenAPI document", p)
		}
		delete(doc, p)
	}
	for p := range doc {
		t.Errorf("the OpenAPI document has %s, which is not a route", p)
	}
	b, err := json.Marshal(document())
	if err != nil || !bytes.Contains(b, []byte(`"openapi":"3.1.0"`)) {
		t.Errorf("document: %v", err)
	}
}

// wsClient is a WebSocket client in either encoding.
type wsClient struct {
	t      *testing.T
	c      *websocket.Conn
	binary bool
	id     uint32
	queue  []*pb.ServerMessage
}

func (s *site) ws(binary bool, token string, header bool) *wsClient {
	s.t.Helper()
	u := "ws" + strings.TrimPrefix(s.ts.URL, "http") + "/api/v1/ws"
	sub := ProtoJSON
	if binary {
		u += "/proto"
		sub = ProtoBinary
	}
	opts := &websocket.DialOptions{Subprotocols: []string{sub}}
	if header && token != "" {
		opts.HTTPHeader = http.Header{"Authorization": {"Bearer " + token}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, u, opts)
	if err != nil {
		s.t.Fatalf("dial: %v", err)
	}
	if resp.Header.Get("Sec-WebSocket-Protocol") != sub {
		s.t.Errorf("subprotocol %q", resp.Header.Get("Sec-WebSocket-Protocol"))
	}
	w := &wsClient{t: s.t, c: c, binary: binary}
	s.t.Cleanup(func() { c.CloseNow() })
	if !header {
		w.send(&pb.ClientMessage{Request: &pb.ClientMessage_Auth{Auth: &pb.Auth{Token: token}}})
	}
	return w
}

func (w *wsClient) send(m *pb.ClientMessage) uint32 {
	w.t.Helper()
	w.id++
	m.Id = w.id
	var b []byte
	typ := websocket.MessageText
	if w.binary {
		b, _ = proto.Marshal(m)
		typ = websocket.MessageBinary
	} else {
		b, _ = protojson.Marshal(m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.c.Write(ctx, typ, b); err != nil {
		w.t.Fatal(err)
	}
	return w.id
}

func (w *wsClient) recv() *pb.ServerMessage {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := w.c.Read(ctx)
	if err != nil {
		w.t.Fatalf("read: %v", err)
	}
	m := &pb.ServerMessage{}
	if w.binary {
		if typ != websocket.MessageBinary {
			w.t.Fatal("a text frame on the protobuf endpoint")
		}
		err = proto.Unmarshal(b, m)
	} else {
		if typ != websocket.MessageText {
			w.t.Fatal("a binary frame on the JSON endpoint")
		}
		err = protojson.Unmarshal(b, m)
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return m
}

// until reads until a message matches (keeping none of the others).
func (w *wsClient) until(what string, ok func(*pb.ServerMessage) bool) *pb.ServerMessage {
	w.t.Helper()
	for i := 0; i < 50; i++ {
		if m := w.recv(); ok(m) {
			return m
		}
	}
	w.t.Fatalf("no %s", what)
	return nil
}

func (w *wsClient) event(kind pb.Event_Kind) *pb.Event {
	w.t.Helper()
	return w.until(kind.String(), func(m *pb.ServerMessage) bool { return m.GetEvent().GetKind() == kind }).GetEvent()
}

func (w *wsClient) reply(id uint32) *pb.ServerMessage {
	w.t.Helper()
	return w.until("a reply", func(m *pb.ServerMessage) bool {
		return m.Id == id && (m.GetOk() != nil || m.GetError() != nil || m.GetNodes() != nil || m.GetPong() != nil || m.GetRooms() != nil)
	})
}

func TestWebSockets(t *testing.T) {
	s := setup(t, Settings{})
	alice := s.ws(false, s.login("alice", "alicepass1"), false) // JSON, Auth message
	sysop := s.ws(true, s.login("sysop", "sysoppass1"), true)   // protobuf, header
	if w := alice.recv().GetWelcome(); w == nil || w.User != "alice" || w.Bbs != "Test BBS" {
		t.Fatalf("alice's welcome: %v", w)
	}
	if w := sysop.recv().GetWelcome(); w == nil || w.User != "sysop" {
		t.Fatalf("sysop's welcome: %v", w)
	}
	// both are nodes, as "web"
	id := alice.send(&pb.ClientMessage{Request: &pb.ClientMessage_Who{Who: &pb.Who{}}})
	if nl := alice.reply(id).GetNodes(); len(nl.GetNodes()) != 2 || nl.Nodes[0].Via != "web" {
		t.Fatalf("who: %v", nl)
	}

	// chat: the protobuf client and the JSON one in a room
	id = sysop.send(&pb.ClientMessage{Request: &pb.ClientMessage_Join{Join: &pb.JoinRoom{Room: "main"}}})
	if r := sysop.reply(id).GetOk(); r.GetRoom() != "main" {
		t.Fatalf("join: %v", r)
	}
	id = alice.send(&pb.ClientMessage{Request: &pb.ClientMessage_Join{Join: &pb.JoinRoom{Room: "main"}}})
	alice.reply(id)
	if e := sysop.event(pb.Event_JOINED); e.From != "alice" {
		t.Errorf("joined: %v", e)
	}
	alice.send(&pb.ClientMessage{Request: &pb.ClientMessage_Say{Say: &pb.Say{Text: "hello from JSON \x1b[31m"}}})
	if e := sysop.event(pb.Event_SAID); e.Text != "hello from JSON [31m" || e.Line != "alice: hello from JSON [31m" || e.Time == nil {
		t.Errorf("said: %v", e)
	}
	sysop.send(&pb.ClientMessage{Request: &pb.ClientMessage_Say{Say: &pb.Say{Text: "waves", Emote: true}}})
	if e := alice.event(pb.Event_EMOTED); e.From != "sysop" {
		t.Errorf("emoted: %v", e)
	}
	id = alice.send(&pb.ClientMessage{Request: &pb.ClientMessage_Command{Command: &pb.ChatCommand{Line: "/who"}}})
	if l := alice.until("lines", func(m *pb.ServerMessage) bool { return m.Id == id && m.GetLines() != nil }).GetLines(); len(l.Lines) != 1 || !strings.HasPrefix(l.Lines[0], "In main:") {
		t.Errorf("/who: %v", l)
	}
	// a command only sysops have
	id = alice.send(&pb.ClientMessage{Request: &pb.ClientMessage_Command{Command: &pb.ChatCommand{Line: "/busy"}}})
	if e := alice.reply(id).GetError(); e.GetCode() != "forbidden" {
		t.Errorf("/busy: %v", e)
	}

	// paging: alice pages, the sysop answers
	id = alice.send(&pb.ClientMessage{Request: &pb.ClientMessage_Page{Page: &pb.Page{Reason: "help"}}})
	r := alice.reply(id)
	if !strings.HasPrefix(r.GetOk().GetRoom(), "sysop-") {
		t.Fatalf("page: %v", r)
	}
	if e := sysop.event(pb.Event_PAGED); e.From != "alice" || e.Text != "help" {
		t.Errorf("paged: %v", e)
	}
	id = sysop.send(&pb.ClientMessage{Request: &pb.ClientMessage_Answer{Answer: &pb.AnswerPage{}}})
	if r := sysop.reply(id).GetOk(); !strings.HasPrefix(r.GetRoom(), "sysop-") {
		t.Fatalf("answer: %v", r)
	}
	if e := alice.event(pb.Event_NOTICE); !strings.Contains(e.Text, "answered your page") {
		t.Errorf("notice: %v", e)
	}
	sysop.send(&pb.ClientMessage{Request: &pb.ClientMessage_Say{Say: &pb.Say{Text: "how can I help?"}}})
	if e := alice.event(pb.Event_SAID); e.From != "sysop" {
		t.Errorf("in the page's room: %v", e)
	}

	// mail comes as an event
	bob := s.login("bob", "bobpass123")
	s.do("POST", "/api/v1/mail", bob, map[string]any{"to": "alice", "subject": "Hello", "body": "x"})
	if e := alice.event(pb.Event_MAIL); e.From != "bob" || e.Text != "Hello" {
		t.Errorf("mail: %v", e)
	}

	// bad input: not a ClientMessage, the wrong kind of frame
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	alice.c.Write(ctx, websocket.MessageText, []byte(`{"nope": 1}`))
	if e := alice.until("an error", func(m *pb.ServerMessage) bool { return m.GetError() != nil }).GetError(); e.Code != "bad_request" {
		t.Errorf("bad message: %v", e)
	}
	sysop.c.Write(ctx, websocket.MessageText, []byte(`{}`))
	if e := sysop.until("an error", func(m *pb.ServerMessage) bool { return m.GetError() != nil }).GetError(); e.Code != "bad_request" {
		t.Errorf("text on proto: %v", e)
	}
}

func TestWebSocketRefusals(t *testing.T) {
	s := setup(t, Settings{})
	// a bad token
	w := s.ws(false, "bbs_wrong", false)
	if e := w.recv().GetError(); e.GetCode() != "unauthorized" {
		t.Errorf("bad token: %v", e)
	}
	// a token without chat
	w = s.ws(true, s.key("alice", "read"), false)
	if e := w.recv().GetError(); e.GetCode() != "forbidden" {
		t.Errorf("no chat scope: %v", e)
	}
	// no auth first
	w = s.ws(false, "", true)
	w.send(&pb.ClientMessage{Request: &pb.ClientMessage_Ping{Ping: &pb.Ping{}}})
	if e := w.recv().GetError(); e.GetCode() != "unauthorized" {
		t.Errorf("ping before auth: %v", e)
	}
	// all lines busy
	s.b.MaxNodes = 0
	w = s.ws(false, s.login("alice", "alicepass1"), true)
	if e := w.recv().GetError(); e.GetCode() != "unavailable" {
		t.Errorf("busy: %v", e)
	}
}

func TestEventsAndNodeMessages(t *testing.T) {
	s := setup(t, Settings{})
	alice := s.login("alice", "alicepass1")
	bob := s.login("bob", "bobpass123")
	// a node message needs a stream
	if code, _, _ := s.do("POST", "/api/v1/nodes/1/message", bob, map[string]any{"text": "hi"}); code != 409 {
		t.Errorf("no stream: %d", code)
	}
	req, _ := http.NewRequest("GET", s.ts.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+alice)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: %v %v", err, resp.Header)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	line := func() string {
		l, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimRight(l, "\n")
	}
	for l := line(); !strings.HasPrefix(l, "data: {\"node\":1"); l = line() {
	}
	// bob opens a WebSocket (his node) and messages alice's node
	w := s.ws(false, bob, true)
	w.recv()
	if code, m, _ := s.do("POST", "/api/v1/nodes/1/message", bob, map[string]any{"text": "over here"}); code != 202 {
		t.Fatalf("node message: %d %v", code, m)
	}
	var ev, data string
	for ev == "" || data == "" {
		l := line()
		if v, ok := strings.CutPrefix(l, "event: "); ok {
			ev = v
		}
		if v, ok := strings.CutPrefix(l, "data: "); ok && ev != "" {
			data = v
		}
	}
	e := &pb.Event{}
	if ev != "private" || protojson.Unmarshal([]byte(data), e) != nil || e.From != "bob" || e.Text != "over here" {
		t.Errorf("event %q %q", ev, data)
	}
	// who's online shows them both, as web
	_, _, who := s.do("GET", "/api/v1/who", "", nil)
	if len(who) != 2 {
		t.Errorf("who: %v", who)
	}
}

func TestMetrics(t *testing.T) {
	s := setup(t, Settings{})
	s.login("alice", "alicepass1")
	rec := httptest.NewRecorder()
	s.s.metrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{"bbs_users 3\n", "bbs_logins_total 1\n", "# TYPE bbs_posts_total counter", "bbs_nodes 0\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in\n%s", want, body)
		}
	}
}

// TestSSHKeys: a user's SSH keys: added only from a login's session.
func TestSSHKeys(t *testing.T) {
	s := setup(t, Settings{})
	sess := s.login("alice", "alicepass1")
	apiKey := s.key("alice", "read post mail chat files")
	const line = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl alice@home"
	if code, m, _ := s.do("POST", "/api/v1/me/keys", apiKey, map[string]any{"key": line}); code != 403 {
		t.Fatalf("by API key: %d %v", code, m)
	}
	code, m, _ := s.do("POST", "/api/v1/me/keys", sess, map[string]any{"key": line})
	if code != 201 || m["type"] != "ssh-ed25519" || m["comment"] != "alice@home" ||
		!strings.HasPrefix(m["fingerprint"].(string), "SHA256:") {
		t.Fatalf("add: %d %v", code, m)
	}
	id := int(m["id"].(float64))
	bob := s.login("bob", "bobpass123")
	if code, _, _ := s.do("POST", "/api/v1/me/keys", bob, map[string]any{"key": line}); code != 409 {
		t.Fatalf("someone else's key: %d", code)
	}
	for _, bad := range []string{"nope", `command="sh" ` + line, "ssh-dss AAAAB3NzaC1kc3MAAACBAP"} {
		if code, m, _ := s.do("POST", "/api/v1/me/keys", sess, map[string]any{"key": bad}); code != 400 {
			t.Fatalf("%q: %d %v", bad, code, m)
		}
	}
	if _, _, ks := s.do("GET", "/api/v1/me/keys", apiKey, nil); len(ks) != 1 {
		t.Fatalf("list: %v", ks)
	}
	if code, _, _ := s.do("DELETE", fmt.Sprintf("/api/v1/me/keys/%d", id), bob, nil); code != 404 {
		t.Fatalf("bob deleted alice's key: %d", code)
	}
	if code, _, _ := s.do("DELETE", fmt.Sprintf("/api/v1/me/keys/%d", id), sess, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if _, _, ks := s.do("GET", "/api/v1/me/keys", sess, nil); len(ks) != 0 {
		t.Fatalf("after delete: %v", ks)
	}
}

// TestAccess: levels and requirements through the API; a user cannot
// change their own access.
func TestAccess(t *testing.T) {
	s := setup(t, Settings{})
	s.b.ValidatedLevel = 20
	alice, sysop := s.login("alice", "alicepass1"), s.login("sysop", "sysoppass1")
	// no raising one's own level or flags
	for _, body := range []map[string]any{{"level": 255}, {"flags": "ABC"}} {
		if code, m, _ := s.do("PATCH", "/api/v1/me", alice, body); code != 400 {
			t.Fatalf("PATCH /me %v: %d %v", body, code, m)
		}
	}
	if code, _, _ := s.do("PATCH", "/api/v1/admin/users/alice", alice, map[string]any{"level": 255}); code != 403 {
		t.Fatalf("alice as admin: %d", code)
	}
	_, me, _ := s.do("GET", "/api/v1/me", alice, nil)
	if acc := me["access"].(map[string]any); acc["level"] != 20.0 || acc["validated"] != true {
		t.Fatalf("access: %v", acc)
	}
	// settings: kept, checked, forgotten
	if code, m, _ := s.do("PATCH", "/api/v1/me", alice, map[string]any{"prefs": map[string]string{"ansi": "off", "editor": "line"}}); code != 200 ||
		m["prefs"].(map[string]any)["ansi"] != "off" {
		t.Fatalf("prefs: %d %v", code, m)
	}
	for _, bad := range []map[string]string{{"ansi": "maybe"}, {"colour": "on"}} {
		if code, _, _ := s.do("PATCH", "/api/v1/me", alice, map[string]any{"prefs": bad}); code != 400 {
			t.Errorf("prefs %v: %d", bad, code)
		}
	}
	s.do("PATCH", "/api/v1/me", alice, map[string]any{"prefs": map[string]string{"ansi": ""}})
	if u, _ := s.b.Store.UserByName("alice"); u.Prefs != "editor=line" {
		t.Errorf("kept %q", u.Prefs)
	}
	// a sysop sets them
	code, m, _ := s.do("PATCH", "/api/v1/admin/users/alice", sysop, map[string]any{"level": 5, "flags": "+F"})
	if code != 200 || m["level"] != 5.0 || m["flags"] != "F" {
		t.Fatalf("set: %d %v", code, m)
	}
	if code, _, _ := s.do("PATCH", "/api/v1/admin/users/alice", sysop, map[string]any{"level": 300}); code != 400 {
		t.Fatalf("level 300: %d", code)
	}
	// level and flags are the user's and the sysop's to see
	bob := s.login("bob", "bobpass123")
	if _, u, _ := s.do("GET", "/api/v1/users/alice", bob, nil); u["level"] != nil || u["flags"] != nil {
		t.Errorf("bob sees alice's access: %v", u)
	}
	if _, u, _ := s.do("GET", "/api/v1/users/alice", alice, nil); u["level"] != 5.0 {
		t.Errorf("alice's own: %v", u)
	}
	// an area for level 20: hidden from alice, its messages too
	_, _, list := s.do("GET", "/api/v1/areas", alice, nil)
	before := len(list)
	_, _, all := s.do("GET", "/api/v1/areas", sysop, nil)
	id := int(all[0].(map[string]any)["id"].(float64))
	code, m, _ = s.do("PATCH", fmt.Sprintf("/api/v1/admin/areas/%d", id), sysop,
		map[string]any{"read_require": "20", "post_require": "20 F"})
	if code != 200 || m["read_require"] != "20" {
		t.Fatalf("require: %d %v", code, m)
	}
	if code, _, _ := s.do("PATCH", fmt.Sprintf("/api/v1/admin/areas/%d", id), sysop, map[string]any{"read_require": "300"}); code != 400 {
		t.Fatalf("a bad requirement: %d", code)
	}
	_, _, list = s.do("GET", "/api/v1/areas", alice, nil)
	if len(list) != before-1 {
		t.Errorf("alice sees %d areas, was %d", len(list), before)
	}
	if code, _, _ := s.do("GET", fmt.Sprintf("/api/v1/areas/%d/messages", id), alice, nil); code != 404 {
		t.Errorf("alice read a hidden area: %d", code)
	}
	if code, _, _ := s.do("POST", fmt.Sprintf("/api/v1/areas/%d/messages", id), bob, map[string]any{"subject": "s", "body": "b"}); code != 403 {
		t.Errorf("bob posted without F: %d", code)
	}
}

func TestBansAndHackLog(t *testing.T) {
	s := setup(t, Settings{})
	s.b.Guard = &guard.Guard{HackLog: filepath.Join(t.TempDir(), "hack.log"), BanTime: time.Hour}
	s.b.Guard.Ban("192.0.2.9", "too many failed logins")
	sys, alice := s.login("sysop", "sysoppass1"), s.login("alice", "alicepass1")
	if code, _, _ := s.do("GET", "/api/v1/admin/bans", alice, nil); code != 403 {
		t.Errorf("alice: %d", code)
	}
	if code, _, l := s.do("GET", "/api/v1/admin/bans", sys, nil); code != 200 || len(l) != 1 ||
		l[0].(map[string]any)["ip"] != "192.0.2.9" {
		t.Errorf("bans: %d %v", code, l)
	}
	if code, _, _ := s.do("DELETE", "/api/v1/admin/bans/192.0.2.9", sys, nil); code != 204 {
		t.Errorf("unban: %d", code)
	}
	if code, _, _ := s.do("DELETE", "/api/v1/admin/bans/192.0.2.9", sys, nil); code != 404 {
		t.Errorf("unban again: %d", code)
	}
	code, _, l := s.do("GET", "/api/v1/admin/hacklog?lines=5", sys, nil)
	if code != 200 || len(l) != 2 || !strings.Contains(l[1].(string), "192.0.2.9 ban lifted by sysop") {
		t.Errorf("hack log: %d %v", code, l)
	}
}

func TestCallersAndAbout(t *testing.T) {
	s := setup(t, Settings{})
	alice := s.login("alice", "alicepass1")
	n, _ := s.b.Enter(&bbs.Caller{Via: "telnet"}, nil)
	if _, err := n.Login("bob", "bobpass123"); err != nil {
		t.Fatal(err)
	}
	n.Leave()
	if code, _, l := s.do("GET", "/api/v1/callers", alice, nil); code != 200 || len(l) != 1 ||
		l[0].(map[string]any)["user"] != "bob" || l[0].(map[string]any)["via"] != "telnet" {
		t.Errorf("callers: %d %v", code, l)
	}
	if code, m, _ := s.do("PATCH", "/api/v1/me", alice, map[string]any{"about": "I like \x1b[2Jterminals"}); code != 200 ||
		m["user"].(map[string]any)["about"] != "I like [2Jterminals" {
		t.Errorf("about: %d %v", code, m)
	}
	if _, m, _ := s.do("GET", "/api/v1/users/alice", alice, nil); m["about"] != "I like [2Jterminals" {
		t.Errorf("user: %v", m)
	}
}

func TestEventsAndAvailable(t *testing.T) {
	s := setup(t, Settings{})
	s.b.Events = &events.Scheduler{Events: []*events.Event{{Name: "hello", Command: []string{"/bin/echo", "hi"}, Every: time.Hour}}}
	sys := s.login("sysop", "sysoppass1")
	code, m, _ := s.do("POST", "/api/v1/admin/events/hello/run", sys, nil)
	if code != 200 || m["ok"] != true || m["output"] != "hi\n" || m["by"] != "sysop" {
		t.Errorf("run: %d %v", code, m)
	}
	if code, _, _ := s.do("POST", "/api/v1/admin/events/nope/run", sys, nil); code != 404 {
		t.Errorf("no such event: %d", code)
	}
	if code, _, l := s.do("GET", "/api/v1/admin/events", sys, nil); code != 200 || len(l) != 1 ||
		l[0].(map[string]any)["last"].(map[string]any)["ok"] != true {
		t.Errorf("events: %d %v", code, l)
	}
	if code, m, _ := s.do("PUT", "/api/v1/admin/available", sys, map[string]any{"available": false}); code != 200 ||
		m["available"] != false || s.b.SysopAvailable() {
		t.Errorf("away: %d %v", code, m)
	}
	if code, _, _ := s.do("PUT", "/api/v1/admin/available", sys, map[string]any{}); code != 400 {
		t.Errorf("nothing said: %d", code)
	}
}

func TestVotesPollsThreads(t *testing.T) {
	s := setup(t, Settings{})
	alice, bob, sys := s.login("alice", "alicepass1"), s.login("bob", "bobpass123"), s.login("sysop", "sysoppass1")
	if code, m, _ := s.do("PATCH", "/api/v1/admin/areas/1", sys, map[string]any{"group": "Local", "aliases": true,
		"max_messages": 500, "max_days": 90}); code != 200 || m["group"] != "Local" || m["aliases"] != true || m["max_messages"] != 500.0 {
		t.Fatalf("area: %d %v", code, m)
	}
	if code, _, _ := s.do("PATCH", "/api/v1/admin/areas/1", sys, map[string]any{"max_days": -1}); code != 400 {
		t.Errorf("negative days: %d", code)
	}
	if code, _, _ := s.do("POST", "/api/v1/areas/1/messages", alice, map[string]any{"subject": "x", "body": "y", "alias": true}); code != 400 {
		t.Errorf("an alias alice has not: %d", code)
	}
	u, _ := s.b.Store.UserByName("alice")
	s.b.Store.SetAlias(u.ID, "Night Owl")
	code, m, _ := s.do("POST", "/api/v1/areas/1/messages", alice, map[string]any{"subject": "Lunch?", "body": "Where?",
		"alias": true, "poll": []string{"Here", "There"}})
	if code != 201 || m["from"] != "Night Owl" {
		t.Fatalf("poll: %d %v", code, m)
	}
	id := int(m["id"].(float64))
	_, m, _ = s.do("POST", "/api/v1/areas/1/messages", bob, map[string]any{"subject": "Re: Lunch?", "body": "Here", "reply_to": id})
	reply := int(m["id"].(float64))
	if code, _, _ := s.do("POST", fmt.Sprintf("/api/v1/messages/%d/vote", id), alice, map[string]any{"value": 1}); code != 400 {
		t.Errorf("own vote: %d", code)
	}
	if code, m, _ := s.do("POST", fmt.Sprintf("/api/v1/messages/%d/vote", id), bob, map[string]any{"value": 1}); code != 200 ||
		m["votes"].(map[string]any)["up"] != 1.0 {
		t.Errorf("vote: %d %v", code, m)
	}
	if code, m, _ := s.do("POST", fmt.Sprintf("/api/v1/messages/%d/answer", id), bob, map[string]any{"answer": 1}); code != 200 ||
		m["poll"].(map[string]any)["mine"] != 1.0 {
		t.Errorf("answer: %d %v", code, m)
	}
	for _, bad := range []map[string]any{{"answer": 2}, {}} {
		if code, _, _ := s.do("POST", fmt.Sprintf("/api/v1/messages/%d/answer", id), bob, bad); code != 400 {
			t.Errorf("answer %v: %d", bad, code)
		}
	}
	if code, _, l := s.do("GET", fmt.Sprintf("/api/v1/messages/%d/thread", reply), alice, nil); code != 200 || len(l) != 2 ||
		l[1].(map[string]any)["depth"] != 1.0 {
		t.Errorf("thread: %d %v", code, l)
	}
}

func TestQWK(t *testing.T) {
	s := setup(t, Settings{})
	gen, _ := s.b.Store.AreaByName("General")
	bob, _ := s.b.Store.UserByName("bob")
	s.b.Agent(bob, "test").Post(gen, "All", "Offline", []string{"For the packet."}, 0)
	alice := s.login("alice", "alicepass1")
	get := func(q string) ([]byte, http.Header) {
		req, _ := http.NewRequest("GET", s.ts.URL+"/api/v1/qwk"+q, nil)
		req.Header.Set("Authorization", "Bearer "+alice)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("qwk: %d %s", resp.StatusCode, data)
		}
		return data, resp.Header
	}
	data, h := get("?mark=no")
	if h.Get("X-QWK-Messages") != "1" || !strings.Contains(h.Get("Content-Disposition"), "TESTBBS.QWK") {
		t.Errorf("headers %v", h)
	}
	if p, err := qwk.ReadPacket(data); err != nil || len(p.Messages) != 1 {
		t.Fatalf("packet %v %v", p, err)
	}
	if _, h = get(""); h.Get("X-QWK-Messages") != "1" {
		t.Error("mark=no marked")
	}
	if _, h = get(""); h.Get("X-QWK-Messages") != "0" {
		t.Error("not marked")
	}

	var rep bytes.Buffer
	qwk.WriteREP(&rep, "TESTBBS", []qwk.Message{{Conf: int(gen.ID), To: "bob", Subject: "Re: Offline", Body: "Got it."},
		{Conf: 0, To: "nobody", Subject: "Lost", Body: "x"}})
	code, m, _ := s.do("POST", "/api/v1/qwk/rep", alice, rep.Bytes())
	if code != 200 || m["posted"] != 1.0 || len(m["errors"].([]any)) != 1 {
		t.Errorf("rep: %d %v", code, m)
	}
	if code, m, _ := s.do("POST", "/api/v1/qwk/rep", alice, "junk"); code != 400 {
		t.Errorf("junk: %d %v", code, m)
	}
	readOnly := s.key("alice", "mail")
	if code, _, _ := s.do("POST", "/api/v1/qwk/rep", readOnly, rep.Bytes()); code != 403 {
		t.Errorf("no post scope: %d", code)
	}
}
