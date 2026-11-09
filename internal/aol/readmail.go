package aol

import (
	"errors"
	"strings"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/store"
)

// Read Mail: the client's own mail windows, which its mail tool
// (AOLMIP.AOL) fills from the host's MIP atoms.  What is here comes from
// reading the tool's code, and has not yet been tried with a real client:
//
//	m7  the New Mail list (the toolbar's Read New Mail); m9 the Old Mail
//	    list; m8 the Sent Mail list.  None carries data.
//	m3  read one mail: mip_preset_message_id <id>, mip_read_mail <n>.
//	m2  change mail: per mail, mip_preset_message_id <id> and
//	    mip_change_message_status <n>: 7 Ignore (marked read), 2 Keep As
//	    New, 4 Delete.
//
// A list is one MIP message with, for each mail, a message of its own
// inside it: its id, author, subject and date, then an empty block.  A
// mail read is a MIP message of its whole: id, author, addressee,
// subject, date and text.

// The list tokens, and the mail each lists.
var mailLists = map[string]int{
	"m7": store.FolderUnread,
	"m9": store.FolderInbox,
	"m8": store.FolderSent,
}

// The status changes in an m2.
const (
	mipStatusKeepNew = 2
	mipStatusDelete  = 4
	mipStatusIgnore  = 7
)

// mipListMax is the most mail a list holds.
const mipListMax = 100

// mipTextMax is the most text one mip_text or mip_data atom holds.
const mipTextMax = 0x4000

// mailList answers m7, m9 or m8: the mail of that list, newest first.
func (ss *session) mailList(tok string) {
	folder := mailLists[tok]
	all, err := ss.s.B.Store.Folder(ss.u.ID, folder)
	if err != nil {
		ss.logf("%s: %v", tok, err)
		ss.notice("Your mail can't be listed just now.")
		return
	}
	if folder == store.FolderInbox {
		// Old Mail: what has been read; New Mail has the rest
		read := all[:0:0]
		for _, m := range all {
			if m.Read {
				read = append(read, m)
			}
		}
		all = read
	}
	if len(all) == 0 {
		ss.notice(map[int]string{
			store.FolderUnread: "You have no unread mail.",
			store.FolderInbox:  "You have no old mail.",
			store.FolderSent:   "You have sent no mail.",
		}[folder])
		return
	}
	fs := []fdo.Frame{fdo.F("uni_start_stream"), fdo.F("mip_start_message")}
	for i := len(all) - 1; i >= 0 && len(all)-i <= mipListMax; i-- {
		m := all[i]
		who := m.From
		if folder == store.FolderSent {
			who = m.To
		}
		fs = append(fs,
			fdo.F("mip_start_message"),
			fdo.F("mip_message_id", mipID(m.ID)...),
			fdo.F("mip_author", fdo.Latin1(who)...),
			fdo.F("mip_subject", fdo.Latin1(m.Subject)...),
			fdo.F("mip_message_datestr", fdo.Latin1(m.Created.Format("01/02/06"))...),
			fdo.F("mip_start_block"),
			fdo.F("mip_end_block"),
			fdo.F("mip_end_message"))
	}
	fs = append(fs, fdo.F("mip_end_message"), fdo.F("uni_wait_off"), fdo.F("uni_end_stream"))
	ss.sendFrames(fs)
}

// readMIP answers m3: one mail, whole, and marks it read.
func (ss *session) readMIP(body []byte) {
	frames := mipFrames(body)
	id, ok := mipNumber(frames, "mip_preset_message_id")
	if !ok {
		ss.logf("m3 without a message id: % x", body)
		ss.notice("That mail could not be found.")
		return
	}
	st := ss.s.B.Store
	m, err := st.Mail(ss.u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		m, err = ss.sentMail(id)
	}
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			ss.logf("m3 %d: %v", id, err)
		}
		ss.notice("That mail is not in your mailbox.")
		return
	}
	if !m.Read && m.ToID == ss.u.ID {
		st.MarkMailRead(ss.u.ID, m.ID)
	}
	fs := []fdo.Frame{
		fdo.F("uni_start_stream"),
		fdo.F("mip_start_message"),
		fdo.F("mip_message_id", mipID(m.ID)...),
		fdo.F("mip_author", fdo.Latin1(m.From)...),
		fdo.F("mip_addressee", fdo.Latin1(m.To)...),
		fdo.F("mip_subject", fdo.Latin1(m.Subject)...),
		fdo.F("mip_message_datestr", fdo.Latin1(m.Created.Format("01/02/06 15:04"))...),
	}
	text := fdo.Latin1(strings.ReplaceAll(m.Body, "\n", "\r\n"))
	for name := "mip_text"; ; name = "mip_data" {
		n := min(len(text), mipTextMax)
		fs = append(fs, fdo.F(name, text[:n]...))
		if text = text[n:]; len(text) == 0 {
			break
		}
	}
	fs = append(fs, fdo.F("mip_end_message"), fdo.F("uni_wait_off"), fdo.F("uni_end_stream"))
	ss.sendFrames(fs)
}

// sentMail is a mail the user sent, by id.
func (ss *session) sentMail(id int64) (*store.Message, error) {
	sent, err := ss.s.B.Store.Folder(ss.u.ID, store.FolderSent)
	if err != nil {
		return nil, err
	}
	for _, m := range sent {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, store.ErrNotFound
}

// changeMail answers m2: Ignore, Keep As New or Delete, for each mail
// the New Mail window had picked.
func (ss *session) changeMail(body []byte) {
	st := ss.s.B.Store
	var id int64
	have := false
	for _, f := range mipFrames(body) {
		switch f.Name() {
		case "mip_preset_message_id":
			id, have = bigEndian(f.Data), true
		case "mip_change_message_status":
			if !have {
				continue
			}
			var err error
			switch n := bigEndian(f.Data); n {
			case mipStatusIgnore:
				err = st.MarkMailRead(ss.u.ID, id)
			case mipStatusKeepNew:
				err = st.MarkMailUnread(ss.u.ID, id)
			case mipStatusDelete:
				err = st.DeleteMail(ss.u.ID, id)
			default:
				ss.logf("m2: mail %d: status %d (not served)", id, n)
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				ss.logf("m2: mail %d: %v", id, err)
			}
			have = false
		}
	}
	ss.send(waitOff)
}

// mipFrames are the atoms of an m2 or m3, with or without a stream id
// before them.
func mipFrames(body []byte) []fdo.Frame {
	for _, skip := range []int{2, 0} {
		if len(body) < skip {
			continue
		}
		if fs, err := fdo.Decode(body[skip:]); err == nil {
			if _, ok := fdo.Find(fs, "mip_preset_message_id"); ok {
				return fs
			}
		}
	}
	return nil
}

// mipNumber is the number an atom carries.
func mipNumber(frames []fdo.Frame, name string) (int64, bool) {
	f, ok := fdo.Find(frames, name)
	if !ok || len(f.Data) == 0 {
		return 0, false
	}
	return bigEndian(f.Data), true
}

// bigEndian is up to four bytes as a number.
func bigEndian(b []byte) int64 {
	var n int64
	for _, c := range b[max(0, len(b)-4):] {
		n = n<<8 | int64(c)
	}
	return n
}

// mipID is a message id as MIP carries it: four bytes.
func mipID(id int64) []byte {
	return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
}

// sendFrames sends atoms built as frames.
func (ss *session) sendFrames(fs []fdo.Frame) {
	if err := ss.sendAtoms(fdo.Encode(fs)); err != nil {
		ss.logf("send: %v", err)
	}
}
