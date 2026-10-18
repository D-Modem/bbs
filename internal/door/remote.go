package door

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Doors elsewhere: the caller's line joined to another system's door
// server, by telnet or RLogin, or by the door networks' own ways in
// (BBSLink, DoorParty).

// Remote kinds.
const (
	KindTelnet    = "telnet"
	KindRLogin    = "rlogin"
	KindBBSLink   = "bbslink"
	KindDoorParty = "doorparty"
)

// The door networks' servers.
const (
	BBSLinkURL       = "http://games.bbslink.net"
	BBSLinkAddress   = "games.bbslink.net:23"
	DoorPartyAddress = "dp.throwbackbbs.com:2022" // its SSH server
	DoorPartyRLogin  = "dp.throwbackbbs.com:513"  // its RLogin server, through the tunnel
)

// Remote is a door elsewhere: where it is, and how to get in.
type Remote struct {
	Kind    string
	Address string // host:port: the telnet or RLogin server; BBSLink's telnet; DoorParty's SSH

	// RLogin (and DoorParty's): its client and server user names and
	// terminal ("{user}" and the rest of Expand filled in)
	ClientUser string
	ServerUser string
	Terminal   string

	// BBSLink: the codes it gave the BBS, and the door's
	URL        string // its web server (default BBSLinkURL)
	SysCode    string
	AuthCode   string
	SchemeCode string
	DoorCode   string // BBSLink's door, or DoorParty's ("": its menu)

	// DoorParty: the SSH account it gave the BBS, its host key's
	// fingerprint (SHA256:..., "": not checked), the BBS's tag, and the
	// RLogin server at the far end of the tunnel
	SSHUser     string
	SSHPassword string
	HostKey     string
	Tag         string
	Target      string

	Timeout time.Duration // to connect (default 30s)
}

// timeout is how long to wait to connect: Timeout, or 30 seconds.
func (r *Remote) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return 30 * time.Second
}

// Dial connects to the door for a caller (logf: for what is worth the
// log, such as an unchecked host key).
func (r *Remote) Dial(in *Info, logf func(string, ...any)) (io.ReadWriteCloser, error) {
	switch r.Kind {
	case KindTelnet:
		c, err := net.DialTimeout("tcp", r.Address, r.timeout())
		if err != nil {
			return nil, err
		}
		return newTelnetClient(c, in), nil
	case KindRLogin:
		c, err := net.DialTimeout("tcp", r.Address, r.timeout())
		if err != nil {
			return nil, err
		}
		if err := r.rlogin(c, in); err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	case KindBBSLink:
		return r.bbslink(in)
	case KindDoorParty:
		return r.doorParty(in, logf)
	}
	return nil, fmt.Errorf("no such kind of door: %q", r.Kind)
}

// Fill in a field, with the user's name as {user}.
func fill(w string, in *Info) string {
	return Expand(strings.ReplaceAll(w, "{user}", in.UserName), in, "")
}

// rlogin does RFC 1282's start: client user, server user, terminal/speed;
// the server answers a zero byte.
func (r *Remote) rlogin(c io.ReadWriter, in *Info) error {
	client, server := fill(r.ClientUser, in), fill(r.ServerUser, in)
	if client == "" {
		client = in.UserName
	}
	if server == "" {
		server = in.UserName
	}
	term := fill(r.Terminal, in)
	if term == "" {
		term = "ansi"
	}
	if !strings.Contains(term, "/") {
		baud := in.Baud
		if baud == 0 {
			baud = 38400
		}
		term += "/" + strconv.Itoa(baud)
	}
	hs := "\x00" + client + "\x00" + server + "\x00" + term + "\x00"
	if _, err := io.WriteString(c, hs); err != nil {
		return err
	}
	if dc, ok := c.(interface{ SetReadDeadline(time.Time) error }); ok {
		dc.SetReadDeadline(time.Now().Add(r.timeout()))
		defer dc.SetReadDeadline(time.Time{})
	}
	var ack [1]byte
	if _, err := io.ReadFull(c, ack[:]); err != nil {
		return fmt.Errorf("rlogin: no answer: %w", err)
	}
	if ack[0] != 0 {
		return errors.New("rlogin: refused")
	}
	return nil
}

