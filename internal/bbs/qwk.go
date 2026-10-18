package bbs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// QWK offline mail: a user takes their new messages and mail away in a
// packet (BBSID.QWK) and brings their answers back in a REP (BBSID.REP).
// Conference 0 is their mail; the others are the areas, by number.

// Limits on a packet.
const (
	QWKPerArea = 500  // new messages of one area
	QWKTotal   = 3000 // in all
)

// BBSID is the name of the BBS's QWK packets.
func (b *BBS) BBSID() string {
	if b.Conf().QWKID != "" {
		return b.Conf().QWKID
	}
	return qwk.MakeID(b.Conf().Name)
}

// QWKOut is a packet made for a user.  Its messages count as read (the
// areas' pointers move past them, the mail is marked) only when Done is
// called: once the packet has gone.
type QWKOut struct {
	Name     string // BBSID.QWK
	Data     []byte
	Messages int
	Mail     int
	done     func() error
}

// Done marks the packet's messages read.
func (o *QWKOut) Done() error { return o.done() }

// QWKPacket makes a packet of the user's new messages in the areas of
// their new scan, and their unread mail.
func (n *Node) QWKPacket() (*QWKOut, error) {
	u := n.User()
	if u == nil {
		return nil, errors.New("not logged in")
	}
	b := n.b
	p := &qwk.Packet{BBSID: b.BBSID(), BBSName: b.Conf().Name, Location: b.Conf().Location, Sysop: b.Conf().Sysop,
		User: u.Name, Created: time.Now(), Welcome: b.MOTDText(),
		Confs: []qwk.Conference{{Number: 0, Name: "Mail"}}}
	out := &QWKOut{Name: p.BBSID + ".QWK"}
	mail, err := b.Store.Inbox(u.ID)
	if err != nil {
		return nil, err
	}
	var read []int64
	for _, m := range mail {
		if m.Read || len(p.Messages) >= QWKTotal {
			continue
		}
		p.Messages = append(p.Messages, qwk.Message{Conf: 0, Number: int(m.ID), Private: true,
			To: m.To, From: m.From, Subject: m.Subject, Date: m.Created, Ref: int(m.ReplyTo), Body: m.Body})
		read = append(read, m.ID)
	}
	out.Mail = len(read)
	areas, err := b.AreasFor(u)
	if err != nil {
		return nil, err
	}
	off, err := b.Store.ScanOff(u.ID)
	if err != nil {
		return nil, err
	}
	last := map[int64]int64{}
	for _, a := range areas {
		if a.ID > 65535 {
			continue // past what a conference's number holds
		}
		p.Confs = append(p.Confs, qwk.Conference{Number: int(a.ID), Name: a.Name})
		if off[a.ID] || a.New == 0 || len(p.Messages) >= QWKTotal {
			continue
		}
		from, err := b.Store.LastRead(u.ID, a.ID)
		if err != nil {
			return nil, err
		}
		ms, err := b.Store.Messages(a.ID, from, min(QWKPerArea, QWKTotal-len(p.Messages)))
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			last[a.ID] = m.ID
			if u.QWKNode && m.FromID == u.ID {
				continue // a node's own come back to it no more
			}
			p.Messages = append(p.Messages, qwk.Message{Conf: int(a.ID), Number: int(m.ID),
				To: m.To, From: m.From, Subject: m.Subject, Date: m.Created, Ref: int(m.ReplyTo), Body: m.Body})
		}
	}
	out.Messages = len(p.Messages)
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		return nil, err
	}
	out.Data = buf.Bytes()
	out.done = func() error {
		for area, id := range last {
			if err := b.Store.SetLastRead(u.ID, area, id); err != nil {
				return err
			}
		}
		for _, id := range read {
			if err := b.Store.MarkMailRead(u.ID, id); err != nil {
				return err
			}
		}
		b.Log.Printf("node %d: %s took a QWK packet of %d messages", n.Num, u.Name, out.Messages)
		return nil
	}
	return out, nil
}

// REPResult is what came of a REP packet.
type REPResult struct {
	Posted, Mailed, Duplicates int
	Errors                     []string // a line for each message refused
}

