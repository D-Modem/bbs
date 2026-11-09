package aol

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/aol/p3"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// streamChunk is the most of an atom stream the host puts in one packet.
const streamChunk = 900

// session is one client's.
type session struct {
	s    *Server
	l    *p3.Link
	host string
	init p3.Init

	mu       sync.Mutex
	u        *store.User // signed on as
	n        *bbs.Node
	room     string          // the chat room its room window shows ("": none)
	slots    map[string]byte // the room window's people, by lower-case name
	nextSlot byte
	ims      map[string]int // IM windows, by lower-case partner
	draft    *draft         // the mail the client is sending

	parts map[partKey][]byte // streams the client has sent part of
}

type partKey struct {
	token string
	sid   string
}

func (ss *session) logf(format string, args ...any) {
	ss.s.B.Log.Printf("aol: %s: "+format, append([]any{ss.host}, args...)...)
}

// serve reads the client's tokens until the link is down.
func (ss *session) serve() {
	defer func() {
		ss.l.Close()
		ss.mu.Lock()
		n, u := ss.n, ss.u
		ss.mu.Unlock()
		if n != nil {
			n.Leave()
			ss.s.B.Log.Printf("node %d: gone (aol)", n.Num)
		} else if u == nil {
			ss.logf("gone before signing on")
		}
	}()
	for {
		d, err := ss.l.Read()
		if err != nil {
			if !errors.Is(err, p3.ErrClosed) {
				ss.logf("%v", err)
			}
			return
		}
		tok, body := p3.Token(d)
		if tok == "" {
			continue
		}
		ss.token(tok, body)
	}
}

// signedOn is whether the client has signed on.
func (ss *session) signedOn() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.u != nil
}

// token handles one token from the client.
func (ss *session) token(tok string, body []byte) {
	if tok == "Kk" && isText(body) { // from a button: the word, as it is
		if ss.signedOn() {
			ss.keyword(keywordOf(body))
		}
		return
	}
	sid, atoms := splitStream(tok, body)
	if atoms = ss.whole(tok, sid, atoms); atoms == nil {
		return // more to come
	}
	switch {
	case tok == "Dd":
		ss.signOn(atoms)
	case !ss.signedOn():
		ss.logf("token %q before signing on", tok)
	case tok == "pE":
		ss.signOff()
	case tok == "CJ":
		ss.enterChat()
	case tok == "Aa":
		ss.say(atoms)
	case tok == "CL":
		ss.leaveChat()
	case tok == "iS" || tok == "is":
		ss.sendIM(atoms)
	case tok == "iO":
		ss.available(atoms)
	case tok == "Kk":
		ss.keyword(keywordOf(append(sid, atoms...)))
	case tok == "m4":
		ss.sendMail(atoms)
	default:
		ss.logf("token %q (not served yet)", tok)
		// never leave the client waiting on an answer that won't come
		ss.send("uni_start_stream\n  uni_wait_off\nuni_end_stream\n")
	}
}

// splitStream splits a token's data into its stream id and the atoms
// after it.  The id is two bytes, but for the a/A t/T family: "aT" none,
// "At" three, "at" four.
func splitStream(tok string, body []byte) (sid, atoms []byte) {
	n := 2
	switch tok {
	case "aT":
		n = 0
	case "At":
		n = 3
	case "at":
		n = 4
	}
	if len(body) < n {
		return body, nil
	}
	return body[:n], body[n:]
}

// whole is a stream the client sent, once all of it has come: AOL 4.0
// spreads a long one (a mail, an IM) over packets of the same token and
// stream id, from uni_start_stream to uni_end_stream.  nil: more to come.
func (ss *session) whole(tok string, sid, atoms []byte) []byte {
	k := partKey{tok, string(sid)}
	if held, ok := ss.parts[k]; ok {
		atoms = append(held, atoms...)
		delete(ss.parts, k)
	}
	frames, err := fdo.Decode(atoms)
	if len(sid) > 0 && (err != nil || len(frames) > 0 && frames[0].Name() == "uni_start_stream" && !hasEnd(frames)) {
		if len(atoms) > 64<<10 {
			ss.logf("token %q: a stream of more than 64K; dropped", tok)
			return nil
		}
		ss.parts[k] = append([]byte(nil), atoms...)
		return nil
	}
	if atoms == nil {
		atoms = []byte{}
	}
	return atoms
}

func hasEnd(frames []fdo.Frame) bool {
	_, ok := fdo.Find(frames, "uni_end_stream")
	return ok
}

// send compiles FDO91 source and sends it to the client to run.  A
// source of more than one stream goes as one stream at a time.
func (ss *session) send(src string) error {
	b, err := fdo.Compile(src)
	if err != nil {
		ss.logf("a form that doesn't compile: %v", err)
		return err
	}
	return ss.sendAtoms(b)
}

