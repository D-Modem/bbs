package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// POP3Server is the POP3 server (RFC 1939, STLS of RFC 2595, AUTH PLAIN
// of RFC 5034).  The maildrop is the user's Inbox; DELE puts mail in the
// trash.
type POP3Server struct {
	svc *Service
	set ServerSettings
	tc  *tls.Config
	ln  *limited
}

// NewPOP3 makes the POP3 server.
func (s *Service) NewPOP3(set ServerSettings) (*POP3Server, error) {
	tc, err := tlsConfig(set)
	if err != nil {
		return nil, fmt.Errorf("pop3: %w", err)
	}
	ln, err := s.listen("pop3", set, tc, func(why string) string { return "-ERR [SYS/TEMP] " + why + "\r\n" })
	if err != nil {
		return nil, err
	}
	return &POP3Server{svc: s, set: set, tc: tc, ln: ln}, nil
}

// Run serves until ctx is done.
func (x *POP3Server) Run(ctx context.Context) error {
	x.svc.B.Log.Printf("pop3: listening on %s (tls %s)", x.set.Address, x.set.TLS)
	var mu sync.Mutex
	conns := map[net.Conn]bool{}
	go func() {
		<-ctx.Done()
		x.ln.Close()
		mu.Lock()
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
	}()
	for {
		c, err := x.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("pop3: %w", err)
		}
		mu.Lock()
		conns[c] = true
		mu.Unlock()
		go func() {
			x.serve(c)
			mu.Lock()
			delete(conns, c)
			mu.Unlock()
		}()
	}
}

// popSession is a POP3 client's connection.
type popSession struct {
	x       *POP3Server
	c       net.Conn
	r       *bufio.Reader
	w       *bufio.Writer
	tls     bool
	who     string
	name    string // USER, waiting for PASS
	user    *store.User
	msgs    []*popMsg
	deleted map[int]bool
}

// popMsg is a mail in the maildrop.
type popMsg struct {
	e    *store.BoxEntry
	size int64
	read bool // RETR'd: marked read at QUIT
}

// serve is a client's session.  A panic is logged and ends only it.
func (x *POP3Server) serve(c net.Conn) {
	defer c.Close()
	defer func() {
		if r := recover(); r != nil {
			x.svc.B.Log.Printf("pop3: panic: %v", r)
		}
	}()
	ps := &popSession{x: x, c: c, who: "pop3 " + c.RemoteAddr().String(), deleted: map[int]bool{}}
	if tc, ok := c.(*tls.Conn); ok {
		tc.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tc.Handshake(); err != nil {
			return
		}
		tc.SetDeadline(time.Time{})
		ps.tls = true
	}
	ps.r = bufio.NewReaderSize(c, 4096)
	ps.w = bufio.NewWriterSize(c, 16384)
	ps.ok(x.svc.B.Conf().Name + " POP3 server ready")
	for {
		ps.w.Flush()
		ps.c.SetReadDeadline(time.Now().Add(x.set.IdleTimeout))
		line, err := ps.r.ReadString('\n')
		if err != nil || len(line) > 512 {
			return
		}
		f := strings.Fields(strings.TrimRight(line, "\r\n"))
		if len(f) == 0 {
			ps.err("No command")
			continue
		}
		cmd, args := strings.ToUpper(f[0]), f[1:]
		if cmd == "QUIT" {
			ps.quit()
			ps.w.Flush()
			return
		}
		if ps.user == nil {
			ps.authorization(cmd, args, line)
		} else {
			ps.transaction(cmd, args)
		}
	}
}

func (ps *popSession) ok(msg string)  { fmt.Fprintf(ps.w, "+OK %s\r\n", msg) }
func (ps *popSession) err(msg string) { fmt.Fprintf(ps.w, "-ERR %s\r\n", msg) }

// mayLogin reports whether a login may be tried now (not in the clear if
// TLS is required), saying why not.
func (ps *popSession) mayLogin() bool {
	if !ps.tls && ps.x.set.TLS == TLSRequired {
		ps.err("[AUTH] Use STLS first")
		return false
	}
	return true
}

