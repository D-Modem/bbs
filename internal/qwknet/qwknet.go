// Package qwknet joins the BBS to QWK networks as a node: it calls the
// network's hub by FTP as the account the hub gave it, puts a REP of what
// was written here in the network's areas, and takes the hub's packet of
// what was written elsewhere.
package qwknet

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/qwk"
	"github.com/D-Modem/bbs/internal/store"
)

// Net is a QWK network this BBS is a node of ([qwknet NAME]).
type Net struct {
	Name     string
	Address  string // the hub's FTP server, host:port
	TLS      string // no, explicit (AUTH TLS), implicit
	User     string // the hub's account for this node
	Password string
	Dir      string        // where the hub keeps its packet ("/qwk"; "/" for one in the root)
	HubID    string        // the hub's BBSID ("": the packet found there)
	Every    time.Duration // between calls (0: only when asked)
	// Confs are the hub's conferences carried here, and their areas
	Confs map[int]string
	// Insecure skips checking the hub's certificate (a test's)
	Insecure bool
}

// Most a call takes and gives.
const (
	maxOut    = 1000     // messages in a REP
	maxPacket = 64 << 20 // bytes of a packet
)

// Result is what came of a call.
type Result struct {
	Sent, Received, Skipped int
}

// String is the result as the log says it.
func (r Result) String() string {
	return fmt.Sprintf("%d sent, %d received, %d passed over", r.Sent, r.Received, r.Skipped)
}

// key is what this network's messages are known by here: by conference,
// for hubs that number each conference's messages from 1.
func (n *Net) key(conf int) string { return fmt.Sprintf("qwk:%s/%d", n.Name, conf) }

// origin is what this network is known by here: the peer whose last message
// sent is kept, and the origin of messages from it.
func (n *Net) origin() string { return "qwk:" + n.Name }

// areas are the network's areas here, by conference, and the other way.
func (n *Net) areas(st *store.Store) (map[int]*store.Area, map[int64]int, error) {
	byConf, byArea := map[int]*store.Area{}, map[int64]int{}
	for conf, name := range n.Confs {
		a, err := st.AreaByName(name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("conference %d: there is no area %q", conf, name)
		}
		if err != nil {
			return nil, nil, err
		}
		byConf[conf], byArea[a.ID] = a, conf
	}
	return byConf, byArea, nil
}

// dial connects to the hub, by TLS as set, and logs in as the node.
func (n *Net) dial() (*ftp.ServerConn, error) {
	opts := []ftp.DialOption{ftp.DialWithTimeout(60 * time.Second)}
	host, _, _ := strings.Cut(n.Address, ":")
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: n.Insecure}
	switch n.TLS {
	case "explicit":
		opts = append(opts, ftp.DialWithExplicitTLS(cfg))
	case "implicit":
		opts = append(opts, ftp.DialWithTLS(cfg))
	}
	c, err := ftp.Dial(n.Address, opts...)
	if err != nil {
		return nil, err
	}
	if err := c.Login(n.User, n.Password); err != nil {
		c.Quit()
		return nil, fmt.Errorf("login: %w", err)
	}
	return c, nil
}

// hubID is the hub's BBSID: as set, or the packet's name in its
// directory.
func (n *Net) hubID(c *ftp.ServerConn) (string, error) {
	if n.HubID != "" {
		return n.HubID, nil
	}
	es, err := c.List(n.Dir)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", n.Dir, err)
	}
	for _, e := range es {
		if id, ok := strings.CutSuffix(strings.ToUpper(e.Name), ".QWK"); ok && e.Type == ftp.EntryTypeFile && qwk.ValidID(id) {
			return id, nil
		}
	}
	return "", fmt.Errorf("no packet in %s (set hub_id)", n.Dir)
}

