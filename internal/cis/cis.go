// Package cis is a CompuServe Information Service front end on the BBS:
// the network's "Host Name:", CompuServe's "User ID:" and "Password:", the
// terminal's identification (ESC I), and the ASCII service: its menu and
// article pages, GO words and the "!" prompts, as a terminal caller of
// the 1980s and 90s saw them.  Offline readers (TAPCIS, AutoSIG, OzCIS)
// match these strings, so they keep the period's wording.
//
// Users are the BBS's: a User ID is the account's number written as a
// CompuServe ID (PPN), or its name.  A CIM client (one whose ESC I answer
// has AP) gets the GUI service instead: HMI over the B Plus Transport
// Layer (hmi.go).
package cis

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/cis/termid"
	"github.com/D-Modem/bbs/internal/store"
)

// Line is the caller's connection: telnet (in binary mode) or raw.
type Line interface {
	io.Reader
	io.Writer
	SetReadDeadline(time.Time) error
}

// Options are a listener's: Connect, a "CONNECT <rate>" line before the
// Host Name prompt (what a dial-up node's caller saw first, and what
// CIM's scripts over TCP wait for); "" for none (a modem caller has had
// the modem's).
type Options struct {
	Connect string
}

// session is a caller's time on the service.
type session struct {
	b     *bbs.BBS
	node  *bbs.Node
	line  Line
	r     *bufio.Reader
	opt   Options
	user  *store.User
	caps  *termid.Caps
	start time.Time
	now   func() time.Time

	// session settings (SET)
	brief bool
	paged bool
	width int
	lines int

	pages   *Pages
	page    *Page   // the page shown
	stack   []*Page // pages gone through, for B
	menus   []*Page // menus gone through, for M
	more    []string
	firstGo string // /GO:word at the User ID
	tty     bool   // ;TTY at the User ID: not the kept terminal settings
	byName  bool   // logged on with a BBS name, not a User ID
	fpage   *Page  // the FORUMS menu, as last made
	mail    *mailState
	pushed  []byte // input put back
	cr      bool   // the last input byte was a CR (drop a LF or NUL after it)
}

// errHangup ends a call: OFF, BYE or the caller gone.
var errHangup = errors.New("cis: logged off")

// Run is a call on the service, from the Host Name prompt to the logoff.
func Run(b *bbs.BBS, node *bbs.Node, line Line, opt Options) error {
	s := &session{b: b, node: node, line: line, r: bufio.NewReader(line), opt: opt,
		start: time.Now(), now: time.Now, width: 80, pages: DefaultPages()}
	err := s.run()
	if errors.Is(err, errHangup) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (s *session) run() error {
	s.node.Doing("CompuServe: logging in")
	if s.opt.Connect != "" {
		s.out("\nCONNECT " + s.opt.Connect + "\n")
	}
	if err := s.hostName(); err != nil {
		return err
	}
	for {
		u, err := s.login()
		if err != nil {
			return err
		}
		s.user = u
		s.loadSettings(s.tty)
		if err := s.identify(); err != nil {
			return err
		}
		if s.caps.AP() {
			return s.hmi() // a CIM client
		}
		err = s.ascii()
		if !errors.Is(err, errRelog) {
			return err
		}
		s.user = nil
	}
}

// out writes text, its newlines as CR LF.
func (s *session) out(text string) error {
	_, err := io.WriteString(s.line, strings.ReplaceAll(text, "\n", "\r\n"))
	return err
}

func (s *session) outf(format string, a ...any) error { return s.out(fmt.Sprintf(format, a...)) }

// errors of input
var (
	errAbort = errors.New("cis: ^C") // the line given up
	errIdle  = errors.New("cis: idle")
)

func (s *session) getc(d time.Duration) (byte, error) {
	if len(s.pushed) > 0 {
		c := s.pushed[0]
		s.pushed = s.pushed[1:]
		return c, nil
	}
	if d > 0 {
		s.line.SetReadDeadline(time.Now().Add(d))
		defer s.line.SetReadDeadline(time.Time{})
	}
	c, err := s.r.ReadByte()
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return 0, errIdle
	}
	return c, err
}

// input reads a line, echoed (or, for a password, not), within d a key.
// ^U throws away what was typed, ^V types it again, ^C and ^P give the
// line up (errAbort).
func (s *session) input(echo bool, d time.Duration) (string, error) {
	var b []byte
	for {
		c, err := s.getc(d)
		if err != nil {
			return "", err
		}
		if s.cr && (c == '\n' || c == 0) {
			s.cr = false
			continue
		}
		s.cr = false
		switch c {
		case '\r', '\n':
			s.cr = c == '\r'
			s.out("\n")
			return string(b), nil
		case 8, 0x7F:
			if len(b) > 0 {
				b = b[:len(b)-1]
				if echo {
					s.out("\b \b")
				}
			}
		case 0x15: // ^U: echoed only if there was something to throw away
			if len(b) > 0 && echo {
				s.out("^U\n")
			}
			b = b[:0]
		case 0x16: // ^V
			if echo {
				s.out("\n" + string(b))
			}
		case 3, 0x10: // ^C, ^P
			s.out("^C\n")
			return "", errAbort
		default:
			if c < 0x20 || len(b) >= 255 {
				continue
			}
			b = append(b, c)
			if echo {
				s.line.Write([]byte{c})
			}
		}
	}
}

// prompt asks and reads the answer, after the login: an idle caller is
// logged off.
func (s *session) prompt(p string) (string, error) {
	s.out(p)
	line, err := s.input(true, s.b.Conf().IdleTimeout)
	if errors.Is(err, errIdle) {
		s.out("\n\nYou have been idle too long.\n")
		s.logoff()
		return "", errHangup
	}
	return line, err
}

// choicePrompt is the prompt for a choice: "!" when brief.
func (s *session) choicePrompt(long string) string {
	if s.brief {
		return "!"
	}
	return long
}

// identify sends ESC I and reads the terminal's answer, if it gives one;
// what the caller typed meanwhile is kept.
func (s *session) identify() error {
	caps, junk, err := termid.Probe(s.line, s.r, 10, 1500*time.Millisecond)
	if err != nil {
		return err
	}
	s.pushed = append(s.pushed, junk...)
	if caps != nil {
		s.caps = caps
		s.node.SetIdent(caps.Raw)
	}
	return nil
}
