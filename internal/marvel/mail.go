package marvel

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// MOSRXP is the mail transport behind Exchange's Inbox when the MSN
// service is installed: the caller's BBS mail comes down as headers, then
// a message at a time, and what they send goes out as BBS mail.  A
// message's entry id is 12 bytes: its mail id, then 0.
type mailbox struct {
	c       *conn
	blocks  []piece
	last    bool
	streams map[byte]*stream
	given   map[int64]bool // whole-message downloads: handed out already
}

const mailIface = 0x01

// MOSRXP methods
const (
	mxConnInfo    = 0x00
	mxOpenInbox   = 0x01
	mxCloseInbox  = 0x02
	mxInitSend    = 0x03
	mxSendBlock   = 0x04
	mxHeaders     = 0x05
	mxDelete      = 0x06
	mxGetMessage  = 0x07
	mxGetFirst    = 0x08
	mxFlagMessage = 0x09
)

// addrtypes the transport claims for: BBS users, and internet addresses
// (which the BBS's own e-mail, where it has it, takes)
var mailAddrTypes = map[string]bool{"MSN": true, "MSNLIST": true, "MSNINET": true, "INTERNET": true, "SMTP": true}

func newMailbox(c *conn) *mailbox {
	return &mailbox{c: c, streams: map[byte]*stream{}, given: map[int64]bool{}}
}

func (m *mailbox) ifaces() []iface { return []iface{mosIID(0x20, mailIface)} }

func status(st uint32) []byte { return reply{}.dword(st).endStatic() }

func (m *mailbox) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		st := m.streams[h.Method]
		if st == nil {
			st = &stream{}
			m.streams[h.Method] = st
		}
		if len(st.data) < 4<<20 {
			st.data = append(st.data, h.Params...)
		}
		st.done = h.Iface == classChunkLast
		if err := m.submit(); err != nil {
			m.c.logf("mail: %v", err)
		}
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	u := m.c.signedOn()
	if u == nil {
		return status(mapiFail), true
	}
	switch h.Method {
	case mxConnInfo:
		return reply{}.dword(0).endStatic().bytes_(memberEntryID(u.Name, u.Name)), true
	case mxOpenInbox:
		box, _ := m.c.s.B.Store.Inbox(u.ID)
		return reply{}.dword(0).dword(uint32(len(box))).endStatic(), true
	case mxCloseInbox, mxInitSend:
		if h.Method == mxInitSend {
			m.blocks, m.last = nil, false
		}
		return status(0), true
	case mxSendBlock:
		var dwords []uint32
		for _, p := range ps {
			switch p.Tag {
			case 0x03:
				dwords = append(dwords, p.Int)
			case 0x04, 0x05:
				m.blocks = append(m.blocks, pieceOf(p))
			}
		}
		if len(dwords) < 2 || dwords[1] != 0 {
			m.last = true
		}
		if err := m.submit(); err != nil {
			m.c.logf("mail: %v", err)
			return status(mapiFail), true
		}
		return status(0), true
	case mxHeaders:
		box, err := m.c.s.B.Store.Inbox(u.ID)
		if err != nil {
			m.c.logf("mail: %v", err)
		}
		var b []byte
		for _, msg := range box {
			b = append(b, entryID(msg.ID)...)
			b = append(b, propList(headerProps(msg))...)
		}
		return reply{}.dword(0).dword(uint32(len(box))).blob(b), true
	case mxDelete:
		for _, p := range ps {
			if p.Tag != 0x04 {
				continue
			}
			for b := p.Data; len(b) >= 12; b = b[12:] {
				id := int64(binary.LittleEndian.Uint64(b))
				if err := m.c.s.B.Store.DeleteMail(u.ID, id); err != nil {
					m.c.logf("mail: deleting %d: %v", id, err)
				}
			}
		}
		return status(0), true
	case mxGetMessage, mxFlagMessage:
		var id int64
		for _, p := range ps {
			if p.Tag == 0x04 && len(p.Data) >= 8 {
				id = int64(binary.LittleEndian.Uint64(p.Data))
				break
			}
		}
		msg, err := m.c.s.B.Store.Mail(u.ID, id)
		if err != nil {
			return status(mapiNotFound), true
		}
		if h.Method == mxFlagMessage {
			return status(0), true
		}
		m.c.s.B.Store.MarkMailRead(u.ID, id)
		return reply{}.dword(0).blob(messageBlob(msg)), true
	case mxGetFirst:
		box, _ := m.c.s.B.Store.Inbox(u.ID)
		for _, msg := range box {
			if m.given[msg.ID] {
				continue
			}
			m.given[msg.ID] = true
			m.c.s.B.Store.MarkMailRead(u.ID, msg.ID)
			return reply{}.dword(0).blob(append(entryID(msg.ID), messageBlob(msg)...)), true
		}
		return status(mapiNotFound), true
	}
	m.c.logf("MOSRXP: no method %#x", h.Method)
	return nil, false
}

func entryID(id int64) []byte {
	return binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(nil, uint64(id)), 0)
}

// sender is a mail's From as a member name or an address.
func sender(msg *store.Message) (name, addr, typ string) {
	if msg.FromAddr != "" {
		return msg.From, msg.FromAddr, "INTERNET"
	}
	return msg.From, msg.From, addrTypeMSN
}

func headerProps(msg *store.Message) []mprop {
	name, _, _ := sender(msg)
	flags := uint32(msgFlagUnmodified)
	if msg.Read {
		flags |= msgFlagRead
	}
	return []mprop{
		{tagObjectType, uint32(mapiMessage)},
		{tagMsgStatus, uint32(0)},
		{tagMessageFlags, flags},
		{tagMessageClass, "IPM.Note"},
		{tagImportance, uint32(importanceNormal)},
		{tagSentReprName, name},
		{tagDisplayTo, msg.To},
		{tagSensitivity, uint32(0)},
		{tagHasAttach, false},
		{tagSubject, msg.Subject},
		{tagDeliveryTime, msg.Created},
		{tagMessageSize, uint32(len(msg.Body) + len(msg.Subject) + 200)},
	}
}