// bbslink gets a token from BBSLink's web server, has it authenticate the
// caller with the BBS's codes, then joins them by telnet.
func (r *Remote) bbslink(in *Info) (io.ReadWriteCloser, error) {
	base := strings.TrimRight(r.URL, "/")
	if base == "" {
		base = BBSLinkURL
	}
	hc := &http.Client{Timeout: r.timeout()}
	get := func(path string, headers map[string]string) (string, error) {
		req, err := http.NewRequest("GET", base+path, nil)
		if err != nil {
			return "", err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("bbslink: %s: %s", path, resp.Status)
		}
		return strings.TrimSpace(string(body)), nil
	}
	key := randomKey()
	token, err := get("/token.php?key="+key, nil)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("bbslink: no token")
	}
	md5hex := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	rows := in.Rows
	if rows == 0 {
		rows = 24
	}
	status, err := get("/auth.php?key="+key, map[string]string{
		"X-User":    strconv.FormatInt(in.UserID, 10),
		"X-System":  r.SysCode,
		"X-Auth":    md5hex(r.AuthCode + token),
		"X-Code":    md5hex(r.SchemeCode + token),
		"X-Rows":    strconv.Itoa(rows),
		"X-Key":     key,
		"X-Door":    r.DoorCode,
		"X-Token":   token,
		"X-Type":    "bbs-sec",
		"X-Version": "1",
	})
	if err != nil {
		return nil, err
	}
	if status != "complete" {
		return nil, fmt.Errorf("bbslink: not let in: %s", status)
	}
	addr := r.Address
	if addr == "" {
		addr = BBSLinkAddress
	}
	c, err := net.DialTimeout("tcp", addr, r.timeout())
	if err != nil {
		return nil, err
	}
	return newTelnetClient(c, in), nil
}

// randomKey is BBSLink's request key: 6 random letters and digits.
func randomKey() string {
	const set = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b [6]byte
	rand.Read(b[:])
	for i := range b {
		b[i] = set[int(b[i])%len(set)]
	}
	return string(b[:])
}

// doorParty opens an SSH tunnel to DoorParty, and RLogins through it as
// [TAG]user, the door code as the terminal (it takes the caller straight
// into that door).
func (r *Remote) doorParty(in *Info, logf func(string, ...any)) (io.ReadWriteCloser, error) {
	addr := r.Address
	if addr == "" {
		addr = DoorPartyAddress
	}
	target := r.Target
	if target == "" {
		target = DoorPartyRLogin
	}
	hostKey := func(_ string, _ net.Addr, k ssh.PublicKey) error {
		logf("doorparty: host key not checked: %s (host_key = %s)", addr, ssh.FingerprintSHA256(k))
		return nil
	}
	if r.HostKey != "" {
		want := r.HostKey
		hostKey = func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(k); got != want {
				return fmt.Errorf("doorparty: host key %s, not %s", got, want)
			}
			return nil
		}
	}
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: r.SSHUser,
		Auth: []ssh.AuthMethod{ssh.Password(r.SSHPassword)}, HostKeyCallback: hostKey, Timeout: r.timeout()})
	if err != nil {
		return nil, fmt.Errorf("doorparty: %w", err)
	}
	ch, err := client.Dial("tcp", target)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("doorparty: the tunnel: %w", err)
	}
	rl := *r
	rl.ServerUser = "[" + strings.Trim(r.Tag, "[]") + "]{user}"
	if rl.ClientUser == "" {
		rl.ClientUser = "{user}"
	}
	rl.Terminal = r.DoorCode
	if rl.Terminal == "" {
		rl.Terminal = "ansi"
	}
	if err := rl.rlogin(ch, in); err != nil {
		ch.Close()
		client.Close()
		return nil, err
	}
	return &tunnel{Conn: ch, client: client}, nil
}

// tunnel is a connection through an SSH client, closed with it.
type tunnel struct {
	net.Conn
	client *ssh.Client
}

// Close closes the connection and the SSH client under it.
func (t *tunnel) Close() error {
	t.Conn.Close()
	return t.client.Close()
}

// telnet's bytes
const (
	tIAC  = 255
	tDONT = 254
	tDO   = 253
	tWONT = 252
	tWILL = 251
	tSB   = 250
	tSE   = 240

	optBinary = 0
	optEcho   = 1
	optSGA    = 3
	optTType  = 24
	optNAWS   = 31
)

// telnetClient is a telnet connection as a door's line: the other side's
// negotiation answered (we are a binary, ANSI terminal of the caller's
// size; it may echo and go ahead), IACs taken out of what comes and
// doubled in what goes.
type telnetClient struct {
	c    net.Conn
	in   *Info
	wmu  sync.Mutex
	werr error  // send's failure, for the next Read or Write (wmu)
	rbuf []byte // read from the connection
	st   int    // the parser's state
	cmd  byte
	sb   []byte
}

// newTelnetClient is c as a telnet client for the caller in (whose size and
// terminal it tells the server).
func newTelnetClient(c net.Conn, in *Info) *telnetClient {
	return &telnetClient{c: c, in: in}
}

// Close closes the connection.
func (t *telnetClient) Close() error { return t.c.Close() }

// Write sends p, its IACs doubled; the whole of p, or an error.
func (t *telnetClient) Write(p []byte) (int, error) {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if t.werr != nil {
		return 0, t.werr
	}
	if bytes.IndexByte(p, tIAC) < 0 {
		return t.c.Write(p)
	}
	if _, err := t.c.Write(bytes.ReplaceAll(p, []byte{tIAC}, []byte{tIAC, tIAC})); err != nil {
		return 0, err
	}
	return len(p), nil
}