// authorization answers the commands before a login.
func (ps *popSession) authorization(cmd string, args []string, line string) {
	switch cmd {
	case "CAPA":
		ps.ok("Capability list follows")
		caps := []string{"TOP", "UIDL", "RESP-CODES", "AUTH-RESP-CODE", "PIPELINING", "IMPLEMENTATION bbs-sec"}
		if ps.tls || ps.x.set.TLS != TLSRequired {
			caps = append(caps, "USER", "SASL PLAIN")
		}
		if !ps.tls && ps.x.tc != nil && ps.x.set.TLS != TLSImplicit {
			caps = append(caps, "STLS")
		}
		for _, c := range caps {
			ps.w.WriteString(c + "\r\n")
		}
		ps.w.WriteString(".\r\n")
	case "STLS":
		ps.stls()
	case "USER":
		if !ps.mayLogin() {
			return
		}
		_, name, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		ps.name = strings.TrimSpace(name)
		ps.ok("Send your password")
	case "PASS":
		if !ps.mayLogin() {
			return
		}
		if ps.name == "" {
			ps.err("USER first")
			return
		}
		_, pw, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		ps.login(ps.name, pw)
	case "AUTH":
		if len(args) == 0 {
			ps.ok("Mechanisms follow")
			ps.w.WriteString("PLAIN\r\n.\r\n")
			return
		}
		if !strings.EqualFold(args[0], "PLAIN") {
			ps.err("Only PLAIN")
			return
		}
		if !ps.mayLogin() {
			return
		}
		resp := ""
		if len(args) > 1 {
			resp = args[1]
		} else {
			ps.w.WriteString("+ \r\n")
			ps.w.Flush()
			l, err := ps.r.ReadString('\n')
			if err != nil {
				return
			}
			resp = strings.TrimSpace(l)
		}
		dec, err := base64.StdEncoding.DecodeString(resp)
		parts := strings.Split(string(dec), "\x00")
		if err != nil || len(parts) != 3 || parts[0] != "" && parts[0] != parts[1] {
			ps.err("[AUTH] Malformed PLAIN")
			return
		}
		ps.login(parts[1], parts[2])
	default:
		ps.err("Log in first")
	}
}

// stls answers STLS and makes the connection TLS.  Anything sent before
// the handshake hangs up (an injection).
func (ps *popSession) stls() {
	if ps.tls || ps.x.tc == nil || ps.x.set.TLS == TLSImplicit {
		ps.err("TLS not available")
		return
	}
	ps.ok("Begin TLS negotiation")
	ps.w.Flush()
	if ps.r.Buffered() > 0 {
		ps.c.Close()
		return
	}
	tc := tls.Server(ps.c, ps.x.tc)
	tc.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tc.Handshake(); err != nil {
		ps.c.Close()
		return
	}
	tc.SetDeadline(time.Time{})
	ps.c, ps.tls, ps.name = tc, true, ""
	ps.r = bufio.NewReaderSize(tc, 4096)
	ps.w = bufio.NewWriterSize(tc, 16384)
}

// login checks a login and opens the maildrop.
func (ps *popSession) login(name, pw string) {
	svc := ps.x.svc
	u, err := svc.Login(ps.who, ps.c.RemoteAddr(), "pop3", name, pw)
	ps.name = ""
	if err != nil {
		ps.err("[AUTH] Authentication failed")
		return
	}
	es, err := svc.B.Store.FolderEntries(u.ID, store.FolderInbox)
	if err != nil {
		ps.err("[SYS/TEMP] The maildrop cannot be read")
		return
	}
	ps.msgs = nil
	for _, e := range es {
		size := e.Size
		if size == 0 {
			m, _, err := svc.B.Store.BoxMail(u.ID, e.Box)
			if err != nil {
				continue
			}
			size = int64(len(svc.Render(m)))
		}
		ps.msgs = append(ps.msgs, &popMsg{e: e, size: size})
	}
	ps.user = u
	svc.B.Log.Printf("%s: %s logged in", ps.who, u.Name)
	ps.ok(fmt.Sprintf("%s has %d messages", u.Name, len(ps.msgs)))
}

// msg is the message a number names, not deleted; nil (and said) if none.
func (ps *popSession) msg(arg string) (int, *popMsg) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > len(ps.msgs) || ps.deleted[n] {
		ps.err("No such message")
		return 0, nil
	}
	return n, ps.msgs[n-1]
}

// content is a message's raw form.
func (ps *popSession) content(m *popMsg) ([]byte, error) {
	msg, _, err := ps.x.svc.B.Store.BoxMail(ps.user.ID, m.e.Box)
	if err != nil {
		return nil, err
	}
	return crlf(ps.x.svc.Render(msg)), nil
}

