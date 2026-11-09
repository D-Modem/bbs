package marvel

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// routing words
const (
	routeOpen    = 0x0000
	routeControl = 0xFFFF
)

// control frame types
const (
	ctrlConnect   = 1 // the client's connection request: echoed
	ctrlParams    = 3 // transport parameters
	ctrlConnected = 4
)

// pipeClose is the one-byte message that closes a pipe.
const pipeClose = 0x01

// a service is what answers one pipe.
type service interface {
	ifaces() []iface
	// call answers h; ok false: no reply.
	call(h hostBlock) (r []byte, ok bool)
}

// A service that holds something for its pipe (a chat room) lets it go
// when the pipe closes or the connection ends.
type ender interface{ end() }

// A service that keeps an iterator open (a chat room's events) is told
// when the client gives it up.
type canceller interface{ cancel(h hostBlock) }

func endService(svc service) {
	if e, ok := svc.(ender); ok {
		e.end()
	}
}

// conn is one client's connection.
type conn struct {
	s      *Server
	host   string
	source string // for throttling failed logins
	l      link

	mu   sync.Mutex
	node *bbs.Node // nil until a TCP client signs on
	own  bool      // the node is this connection's to leave
	user *store.User

	pipes  map[uint16]service
	closed map[uint16]bool
}

func (c *conn) logf(f string, a ...any) {
	c.s.B.Log.Printf("marvel: %s: "+f, append([]any{c.host}, a...)...)
}

// signedOn is the account the connection signed on as (nil: none yet).
func (c *conn) signedOn() *store.User {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.user
}

// serve runs the connection, on node n for a modem caller (nil over TCP).
func (c *conn) serve(n *bbs.Node) {
	c.s.mu.Lock()
	c.s.conns[c] = true
	c.s.mu.Unlock()
	c.node = n
	defer func() {
		c.s.mu.Lock()
		delete(c.s.conns, c)
		c.s.mu.Unlock()
		for p, svc := range c.pipes {
			if !c.closed[p] {
				endService(svc)
			}
		}
		c.l.close()
		c.mu.Lock()
		node, own := c.node, c.own
		c.mu.Unlock()
		if own {
			node.Leave()
		}
	}()
	c.closed = map[uint16]bool{}
	if err := c.sendParams(); err != nil {
		return
	}
	for {
		msg, err := c.l.recv()
		if err != nil {
			return
		}
		if !c.dispatch(msg) {
			return
		}
	}
}

// sendParams opens the link: packet size, window, ACK spacing and timeout.
func (c *conn) sendParams() error {
	p := []byte{0xFF, 0xFF, ctrlParams}
	for _, v := range []uint32{maxPacket, maxPacket, 16, 1, 600} {
		p = binary.LittleEndian.AppendUint32(p, v)
	}
	return c.l.send(p)
}

// dispatch handles one message from the client; false to hang up.
func (c *conn) dispatch(msg []byte) bool {
	if len(msg) < 2 {
		return true
	}
	switch r := binary.LittleEndian.Uint16(msg); r {
	case routeControl:
		if len(msg) > 2 && msg[2] == ctrlConnect {
			c.l.send(append([]byte{0xFF, 0xFF, ctrlConnect}, msg[3:]...))
		}
		return true
	case routeOpen:
		c.open(msg[2:])
		return true
	default:
		return c.data(r, msg[2:])
	}
}

// open opens a pipe: [unused:u16][pipe:u16] service\0 version\0 [version:u32].
func (c *conn) open(b []byte) {
	if len(b) < 4 {
		return
	}
	pipe := binary.LittleEndian.Uint16(b[2:])
	parts := strings.SplitN(string(b[4:]), "\x00", 3)
	name := parts[0]
	reply := make([]byte, 0, 8)
	for _, v := range []uint16{pipe, 1, pipe, 0} {
		reply = binary.LittleEndian.AppendUint16(reply, v)
	}
	c.l.send(reply)
	version := ""
	if len(parts) > 1 {
		version = parts[1]
	}
	svc := c.newService(name, version, pipe)
	if svc == nil {
		c.logf("pipe %d: no service %q", pipe, name)
		return
	}
	if old, ok := c.pipes[pipe]; ok && !c.closed[pipe] {
		endService(old)
	}
	c.pipes[pipe] = svc
	delete(c.closed, pipe)
	time.Sleep(100 * time.Millisecond)
	c.sendPipe(pipe, discovery(svc.ifaces()))
}

