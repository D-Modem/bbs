package nntp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Peer is a news server this one exchanges newsgroups with ([peer NAME]).
type Peer struct {
	Name     string
	Address  string // host:port
	TLS      string // no, starttls, implicit
	CAFile   string // a CA for its certificate (else the system's)
	Username string // AUTHINFO, if it wants it
	Password string
	Groups   string // wildmat of the newsgroups exchanged
	Pull     bool   // fetch its new articles
	Push     bool   // offer it ours (IHAVE, or POST if it takes no IHAVE)
	Interval time.Duration
	// addresses it connects from: it may IHAVE (and read)
	AcceptFrom []net.IP
	// the most articles a run takes or gives, each way
	MaxPerRun int
	// articles fetched of a group the first time (its newest)
	Initial int
}

const peerTimeout = 60 * time.Second

// client is a connection to a peer.
type client struct {
	c net.Conn
	r *bufio.Reader
	w *bufio.Writer
}

func (p *Peer) tlsConfig() (*tls.Config, error) {
	host, _, _ := net.SplitHostPort(p.Address)
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if p.CAFile != "" {
		pem, err := os.ReadFile(p.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificates in " + p.CAFile)
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

// dial connects, with TLS and AUTHINFO as the peer wants; reader asks for
// MODE READER (for pulling).
func (p *Peer) dial(reader bool) (*client, error) {
	d := net.Dialer{Timeout: 30 * time.Second}
	c, err := d.Dial("tcp", p.Address)
	if err != nil {
		return nil, err
	}
	if p.TLS == "implicit" {
		cfg, err := p.tlsConfig()
		if err != nil {
			c.Close()
			return nil, err
		}
		tc := tls.Client(c, cfg)
		tc.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tc.Handshake(); err != nil {
			c.Close()
			return nil, err
		}
		tc.SetDeadline(time.Time{})
		c = tc
	}
	cl := &client{c: c, r: bufio.NewReaderSize(c, 16384), w: bufio.NewWriter(c)}
	code, msg, err := cl.response()
	if err != nil {
		c.Close()
		return nil, err
	}
	if code != 200 && code != 201 {
		c.Close()
		return nil, fmt.Errorf("greeting: %d %s", code, msg)
	}
	if p.TLS == "starttls" {
		if code, msg, err := cl.cmd("STARTTLS"); err != nil || code != 382 {
			c.Close()
			return nil, fmt.Errorf("STARTTLS: %d %s %v", code, msg, err)
		}
		cfg, err := p.tlsConfig()
		if err != nil {
			c.Close()
			return nil, err
		}
		tc := tls.Client(c, cfg)
		tc.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tc.Handshake(); err != nil {
			c.Close()
			return nil, err
		}
		tc.SetDeadline(time.Time{})
		cl.c, cl.r, cl.w = tc, bufio.NewReaderSize(tc, 16384), bufio.NewWriter(tc)
	}
	if reader {
		cl.cmd("MODE READER") // servers that are readers anyway may say 5xx
	}
	if p.Username != "" {
		code, msg, err := cl.cmd("AUTHINFO USER " + p.Username)
		if err == nil && code == 381 {
			code, msg, err = cl.cmd("AUTHINFO PASS " + p.Password)
		}
		if err != nil || code != 281 {
			cl.close()
			return nil, fmt.Errorf("AUTHINFO: %d %s %v", code, msg, err)
		}
	}
	return cl, nil
}

func (cl *client) close() {
	cl.cmd("QUIT")
	cl.c.Close()
}

func (cl *client) response() (int, string, error) {
	cl.c.SetReadDeadline(time.Now().Add(peerTimeout))
	defer cl.c.SetReadDeadline(time.Time{})
	line, err := cl.r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	if len(line) > 4096 {
		return 0, "", errors.New("response line too long")
	}
	line = strings.TrimRight(line, "\r\n")
	code, msg, _ := strings.Cut(line, " ")
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0, "", fmt.Errorf("bad response %q", line)
	}
	return n, msg, nil
}

func (cl *client) cmd(line string) (int, string, error) {
	cl.c.SetWriteDeadline(time.Now().Add(peerTimeout))
	if _, err := cl.w.WriteString(line + "\r\n"); err != nil {
		return 0, "", err
	}
	if err := cl.w.Flush(); err != nil {
		return 0, "", err
	}
	return cl.response()
}

// block reads a dot-terminated block of at most max bytes.
func (cl *client) block(max int) ([]byte, error) {
	var b bytes.Buffer
	tooBig := false
	for {
		cl.c.SetReadDeadline(time.Now().Add(peerTimeout))
		line, err := cl.r.ReadBytes('\n')
		cl.c.SetReadDeadline(time.Time{})
		if err != nil {
			return nil, err
		}
		if bytes.Equal(line, []byte(".\r\n")) || bytes.Equal(line, []byte(".\n")) {
			if tooBig {
				return nil, news.ErrTooLarge
			}
			return b.Bytes(), nil
		}
		if line[0] == '.' {
			line = line[1:]
		}
		if b.Len()+len(line) > max {
			tooBig = true
			continue
		}
		b.Write(line)
	}
}

// send sends an article, dot-stuffed and ended.
func (cl *client) send(raw []byte) (int, string, error) {
	cl.c.SetWriteDeadline(time.Now().Add(peerTimeout))
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
			cl.w.WriteByte('.')
		}
		cl.w.Write(line)
		cl.w.WriteString("\r\n")
	}
	cl.w.WriteString(".\r\n")
	if err := cl.w.Flush(); err != nil {
		return 0, "", err
	}
	return cl.response()
}

