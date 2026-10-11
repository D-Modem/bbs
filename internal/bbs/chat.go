package bbs

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// EventKind says what an Event is.
type EventKind int

// Events a node is told of.
const (
	Said    EventKind = iota // a line in a chat room
	Emoted                   // /me, in a chat room
	Joined                   // someone came into the room
	Left                     // someone left it
	Private                  // a message to this node alone
	Paged                    // a caller pages the sysop (to sysops)
	Notice                   // from the system
	Mail                     // a new mail for the caller
)

// Event is something for a node to show its caller.
type Event struct {
	Kind EventKind
	Room string
	From string
	Node int
	Text string
	Time time.Time
}

// String is the event as a line for the caller.
func (e Event) String() string {
	switch e.Kind {
	case Said:
		return e.From + ": " + e.Text
	case Emoted:
		return "* " + e.From + " " + e.Text
	case Joined:
		return "-> " + e.From + " came in"
	case Left:
		return "<- " + e.From + " left"
	case Private:
		return fmt.Sprintf("Message from %s (node %d): %s", e.From, e.Node, e.Text)
	case Mail:
		return "New mail from " + e.From + ": " + e.Text
	case Paged:
		s := fmt.Sprintf("%s (node %d) is paging you", e.From, e.Node)
		if e.Text != "" {
			s += ": " + e.Text
		}
		return s + "  (Chat, then /answer)"
	}
	return e.Text
}

// limits of chat
const (
	inboxMax    = 200 // events kept for a node not reading them
	historyMax  = 20  // lines a room shows who comes in
	MaxChatLine = 200 // runes a line
	floodLines  = 8   // lines ...
	floodWindow = 10 * time.Second
	MainRoom    = "main"
)

// chatState is a node's part in chat.
type chatState struct {
	mu      sync.Mutex
	inbox   []Event
	wake    chan struct{}
	room    string
	busy    bool // a sysop not to be paged
	sent    []time.Time
	paging  string    // why this caller pages the sysop ("": not)
	pagedAt time.Time // when
}

// room is a chat room's members and its last lines.
type room struct {
	members map[*Node]bool
	history []Event
}

// hub is the BBS's chat: its rooms.
type hub struct {
	mu    sync.Mutex
	rooms map[string]*room
}

// ErrChat are refusals of chat.
var (
	ErrNoRoom    = errors.New("a room's name is 1 to 20 letters, digits or -")
	ErrNotYours  = errors.New("that room is a private one")
	ErrFlood     = errors.New("slow down")
	ErrNoNode    = errors.New("there is no one on that node")
	ErrNotInRoom = errors.New("you are not in a room")
)