// send writes telnet commands as they are, between Writes; a failure is
// kept for the next Read or Write to return.
func (t *telnetClient) send(b ...byte) {
	t.wmu.Lock()
	if _, err := t.c.Write(b); err != nil && t.werr == nil {
		t.werr = err
	}
	t.wmu.Unlock()
}

// sendErr is send's failure, if there was one.
func (t *telnetClient) sendErr() error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.werr
}

// states
const (
	sData = iota
	sIAC
	sOpt
	sSB
	sSBIAC
)

// Read is the server's data, telnet commands taken out and answered; it
// reads on until there is some data or an error (send's too).
func (t *telnetClient) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil // nothing could be read into it: reading on would never end
	}
	for {
		if cap(t.rbuf) < len(p) {
			t.rbuf = make([]byte, len(p))
		}
		buf := t.rbuf[:len(p)]
		n, err := t.c.Read(buf)
		out := p[:0]
		for _, b := range buf[:n] {
			switch t.st {
			case sData:
				if b == tIAC {
					t.st = sIAC
				} else {
					out = append(out, b)
				}
			case sIAC:
				switch b {
				case tIAC:
					out = append(out, tIAC)
					t.st = sData
				case tDO, tDONT, tWILL, tWONT:
					t.cmd, t.st = b, sOpt
				case tSB:
					t.sb, t.st = t.sb[:0], sSB
				default:
					t.st = sData
				}
			case sOpt:
				t.option(t.cmd, b)
				t.st = sData
			case sSB:
				if b == tIAC {
					t.st = sSBIAC
				} else if len(t.sb) < 64 {
					t.sb = append(t.sb, b)
				}
			case sSBIAC:
				if b == tSE {
					t.subneg()
					t.st = sData
				} else {
					if len(t.sb) < 64 {
						t.sb = append(t.sb, b)
					}
					t.st = sSB
				}
			}
		}
		if len(out) > 0 || err != nil {
			return len(out), err
		}
		if err := t.sendErr(); err != nil {
			return 0, err
		}
	}
}

// option answers the other side's DO, DONT, WILL and WONT.
func (t *telnetClient) option(cmd, opt byte) {
	switch cmd {
	case tDO:
		switch opt {
		case optBinary, optSGA, optTType:
			t.send(tIAC, tWILL, opt)
		case optNAWS:
			t.send(tIAC, tWILL, opt)
			t.naws()
		default:
			t.send(tIAC, tWONT, opt)
		}
	case tWILL:
		switch opt {
		case optBinary, optEcho, optSGA:
			t.send(tIAC, tDO, opt)
		default:
			t.send(tIAC, tDONT, opt)
		}
	case tDONT:
		t.send(tIAC, tWONT, opt)
	case tWONT:
		t.send(tIAC, tDONT, opt)
	}
}

// naws tells the server the caller's screen size (80 by 24 if not known).
func (t *telnetClient) naws() {
	cols, rows := t.in.Cols, t.in.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	b := []byte{tIAC, tSB, optNAWS}
	for _, v := range []byte{byte(cols >> 8), byte(cols), byte(rows >> 8), byte(rows)} {
		b = append(b, v)
		if v == tIAC {
			b = append(b, tIAC)
		}
	}
	t.send(append(b, tIAC, tSE)...)
}

// subneg answers TERMINAL-TYPE SEND.
func (t *telnetClient) subneg() {
	if len(t.sb) >= 2 && t.sb[0] == optTType && t.sb[1] == 1 {
		term := "ANSI"
		if !t.in.ANSI {
			term = "DUMB"
		}
		t.send(append(append([]byte{tIAC, tSB, optTType, 0}, term...), tIAC, tSE)...)
	}
}

// runRemote joins the caller to a door elsewhere until either side goes.
func (d *Door) runRemote(line Line, in *Info, utf8Term bool, limit, idle time.Duration,
	logf func(string, ...any)) (Result, error) {
	conn, err := d.Remote.Dial(in, logf)
	if err != nil {
		return Result{}, err
	}
	logf("door %s: connected (%s)", d.Name, d.Remote.Kind)
	exited := make(chan error, 1)
	from := &endReader{rc: conn, end: func() {
		select {
		case exited <- nil: // the far side went: the door has ended
		default:
		}
	}}
	stop := func() { conn.Close() }
	res := d.bridge(line, conn, from, exited, stop, func() {}, utf8Term, limit, idle)
	conn.Close()
	logf("door %s: ended (time up %v, hung up %v, idle %v)", d.Name, res.TimeUp, res.HungUp, res.Idle)
	return res, nil
}

// endReader tells when its reader ends.
type endReader struct {
	rc   io.ReadCloser
	end  func()
	once sync.Once
}

// Read reads from the reader, calling end, once, at its first error.
func (e *endReader) Read(p []byte) (int, error) {
	n, err := e.rc.Read(p)
	if err != nil {
		e.once.Do(e.end)
	}
	return n, err
}

// Close closes the reader.
func (e *endReader) Close() error { return e.rc.Close() }