// Call calls the hub: our REP up, its packet down.
func (n *Net) Call(b *bbs.BBS) (Result, error) {
	var res Result
	byConf, byArea, err := n.areas(b.Store)
	if err != nil {
		return res, err
	}
	c, err := n.dial()
	if err != nil {
		return res, err
	}
	defer c.Quit()
	id, err := n.hubID(c)
	if err != nil {
		return res, err
	}

	// what was written here since the last call
	peer := n.origin()
	after, ok, err := b.Store.PeerPushed(peer)
	if err != nil {
		return res, err
	}
	if !ok {
		// the first call: what is here already is not sent
		if after, err = b.Store.MaxMessageID(); err != nil {
			return res, err
		}
		if err := b.Store.SetPeerPushed(peer, after); err != nil {
			return res, err
		}
	}
	ids := make([]int64, 0, len(byArea))
	for a := range byArea {
		ids = append(ids, a)
	}
	out, err := b.Store.LocalSince(after, ids, maxOut)
	if err != nil {
		return res, err
	}
	if len(out) > 0 {
		var ms []qwk.Message
		for _, m := range out {
			conf := byArea[m.AreaID]
			qm := qwk.Message{Conf: conf, To: m.To, From: m.From, Subject: m.Subject, Date: m.Created, Body: m.Body}
			if m.ReplyTo > 0 {
				if num, err := b.Store.QWKNumber(n.key(conf), m.ReplyTo); err == nil {
					qm.Ref = num
				}
			}
			ms = append(ms, qm)
		}
		var rep bytes.Buffer
		if err := qwk.WriteREP(&rep, id, ms); err != nil {
			return res, err
		}
		if err := c.Stor(path.Join(n.Dir, id+".REP"), &rep); err != nil {
			return res, fmt.Errorf("put %s.REP: %w", id, err)
		}
		if err := b.Store.SetPeerPushed(peer, out[len(out)-1].ID); err != nil {
			return res, err
		}
		res.Sent = len(out)
	}

	// what was written elsewhere
	r, err := c.Retr(path.Join(n.Dir, id+".QWK"))
	if err != nil {
		return res, fmt.Errorf("get %s.QWK: %w", id, err)
	}
	data, err := io.ReadAll(io.LimitReader(r, maxPacket+1))
	r.Close()
	if err != nil {
		return res, fmt.Errorf("get %s.QWK: %w", id, err)
	}
	if len(data) > maxPacket {
		return res, fmt.Errorf("%s.QWK is over %d bytes", id, maxPacket)
	}
	p, err := qwk.ReadPacket(data)
	if err != nil {
		return res, fmt.Errorf("%s.QWK: %w", id, err)
	}
	for _, m := range p.Messages {
		a := byConf[m.Conf]
		if a == nil || m.Conf == 0 {
			res.Skipped++ // not carried here (or mail to the node's account)
			continue
		}
		key := n.key(m.Conf)
		if _, err := b.Store.QWKMessage(key, m.Number); err == nil {
			res.Skipped++ // had already
			continue
		}
		var reply int64
		if m.Ref > 0 {
			if pid, err := b.Store.QWKMessage(key, m.Ref); err == nil {
				reply = pid
			}
		}
		mid, err := b.PostNet(nil, a, m.From, m.To, m.Subject, m.Body, reply, n.origin())
		if err != nil {
			b.Log.Printf("qwknet %s: message %d of conference %d: %v", n.Name, m.Number, m.Conf, err)
			res.Skipped++
			continue
		}
		if err := b.Store.MarkQWKMessage(key, m.Number, mid); err != nil {
			return res, err
		}
		res.Received++
	}
	return res, nil
}

// Runner calls the networks, each when it is due, and when asked.
type Runner struct {
	B    *bbs.BBS
	Nets []*Net
	mu   sync.Mutex // one call at a time
}

// CallNow calls a network (or all: name "") now.
func (r *Runner) CallNow(name string) error {
	var errs []error
	found := false
	for _, n := range r.Nets {
		if name != "" && !strings.EqualFold(n.Name, name) {
			continue
		}
		found = true
		if err := r.call(n); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Name, err))
		}
	}
	if !found {
		return fmt.Errorf("no [qwknet %s]", name)
	}
	return errors.Join(errs...)
}

// call calls a network, after any call under way, and logs what came of it.
func (r *Runner) call(n *Net) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := n.Call(r.B)
	if err != nil {
		r.B.Log.Printf("qwknet %s: %v (%s)", n.Name, err, res)
		return err
	}
	r.B.Log.Printf("qwknet %s: %s", n.Name, res)
	return nil
}

// Run calls each network every so often until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, n := range r.Nets {
		if n.Every <= 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTimer(30 * time.Second) // the first a little after the start
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					r.call(n)
					t.Reset(n.Every)
				}
			}
		}()
	}
	wg.Wait()
}
