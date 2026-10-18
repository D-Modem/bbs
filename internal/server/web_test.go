package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Neo-Desktop/bbs-sec/internal/api"
	"github.com/Neo-Desktop/bbs-sec/internal/api/pb"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// webClient is a WebSocket client of the API, in JSON.
type webClient struct {
	t  *testing.T
	c  *websocket.Conn
	id uint32
}

// send sends m as JSON with the next id, and is that id.
func (w *webClient) send(m *pb.ClientMessage) uint32 {
	w.t.Helper()
	w.id++
	m.Id = w.id
	b, err := protojson.Marshal(m)
	if err != nil {
		w.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.c.Write(ctx, websocket.MessageText, b); err != nil {
		w.t.Fatalf("write: %v", err)
	}
	return w.id
}

// until reads messages until one passes ok.
func (w *webClient) until(what string, ok func(*pb.ServerMessage) bool) *pb.ServerMessage {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := w.c.Read(ctx)
		if err != nil {
			w.t.Fatalf("waiting for %s: %v", what, err)
		}
		m := &pb.ServerMessage{}
		if err := protojson.Unmarshal(b, m); err != nil {
			w.t.Fatalf("%s: %v", b, err)
		}
		if ok(m) {
			return m
		}
	}
}

// event waits for an event whose line has want.
func (w *webClient) event(want string) *pb.Event {
	w.t.Helper()
	return w.until(want, func(m *pb.ServerMessage) bool {
		return m.GetEvent() != nil && strings.Contains(m.GetEvent().Line, want)
	}).GetEvent()
}

// answer waits for request id's Ok (failing on its Error).
func (w *webClient) answer(id uint32) *pb.Ok {
	w.t.Helper()
	m := w.until("an answer", func(m *pb.ServerMessage) bool {
		return m.Id == id && (m.GetOk() != nil || m.GetError() != nil)
	})
	if m.GetError() != nil {
		w.t.Fatalf("request %d: %v", id, m.GetError())
	}
	return m.GetOk()
}

// TestChatWeb: a terminal caller and a web caller (the JSON WebSocket) in
// the same chat room, node messages both ways, and the web caller paging
// the sysop at the terminal.
func TestChatWeb(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	srv := &api.Server{B: e.b, S: api.Settings{SessionTTL: time.Hour}}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	u, err := e.b.Store.UserByName("alice")
	if err != nil {
		t.Fatal(err)
	}
	secret, hash := api.NewToken()
	if _, err := e.b.Store.AddToken(&store.Token{UserID: u.ID, Name: "test", Hash: hash, Scopes: "read chat"}); err != nil {
		t.Fatal(err)
	}

	sysop := login(e, "sysop", "sysoppass1")
	sysop.send("C")
	sysop.expect("You are in main.")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/ws",
		&websocket.DialOptions{Subprotocols: []string{api.ProtoJSON}, HTTPHeader: http.Header{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	web := &webClient{t: t, c: c}
	web.send(&pb.ClientMessage{Request: &pb.ClientMessage_Auth{Auth: &pb.Auth{Token: secret}}})
	wel := web.until("welcome", func(m *pb.ServerMessage) bool { return m.GetWelcome() != nil }).GetWelcome()
	if wel.User != "alice" || wel.Node != 2 {
		t.Fatalf("welcome: %v", wel)
	}

	// the same room: each sees the other's lines
	id := web.send(&pb.ClientMessage{Request: &pb.ClientMessage_Join{Join: &pb.JoinRoom{Room: "main"}}})
	if ok := web.answer(id); ok.Room != "main" {
		t.Fatalf("join: %v", ok)
	}
	sysop.expect("-> alice came in")
	web.send(&pb.ClientMessage{Request: &pb.ClientMessage_Say{Say: &pb.Say{Text: "hello from the web"}}})
	sysop.expect("alice: hello from the web")
	web.send(&pb.ClientMessage{Request: &pb.ClientMessage_Say{Say: &pb.Say{Text: "waves", Emote: true}}})
	sysop.expect("* alice waves")
	sysop.send("hello alice\r")
	if ev := web.event("sysop: hello alice"); ev.Kind != pb.Event_SAID || ev.From != "sysop" || ev.Node != 1 || ev.Room != "main" {
		t.Fatalf("said: %v", ev)
	}
	sysop.send("/who\r")
	sysop.expect("In main: alice (2), sysop (1)")

	// node messages both ways
	sysop.send("/msg 2 are you on the web?\r")
	if ev := web.event("are you on the web?"); ev.Kind != pb.Event_PRIVATE || ev.From != "sysop" {
		t.Fatalf("private: %v", ev)
	}
	id = web.send(&pb.ClientMessage{Request: &pb.ClientMessage_NodeMessage{NodeMessage: &pb.NodeMessage{Node: 1, Text: "I am"}}})
	web.answer(id)
	sysop.expect("** Message from alice (node 2): I am **")

	// the web caller pages; the sysop answers at the terminal
	id = web.send(&pb.ClientMessage{Request: &pb.ClientMessage_Page{Page: &pb.Page{Reason: "help with a file"}}})
	if ok := web.answer(id); ok.Room != "sysop-2" {
		t.Fatalf("page: %v", ok)
	}
	sysop.expect("** alice (node 2) is paging you: help with a file  (Chat, then /answer) **")
	sysop.expect("<- alice left")
	sysop.send("/answer\r")
	sysop.expect("You are with the caller on node 2 (room sysop-2).")
	web.event("sysop answered your page.")
	sysop.send("which file?\r")
	web.event("sysop: which file?")
	web.send(&pb.ClientMessage{Request: &pb.ClientMessage_Say{Say: &pb.Say{Text: "doom.zip"}}})
	sysop.expect("alice: doom.zip")

	// the web caller goes: the terminal sees it leave
	c.Close(websocket.StatusNormalClosure, "")
	sysop.expect("<- alice left")
}