func (c *conn) newService(name, version string, pipe uint16) service {
	switch strings.ToUpper(name) {
	case "MOSRXP":
		return newMailbox(c)
	case "MOSABP":
		return &addrBook{c: c}
	case "CONFLOC":
		return &confloc{c: c}
	case "CONFSRV":
		return newChat(c, pipe)
	case "LOGSRV":
		return &logsrv{c: c}
	case "DIRSRV":
		return &dirsrv{c: c}
	case "BBS":
		return newBoards(c)
	}
	return nil
}

func (c *conn) sendPipe(pipe uint16, b []byte) error {
	return c.l.send(append(binary.LittleEndian.AppendUint16(nil, pipe), b...))
}

// data is a message on a pipe; false when the last pipe closes.
func (c *conn) data(pipe uint16, b []byte) bool {
	if len(b) == 1 && b[0] == pipeClose {
		if svc, ok := c.pipes[pipe]; ok {
			if !c.closed[pipe] {
				endService(svc)
			}
			c.closed[pipe] = true
		}
		if len(c.pipes) == 0 {
			return true
		}
		for p := range c.pipes {
			if !c.closed[p] {
				return true
			}
		}
		return false // the client signed out
	}
	svc := c.pipes[pipe]
	if svc == nil {
		return true
	}
	h, err := parseHostBlock(b)
	if err != nil {
		c.logf("pipe %d: %v", pipe, err)
		return true
	}
	// an iterator given up: acknowledged on the call it ends
	if !h.oneWay() && len(h.Params) == 1 && h.Params[0] == 0x0F {
		if cl, ok := svc.(canceller); ok {
			cl.cancel(h)
		}
		h.Params = []byte{tagEndStatic, tagStreamEnd}
		c.sendPipe(pipe, h.bytes())
		return true
	}
	r, ok := svc.call(h)
	if !ok {
		return true
	}
	if h.oneWay() {
		return true // a reply to a call a chunk ended is the service's to send
	}
	h.Params = r
	time.Sleep(100 * time.Millisecond)
	c.sendPipe(pipe, h.bytes())
	return true
}

// signOn signs the connection on as name with pw: 0 or the code LOGSRV's
// reply says why not.
func (c *conn) signOn(name, pw string) uint32 {
	b := c.s.B
	if u, err := b.Store.UserByScreenName(name); err == nil {
		name = u.Name // a member ID with the spaces left out
	}
	c.mu.Lock()
	node := c.node
	c.mu.Unlock()
	var u *store.User
	var err error
	if node != nil {
		u, err = node.Login(name, pw)
	} else {
		u, err = b.Authenticate("marvel", c.source, name, pw)
	}
	if err != nil {
		c.logf("sign-on as %q: %v", name, err)
		switch {
		case errors.Is(err, bbs.ErrLocked):
			return loginLocked
		case errors.Is(err, bbs.ErrThrottled):
			return loginBusy
		}
		if _, e := b.Store.UserByName(name); e != nil {
			return loginNoMember
		}
		return loginBadPassword
	}
	if node == nil {
		n, err := b.Enter(&bbs.Caller{Via: "msn", Listener: "marvel", Remote: c.host, Source: c.source},
			func() { c.l.close() })
		if err != nil {
			return loginBusy
		}
		n.SetUser(u)
		c.mu.Lock()
		c.node, c.own = n, true
		c.mu.Unlock()
		node = n
	}
	node.Doing("MSN")
	c.mu.Lock()
	c.user = u
	c.mu.Unlock()
	b.Log.Printf("node %d: %s on MSN (%s)", node.Num, u.Name, c.host)
	return loginOK
}

// tree is the directory as the caller sees it: the areas it may read.
func (c *conn) tree() *tree {
	b := c.s.B
	u := c.signedOn()
	all, err := b.Store.Areas()
	if err != nil {
		c.logf("areas: %v", err)
	}
	var areas []*store.Area
	for _, a := range all {
		if b.CanRead(u, a) {
			areas = append(areas, a)
		}
	}
	var rooms []roomEntry
	if u != nil {
		rooms = c.rooms()
	}
	return buildTree(b.Conf().Name, areas, rooms)
}