var roomName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`)

func (b *BBS) hub() *hub {
	b.hubOnce.Do(func() { b.chat = &hub{rooms: map[string]*room{}} })
	return b.chat
}

func (n *Node) chat() *chatState {
	n.chatOnce.Do(func() { n.cs = &chatState{wake: make(chan struct{}, 1)} })
	return n.cs
}

// deliver gives a node an event.
func (n *Node) deliver(e Event) {
	cs := n.chat()
	cs.mu.Lock()
	if len(cs.inbox) >= inboxMax {
		cs.inbox = cs.inbox[1:]
	}
	cs.inbox = append(cs.inbox, e)
	cs.mu.Unlock()
	select {
	case cs.wake <- struct{}{}:
	default:
	}
}

// Pending takes the events waiting for the node.
func (n *Node) Pending() []Event {
	cs := n.chat()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := cs.inbox
	cs.inbox = nil
	return out
}

// Wake is signalled when an event comes.
func (n *Node) Wake() <-chan struct{} { return n.chat().wake }

// Room is the room the node is in ("": none).
func (n *Node) Room() string {
	cs := n.chat()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.room
}

func (n *Node) name() string {
	if u := n.User(); u != nil {
		return u.Name
	}
	return fmt.Sprintf("node %d", n.Num)
}

// pageRoom is the private room a caller paging the sysop waits in.
func pageRoom(node int) string { return fmt.Sprintf("sysop-%d", node) }

// JoinRoom takes the node into a room (leaving the one it is in), and
// gives it the room's last lines.
func (n *Node) JoinRoom(name string) ([]Event, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !roomName.MatchString(name) {
		return nil, ErrNoRoom
	}
	u := n.User()
	if u == nil {
		return nil, errors.New("not logged in")
	}
	if strings.HasPrefix(name, "sysop-") && name != pageRoom(n.Num) && !u.Sysop {
		return nil, ErrNotYours
	}
	if n.Room() == name {
		return nil, nil
	}
	n.LeaveRoom()
	h := n.b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	if r == nil {
		r = &room{members: map[*Node]bool{}}
		h.rooms[name] = r
	}
	others := r.membersList()
	r.members[n] = true
	hist := append([]Event(nil), r.history...)
	h.mu.Unlock()
	cs := n.chat()
	cs.mu.Lock()
	cs.room = name
	cs.mu.Unlock()
	e := Event{Kind: Joined, Room: name, From: u.Name, Node: n.Num, Time: time.Now()}
	for _, o := range others {
		o.deliver(e)
	}
	return hist, nil
}

func (r *room) membersList() []*Node {
	out := make([]*Node, 0, len(r.members))
	for m := range r.members {
		out = append(out, m)
	}
	return out
}

// LeaveRoom takes the node out of its room.
func (n *Node) LeaveRoom() {
	cs := n.chat()
	cs.mu.Lock()
	name := cs.room
	cs.room = ""
	cs.mu.Unlock()
	if name == "" {
		return
	}
	h := n.b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	var others []*Node
	if r != nil {
		delete(r.members, n)
		others = r.membersList()
		if len(r.members) == 0 {
			delete(h.rooms, name)
		}
	}
	h.mu.Unlock()
	e := Event{Kind: Left, Room: name, From: n.name(), Node: n.Num, Time: time.Now()}
	for _, o := range others {
		o.deliver(e)
	}
}

// flood reports whether the node has sent too much lately.
func (n *Node) flood() bool {
	cs := n.chat()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	now := time.Now()
	keep := cs.sent[:0]
	for _, t := range cs.sent {
		if now.Sub(t) < floodWindow {
			keep = append(keep, t)
		}
	}
	cs.sent = keep
	if len(cs.sent) >= floodLines {
		return true
	}
	cs.sent = append(cs.sent, now)
	return false
}

// Say says a line in the node's room (emote: as /me).
func (n *Node) Say(text string, emote bool) error {
	text = Clean(text, MaxChatLine)
	if text == "" {
		return nil
	}
	name := n.Room()
	if name == "" {
		return ErrNotInRoom
	}
	if n.flood() {
		return ErrFlood
	}
	e := Event{Kind: Said, Room: name, From: n.name(), Node: n.Num, Text: text, Time: time.Now()}
	if emote {
		e.Kind = Emoted
	}
	n.b.Stats.ChatLines.Add(1)
	h := n.b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	var members []*Node
	if r != nil {
		members = r.membersList()
		r.history = append(r.history, e)
		if len(r.history) > historyMax {
			r.history = r.history[len(r.history)-historyMax:]
		}
	}
	h.mu.Unlock()
	for _, m := range members {
		m.deliver(e)
	}
	return nil
}

// RoomInfo is a room and who is in it.
type RoomInfo struct {
	Name  string
	Users []string
}

// Rooms lists the chat rooms in use (private ones only to sysops and
// their own caller).
func (n *Node) Rooms() []RoomInfo {
	u := n.User()
	h := n.b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []RoomInfo
	for name, r := range h.rooms {
		if strings.HasPrefix(name, "sysop-") && name != pageRoom(n.Num) && (u == nil || !u.Sysop) {
			continue
		}
		ri := RoomInfo{Name: name}
		for m := range r.members {
			ri.Users = append(ri.Users, fmt.Sprintf("%s (%d)", m.name(), m.Num))
		}
		sort.Strings(ri.Users)
		out = append(out, ri)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// node finds a node online.
func (b *BBS) node(num int) *Node {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nodes[num]
}

// Message sends a line to the caller on another node.
func (n *Node) Message(to int, text string) error {
	text = Clean(text, MaxChatLine)
	if text == "" {
		return errors.New("say something")
	}
	o := n.b.node(to)
	if o == nil || o.User() == nil || o == n {
		return ErrNoNode
	}
	if n.flood() {
		return ErrFlood
	}
	o.deliver(Event{Kind: Private, From: n.name(), Node: n.Num, Text: text, Time: time.Now()})
	n.b.Log.Printf("node %d: %s sent a message to node %d", n.Num, n.name(), to)
	return nil
}

// SetBusy says whether a sysop may be paged.
func (n *Node) SetBusy(busy bool) {
	cs := n.chat()
	cs.mu.Lock()
	cs.busy = busy
	cs.mu.Unlock()
}

// Busy reports it.
func (n *Node) Busy() bool {
	cs := n.chat()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.busy
}

// PageSysop pages the sysops online (not busy) for the caller, who should
// then wait in the room it returns.  It returns how many sysops were told;
// with none, the page command (if any) is run.
func (n *Node) PageSysop(reason string) (string, int, error) {
	u := n.User()
	if u == nil {
		return "", 0, errors.New("not logged in")
	}
	reason = Clean(reason, 60)
	cs := n.chat()
	cs.mu.Lock()
	if !cs.pagedAt.IsZero() && time.Since(cs.pagedAt) < time.Minute {
		cs.mu.Unlock()
		return pageRoom(n.Num), 0, errors.New("you paged a moment ago: wait for the sysop")
	}
	cs.paging, cs.pagedAt = reason, time.Now()
	cs.mu.Unlock()
	e := Event{Kind: Paged, From: u.Name, Node: n.Num, Text: reason, Time: time.Now()}
	told := 0
	n.b.mu.Lock()
	var sysops []*Node
	for _, o := range n.b.nodes {
		if o != n && o.user != nil && o.user.Sysop {
			sysops = append(sysops, o)
		}
	}
	n.b.mu.Unlock()
	for _, o := range sysops {
		if !o.Busy() {
			o.deliver(e)
			told++
		}
	}
	n.b.Stats.Pages.Add(1)
	n.b.Log.Printf("node %d: %s paged the sysop (%d told): %s", n.Num, u.Name, told, reason)
	if told == 0 && len(n.b.PageCommand) > 0 {
		n.b.runPageCommand(u.Name, n.Num, reason)
	}
	return pageRoom(n.Num), told, nil
}

// runPageCommand tells the sysop, who is not on, by a program: its words
// with {user}, {node} and {reason} filled in (no shell: nothing a caller
// typed is run).
func (b *BBS) runPageCommand(user string, node int, reason string) {
	r := strings.NewReplacer("{user}", user, "{node}", fmt.Sprint(node), "{reason}", reason)
	args := make([]string, len(b.PageCommand))
	for i, w := range b.PageCommand {
		args[i] = r.Replace(w)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(cmd.Environ(), "BBS_PAGE_USER="+user, fmt.Sprintf("BBS_PAGE_NODE=%d", node),
		"BBS_PAGE_REASON="+reason)
	go func() {
		done := make(chan error, 1)
		if err := cmd.Start(); err != nil {
			b.Log.Printf("page command: %v", err)
			return
		}
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				b.Log.Printf("page command: %v", err)
			}
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			b.Log.Printf("page command: killed after 30s")
		}
	}()
}

// PageInfo is a caller waiting for the sysop.
type PageInfo struct {
	Node   int
	User   string
	Reason string
	Since  time.Time
}

// Pages lists the callers paging the sysop now, oldest first.
func (b *BBS) Pages() []PageInfo {
	b.mu.Lock()
	nodes := make([]*Node, 0, len(b.nodes))
	for _, n := range b.nodes {
		nodes = append(nodes, n)
	}
	b.mu.Unlock()
	var out []PageInfo
	for _, n := range nodes {
		cs := n.chat()
		cs.mu.Lock()
		if !cs.pagedAt.IsZero() {
			out = append(out, PageInfo{n.Num, n.name(), cs.paging, cs.pagedAt})
		}
		cs.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// Answer takes a sysop into the room of a caller paging (the one waiting
// longest if node is 0), and ends the page.
func (n *Node) Answer(node int) ([]Event, int, error) {
	u := n.User()
	if u == nil || !u.Sysop {
		return nil, 0, errors.New("only a sysop answers pages")
	}
	if node == 0 {
		ps := n.b.Pages()
		if len(ps) == 0 {
			return nil, 0, errors.New("no one is paging")
		}
		node = ps[0].Node
	}
	o := n.b.node(node)
	if o == nil {
		return nil, 0, ErrNoNode
	}
	o.endPage()
	hist, err := n.JoinRoom(pageRoom(node))
	if err == nil {
		o.deliver(Event{Kind: Notice, Text: u.Name + " answered your page.", Time: time.Now()})
	}
	return hist, node, err
}

// endPage forgets the node's page.
func (n *Node) endPage() {
	cs := n.chat()
	cs.mu.Lock()
	cs.paging, cs.pagedAt = "", time.Time{}
	cs.mu.Unlock()
}

// CancelPage stops paging (the caller gave up waiting).
func (n *Node) CancelPage() { n.endPage() }

// leaveChat is the node going: out of its room, its page forgotten.
func (n *Node) leaveChat() {
	n.LeaveRoom()
	n.endPage()
}
