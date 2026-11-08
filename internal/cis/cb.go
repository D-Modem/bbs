package cis

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
)

// The CB Simulator on the BBS's chat rooms: channel n is the room "cb-n",
// so the BBS's own chat callers can tune in too.  The commands are the
// 1982 /HELP's; the line format, "(Handle) text" on the tuned channel and
// "(ch,Handle) text" on one monitored, follows a 1985 quote and the forum
// conferences' "(room,name)" [I].

const cbChannels = 36

func cbRoom(ch int) string { return "cb-" + strconv.Itoa(ch) }

// cbChannel is the channel of a room, or 0.
func cbChannel(room string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(room, "cb-"))
	if !strings.HasPrefix(room, "cb-") || err != nil || n < 1 || n > cbChannels {
		return 0
	}
	return n
}

// cbState is a caller's time in CB.
type cbState struct {
	tuned    int
	monitor  map[int]bool
	xmitKey  string // /SCR, /SMC: what is sent is scrambled
	rcvKey   string // /SCR, /XCL: what comes is unscrambled
	squelch  map[string]bool
	talk     int    // /TALK: the job talked to (0: none)
	lastFrom string // the last talker on the tuned channel, for /WHO
}

// cbFlags are the CB callers' scrambling and talking, for /UST.
var cbFlags = struct {
	sync.Mutex
	m map[int]string // by node: "s" scrambled, "Tlk" talking
}{m: map[int]string{}}

func setCBFlag(node int, f string) {
	cbFlags.Lock()
	defer cbFlags.Unlock()
	if f == "" {
		delete(cbFlags.m, node)
	} else {
		cbFlags.m[node] = f
	}
}

func cbFlag(node int) string {
	cbFlags.Lock()
	defer cbFlags.Unlock()
	return cbFlags.m[node]
}

// errLeaveCB is /EXI: the CB Simulator left.
var errLeaveCB = errors.New("cis: leaving CB")

// enterCB is GO CB: a handle, a channel, then talk until /EXI.
func (s *session) enterCB() error {
	if !s.b.LevelOf(s.user).Chat {
		s.out("\nYour access does not include the CB Simulator.\n")
		return nil
	}
	s.node.Doing("CompuServe: CB")
	s.out("\nCompuServe CB Simulator\n\n")
	st := &cbState{monitor: map[int]bool{}, squelch: map[string]bool{}}
	if s.node.Handle() == "" {
		for {
			h, err := s.prompt("What's your handle? ")
			if err != nil {
				return s.abortOK0(err)
			}
			if err := s.setHandle(h); err != nil {
				s.out(err.Error() + "\n")
				continue
			}
			break
		}
	}
	for st.tuned == 0 {
		c, err := s.prompt("What channel (1-36)? ")
		if err != nil {
			return s.abortOK0(err)
		}
		if strings.TrimSpace(c) == "" {
			c = "1"
		}
		if err := s.tune(st, c); err != nil {
			s.out(err.Error() + "\n")
		}
	}
	defer func() {
		s.node.LeaveRoom()
		for ch := range st.monitor {
			s.node.Part(cbRoom(ch))
		}
		setCBFlag(s.node.Num, "")
	}()
	err := s.cbLoop(st)
	if errors.Is(err, errLeaveCB) {
		s.out("\nThank you for using CB.\n")
		s.show(s.page)
		return nil
	}
	return err
}

// setHandle checks and sets the caller's handle: not another member's
// name ("" is the caller's own).
func (s *session) setHandle(h string) error {
	h = bbs.Clean(h, 24)
	if h == "" {
		h = s.user.Name
	}
	if u, err := s.b.Store.UserByName(h); err == nil && u.ID != s.user.ID {
		return errors.New("That handle is another member's name.")
	}
	s.node.SetHandle(h)
	return nil
}

// tune is /TUN n.
func (s *session) tune(st *cbState, arg string) error {
	ch, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || ch < 1 || ch > cbChannels {
		return errors.New("Channels are 1 to 36.")
	}
	was := st.monitor[ch]
	if was {
		s.node.Part(cbRoom(ch))
		delete(st.monitor, ch)
	}
	hist, err := s.node.JoinRoom(cbRoom(ch))
	if err != nil {
		return fmt.Errorf("You cannot tune to channel %d: %v.", ch, err)
	}
	st.tuned = ch
	st.lastFrom = ""
	n := len(s.b.RoomMembers(cbRoom(ch))) - 1
	s.outf("(Tuned to channel %d, %d other%s here)\n", ch, n, plural(n))
	for _, e := range hist {
		if !was && (e.Kind == bbs.Said || e.Kind == bbs.Emoted) {
			s.cbShow(st, e)
		}
	}
	return nil
}

