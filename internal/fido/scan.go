package fido

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Tearline is the tear line of messages written here.
const Tearline = "--- bbs-sec"

// ScanAll scans for every link: what is new goes to their outbounds.  It
// reports whether anything was packed.
func (f *Fido) ScanAll() (bool, error) {
	any := false
	for _, l := range f.S.Links {
		n, err := f.Scan(l)
		if err != nil {
			return any, fmt.Errorf("%s: %w", l.Name, err)
		}
		any = any || n > 0
	}
	return any, nil
}

// Scan packs for a link the echomail written since the last scan (in the
// echoes it carries, but what came from it) and the netmail it routes.
// The first scan for a link only marks where to start.
func (f *Fido) Scan(l *Link) (int, error) {
	mu := f.lock(l)
	mu.Lock()
	defer mu.Unlock()
	return f.scan(l)
}

func (f *Fido) scan(l *Link) (int, error) {
	st := f.B.Store
	var msgs []*ftn.Message
	key := l.origin()
	after, ok, err := st.PeerPushed(key)
	if err != nil {
		return 0, err
	}
	if !ok {
		max, err := st.MaxMessageID()
		if err != nil {
			return 0, err
		}
		if err := st.SetPeerPushed(key, max); err != nil {
			return 0, err
		}
		after = max
	}
	areas, err := st.EchoAreas()
	if err != nil {
		return 0, err
	}
	var ids []int64
	byID := map[int64]*store.Area{}
	for _, a := range areas {
		if l.carries(a.EchoTag) {
			ids = append(ids, a.ID)
			byID[a.ID] = a
		}
	}
	last := after
	for len(msgs) < 5000 {
		batch, err := st.ToExport(key, last, ids, 500)
		if err != nil {
			return 0, err
		}
		if len(batch) == 0 {
			break
		}
		for _, m := range batch {
			last = m.ID
			if em, err := f.echomail(l, byID[m.AreaID], m); err != nil {
				f.B.Log.Printf("fido: message #%d for %s: %v", m.ID, l.Name, err)
			} else if em != nil {
				msgs = append(msgs, em)
			}
		}
	}
	nets, err := st.PendingNetmail(1000)
	if err != nil {
		return 0, err
	}
	var packed []int64
	for _, n := range nets {
		dest, err := ftn.ParseAddr(n.ToAddr)
		if err != nil || f.route(dest) != l {
			continue
		}
		msgs = append(msgs, f.netmail(l, n, dest))
		packed = append(packed, n.ID)
	}
	if len(msgs) > 0 {
		if err := f.writeOutbound(l, msgs); err != nil {
			return 0, err
		}
	}
	if last != after {
		if err := st.SetPeerPushed(key, last); err != nil {
			return 0, err
		}
	}
	for _, id := range packed {
		st.NetmailPacked(id)
	}
	if len(msgs) > 0 {
		f.B.Log.Printf("fido: packed %d messages for %s", len(msgs), l.Name)
	}
	return len(msgs), nil
}

// tzutc is the TZUTC kludge's value: -0500, 0100.
func tzutc(t time.Time) string {
	_, off := t.Zone()
	sign := ""
	if off < 0 {
		sign, off = "-", -off
	}
	return fmt.Sprintf("%s%02d%02d", sign, off/3600, off%3600/60)
}

// cutRunes cuts s to max bytes, not in the middle of a character.
func cutRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// msgidOf is a message's MSGID, given it (and kept) if it has none yet.
func (f *Fido) msgidOf(m *store.Message) string {
	if m.FTNMsgID != "" {
		return m.FTNMsgID
	}
	var id string
	if m.MessageID != "" && !strings.HasPrefix(m.MessageID, "<NOMSGID_") {
		id = ftn.MSGIDOf(m.MessageID, f.Main())
	} else {
		id = ftn.NewMSGID(f.Main())
	}
	f.B.Store.SetFTNMsgID(m.ID, id)
	m.FTNMsgID = id
	return id
}

// echomail is a message of an echo to send to a link: as it came from
// another link, or written for it; nil if the link has seen it.
func (f *Fido) echomail(l *Link, a *store.Area, m *store.Message) (*ftn.Message, error) {
	me := f.akaFor(l)
	raw, err := f.B.Store.FTN(m.ID)
	if err != nil {
		return nil, err
	}
	var em *ftn.Message
	if raw != nil {
		if em, err = ftn.ParseMessage(raw, len(raw)+1); err != nil {
			return nil, err
		}
		if l.Addr.Point == 0 {
			for _, s := range em.SeenBy {
				if s.Net == l.Addr.Net && s.Node == l.Addr.Node {
					return nil, nil
				}
			}
		}
	} else {
		em = f.write(me, m)
	}
	em.Area = a.EchoTag
	em.OrigNode, em.OrigNet = me.Node, me.Net
	em.DestNode, em.DestNet = l.Addr.Node, l.Addr.Net
	em.Attr = 0
	if me.Point == 0 {
		seen := append([]ftn.Net2D{}, em.SeenBy...)
		seen = append(seen, ftn.Net2D{Net: me.Net, Node: me.Node})
		for _, o := range f.S.Links {
			if o.Addr.Point == 0 && o.Addr.Zone == me.Zone && o.carries(a.EchoTag) {
				seen = append(seen, ftn.Net2D{Net: o.Addr.Net, Node: o.Addr.Node})
			}
		}
		em.SeenBy = sortSeen(seen)
		if n := len(em.Path); n == 0 || em.Path[n-1] != (ftn.Net2D{Net: me.Net, Node: me.Node}) {
			em.Path = append(em.Path, ftn.Net2D{Net: me.Net, Node: me.Node})
		}
	}
	return em, nil
}

