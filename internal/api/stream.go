package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Neo-Desktop/bbs-sec/internal/api/pb"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// Subprotocols of the WebSocket endpoints.
const (
	ProtoJSON   = "bbs.v1.json"
	ProtoBinary = "bbs.v1.proto"
)

var kinds = map[bbs.EventKind]pb.Event_Kind{
	bbs.Said: pb.Event_SAID, bbs.Emoted: pb.Event_EMOTED, bbs.Joined: pb.Event_JOINED,
	bbs.Left: pb.Event_LEFT, bbs.Private: pb.Event_PRIVATE, bbs.Paged: pb.Event_PAGED,
	bbs.Notice: pb.Event_NOTICE, bbs.Mail: pb.Event_MAIL,
}

func pbEvent(e bbs.Event) *pb.Event {
	return &pb.Event{Kind: kinds[e.Kind], Room: e.Room, From: e.From, Node: int32(e.Node),
		Text: e.Text, Time: timestamppb.New(e.Time), Line: e.String()}
}

// openStream gives a stream's client a node of its own: in who's online
// (as "web"), in chat, reached by node messages and pages.
func (s *Server) openStream(r *http.Request, wh *who) (*bbs.Node, func(), error) {
	host := hostOf(r)
	n, err := s.B.Enter(&bbs.Caller{Via: "web", Listener: "http", Remote: host, Source: "ip:" + host}, nil)
	if err != nil {
		return nil, nil, err
	}
	n.SetUser(wh.user)
	n.Doing("Web client")
	id := wh.token.ID
	s.mu.Lock()
	s.streams[id] = append(s.streams[id], n)
	s.mu.Unlock()
	s.B.Log.Printf("node %d: %s on the web (%s)", n.Num, wh.user.Name, host)
	return n, func() {
		s.mu.Lock()
		ns := s.streams[id]
		for i, x := range ns {
			if x == n {
				ns = append(ns[:i], ns[i+1:]...)
				break
			}
		}
		if len(ns) == 0 {
			delete(s.streams, id)
		} else {
			s.streams[id] = ns
		}
		s.mu.Unlock()
		n.Leave()
		s.B.Log.Printf("node %d: gone (web)", n.Num)
	}, nil
}

// events is the event stream: server-sent events, each an Event in the
// WebSocket's JSON.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	wh := whoOf(r)
	if !wh.has("chat") && !wh.has("read") {
		fail(w, http.StatusForbidden, "the token has no chat or read scope")
		return
	}
	rc := http.NewResponseController(w)
	n, done, err := s.openStream(r, wh)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer done()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: 5000\nevent: welcome\ndata: {\"node\":%d,\"user\":%q}\n\n", n.Num, wh.user.Name)
	if rc.Flush() != nil {
		return
	}
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			fmt.Fprint(w, ": still here\n\n")
		case <-n.Wake():
			for _, e := range n.Pending() {
				b, _ := protojson.Marshal(pbEvent(e))
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", strings.ToLower(kinds[e.Kind].String()), b)
			}
		}
		if rc.Flush() != nil {
			return
		}
	}
}

func (s *Server) wsJSON(w http.ResponseWriter, r *http.Request)  { s.ws(w, r, false) }
func (s *Server) wsProto(w http.ResponseWriter, r *http.Request) { s.ws(w, r, true) }

// wsConn is a WebSocket client: its encoding, and one writer at a time.
type wsConn struct {
	c      *websocket.Conn
	binary bool
	mu     sync.Mutex
}

func (wc *wsConn) send(ctx context.Context, m *pb.ServerMessage) error {
	var b []byte
	var err error
	typ := websocket.MessageText
	if wc.binary {
		b, err = proto.Marshal(m)
		typ = websocket.MessageBinary
	} else {
		b, err = protojson.MarshalOptions{EmitUnpopulated: false}.Marshal(m)
	}
	if err != nil {
		return err
	}
	wc.mu.Lock()
	defer wc.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return wc.c.Write(ctx, typ, b)
}

func (wc *wsConn) read(ctx context.Context) (*pb.ClientMessage, error) {
	typ, b, err := wc.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	m := &pb.ClientMessage{}
	if wc.binary {
		if typ != websocket.MessageBinary {
			return nil, errBadFrame
		}
		err = proto.Unmarshal(b, m)
	} else {
		if typ != websocket.MessageText {
			return nil, errBadFrame
		}
		err = protojson.Unmarshal(b, m)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBadMessage, err)
	}
	return m, nil
}

var (
	errBadFrame   = errors.New("wrong kind of frame for this endpoint")
	errBadMessage = errors.New("not a ClientMessage")
)