// String sums up the result in a line, for the caller.
func (r *REPResult) String() string {
	s := fmt.Sprintf("%d posted, %d mailed", r.Posted, r.Mailed)
	if r.Duplicates > 0 {
		s += fmt.Sprintf(", %d already in", r.Duplicates)
	}
	if len(r.Errors) > 0 {
		s += fmt.Sprintf(", %d refused", len(r.Errors))
	}
	return s
}

// ImportREP posts and mails what a REP packet holds, as the user wrote
// it (whatever its From says).  A message already brought by an earlier
// REP is passed over.
func (n *Node) ImportREP(data []byte) (*REPResult, error) {
	u := n.User()
	if u == nil {
		return nil, errors.New("not logged in")
	}
	b := n.b
	ms, err := qwk.ReadREP(data, b.BBSID())
	if err != nil && len(ms) == 0 {
		return nil, err
	}
	res := &REPResult{}
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
	}
	for i, m := range ms {
		sum := sha256.Sum256([]byte(strconv.Itoa(m.Conf) + "\x00" + m.From + "\x00" + m.To + "\x00" + m.Subject + "\x00" + m.Body))
		hash := hex.EncodeToString(sum[:16])
		seen, err := b.Store.REPSeen(u.ID, hash)
		if err != nil {
			return res, err
		}
		if seen {
			res.Duplicates++
			continue
		}
		body := strings.Split(m.Body, "\n")
		if m.Conf == 0 || m.Private {
			ref := int64(m.Ref)
			if _, err := b.Store.Mail(u.ID, ref); err != nil {
				ref = 0 // only an answer to a mail of theirs
			}
			err = n.SendMail(m.To, m.Subject, body, ref)
			if err == nil {
				res.Mailed++
			}
		} else {
			var a *store.Area
			a, err = b.Store.Area(int64(m.Conf))
			if errors.Is(err, store.ErrNotFound) {
				err = fmt.Errorf("there is no conference %d", m.Conf)
			}
			if err == nil {
				if !b.CanRead(u, a) {
					err = fmt.Errorf("there is no conference %d", m.Conf)
				} else if u.QWKNode {
					// a node's REP: its writers' messages, by their names
					_, err = b.PostNet(u, a, m.From, m.To, m.Subject, m.Body, n.replyIn(a.ID, int64(m.Ref)),
						"qwk:"+strings.ToLower(u.Name))
				} else {
					_, err = n.Post(a, m.To, m.Subject, body, n.replyIn(a.ID, int64(m.Ref)))
				}
			}
			if err == nil {
				res.Posted++
			}
		}
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("message %d (%s): %v", i+1, Clean(m.Subject, MaxSubjectLen), err))
			continue
		}
		if err := b.Store.MarkREP(u.ID, hash); err != nil {
			return res, err
		}
	}
	b.Log.Printf("node %d: %s sent a REP packet: %s", n.Num, u.Name, res)
	return res, nil
}

// PostNet posts a message that came by a QWK network: by its writer's
// name, from a node's account (by, who must be allowed to post there;
// nil: it came from the network's hub), its origin the network.
func (b *BBS) PostNet(by *store.User, a *store.Area, from, to, subject, body string, replyTo int64, origin string) (int64, error) {
	var fromID int64
	if by != nil {
		if !b.CanPost(by, a) {
			return 0, errors.New("your access does not allow posting here")
		}
		fromID = by.ID
	}
	m := &store.Message{AreaID: a.ID, FromID: fromID, From: Clean(from, MaxNameLen), To: Clean(to, MaxNameLen),
		Subject: Clean(subject, MaxSubjectLen), Body: CleanBody(strings.Split(body, "\n")), ReplyTo: replyTo,
		Origin: origin}
	if m.From == "" {
		m.From = "Unknown"
	}
	if m.To == "" {
		m.To = "All"
	}
	if m.Subject == "" {
		m.Subject = "(no subject)"
	}
	if m.Body == "" {
		return 0, errors.New("the message is empty")
	}
	b.MakeArticle(by, a, m)
	return b.Store.Post(m)
}

// replyIn is ref if it is a message of the area (0 if not).
func (n *Node) replyIn(area, ref int64) int64 {
	if ref <= 0 {
		return 0
	}
	if m, err := n.b.Store.Message(ref); err == nil && m.AreaID == area {
		return ref
	}
	return 0
}