// cbLoop reads lines, showing what is said meanwhile.
func (s *session) cbLoop(st *cbState) error {
	var buf []byte
	last := time.Now()
	idle := s.b.Conf().IdleTimeout
	for {
		if evs := s.node.Pending(); len(evs) > 0 {
			if len(buf) > 0 {
				s.out("\n")
			}
			for _, e := range evs {
				s.cbShow(st, e)
			}
			if len(buf) > 0 {
				s.line.Write(buf)
			}
		}
		c, err := s.getc(250 * time.Millisecond)
		if errors.Is(err, errIdle) {
			if idle > 0 && time.Since(last) > idle {
				s.out("\n\nYou have been idle too long.\n")
				s.logoff()
				return errHangup
			}
			continue
		}
		if err != nil {
			return err
		}
		last = time.Now()
		if s.cr && (c == '\n' || c == 0) {
			s.cr = false
			continue
		}
		s.cr = false
		switch c {
		case '\r', '\n':
			s.cr = c == '\r'
			s.out("\n")
			line := string(buf)
			buf = buf[:0]
			if err := s.cbLine(st, line); err != nil {
				return err
			}
		case 8, 0x7F:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				s.out("\b \b")
			}
		case 0x15:
			if len(buf) > 0 {
				s.out("^U\n")
			}
			buf = buf[:0]
		case 0x16:
			s.out("\n" + string(buf))
		case 3, 0x10:
			s.out("^C\n")
			buf = buf[:0]
		default:
			if c < 0x20 || len(buf) >= bbs.MaxChatLine {
				continue
			}
			buf = append(buf, c)
			s.line.Write([]byte{c})
		}
	}
}

// cbName is who an event is from: the handle, or the name.
func cbName(e bbs.Event) string {
	if e.Handle != "" {
		return e.Handle
	}
	return e.From
}

// cbShow shows an event as CB does.
func (s *session) cbShow(st *cbState, e bbs.Event) {
	name := cbName(e)
	if st.squelch[strings.ToUpper(name)] {
		return
	}
	ch := cbChannel(e.Room)
	tag := "(" + name + ")"
	if ch != st.tuned {
		tag = fmt.Sprintf("(%d,%s)", ch, name)
	}
	switch e.Kind {
	case bbs.Said, bbs.Emoted:
		if e.Node == s.node.Num {
			return // typed, so already seen
		}
		if ch == st.tuned {
			st.lastFrom = e.From
		}
		text := e.Text
		if st.rcvKey != "" {
			text = unscramble(st.rcvKey, text)
		}
		if e.Kind == bbs.Emoted {
			s.outf("%s *%s*\n", tag, text)
		} else {
			s.outf("%s %s\n", tag, text)
		}
	case bbs.Joined:
		if e.Node != s.node.Num {
			s.outf("** %s tuned in to channel %d **\n", name, ch)
		}
	case bbs.Left:
		if e.Node != s.node.Num {
			s.outf("** %s left channel %d **\n", name, ch)
		}
	case bbs.Private:
		s.outf("(Talk %d,%s) %s\n", e.Node, name, e.Text)
	case bbs.Topic:
	default:
		s.out(e.String() + "\n")
	}
}