func errMsg(id uint32, code, msg string) *pb.ServerMessage {
	return &pb.ServerMessage{Id: id, Body: &pb.ServerMessage_Error{Error: &pb.Error{Code: code, Message: msg}}}
}

func okMsg(id uint32, room string) *pb.ServerMessage {
	return &pb.ServerMessage{Id: id, Body: &pb.ServerMessage_Ok{Ok: &pb.Ok{Room: room}}}
}

func linesMsg(id uint32, ls []string) *pb.ServerMessage {
	return &pb.ServerMessage{Id: id, Body: &pb.ServerMessage_Lines{Lines: &pb.Lines{Lines: ls}}}
}

// codeOf is an error's code for the client.
func codeOf(err error) string {
	switch {
	case errors.Is(err, bbs.ErrFlood):
		return "too_many"
	case errors.Is(err, bbs.ErrNoNode):
		return "not_found"
	case errors.Is(err, bbs.ErrNotInRoom):
		return "conflict"
	case errors.Is(err, bbs.ErrNotYours):
		return "forbidden"
	}
	return "bad_request"
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request, binary bool) {
	sub := ProtoJSON
	if binary {
		sub = ProtoBinary
	}
	opts := &websocket.AcceptOptions{Subprotocols: []string{sub}}
	if s.S.CORS != "" {
		if u, err := url.Parse(s.S.CORS); err == nil {
			opts.OriginPatterns = []string{u.Host}
		}
	}
	wh := whoOf(r)
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(16 << 10)
	wc := &wsConn{c: c, binary: binary}
	ctx := r.Context()

	// who: from the upgrade's Authorization, or the first message
	if wh == nil {
		actx, cancel := context.WithTimeout(ctx, 10*time.Second)
		m, err := wc.read(actx)
		cancel()
		if err != nil {
			c.Close(websocket.StatusPolicyViolation, "authenticate first")
			return
		}
		auth := m.GetAuth()
		if auth == nil {
			wc.send(ctx, errMsg(m.Id, "unauthorized", "the first message must be an Auth"))
			c.Close(websocket.StatusPolicyViolation, "authenticate first")
			return
		}
		if !s.allow("ip:" + hostOf(r)) {
			c.Close(websocket.StatusTryAgainLater, "too many requests")
			return
		}
		if wh, err = s.byToken(auth.Token); err != nil {
			time.Sleep(s.B.FailDelay)
			wc.send(ctx, errMsg(m.Id, "unauthorized", err.Error()))
			c.Close(websocket.StatusPolicyViolation, "unauthorized")
			return
		}
	}
	if !wh.has("chat") {
		wc.send(ctx, errMsg(0, "forbidden", "the token has no chat scope"))
		c.Close(websocket.StatusPolicyViolation, "forbidden")
		return
	}
	n, done, err := s.openStream(r, wh)
	if err != nil {
		wc.send(ctx, errMsg(0, "unavailable", err.Error()))
		c.Close(websocket.StatusTryAgainLater, "busy")
		return
	}
	defer done()
	var scopes []string
	for k := range wh.scopes {
		scopes = append(scopes, k)
	}
	if err := wc.send(ctx, &pb.ServerMessage{Body: &pb.ServerMessage_Welcome{Welcome: &pb.Welcome{
		Bbs: s.B.Name, User: wh.user.Name, Node: int32(n.Num), Scopes: scopes}}}); err != nil {
		return
	}

	// events as they come, and pings to see the client is there
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ping := time.NewTicker(30 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				pctx, pc := context.WithTimeout(ctx, 20*time.Second)
				err := c.Ping(pctx)
				pc()
				if err != nil {
					cancel()
					return
				}
			case <-n.Wake():
				for _, e := range n.Pending() {
					if wc.send(ctx, &pb.ServerMessage{Body: &pb.ServerMessage_Event{Event: pbEvent(e)}}) != nil {
						cancel()
						return
					}
				}
			}
		}
	}()

	key := fmt.Sprintf("token:%d", wh.token.ID)
	for {
		m, err := wc.read(ctx)
		if err != nil {
			if errors.Is(err, errBadMessage) || errors.Is(err, errBadFrame) {
				wc.send(ctx, errMsg(0, "bad_request", err.Error()))
				continue
			}
			return
		}
		if !s.allow(key) {
			wc.send(ctx, errMsg(m.Id, "too_many", "too many requests: slow down"))
			continue
		}
		for _, out := range s.dispatch(n, wh, m) {
			if wc.send(ctx, out) != nil {
				return
			}
		}
	}
}