// sortSeen sorts SEEN-BY addresses and drops repeats.
func sortSeen(s []ftn.Net2D) []ftn.Net2D {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Net != s[j].Net {
			return s[i].Net < s[j].Net
		}
		return s[i].Node < s[j].Node
	})
	out := s[:0]
	for i, a := range s {
		if i == 0 || a != s[i-1] {
			out = append(out, a)
		}
	}
	return out
}

// write makes a message written here (or come by news) an FTN message.
func (f *Fido) write(me ftn.Addr, m *store.Message) *ftn.Message {
	enc := ftn.Encoder{UTF8: f.S.UTF8}
	t := m.Created
	if t.IsZero() {
		t = time.Now()
	}
	em := &ftn.Message{DateTime: ftn.FormatDate(t),
		From: cutRunes(enc.String(m.From), 35), To: cutRunes(enc.String(m.To), 35),
		Subject: cutRunes(enc.String(m.Subject), 71)}
	em.SetKludge("MSGID", f.msgidOf(m))
	if m.ReplyTo > 0 {
		if p, err := f.B.Store.Message(m.ReplyTo); err == nil {
			em.SetKludge("REPLY", f.msgidOf(p))
		}
	}
	em.SetKludge("PID", "bbs-sec")
	em.SetKludge("TZUTC", tzutc(t))
	em.SetKludge("CHRS", enc.CHRS())
	for _, line := range strings.Split(m.Body, "\n") {
		em.Lines = append(em.Lines, enc.String(line))
	}
	origin := f.S.Origin
	room := 79 - len(" * Origin:  ()") - len(me.String4D())
	origin = cutRunes(enc.String(origin), room)
	em.Lines = append(em.Lines, "", Tearline, " * Origin: "+origin+" ("+me.String4D()+")")
	return em
}

// netmail is a queued netmail as an FTN message.
func (f *Fido) netmail(l *Link, n *store.Netmail, dest ftn.Addr) *ftn.Message {
	me := f.akaFor(l)
	enc := ftn.Encoder{UTF8: f.S.UTF8}
	t := n.Created
	if t.IsZero() {
		t = time.Now()
	}
	em := &ftn.Message{OrigNode: me.Node, OrigNet: me.Net, DestNode: dest.Node, DestNet: dest.Net,
		Attr: ftn.AttrPrivate, DateTime: ftn.FormatDate(t),
		From: cutRunes(enc.String(n.From), 35), To: cutRunes(enc.String(n.To), 35),
		Subject: cutRunes(enc.String(n.Subject), 71)}
	em.SetKludge("INTL", dest.String3D()+" "+me.String3D())
	if me.Point != 0 {
		em.SetKludge("FMPT", fmt.Sprint(me.Point))
	}
	if dest.Point != 0 {
		em.SetKludge("TOPT", fmt.Sprint(dest.Point))
	}
	em.SetKludge("MSGID", n.MsgID)
	if n.Reply != "" {
		em.SetKludge("REPLY", n.Reply)
	}
	em.SetKludge("PID", "bbs-sec")
	em.SetKludge("TZUTC", tzutc(t))
	em.SetKludge("CHRS", enc.CHRS())
	for _, line := range strings.Split(n.Body, "\n") {
		em.Lines = append(em.Lines, enc.String(line))
	}
	em.Lines = append(em.Lines, "", Tearline)
	return em
}

var days = []string{"su", "mo", "tu", "we", "th", "fr", "sa"}

// writeOutbound writes messages as a packet (bundled, if the link takes
// bundles) into its outbound.
func (f *Fido) writeOutbound(l *Link, msgs []*ftn.Message) error {
	var pkt bytes.Buffer
	h := &ftn.Header{Orig: f.akaFor(l), Dest: l.Addr, Password: l.PktPassword}
	ftn.WriteHeader(&pkt, h)
	for _, m := range msgs {
		ftn.WriteMessage(&pkt, m)
	}
	ftn.WriteEnd(&pkt)

	out := f.outbound(l)
	base := uint32(time.Now().UnixNano() / int64(time.Millisecond))
	var name string
	for i := uint32(0); ; i++ {
		name = fmt.Sprintf("%08x", base+i)
		if !exists(filepath.Join(out, name+".pkt")) && !exists(filepath.Join(f.S.Dir, "tmp", name+".pkt")) {
			break
		}
	}
	data := pkt.Bytes()
	file := name + ".pkt"
	if l.Packer != "" {
		var b bytes.Buffer
		if err := archive.Write(&b, l.Packer, name, []archive.File{{Name: file, Data: data, Modified: time.Now()}}); err != nil {
			return err
		}
		data = b.Bytes()
		ext := days[time.Now().Weekday()]
		for c := 0; ; c++ {
			file = fmt.Sprintf("%s.%s%c", name, ext, "0123456789abcdefghijklmnopqrstuvwxyz"[c%36])
			if !exists(filepath.Join(out, file)) || c >= 35 {
				break
			}
		}
	}
	tmp := filepath.Join(f.S.Dir, "tmp", file)
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(out, file))
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