// sendAtoms sends an atom stream: in one packet as token aT, or, too big
// for one, in pieces as token at, each with the same four-byte id.
func (ss *session) sendAtoms(b []byte) error {
	frames, err := fdo.Decode(b)
	if err != nil {
		return err
	}
	for _, st := range topStreams(frames) {
		enc := fdo.Encode(st)
		if len(enc) <= streamChunk {
			if err := ss.l.WriteToken("aT", enc); err != nil {
				return err
			}
			continue
		}
		var id [4]byte
		rand.Read(id[:])
		for _, piece := range fdo.Packets(st, streamChunk) {
			if err := ss.l.WriteToken("at", append(id[:], piece...)); err != nil {
				return err
			}
		}
	}
	return nil
}

// topStreams splits frames into its streams at the top level, each from
// uni_start_stream to its uni_end_stream.
func topStreams(frames []fdo.Frame) [][]fdo.Frame {
	var out [][]fdo.Frame
	depth, start := 0, 0
	for i, f := range frames {
		switch f.Name() {
		case "uni_start_stream":
			if depth == 0 {
				start = i
			}
			depth++
		case "uni_end_stream":
			depth--
			if depth == 0 {
				out = append(out, frames[start:i+1])
				start = i + 1
			}
		}
	}
	if start < len(frames) {
		out = append(out, frames[start:])
	}
	return out
}

// signOn is Dd: the Welcome form's screen name and password, its two
// de_data, checked as a BBS login.
func (ss *session) signOn(atoms []byte) {
	if ss.signedOn() {
		return
	}
	frames, err := fdo.Decode(atoms)
	if err != nil {
		ss.logf("Dd: %v", err)
	}
	de := fdo.FindAll(frames, "de_data")
	if len(de) < 2 {
		ss.logf("Dd without a name and password: % x", atoms)
		ss.refuse("Please type your screen name and password.")
		return
	}
	name := strings.TrimSpace(fdo.FromLatin1(de[0].Data))
	pw := strings.TrimSpace(fdo.FromLatin1(bytes.TrimRight(de[1].Data, "\x00")))
	b := ss.s.B
	if strings.EqualFold(name, "New User") {
		ss.refuse(fmt.Sprintf("To join %s, call it by telnet or the web first; then sign on here with "+
			"your name and password.", b.Conf().Name))
		return
	}
	if u := ss.member(name); u != nil {
		name = u.Name // a screen name has no spaces: "CarolAnn" is Carol Ann
	}
	u, err := b.Authenticate("aol", "ip:"+ss.host, name, pw)
	if err != nil {
		ss.logf("%s: %v", name, err)
		msg := "Incorrect screen name or password.  Please check them and try again."
		switch {
		case errors.Is(err, bbs.ErrLocked):
			msg = "This account is locked."
		case errors.Is(err, bbs.ErrThrottled):
			msg = "Too many failed sign-ons.  Please try again later."
		}
		ss.refuse(msg)
		return
	}
	n, err := b.Enter(&bbs.Caller{Via: "aol", Listener: "aol", Remote: ss.host, Source: "ip:" + ss.host,
		Terminal: ss.init.String()}, func() { ss.l.Close() })
	if err != nil {
		ss.refuse("All lines are busy.  Please try again later.")
		return
	}
	n.SetUser(u)
	n.Doing("AOL")
	ss.mu.Lock()
	ss.u, ss.n = u, n
	ss.mu.Unlock()
	b.Log.Printf("node %d: %s on AOL (%s, %s)", n.Num, u.Name, ss.host, ss.init)
	ss.send(onlineForm)
	unread, _ := b.Store.UnreadMail(u.ID)
	ss.send(welcomeForm(b.Conf().Name, u.Name, len(b.Nodes()), unread))
	for _, e := range n.Kept() {
		ss.event(e)
	}
	go ss.events()
}

// events shows the client what its node is told, until the link is down.
func (ss *session) events() {
	for {
		select {
		case <-ss.l.Done():
			return
		case <-ss.n.Wake():
			for _, e := range ss.n.Pending() {
				ss.event(e)
			}
		}
	}
}

// event shows the client one thing its node is told.
func (ss *session) event(e bbs.Event) {
	switch e.Kind {
	case bbs.Said, bbs.Emoted, bbs.Joined, bbs.Left, bbs.Topic:
		ss.roomEvent(e)
	case bbs.Private:
		ss.privateEvent(e)
	case bbs.Mail:
		ss.alert(fmt.Sprintf("You have new mail from %s: %s", e.From, e.Text))
	case bbs.Notice, bbs.Paged:
		ss.alert(e.String())
	}
}

// alert is a message box in the client.
func (ss *session) alert(text string) {
	ss.send(alertForm(text))
}

// refuse tells the client why it can't sign on, and has it hang up.
func (ss *session) refuse(why string) {
	ss.send(alertForm(why) + "uni_start_stream\n  async_force_off\nuni_end_stream\n")
}

// signOff is pE, the Sign Off menu: XS has the client sign off as if the
// host had (its offline broadcast, the Goodbye window), and hang up.
func (ss *session) signOff() {
	ss.mu.Lock()
	u := ss.u
	ss.mu.Unlock()
	if u != nil {
		ss.logf("%s signs off", u.Name)
	}
	ss.l.WriteToken("XS", []byte{0})
}