// dispatch does a client's request: the answers to send.
func (s *Server) dispatch(n *bbs.Node, wh *who, m *pb.ClientMessage) []*pb.ServerMessage {
	id := m.Id
	fails := func(err error) []*pb.ServerMessage { return []*pb.ServerMessage{errMsg(id, codeOf(err), err.Error())} }
	ok := []*pb.ServerMessage{okMsg(id, "")}
	switch req := m.Request.(type) {
	case *pb.ClientMessage_Auth:
		return []*pb.ServerMessage{errMsg(id, "bad_request", "already logged in")}
	case *pb.ClientMessage_Ping:
		return []*pb.ServerMessage{{Id: id, Body: &pb.ServerMessage_Pong{Pong: &pb.Pong{}}}}
	case *pb.ClientMessage_Join:
		hist, err := n.JoinRoom(req.Join.Room)
		if err != nil {
			return fails(err)
		}
		n.Doing("Web chat in " + n.Room())
		var ls []string
		for _, e := range hist {
			ls = append(ls, e.String())
		}
		return []*pb.ServerMessage{linesMsg(id, ls), okMsg(id, n.Room())}
	case *pb.ClientMessage_Leave:
		n.LeaveRoom()
		n.Doing("Web client")
		return ok
	case *pb.ClientMessage_Say:
		if err := n.Say(req.Say.Text, req.Say.Emote); err != nil {
			return fails(err)
		}
		return ok
	case *pb.ClientMessage_NodeMessage:
		if err := n.Message(int(req.NodeMessage.Node), req.NodeMessage.Text); err != nil {
			return fails(err)
		}
		return ok
	case *pb.ClientMessage_Page:
		room, told, err := n.PageSysop(req.Page.Reason)
		if err != nil {
			return []*pb.ServerMessage{errMsg(id, "too_many", err.Error())}
		}
		if told == 0 {
			n.CancelPage()
			return []*pb.ServerMessage{errMsg(id, "unavailable", "the sysop is not available just now")}
		}
		n.JoinRoom(room)
		return []*pb.ServerMessage{okMsg(id, room)}
	case *pb.ClientMessage_Answer:
		if !wh.has("sysop") {
			return []*pb.ServerMessage{errMsg(id, "forbidden", "answering pages is for sysops")}
		}
		hist, _, err := n.Answer(int(req.Answer.Node))
		if err != nil {
			return fails(err)
		}
		var ls []string
		for _, e := range hist {
			ls = append(ls, e.String())
		}
		return []*pb.ServerMessage{linesMsg(id, ls), okMsg(id, n.Room())}
	case *pb.ClientMessage_Command:
		line := strings.TrimSpace(req.Command.Line)
		if strings.HasPrefix(strings.ToLower(line), "/answer") || strings.HasPrefix(strings.ToLower(line), "/a ") ||
			strings.ToLower(line) == "/a" || strings.HasPrefix(strings.ToLower(line), "/busy") {
			if !wh.has("sysop") {
				return []*pb.ServerMessage{errMsg(id, "forbidden", "that is for sysops")}
			}
		}
		before := n.Room()
		out, leave, err := n.ChatCommand(line)
		if leave {
			n.LeaveRoom()
		}
		res := []*pb.ServerMessage{}
		if len(out) > 0 {
			res = append(res, linesMsg(id, out))
		}
		if err != nil {
			return append(res, errMsg(id, codeOf(err), err.Error()))
		}
		room := ""
		if n.Room() != before {
			room = n.Room()
		}
		return append(res, okMsg(id, room))
	case *pb.ClientMessage_Who:
		nl := &pb.NodeList{}
		for _, x := range s.B.Nodes() {
			if x.User == "" {
				continue
			}
			nl.Nodes = append(nl.Nodes, &pb.NodeInfo{Node: int32(x.Num), User: x.User, Via: x.Via,
				Since: timestamppb.New(x.Since), Doing: x.Activity})
		}
		return []*pb.ServerMessage{{Id: id, Body: &pb.ServerMessage_Nodes{Nodes: nl}}}
	case *pb.ClientMessage_Rooms:
		rl := &pb.RoomList{}
		for _, ro := range n.Rooms() {
			rl.Rooms = append(rl.Rooms, &pb.Room{Name: ro.Name, Users: ro.Users})
		}
		return []*pb.ServerMessage{{Id: id, Body: &pb.ServerMessage_Rooms{Rooms: rl}}}
	}
	return []*pb.ServerMessage{errMsg(id, "bad_request", "no request in the message")}
}
