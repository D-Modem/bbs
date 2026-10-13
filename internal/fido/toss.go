package fido

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Limits on what is tossed.
const (
	maxPacket       = 32 << 20 // a packet, also one in a bundle
	maxBundleFiles  = 1000
	maxBundleUnpack = 256 << 20
)

// bundleName is an ARCmail bundle's name: 00a1b2c3.mo0, .tu1, ...
var bundleName = regexp.MustCompile(`(?i)\.(mo|tu|we|th|fr|sa|su)[0-9a-z]$`)

// TossStats counts what a toss did.
type TossStats struct {
	Echomail, Netmail, Dupes, Bad, Forwarded int
}

func (s TossStats) String() string {
	return fmt.Sprintf("%d echomail, %d netmail, %d dupes, %d bad", s.Echomail, s.Netmail, s.Dupes, s.Bad)
}

// Toss takes in what a link has sent: its packets, bare or bundled.
// Files that are neither are left for the sysop (in/LINK/files).
func (f *Fido) Toss(l *Link) (TossStats, error) {
	mu := f.lock(l)
	mu.Lock()
	defer mu.Unlock()
	return f.toss(l)
}

func (f *Fido) toss(l *Link) (TossStats, error) {
	var st TossStats
	dir := f.inbound(l)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return st, err
	}
	for _, e := range ents {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(strings.ToLower(name), ".pkt"):
			err = f.tossFile(l, path, &st)
		case bundleName.MatchString(name):
			err = f.tossBundle(l, path, &st)
		default:
			// a file echo's file, a TIC, a file request: not ours to do
			files := filepath.Join(dir, "files")
			os.MkdirAll(files, 0700)
			os.Rename(path, filepath.Join(files, name))
			f.B.Log.Printf("fido: %s sent %s: not mail, left in %s", l.Name, name, files)
			continue
		}
		if err != nil {
			f.B.Log.Printf("fido: %s: %s: %v (moved to bad)", l.Name, name, err)
			st.Bad++
			os.Rename(path, filepath.Join(f.S.Dir, "bad", l.Name+"-"+name))
			continue
		}
		os.Remove(path)
	}
	if st != (TossStats{}) {
		f.B.Log.Printf("fido: tossed from %s: %s", l.Name, st)
	}
	return st, nil
}

func (f *Fido) tossFile(l *Link, path string, st *TossStats) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	return f.tossPacket(l, io.LimitReader(fh, maxPacket), st)
}

func (f *Fido) tossBundle(l *Link, path string, st *TossStats) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return nil // an empty bundle: some mailers leave them
	}
	head := make([]byte, 8)
	n, _ := fh.ReadAt(head, 0)
	if archive.Detect(head[:n]) == "" {
		return errors.New("not a bundle: not ZIP, ARC, ARJ or LHA")
	}
	var total int64
	count := 0
	var werr error
	err = archive.Walk(fh, fi.Size(), func(e archive.Entry, open archive.Opener) bool {
		if count++; count > maxBundleFiles {
			werr = errors.New("too many files in the bundle")
			return false
		}
		if !strings.HasSuffix(strings.ToLower(e.Name), ".pkt") || strings.ContainsAny(e.Name, "/\\") {
			f.B.Log.Printf("fido: %s: %s in the bundle is not a packet: skipped", l.Name, e.Name)
			return true
		}
		if total += e.Size; e.Size > maxPacket || total > maxBundleUnpack {
			werr = errors.New("the bundle unpacks too big")
			return false
		}
		data, err := open(maxPacket)
		if err != nil {
			werr = fmt.Errorf("%s: %w", e.Name, err)
			return false
		}
		if err := f.tossPacket(l, bytes.NewReader(data), st); err != nil {
			werr = fmt.Errorf("%s: %w", e.Name, err)
			return false
		}
		return true
	})
	if werr != nil {
		return werr
	}
	return err
}

// tossPacket takes a packet from a link: from its address, to ours, with
// its password if it has one.
func (f *Fido) tossPacket(l *Link, r io.Reader, st *TossStats) error {
	pr, h, err := ftn.NewReader(r, f.S.MaxMessage)
	if err != nil {
		return err
	}
	if h.Orig.Zone == 0 {
		h.Orig.Zone = l.Addr.Zone
	}
	if !h.Orig.Same(l.Addr) {
		return fmt.Errorf("the packet is from %s, not %s", h.Orig, l.Addr)
	}
	if h.Dest.Zone == 0 {
		h.Dest.Zone = f.akaFor(l).Zone
	}
	if !f.ours(h.Dest) {
		return fmt.Errorf("the packet is for %s, not us", h.Dest)
	}
	if l.PktPassword != "" && !strings.EqualFold(h.Password, l.PktPassword) {
		return errors.New("wrong packet password")
	}
	for {
		m, err := pr.Next()
		if err == io.EOF {
			return nil
		}
		if errors.Is(err, ftn.ErrTooBig) {
			f.B.Log.Printf("fido: %s: a message too big: skipped", l.Name)
			st.Bad++
			continue
		}
		if err != nil {
			return err
		}
		if m.Area != "" {
			err = f.tossEcho(l, h, m, st)
		} else {
			err = f.tossNetmail(l, h, m, st)
		}
		if err != nil {
			return err
		}
	}
}