// cbLine does a line typed in CB: a /command, or something to say.
func (s *session) cbLine(st *cbState, line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	if !strings.HasPrefix(line, "/") {
		if st.talk != 0 {
			if err := s.node.Message(st.talk, line); err != nil {
				s.outf("?%v\n", err)
			}
			return nil
		}
		if st.xmitKey != "" {
			line = scramble(st.xmitKey, line)
		}
		if err := s.node.Say(line, false); err != nil {
			s.outf("?%v\n", err)
		}
		return nil
	}
	verb, arg, _ := strings.Cut(line[1:], " ")
	verb, arg = strings.ToUpper(verb), strings.TrimSpace(arg)
	switch {
	case is(verb, "TUNE", 3):
		if err := s.tune(st, arg); err != nil {
			s.out(err.Error() + "\n")
		}
	case is(verb, "MONITOR", 3):
		for _, ch := range channels(arg) {
			if ch == st.tuned || st.monitor[ch] {
				continue
			}
			if _, err := s.node.JoinAlso(cbRoom(ch)); err != nil {
				s.outf("?Channel %d: %v\n", ch, err)
				continue
			}
			st.monitor[ch] = true
		}
		s.monitoring(st)
	case is(verb, "UNMONITOR", 3):
		for _, ch := range channels(arg) {
			if st.monitor[ch] {
				s.node.Part(cbRoom(ch))
				delete(st.monitor, ch)
			}
		}
		s.monitoring(st)
	case is(verb, "STATUS", 3):
		s.cbStatus()
	case is(verb, "TIME", 3):
		s.out(s.now().Format("3:04:05 PM MST Monday, January 2, 2006") + "\n")
	case is(verb, "USTATUS", 3):
		s.ust(arg)
	case is(verb, "EXIT", 3) || is(verb, "OFF", 3) || is(verb, "QUIT", 4):
		if st.talk != 0 && is(verb, "EXIT", 3) {
			s.endTalk(st)
			return nil
		}
		if is(verb, "OFF", 3) {
			s.logoff()
			return errHangup
		}
		return errLeaveCB
	case is(verb, "WHO", 3):
		if st.lastFrom == "" {
			s.out("No one has talked yet.\n")
		} else if u, err := s.b.Store.UserByName(st.lastFrom); err == nil {
			s.out(PPN(u.ID) + "\n")
		} else {
			s.out(st.lastFrom + "\n")
		}
	case is(verb, "HANDLE", 3):
		if arg == "" {
			var err error
			if arg, err = s.prompt("New handle: "); err != nil {
				return s.abortOK0(err)
			}
		}
		if err := s.setHandle(arg); err != nil {
			s.out(err.Error() + "\n")
		} else {
			s.outf("Your handle is %s\n", s.node.Handle())
		}
	case verb == "SCR" || verb == "SCRAMBLE":
		st.xmitKey, st.rcvKey = arg, arg
		s.scrambled(st)
	case verb == "SMC":
		st.xmitKey, st.rcvKey = arg, ""
		s.scrambled(st)
	case verb == "XCL":
		st.xmitKey, st.rcvKey = "", arg
		s.scrambled(st)
	case is(verb, "UNSCRAMBLE", 3):
		st.xmitKey, st.rcvKey = "", ""
		s.scrambled(st)
	case is(verb, "SQUELCH", 3):
		if arg == "" {
			s.out("Squelch whose handle?\n")
		} else if k := strings.ToUpper(arg); st.squelch[k] {
			delete(st.squelch, k)
			s.outf("%s is no longer squelched\n", arg)
		} else {
			st.squelch[k] = true
			s.outf("%s is squelched\n", arg)
		}
	case is(verb, "JOB", 3):
		s.outf("Your job number is %d\n", s.node.Num)
	case is(verb, "TALK", 3):
		return s.startTalk(st, arg)
	case is(verb, "ROLL", 3):
		n, sides := 2, 6
		if a, b, ok := strings.Cut(strings.ToUpper(arg), "D"); ok {
			n, _ = strconv.Atoi(a)
			sides, _ = strconv.Atoi(b)
		}
		n, sides = min(max(n, 1), 10), min(max(sides, 2), 100)
		var rolls []string
		for range n {
			rolls = append(rolls, strconv.Itoa(1+rand.IntN(sides)))
		}
		text := "rolls " + strings.Join(rolls, ",")
		s.outf("You roll %s\n", strings.Join(rolls, ","))
		if err := s.node.Say(text, true); err != nil {
			s.outf("?%v\n", err)
		}
	case is(verb, "GAG", 3):
		s.out("/GAG is for CompuServe's staff.\n")
	case is(verb, "HELP", 1) || verb == "?":
		s.cbHelp()
	default:
		s.outf("?Unknown command /%s; /HELP for a list\n", verb)
	}
	return nil
}

// channels reads "1,4" or "1 4".
func channels(arg string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == ' ' }) {
		if n, err := strconv.Atoi(f); err == nil && n >= 1 && n <= cbChannels {
			out = append(out, n)
		}
	}
	return out
}

func (s *session) monitoring(st *cbState) {
	if len(st.monitor) == 0 {
		s.out("Monitoring no other channels\n")
		return
	}
	var chs []int
	for ch := range st.monitor {
		chs = append(chs, ch)
	}
	sort.Ints(chs)
	var ss []string
	for _, ch := range chs {
		ss = append(ss, strconv.Itoa(ch))
	}
	s.out("Monitoring channels " + strings.Join(ss, ",") + "\n")
}

func (s *session) scrambled(st *cbState) {
	switch {
	case st.xmitKey != "" && st.rcvKey != "":
		s.out("Scrambled\n")
	case st.xmitKey != "":
		s.out("Transmitting scrambled, receiving clear\n")
	case st.rcvKey != "":
		s.out("Transmitting clear, unscrambling what you receive\n")
	default:
		s.out("Clear\n")
	}
	if st.talk == 0 {
		f := ""
		if st.xmitKey != "" {
			f = "s"
		}
		setCBFlag(s.node.Num, f)
	}
}

