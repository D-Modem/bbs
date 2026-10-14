package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/confedit"
	"github.com/Neo-Desktop/bbs-sec/internal/confui"
	"github.com/Neo-Desktop/bbs-sec/internal/server"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/textui"
)

// configCmd edits the configuration file: the screens (on this
// terminal), or a key at a time.
func configCmd(path string, args []string) {
	if path == "" {
		fatal(errors.New("config: which file? bbs -config FILE config"))
	}
	if len(args) == 0 {
		configScreens(path)
		return
	}
	data, err := os.ReadFile(path)
	absent := errors.Is(err, os.ErrNotExist)
	if err != nil && !absent {
		fatal(err)
	}
	doc, err := confedit.Parse(data)
	if err != nil {
		fatal(fmt.Errorf("%s: %w", path, err))
	}
	switch {
	case args[0] == "get" && (len(args) == 2 || len(args) == 3):
		s := section(args[1])
		if !doc.Has(s) {
			fatal(fmt.Errorf("no %s", s))
		}
		if len(args) == 2 {
			for _, k := range doc.Keys(s) {
				v, _ := doc.Get(s, k)
				fmt.Printf("%s = %s\n", k, confedit.Quote(v))
			}
			return
		}
		v, ok := doc.Get(s, strings.ToLower(args[2]))
		if !ok {
			fatal(fmt.Errorf("%s %s is not set", s, args[2]))
		}
		fmt.Println(v)
		return
	case args[0] == "set" && len(args) == 4:
		if err := doc.Set(section(args[1]), args[2], args[3]); err != nil {
			fatal(err)
		}
	case args[0] == "unset" && len(args) == 3:
		if !doc.Unset(section(args[1]), strings.ToLower(args[2])) {
			fatal(fmt.Errorf("%s %s is not set", section(args[1]), args[2]))
		}
	default:
		usage()
	}
	out := doc.Bytes()
	cfg, err := server.Parse(out)
	if err != nil {
		fatal(fmt.Errorf("not written: the BBS would not start: %w", err))
	}
	for _, w := range cfg.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err := confedit.WriteFile(path, out, data, absent); err != nil {
		fatal(err)
	}
	old, _ := confedit.Parse(data)
	for _, c := range confedit.Diff(old, doc) {
		when := "live: a reload puts it in force"
		if !c.Live() {
			when = "when the BBS starts again"
		}
		fmt.Printf("%s  (%s)\n", c, when)
	}
}

// section reads "general", "listen telnet" or "[listen telnet]".
func section(s string) confedit.Section {
	f := strings.Fields(strings.Trim(strings.TrimSpace(s), "[]"))
	switch len(f) {
	case 1:
		return confedit.Section{Kind: strings.ToLower(f[0])}
	case 2:
		return confedit.Section{Kind: strings.ToLower(f[0]), Name: f[1]}
	}
	fatal(fmt.Errorf("%q: a section is KIND or \"KIND NAME\"", s))
	return confedit.Section{}
}

// configScreens are the configuration screens on this terminal.
func configScreens(path string) {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		fatal(errors.New("config: the screens want a terminal (config get, set and unset do not)"))
	}
	b := bbs.New(bbs.Defaults(), nil, log.New(io.Discard, "", 0))
	n, err := b.Enter(&bbs.Caller{Via: "console", Listener: "console"}, nil)
	if err != nil {
		fatal(err)
	}
	old, err := term.MakeRaw(in)
	if err != nil {
		fatal(err)
	}
	t := textui.NewTerm(newTTY(os.Stdin, os.Stdout))
	t.ANSI = os.Getenv("TERM") != "" && os.Getenv("TERM") != "dumb"
	t.UTF8 = utf8Locale()
	t.Clean8 = true
	t.Idle = 24 * time.Hour
	if cols, rows, err := term.GetSize(out); err == nil && cols > 0 {
		t.Cols, t.Rows = cols, rows
	}
	var st *store.Store
	ed := &confui.Editor{UI: textui.Console(b, n, t),
		Tools: bbs.ConfigTools{Path: path, Check: server.Check},
		Open: func() (*bbs.BBS, error) {
			// the database the file names, as it was read
			cfg, err := server.Load(path)
			if err != nil {
				return nil, err
			}
			if p := strings.TrimPrefix(cfg.Database, "sqlite:"); store.IsFile(cfg.Database) {
				if _, err := os.Stat(p); err != nil {
					return nil, fmt.Errorf("%s: no database yet (bbs init makes it)", p)
				}
			}
			if st, err = store.Open(cfg.Database); err != nil {
				return nil, err
			}
			return bbs.New(cfg.Settings, st, log.New(io.Discard, "", 0)), nil
		}}
	err = ed.Run()
	if st != nil {
		st.Close()
	}
	t.Flush()
	term.Restore(in, old)
	fmt.Println()
	if err != nil {
		fatal(err)
	}
}

func utf8Locale() bool {
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if l := os.Getenv(v); l != "" {
			l = strings.ToLower(l)
			return strings.Contains(l, "utf-8") || strings.Contains(l, "utf8")
		}
	}
	return false
}

// tty is this terminal as a textui.Conn: its keys read by a goroutine,
// so that a read can time out (as the terminal's questions want).
type tty struct {
	in       chan []byte
	pending  []byte
	deadline time.Time
	out      io.Writer
}

func newTTY(r io.Reader, w io.Writer) *tty {
	t := &tty{in: make(chan []byte, 16), out: w}
	go func() {
		for {
			buf := make([]byte, 256)
			n, err := r.Read(buf)
			if n > 0 {
				t.in <- buf[:n]
			}
			if err != nil {
				close(t.in)
				return
			}
		}
	}()
	return t
}

func (t *tty) Read(p []byte) (int, error) {
	if len(t.pending) == 0 {
		var timeout <-chan time.Time
		if !t.deadline.IsZero() {
			d := time.Until(t.deadline)
			if d <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			tm := time.NewTimer(d)
			defer tm.Stop()
			timeout = tm.C
		}
		select {
		case b, ok := <-t.in:
			if !ok {
				return 0, io.EOF
			}
			t.pending = b
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		}
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

func (t *tty) Write(p []byte) (int, error) { return t.out.Write(p) }

func (t *tty) SetReadDeadline(d time.Time) error {
	t.deadline = d
	return nil
}