// text is a message's lines decoded and cleaned for callers.
func text(m *ftn.Message, dec func(string) string) string {
	lines := make([]string, len(m.Lines))
	for i, l := range m.Lines {
		lines[i] = dec(l)
	}
	body := bbs.CleanBody(lines)
	if body == "" {
		body = "(empty)"
	}
	return body
}

func (f *Fido) tossEcho(l *Link, h *ftn.Header, m *ftn.Message, st *TossStats) error {
	area, err := f.B.Store.AreaByEchoTag(m.Area)
	if errors.Is(err, store.ErrNotFound) || err == nil && !l.carries(m.Area) {
		f.B.Log.Printf("fido: %s sent echomail in %s, which is not exchanged with it: dropped", l.Name, m.Area)
		st.Bad++
		return nil
	}
	if err != nil {
		return err
	}
	msgid := strings.TrimSpace(m.Kludge("MSGID"))
	mid := ftn.NoMSGIDMessageID(m)
	if msgid != "" {
		mid = ftn.MessageID(msgid)
	}
	if !news.ValidMessageID(mid) {
		st.Bad++
		return nil
	}
	if seen, err := f.B.Store.Seen(mid); err != nil {
		return err
	} else if seen {
		st.Dupes++
		return nil
	}
	if msgid != "" {
		if _, err := f.B.Store.MessageByFTNMsgID(msgid); err == nil {
			st.Dupes++
			return nil
		}
	}
	// been here: our address in its PATH
	if me := f.akaFor(l); me.Point == 0 {
		for _, p := range m.Path {
			if p.Net == me.Net && p.Node == me.Node {
				f.B.Store.Remember(mid)
				st.Dupes++
				return nil
			}
		}
	}
	dec := m.Decoder()
	msg := &store.Message{AreaID: area.ID, From: bbs.Clean(dec(m.From), bbs.MaxFTNNameLen),
		To: bbs.Clean(dec(m.To), bbs.MaxFTNNameLen), Subject: bbs.Clean(dec(m.Subject), bbs.MaxSubjectLen),
		Body: text(m, dec), MessageID: mid, FTNMsgID: msgid, Origin: l.origin()}
	if msg.From == "" {
		msg.From = "(unknown)"
	}
	if msg.To == "" {
		msg.To = "All"
	}
	if msg.Subject == "" {
		msg.Subject = "(no subject)"
	}
	if reply := strings.TrimSpace(m.Kludge("REPLY")); reply != "" {
		if p, err := f.B.Store.MessageByFTNMsgID(reply); err == nil && p.AreaID == area.ID {
			msg.ReplyTo = p.ID
		}
	}
	var packed bytes.Buffer
	ftn.WriteMessage(&packed, m)
	msg.FTN = packed.Bytes()
	if area.Newsgroup != "" {
		msg.Raw = f.article(area, m, msg)
	}
	if _, err := f.B.Store.Post(msg); err != nil {
		if errors.Is(err, store.ErrExists) {
			st.Dupes++
			return nil
		}
		return err
	}
	st.Echomail++
	return nil
}

// article is an echomail message as a newsgroup's article (the area is
// both): from Name <Name@pP.fF.nN.zZ.fidonet.org>, as gateways write it.
func (f *Fido) article(area *store.Area, m *ftn.Message, msg *store.Message) []byte {
	ns := f.B.News
	from := originAddr(m)
	addr := "unknown@fidonet.org"
	if from.Zone != 0 {
		host := fmt.Sprintf("f%d.n%d.z%d.fidonet.org", from.Node, from.Net, from.Zone)
		if from.Point != 0 {
			host = fmt.Sprintf("p%d.", from.Point) + host
		}
		addr = strings.ReplaceAll(strings.Trim(asciiName(msg.From), "_"), " ", "_") + "@" + host
		if strings.HasPrefix(addr, "@") {
			addr = "unknown" + addr
		}
	}
	var refs []string
	if msg.ReplyTo > 0 {
		if p, err := f.B.Store.Message(msg.ReplyTo); err == nil && p.MessageID != "" {
			refs = []string{p.MessageID}
		}
	}
	a := news.New(news.Local{Name: msg.From, Address: addr, Group: area.Newsgroup, Subject: msg.Subject,
		Body: msg.Body, References: refs, MessageID: msg.MessageID, PathHost: ns.PathHost,
		Organization: ns.Organization})
	if id := m.Kludge("MSGID"); id != "" {
		a.Set("X-FTN-MSGID", bbs.Clean(id, 80))
	}
	a.Set("X-FTN-Area", m.Area)
	return a.Bytes()
}