// transaction answers the commands of a logged-in session.
func (ps *popSession) transaction(cmd string, args []string) {
	switch cmd {
	case "CAPA":
		ps.ok("Capability list follows")
		ps.w.WriteString("TOP\r\nUIDL\r\nRESP-CODES\r\nPIPELINING\r\nIMPLEMENTATION bbs-sec\r\n.\r\n")
	case "STAT":
		n, size := 0, int64(0)
		for i, m := range ps.msgs {
			if !ps.deleted[i+1] {
				n++
				size += m.size
			}
		}
		ps.ok(fmt.Sprintf("%d %d", n, size))
	case "LIST", "UIDL":
		val := func(m *popMsg) string {
			if cmd == "LIST" {
				return strconv.FormatInt(m.size, 10)
			}
			return strconv.FormatInt(m.e.Box, 10)
		}
		if len(args) > 0 {
			if n, m := ps.msg(args[0]); m != nil {
				ps.ok(fmt.Sprintf("%d %s", n, val(m)))
			}
			return
		}
		ps.ok("Listing follows")
		for i, m := range ps.msgs {
			if !ps.deleted[i+1] {
				fmt.Fprintf(ps.w, "%d %s\r\n", i+1, val(m))
			}
		}
		ps.w.WriteString(".\r\n")
	case "RETR", "TOP":
		if len(args) < 1 || cmd == "TOP" && len(args) < 2 {
			ps.err("Which message?")
			return
		}
		_, m := ps.msg(args[0])
		if m == nil {
			return
		}
		raw, err := ps.content(m)
		if err != nil {
			ps.err("[SYS/TEMP] The message cannot be read")
			return
		}
		if cmd == "TOP" {
			lines, err := strconv.Atoi(args[1])
			if err != nil || lines < 0 {
				ps.err("TOP n lines")
				return
			}
			raw = top(raw, lines)
		} else {
			m.read = true
		}
		ps.ok(fmt.Sprintf("%d octets", len(raw)))
		dotted(ps.w, raw)
	case "DELE":
		if len(args) < 1 {
			ps.err("Which message?")
			return
		}
		if n, m := ps.msg(args[0]); m != nil {
			ps.deleted[n] = true
			ps.ok("Deleted")
		}
	case "RSET":
		ps.deleted = map[int]bool{}
		for _, m := range ps.msgs {
			m.read = false
		}
		ps.ok("Reset")
	case "NOOP":
		ps.ok("")
	default:
		ps.err("Unknown command")
	}
}

// quit ends the session: in the transaction state, what was deleted goes
// in the trash and what was retrieved is read.
func (ps *popSession) quit() {
	if ps.user == nil {
		ps.ok("Goodbye")
		return
	}
	st := ps.x.svc.B.Store
	failed := false
	for i, m := range ps.msgs {
		var err error
		switch {
		case ps.deleted[i+1]:
			err = st.TrashBox(ps.user.ID, m.e.Box)
		case m.read && !m.e.Read:
			err = st.SetBoxFlags(ps.user.ID, m.e.Box, true, m.e.Starred, m.e.Flags)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			failed = true
		}
	}
	if failed {
		ps.err("[SYS/TEMP] Some changes were not kept")
		return
	}
	ps.ok("Goodbye")
}

// top is a mail's header and its first lines of body.
func top(raw []byte, lines int) []byte {
	end := headerEnd(raw)
	if end < 0 {
		return raw
	}
	body := raw[end:]
	var out bytes.Buffer
	out.Write(raw[:end])
	parts := bytes.SplitAfter(body, []byte("\n"))
	// the blank line, then the lines asked for
	for i := 0; i < len(parts) && i <= lines; i++ {
		out.Write(parts[i])
	}
	return out.Bytes()
}

// dotted writes a multi-line response: lines dot-stuffed, ended by ".".
func dotted(w *bufio.Writer, raw []byte) {
	for len(raw) > 0 {
		i := bytes.IndexByte(raw, '\n')
		var line []byte
		if i < 0 {
			line, raw = raw, nil
		} else {
			line, raw = raw[:i], raw[i+1:]
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) > 0 && line[0] == '.' {
			w.WriteByte('.')
		}
		w.Write(line)
		w.WriteString("\r\n")
	}
	w.WriteString(".\r\n")
}