// groupsFor are the local newsgroups exchanged with p.
func (s *Service) groupsFor(p *Peer) ([]*store.Area, error) {
	all, err := s.B.Store.Newsgroups()
	if err != nil {
		return nil, err
	}
	var out []*store.Area
	for _, a := range all {
		if news.Wildmat(p.Groups, a.Newsgroup) {
			out = append(out, a)
		}
	}
	return out, nil
}

// Sync runs one exchange with a peer: ours to it, then its to us.
func (s *Service) Sync(p *Peer) (pushed, pulled int, err error) {
	if p.Push {
		pushed, err = s.push(p)
		if err != nil {
			return pushed, 0, fmt.Errorf("push: %w", err)
		}
	}
	if p.Pull {
		pulled, err = s.pull(p)
		if err != nil {
			return pushed, pulled, fmt.Errorf("pull: %w", err)
		}
	}
	return pushed, pulled, nil
}

// push offers the peer what came here (from here, or other peers) since
// the last run.  The first run only starts the count: the peer is not
// sent what was here before it was one.
func (s *Service) push(p *Peer) (int, error) {
	st := s.B.Store
	after, known, err := st.PeerPushed(p.Name)
	if err != nil {
		return 0, err
	}
	if !known {
		max, err := st.MaxMessageID()
		if err != nil {
			return 0, err
		}
		return 0, st.SetPeerPushed(p.Name, max)
	}
	areas, err := s.groupsFor(p)
	if err != nil || len(areas) == 0 {
		return 0, err
	}
	var ids []int64
	for _, a := range areas {
		ids = append(ids, a.ID)
	}
	ms, err := st.ToPush(p.Name, after, ids, p.MaxPerRun)
	if err != nil || len(ms) == 0 {
		// nothing to give: still move past what was skipped
		return 0, err
	}
	cl, err := p.dial(false)
	if err != nil {
		return 0, err
	}
	defer cl.close()
	usePost := false
	n := 0
	for _, m := range ms {
		raw, err := st.Raw(m.ID)
		if err != nil || len(raw) == 0 {
			continue
		}
		if !usePost {
			code, msg, err := cl.cmd("IHAVE " + m.MessageID)
			if err != nil {
				return n, err
			}
			switch code {
			case 335:
				code, msg, err = cl.send(raw)
				if err != nil {
					return n, err
				}
				if code == 436 {
					return n, fmt.Errorf("%s: try again later: %s", m.MessageID, msg)
				}
				if code == 235 {
					n++
				}
				// 437: refused for good; on to the next
			case 435:
				// it has it
			case 436:
				return n, fmt.Errorf("IHAVE %s: try again later: %s", m.MessageID, msg)
			case 480, 500, 502:
				usePost = true
			default:
				return n, fmt.Errorf("IHAVE: %d %s", code, msg)
			}
		}
		if usePost {
			code, msg, err := cl.cmd("POST")
			if err != nil {
				return n, err
			}
			if code != 340 {
				return n, fmt.Errorf("POST: %d %s", code, msg)
			}
			code, _, err = cl.send(raw)
			if err != nil {
				return n, err
			}
			if code == 240 {
				n++
			}
		}
		if err := st.SetPeerPushed(p.Name, m.ID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// pull fetches the peer's new articles of the newsgroups exchanged, by
// article number since the last run (the newest Initial ones the first
// time).
func (s *Service) pull(p *Peer) (int, error) {
	areas, err := s.groupsFor(p)
	if err != nil || len(areas) == 0 {
		return 0, err
	}
	cl, err := p.dial(true)
	if err != nil {
		return 0, err
	}
	defer cl.close()
	st := s.B.Store
	total := 0
	for _, a := range areas {
		if total >= p.MaxPerRun {
			break
		}
		code, msg, err := cl.cmd("GROUP " + a.Newsgroup)
		if err != nil {
			return total, err
		}
		if code == 411 {
			continue // not there
		}
		if code != 211 {
			return total, fmt.Errorf("GROUP %s: %d %s", a.Newsgroup, code, msg)
		}
		f := strings.Fields(msg)
		if len(f) < 3 {
			return total, fmt.Errorf("GROUP %s: %q", a.Newsgroup, msg)
		}
		lo, _ := strconv.ParseInt(f[1], 10, 64)
		hi, _ := strconv.ParseInt(f[2], 10, 64)
		last, known, err := st.PeerLast(p.Name, a.Newsgroup)
		if err != nil {
			return total, err
		}
		if !known {
			// the first time: from the newest Initial on, remembered
			// even if there is nothing to take yet
			last = max(hi-int64(p.Initial), lo-1, 0)
			if err := st.SetPeerLast(p.Name, a.Newsgroup, last); err != nil {
				return total, err
			}
		}
		if hi < last {
			last = lo - 1 // the peer renumbered
		}
		if hi <= last {
			continue
		}
		to := min(hi, last+int64(p.MaxPerRun-total))
		nums, err := cl.overview(last+1, to)
		if err != nil {
			return total, err
		}
		for _, item := range nums {
			if item.id != "" {
				if want, err := s.Wanted(item.id); err != nil {
					return total, err
				} else if !want {
					continue
				}
			}
			code, msg, err := cl.cmd(fmt.Sprintf("ARTICLE %d", item.n))
			if err != nil {
				return total, err
			}
			if code == 423 || code == 430 {
				continue
			}
			if code != 220 {
				return total, fmt.Errorf("ARTICLE %d: %d %s", item.n, code, msg)
			}
			raw, err := cl.block(s.MaxArticle)
			if errors.Is(err, news.ErrTooLarge) {
				if item.id != "" {
					st.Remember(item.id)
				}
				continue
			}
			if err != nil {
				return total, err
			}
			if _, err := s.Accept(raw, item.id, p.Name); err == nil {
				total++
			} else if !errors.Is(err, ErrHave) {
				s.B.Log.Printf("nntp: peer %s: %s article %d: %v", p.Name, a.Newsgroup, item.n, err)
			}
		}
		if err := st.SetPeerLast(p.Name, a.Newsgroup, to); err != nil {
			return total, err
		}
	}
	return total, nil
}

type overItem struct {
	n  int64
	id string
}

// overview lists the articles from lo to hi with their Message-IDs (OVER,
// or XOVER; failing both, the numbers alone).
func (cl *client) overview(lo, hi int64) ([]overItem, error) {
	for _, cmd := range []string{"OVER", "XOVER"} {
		code, _, err := cl.cmd(fmt.Sprintf("%s %d-%d", cmd, lo, hi))
		if err != nil {
			return nil, err
		}
		if code == 423 || code == 420 {
			return nil, nil
		}
		if code != 224 {
			continue
		}
		data, err := cl.block(64 << 20)
		if err != nil {
			return nil, err
		}
		var out []overItem
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Split(strings.TrimRight(line, "\r"), "\t")
			if len(f) < 5 {
				continue
			}
			n, err := strconv.ParseInt(f[0], 10, 64)
			if err != nil {
				continue
			}
			id := strings.TrimSpace(f[4])
			if !news.ValidMessageID(id) {
				id = ""
			}
			out = append(out, overItem{n, id})
		}
		return out, nil
	}
	var out []overItem
	for n := lo; n <= hi; n++ {
		out = append(out, overItem{n: n})
	}
	return out, nil
}

// RunPeers syncs with each peer at its interval until ctx is done.
func (s *Service) RunPeers(ctx context.Context, peers []*Peer) {
	for _, p := range peers {
		if !p.Pull && !p.Push {
			continue
		}
		go func(p *Peer) {
			t := time.NewTimer(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				pushed, pulled, err := s.Sync(p)
				if err != nil {
					s.B.Log.Printf("nntp: peer %s: %v", p.Name, err)
				} else if pushed+pulled > 0 {
					s.B.Log.Printf("nntp: peer %s: %d sent, %d taken", p.Name, pushed, pulled)
				}
				t.Reset(p.Interval)
			}
		}(p)
	}
}