// messageBlob is a mail as the transport serialises it: header
// properties, the recipients, the body's, and no attachments.  (The
// client drops the transport's own tags, 0x0E00 to 0x0FFF, from the
// body's.)
func messageBlob(msg *store.Message) []byte {
	name, addr, typ := sender(msg)
	head := []mprop{
		{tagMessageClass, "IPM.Note"},
		{tagImportance, uint32(importanceNormal)},
		{tagSentReprName, name},
		{tagSensitivity, uint32(0)},
		{tagSubject, msg.Subject},
	}
	b := propList(head)
	toAddr, toType := msg.To, addrTypeMSN
	if msg.ToAddr != "" {
		toAddr, toType = msg.ToAddr, "INTERNET"
	}
	b = binary.LittleEndian.AppendUint32(b, 1)
	b = append(b, propList([]mprop{
		{tagObjectType, uint32(mapiMailUser)},
		{tagRecipientType, uint32(mapiTo)},
		{tagDisplayName, msg.To},
		{tagAddrType, toType},
		{tagEmailAddress, toAddr},
	})...)
	body := strings.ReplaceAll(strings.ReplaceAll(msg.Body, "\r\n", "\n"), "\n", "\r\n")
	b = append(b, propList([]mprop{
		{tagBody, body},
		{tagClientSubmitTime, msg.Created},
		{tagSenderName, name},
		{tagSenderAddrType, typ},
		{tagSenderEmail, addr},
		{tagSenderSearchKey, append([]byte(strings.ToUpper(typ)+":"+addr), 0)},
		{tagSentReprAddrType, typ},
		{tagSentReprEmail, addr},
	})...)
	return binary.LittleEndian.AppendUint32(b, 0)
}

// submit sends a message once its last block (and every stream a block
// names) is in.
func (m *mailbox) submit() error {
	if !m.last {
		return nil
	}
	var raw []byte
	for _, p := range m.blocks {
		if !p.ref {
			raw = append(raw, p.data...)
			continue
		}
		st := m.streams[p.stream]
		if st == nil || !st.done {
			return nil
		}
		raw = append(raw, st.data...)
	}
	m.blocks, m.last = nil, false
	for k := range m.streams {
		delete(m.streams, k)
	}
	return m.send(raw)
}

type recipient struct {
	name, addr, typ string
}

// parseMessage takes a submitted message apart: its subject and body, and
// whom it is for.
func parseMessage(raw []byte) (subject, body string, to []recipient, attached bool, err error) {
	r := &mapiReader{b: raw}
	head, err := r.props()
	if err != nil {
		return
	}
	n, err := r.u32()
	if err != nil {
		return
	}
	for i := uint32(0); i < n; i++ {
		var p map[uint32]any
		if p, err = r.props(); err != nil {
			return
		}
		to = append(to, recipient{name: str(p, tagDisplayName, tagDisplayNameW),
			addr: str(p, tagEmailAddress, tagEmailAddressW), typ: str(p, tagAddrType)})
	}
	rest, err := r.props()
	if err != nil {
		return
	}
	if na, e := r.u32(); e == nil && na > 0 {
		attached = true
	}
	subject = str(head, tagSubject, tagSubjectW)
	if subject == "" {
		subject = str(rest, tagSubject, tagSubjectW)
	}
	body = str(rest, tagBody, tagBodyW)
	if body == "" {
		body = str(head, tagBody, tagBodyW)
	}
	if body == "" {
		if rtf, ok := rest[tagRTFCompressed].([]byte); ok {
			if t, e := bodyText("RTFCOMP", rtf); e == nil {
				body = t
			}
		}
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return
}

// send delivers a submitted message to each recipient, as BBS mail.
func (m *mailbox) send(raw []byte) error {
	subject, body, to, attached, err := parseMessage(raw)
	if err != nil {
		return err
	}
	if attached {
		body += "\n\n[An attachment was left out: this system's mail takes none.]"
	}
	m.c.mu.Lock()
	n := m.c.node
	m.c.mu.Unlock()
	if n == nil {
		return fmt.Errorf("not signed on")
	}
	if subject == "" {
		subject = "(No subject)"
	}
	var failed []string
	sent := 0
	for _, r := range to {
		addr := r.addr
		if addr == "" {
			addr = r.name
		}
		if r.typ != "" && !mailAddrTypes[strings.ToUpper(r.typ)] {
			failed = append(failed, addr+" (a "+r.typ+" address)")
			continue
		}
		if err := n.SendMail(addr, subject, strings.Split(body, "\n"), 0); err != nil {
			failed = append(failed, addr+": "+err.Error())
			continue
		}
		sent++
	}
	if len(failed) == 0 {
		return nil
	}
	err = fmt.Errorf("%q not delivered to %s", subject, strings.Join(failed, "; "))
	if sent == 0 {
		return err // the client says the message couldn't be sent
	}
	// some went: the client can't be told, so the sender gets a note
	u := m.c.signedOn()
	note := &store.Message{From: "Postmaster", ToID: u.ID, To: u.Name, Subject: "Undelivered: " + subject,
		Body: "Your message was sent, but not to:\n\n" + strings.Join(failed, "\n")}
	if _, e := m.c.s.B.Store.SendMail(note); e != nil {
		m.c.logf("mail: %v", e)
	}
	return nil
}