// asciiName keeps a name's letters, digits and spaces.
func asciiName(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '.' || r == '-') {
			return r
		}
		return -1
	}, s)
}

// originAddr is where an echomail message was written: its origin line's
// address, else its MSGID's.
var originPattern = regexp.MustCompile(`\(([0-9]+:[0-9]+/[0-9]+(\.[0-9]+)?)(@[A-Za-z0-9_-]+)?\)\s*$`)

func originAddr(m *ftn.Message) ftn.Addr {
	for i := len(m.Lines) - 1; i >= 0 && i >= len(m.Lines)-3; i-- {
		if strings.HasPrefix(m.Lines[i], " * Origin:") {
			if s := originPattern.FindStringSubmatch(m.Lines[i]); s != nil {
				if a, err := ftn.ParseAddr(s[1]); err == nil {
					return a
				}
			}
		}
	}
	if id := strings.Fields(m.Kludge("MSGID")); len(id) > 0 {
		if a, err := ftn.ParseAddr(id[0]); err == nil {
			return a
		}
	}
	return ftn.Addr{}
}

// netmailAddrs are a netmail's addresses: by INTL, FMPT and TOPT, else
// the packed message's (in the packet's zone).
func netmailAddrs(h *ftn.Header, m *ftn.Message) (orig, dest ftn.Addr) {
	orig = ftn.Addr{Zone: h.Orig.Zone, Net: m.OrigNet, Node: m.OrigNode}
	dest = ftn.Addr{Zone: h.Dest.Zone, Net: m.DestNet, Node: m.DestNode}
	if f := strings.Fields(m.Kludge("INTL")); len(f) == 2 {
		if d, err := ftn.ParseAddr(f[0]); err == nil {
			dest = d.Boss()
			dest.Domain = ""
		}
		if o, err := ftn.ParseAddr(f[1]); err == nil {
			orig = o.Boss()
			orig.Domain = ""
		}
	}
	fmt.Sscanf(m.Kludge("FMPT"), "%d", &orig.Point)
	fmt.Sscanf(m.Kludge("TOPT"), "%d", &dest.Point)
	return orig, dest
}

func (f *Fido) tossNetmail(l *Link, h *ftn.Header, m *ftn.Message, st *TossStats) error {
	orig, dest := netmailAddrs(h, m)
	if !f.ours(dest) {
		// we are a leaf: we do not route others' netmail
		f.B.Log.Printf("fido: %s sent netmail for %s, not us: dropped", l.Name, dest)
		st.Bad++
		return nil
	}
	// netmail's dupes are its own: a MSGID an echomail message had too
	// (careless tools make them of the clock) does not count
	msgid := strings.TrimSpace(m.Kludge("MSGID"))
	mid := "netmail " + ftn.NoMSGIDMessageID(m)
	if msgid != "" {
		mid = "netmail " + msgid
	}
	if seen, err := f.B.Store.Seen(mid); err != nil {
		return err
	} else if seen {
		st.Dupes++
		return nil
	}
	dec := m.Decoder()
	to := strings.TrimSpace(dec(m.To))
	rcpt, err := f.B.Store.UserByName(to)
	if errors.Is(err, store.ErrNotFound) {
		rcpt, err = f.sysop()
	}
	if err != nil {
		f.B.Log.Printf("fido: netmail from %s for %q: no such user, and no sysop: dropped", orig, to)
		st.Bad++
		return nil
	}
	from := bbs.Clean(dec(m.From), bbs.MaxFTNNameLen)
	if from == "" {
		from = "Sysop"
	}
	subject := bbs.Clean(dec(m.Subject), bbs.MaxSubjectLen)
	if subject == "" {
		subject = "(no subject)"
	}
	body := text(m, dec)
	if !strings.EqualFold(rcpt.Name, to) {
		body = fmt.Sprintf("[Netmail for %q, who is not a user here]\n\n", bbs.Clean(to, bbs.MaxFTNNameLen)) + body
	}
	mail := &store.Message{From: from + "@" + orig.String4D(), ToID: rcpt.ID, To: rcpt.Name,
		Subject: subject, Body: body, FTNMsgID: msgid}
	if _, err := f.B.Store.SendMail(mail); err != nil {
		return err
	}
	f.B.Store.Remember(mid)
	f.B.Log.Printf("fido: netmail from %s at %s for %s", from, orig, rcpt.Name)
	st.Netmail++
	return nil
}

// sysop is the first sysop, who gets netmail for no one here.
func (f *Fido) sysop() (*store.User, error) {
	us, err := f.B.Store.Users()
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		if u.Sysop && !u.Locked {
			return u, nil
		}
	}
	return nil, store.ErrNotFound
}
