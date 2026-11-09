package quotes

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// HISTURL lists IEX's historical files: for each day, the feeds' pcap
// files and their sizes.  They are free, from the day after the session.
const HISTURL = "https://iextrading.com/api/1.0/hist"

// Credit is IEX's, which its terms for HIST data ask for.
const Credit = "Data provided for free by IEX (iextrading.com). View IEX's Terms of Use."

// HISTFile is one of the HIST list's files.
type HISTFile struct {
	Link     string `json:"link"`
	Date     string `json:"date"`
	Feed     string `json:"feed"`
	Version  string `json:"version"`
	Protocol string `json:"protocol"`
	Size     int64  `json:"size,string"`
}

// LatestTOPS is the newest day's TOPS file (or that day's, YYYYMMDD).
func LatestTOPS(ctx context.Context, cl *http.Client, ua, date string) (*HISTFile, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", HISTURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", HISTURL, resp.Status)
	}
	var days map[string][]HISTFile
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&days); err != nil {
		return nil, fmt.Errorf("%s: %w", HISTURL, err)
	}
	var dates []string
	for d := range days {
		if date == "" || d == date {
			dates = append(dates, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	for _, d := range dates {
		for _, f := range days[d] {
			if f.Feed == "TOPS" && f.Link != "" {
				return &f, nil
			}
		}
	}
	if date != "" {
		return nil, fmt.Errorf("IEX has no TOPS file for %s", date)
	}
	return nil, errors.New("IEX lists no TOPS files")
}

// Progress is told how far a reading has come.
type Progress func(read int64)

// countReader counts what is read through it.
type countReader struct {
	r    io.Reader
	n    int64
	tell Progress
	next int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.tell != nil && c.n >= c.next {
		c.tell(c.n)
		c.next = c.n + 256<<20
	}
	return n, err
}

// ReadCapture reads a TOPS (or DEEP) capture, gzipped or not, into b.
func ReadCapture(r io.Reader, b *Book, tell Progress) error {
	cr := &countReader{r: r, tell: tell}
	br := bufio.NewReaderSize(cr, 1<<20)
	in := io.Reader(br)
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		zr.Multistream(true)
		in = zr
	}
	return Packets(in, func(p []byte) error {
		if err := Segment(p, b); err != nil && !errors.Is(err, errSegment) {
			return err
		}
		return nil
	})
}

// FetchHIST reads a HIST file from IEX straight into b, as it comes: it
// is not kept, which spares the disk (the files are some 16 GB a day).  A
// download cut off is taken up where it stopped (by a range request), a
// few times.
func FetchHIST(ctx context.Context, cl *http.Client, ua string, f *HISTFile, b *Book, tell Progress) error {
	rr := &resumer{ctx: ctx, cl: cl, ua: ua, url: f.Link, tries: 8}
	defer rr.close()
	return ReadCapture(rr, b, tell)
}

// resumer reads a URL, asking again from where it was when the connection
// fails.
type resumer struct {
	ctx   context.Context
	cl    *http.Client
	ua    string
	url   string
	tries int
	off   int64
	body  io.ReadCloser
}

func (r *resumer) open() error {
	req, err := http.NewRequestWithContext(r.ctx, "GET", r.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", r.ua)
	if r.off > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", r.off))
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return err
	}
	switch {
	case r.off == 0 && resp.StatusCode == 200, r.off > 0 && resp.StatusCode == 206:
		r.body = resp.Body
		return nil
	}
	resp.Body.Close()
	return fmt.Errorf("%s: %s", r.url, resp.Status)
}

func (r *resumer) Read(p []byte) (int, error) {
	for {
		if r.body == nil {
			if err := r.open(); err != nil {
				if r.tries--; r.tries < 0 || r.ctx.Err() != nil {
					return 0, err
				}
				time.Sleep(5 * time.Second)
				continue
			}
		}
		n, err := r.body.Read(p)
		r.off += int64(n)
		if err == nil || err == io.EOF {
			return n, err
		}
		// cut off: again from here
		r.body.Close()
		r.body = nil
		if r.tries--; r.tries < 0 || r.ctx.Err() != nil {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}
}

func (r *resumer) close() {
	if r.body != nil {
		r.body.Close()
	}
}

// Board is the quotes the callers see: the newest snapshot made (read
// again when its file changes), or a live feed's book.
type Board struct {
	Path string // the snapshot file ("": none)
	Live *Book  // a live feed (nil: none)
	// LiveDelay is how far behind the live feed is ("": real time)
	LiveDelay string
	// LiveSource names the live feed ("": IEX TOPS); LiveScope and
	// LiveCredit are its Snapshot's Scope and Credit
	LiveSource, LiveScope, LiveCredit string
	// Fetch, if set, is asked for symbols a caller is about to see (the
	// live feed's quotes are only of those asked for: Alpaca's)
	Fetch func(ctx context.Context, syms []string)

	mu    sync.Mutex
	snap  *Snapshot
	mtime time.Time
	err   error
}

// Current is the quotes as they are now; nil (and why) if there are none.
func (b *Board) Current() (*Snapshot, error) {
	if b.Live != nil {
		src := b.LiveSource
		if src == "" {
			src = "IEX TOPS"
		}
		s := b.Live.Snapshot(src + " (live)")
		s.Scope, s.Credit = b.LiveScope, b.LiveCredit
		if len(s.Quotes) > 0 || b.Path == "" {
			return s, nil
		}
	}
	if b.Path == "" {
		return nil, errors.New("no quotes are configured")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	fi, err := os.Stat(b.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("the quotes have not been fetched yet")
		}
		return nil, err
	}
	if b.snap == nil || !fi.ModTime().Equal(b.mtime) {
		b.snap, b.err = Load(b.Path)
		b.mtime = fi.ModTime()
	}
	return b.snap, b.err
}

// Want: the symbols are about to be seen, so a source that fetches them
// on demand is asked for them (a few seconds at most).
func (b *Board) Want(syms ...string) {
	if b.Fetch == nil || len(syms) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.Fetch(ctx, syms)
}

// Listen reads a live TOPS feed from UDP (a multicast group, joined on
// the interface named, or a unicast port) into b until ctx is done.
func Listen(ctx context.Context, addr, iface string, b *Book) error {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	var c *net.UDPConn
	if ua.IP != nil && ua.IP.IsMulticast() {
		var ifi *net.Interface
		if iface != "" {
			if ifi, err = net.InterfaceByName(iface); err != nil {
				return err
			}
		}
		c, err = net.ListenMulticastUDP("udp", ifi, ua)
	} else {
		c, err = net.ListenUDP("udp", ua)
	}
	if err != nil {
		return err
	}
	c.SetReadBuffer(8 << 20)
	go func() { <-ctx.Done(); c.Close() }()
	buf := make([]byte, 65536)
	for {
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		Segment(buf[:n], b)
	}
}

// Normalize is a symbol as a caller types it, as IEX has it ("brk.b" is
// "BRK.B"); "" if it is not one.
func Normalize(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" || len(s) > 8 {
		return ""
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '+' || r == '=' || r == '^' || r == '#' || r == '*') {
			return ""
		}
	}
	return s
}