// cbStatus is /STA: how many are on each channel in use.
func (s *session) cbStatus() {
	s.out("Ch  Users\n--  -----\n")
	any := false
	for ch := 1; ch <= cbChannels; ch++ {
		if n := len(s.b.RoomMembers(cbRoom(ch))); n > 0 {
			s.outf("%2d  %5d\n", ch, n)
			any = true
		}
	}
	if !any {
		s.out("No one is on CB.\n")
	}
}

// ust is /UST [ch]: who is on CB, the 1986 way.
func (s *session) ust(arg string) {
	only := channels(arg)
	s.out("   Job   User Id   Ch.  Node  Handle\n   ---  ---------- ---  ----  ------\n")
	i := 0
	for ch := 1; ch <= cbChannels; ch++ {
		if len(only) > 0 && only[0] != ch {
			continue
		}
		for _, m := range s.b.RoomMembers(cbRoom(ch)) {
			id := ""
			if u, err := s.b.Store.UserByName(m.Name); err == nil {
				id = PPN(u.ID)
			}
			c := strconv.Itoa(ch)
			switch f := cbFlag(m.Node); f {
			case "Tlk":
				c = f
			case "s":
				c += f
			}
			node := strings.ToUpper(m.Via)
			if len(node) > 3 {
				node = node[:3]
			}
			h := m.Handle
			if h == "" {
				h = m.Name
			}
			i++
			s.outf("%d) %3d  %-10s %3s  %-4s  %s\n", i, m.Node, id, c, node, h)
		}
	}
}

// startTalk is /TALK job: what is typed goes to that caller alone.
func (s *session) startTalk(st *cbState, arg string) error {
	if arg == "" && st.talk != 0 {
		s.endTalk(st)
		return nil
	}
	job, err := strconv.Atoi(arg)
	if err != nil {
		for _, ch := range append([]int{st.tuned}, keys(st.monitor)...) {
			for _, m := range s.b.RoomMembers(cbRoom(ch)) {
				if strings.EqualFold(m.Handle, arg) || strings.EqualFold(m.Name, arg) {
					job = m.Node
				}
			}
		}
	}
	if job == 0 || job == s.node.Num {
		s.out("Talk to which job?  /UST shows them.\n")
		return nil
	}
	if err := s.node.Message(job, fmt.Sprintf("wants to /TALK with you: /TALK %d", s.node.Num)); err != nil {
		s.outf("?%v\n", err)
		return nil
	}
	st.talk = job
	setCBFlag(s.node.Num, "Tlk")
	s.outf("Talking with job %d; /EXIT to return to channel %d\n", job, st.tuned)
	return nil
}

func (s *session) endTalk(st *cbState) {
	st.talk = 0
	s.scrambled(st)
	s.outf("Back on channel %d\n", st.tuned)
}

func keys(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (s *session) cbHelp() {
	s.out(`  /TUN # - Tunes channel # (1-36)
  /MON 1,4 - Listen to extra chnls
  /UNM 7,3 - Unmonitor channels
  /STA - Type channel status
  /TIM - Type time, day, and date
  /UST - User STatus typeout
  /EXI - Exit CB Simulator
  /WHO - Type PPN of last talker
  /HAN - Change handle
  /SCR xyz - Scramble on key "xyz"
  /SMC xyz - Scr & Monitor Clear
  /XCL xyz - Xmt CLear; unscr rcvr
  /UNS - Unscramble (both clear)
  /SQU abc - Squelch handle "abc"
  /JOB - your job #
  /TALK # - talk with job # alone
  /ROLL - roll dice (/ROLL 3D6)
  /HELP - Type this message
`)
}

// The scramble: the printable characters but space, permuted by the key.
func scrambleTable(key string) (fwd, back [128]byte) {
	h := fnv.New64a()
	h.Write([]byte(strings.ToUpper(key)))
	r := rand.New(rand.NewPCG(h.Sum64(), 0x43425349)) // "CBSI"
	perm := r.Perm(94)
	for i := range fwd {
		fwd[i], back[i] = byte(i), byte(i)
	}
	for i, p := range perm {
		fwd[0x21+i] = byte(0x21 + p)
		back[0x21+p] = byte(0x21 + i)
	}
	return
}

func mapText(t [128]byte, text string) string {
	b := []byte(text)
	for i, c := range b {
		if c < 128 {
			b[i] = t[c]
		}
	}
	return string(b)
}

func scramble(key, text string) string {
	f, _ := scrambleTable(key)
	return mapText(f, text)
}

func unscramble(key, text string) string {
	_, b := scrambleTable(key)
	return mapText(b, text)
}
