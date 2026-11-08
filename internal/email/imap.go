package email

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-message/textproto"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// IMAPServer is the IMAP server (RFC 3501, with NAMESPACE, UIDPLUS, MOVE,
// SPECIAL-USE, IDLE...).  Its mailboxes are the user's folders, views of
// their mail (store.FolderNames), and stay as they are: none is made,
// renamed or deleted.  A mail is put in a folder by what it is (COPY into
// Starred stars it, into Trash deletes it, out of Trash restores it), and
// taken out of one by EXPUNGE: to the trash, or for good from the trash.
type IMAPServer struct {
	svc *Service
	set ServerSettings
	srv *imapserver.Server
	ln  *limited
}

const delim = '/'

// pollEvery is how often IDLE looks for mail delivered without a word to
// this server (by another process).
var pollEvery = 15 * time.Second

// NewIMAP makes the IMAP server.
func (s *Service) NewIMAP(set ServerSettings) (*IMAPServer, error) {
	tc, err := tlsConfig(set)
	if err != nil {
		return nil, fmt.Errorf("imap: %w", err)
	}
	x := &IMAPServer{svc: s, set: set}
	opts := &imapserver.Options{
		NewSession: x.newSession,
		Caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapNamespace: {}, imap.CapUIDPlus: {}, imap.CapMove: {},
			imap.CapSpecialUse: {}, imap.CapListStatus: {}, imap.CapListExtended: {}, imap.CapStatusSize: {},
			imap.CapESearch: {}, imap.CapSearchRes: {}, imap.CapChildren: {}},
		Logger:       s.B.Log,
		InsecureAuth: set.TLS == TLSNo || set.TLS == TLSOptional,
	}
	if set.TLS == TLSOptional || set.TLS == TLSRequired {
		opts.TLSConfig = tc
	}
	x.srv = imapserver.New(opts)
	x.ln, err = s.listen("imap", set, tc, func(why string) string { return "* BYE " + why + "\r\n" })
	if err != nil {
		return nil, err
	}
	return x, nil
}

// Run serves until ctx is done.
func (x *IMAPServer) Run(ctx context.Context) error {
	x.svc.B.Log.Printf("imap: listening on %s (tls %s)", x.set.Address, x.set.TLS)
	go func() {
		<-ctx.Done()
		x.srv.Close()
	}()
	err := x.srv.Serve(x.ln)
	if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("imap: %w", err)
	}
	return nil
}

func (x *IMAPServer) newSession(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
	nc := c.NetConn()
	nc.SetDeadline(time.Now().Add(x.set.IdleTimeout))
	return &imapSession{x: x, conn: c, who: "imap " + nc.RemoteAddr().String()}, nil, nil
}

// imapSession is an IMAP client's connection.
type imapSession struct {
	x    *IMAPServer
	conn *imapserver.Conn
	who  string
	user *store.User
	wake <-chan struct{}
	stop func()
	sel  *selection
}

// selection is the folder selected, as the client knows it: its mail in
// order of their numbers (sequence number i+1 is msgs[i]).
type selection struct {
	folder    int
	readOnly  bool
	msgs      []*store.BoxEntry
	searchRes imap.UIDSet
}

var (
	errNoFolder = &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeNonExistent, Text: "No such folder"}
	errFixed    = &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeCannot, Text: "The folders here are fixed: Inbox, Sent, Drafts, Starred, Unread, All Mail and Trash"}
	errReadOnly = &imap.Error{Type: imap.StatusResponseTypeNo, Text: "The folder is read-only (EXAMINE)"}
)

// touch keeps the connection from timing out while it is used.
func (is *imapSession) touch() {
	is.conn.NetConn().SetDeadline(time.Now().Add(is.x.set.IdleTimeout))
}

func (is *imapSession) Close() error {
	if is.stop != nil {
		is.stop()
	}
	return nil
}

// Login checks a login, as the BBS's are.
func (is *imapSession) Login(username, password string) error {
	u, err := is.x.svc.Login(is.who, is.conn.NetConn().RemoteAddr(), "imap", username, password)
	if err != nil {
		return imapserver.ErrAuthFailed
	}
	is.user = u
	is.wake, is.stop = is.x.svc.watch(u.ID)
	is.x.svc.B.Log.Printf("%s: %s logged in", is.who, u.Name)
	return nil
}

// uidValidity is the user's UIDVALIDITY, the same for every folder: the
// numbers are never used again.
func (is *imapSession) uidValidity() uint32 {
	v := uint32(is.user.Created.Unix())
	if v == 0 {
		v = 1
	}
	return v
}

// folder is a folder by its name.
func folderOf(name string) (int, error) {
	f := store.FolderByName(strings.TrimSuffix(name, string(delim)))
	if f < 0 {
		return 0, errNoFolder
	}
	return f, nil
}

// specialUse is a folder's SPECIAL-USE attribute.
func specialUse(f int) imap.MailboxAttr {
	switch f {
	case store.FolderSent:
		return imap.MailboxAttrSent
	case store.FolderDrafts:
		return imap.MailboxAttrDrafts
	case store.FolderStarred:
		return imap.MailboxAttrFlagged
	case store.FolderAll:
		return imap.MailboxAttrAll
	case store.FolderTrash:
		return imap.MailboxAttrTrash
	}
	return ""
}

// flagsOf is an entry's flags.
func flagsOf(e *store.BoxEntry) []imap.Flag {
	var out []imap.Flag
	if e.Read {
		out = append(out, imap.FlagSeen)
	}
	if e.Starred {
		out = append(out, imap.FlagFlagged)
	}
	if e.Role == store.RoleDraft {
		out = append(out, imap.FlagDraft)
	}
	for _, f := range strings.Fields(e.Flags) {
		out = append(out, imap.Flag(f))
	}
	return out
}

// hasFlag reports whether an entry has a flag.
func hasFlag(e *store.BoxEntry, f imap.Flag) bool {
	for _, g := range flagsOf(e) {
		if strings.EqualFold(string(g), string(f)) {
			return true
		}
	}
	return false
}

// splitFlags is a set of flags as an entry keeps them: read, starred, and
// the others (\Draft and \Recent are not set, but by what a mail is).
func splitFlags(flags []imap.Flag) (read, starred bool, others string) {
	var rest []string
	seen := map[string]bool{}
	for _, f := range flags {
		switch strings.ToLower(string(f)) {
		case `\seen`:
			read = true
		case `\flagged`:
			starred = true
		case `\draft`, `\recent`, `\*`:
		default:
			k := strings.ToLower(string(f))
			if !seen[k] && validKeyword(string(f)) {
				seen[k] = true
				rest = append(rest, string(f))
			}
		}
	}
	sort.Strings(rest)
	others = strings.Join(rest, " ")
	if len(others) > 900 {
		others = others[:strings.LastIndex(others[:900], " ")+1]
	}
	return read, starred, strings.TrimSpace(others)
}

// validKeyword: a flag of printable ASCII, no specials.
func validKeyword(f string) bool {
	if f == "" || len(f) > 64 {
		return false
	}
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c <= ' ' || c > '~' || strings.IndexByte(`(){%*"]`, c) >= 0 || c == '\\' && i > 0 {
			return false
		}
	}
	return true
}

// permanentFlags are those a client may set.
var permanentFlags = []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted,
	imap.FlagDraft, imap.FlagForwarded, imap.FlagWildcard}

// Select selects a folder.
func (is *imapSession) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	is.touch()
	f, err := folderOf(name)
	if err != nil {
		return nil, err
	}
	es, err := is.x.svc.B.Store.FolderEntries(is.user.ID, f)
	if err != nil {
		return nil, err
	}
	next, err := is.x.svc.B.Store.MailUIDNext()
	if err != nil {
		return nil, err
	}
	is.sel = &selection{folder: f, readOnly: options != nil && options.ReadOnly, msgs: es}
	d := &imap.SelectData{Flags: permanentFlags[:len(permanentFlags)-1], PermanentFlags: permanentFlags,
		NumMessages: uint32(len(es)), UIDNext: imap.UID(next), UIDValidity: is.uidValidity()}
	for i, e := range es {
		if !e.Read {
			d.FirstUnseenSeqNum = uint32(i + 1)
			break
		}
	}
	if is.sel.readOnly {
		d.PermanentFlags = nil
	}
	return d, nil
}

func (is *imapSession) Unselect() error {
	is.sel = nil
	return nil
}

func (is *imapSession) Create(name string, options *imap.CreateOptions) error {
	if store.FolderByName(name) >= 0 {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAlreadyExists, Text: "It is there already"}
	}
	return errFixed
}

func (is *imapSession) Delete(name string) error { return errFixed }

func (is *imapSession) Rename(name, newName string, options *imap.RenameOptions) error {
	return errFixed
}

// Subscribe: every folder is subscribed.
func (is *imapSession) Subscribe(name string) error {
	_, err := folderOf(name)
	return err
}

func (is *imapSession) Unsubscribe(name string) error {
	_, err := folderOf(name)
	return err
}

// Namespace: the folders are personal, at the top.
func (is *imapSession) Namespace() (*imap.NamespaceData, error) {
	return &imap.NamespaceData{Personal: []imap.NamespaceDescriptor{{Prefix: "", Delim: delim}}}, nil
}

// List lists the folders that match.
func (is *imapSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	is.touch()
	if len(patterns) == 0 {
		return w.WriteList(&imap.ListData{Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}, Delim: delim})
	}
	for f, name := range store.FolderNames {
		match := false
		for _, p := range patterns {
			match = match || imapserver.MatchList(name, delim, ref, p)
		}
		use := specialUse(f)
		if !match || options.SelectSpecialUse && use == "" {
			continue
		}
		d := &imap.ListData{Mailbox: name, Delim: delim, Attrs: []imap.MailboxAttr{imap.MailboxAttrHasNoChildren}}
		if use != "" {
			d.Attrs = append(d.Attrs, use)
		}
		if options.SelectSubscribed || options.ReturnSubscribed {
			d.Attrs = append(d.Attrs, imap.MailboxAttrSubscribed)
		}
		if options.ReturnStatus != nil {
			st, err := is.Status(name, options.ReturnStatus)
			if err != nil {
				return err
			}
			d.Status = st
		}
		if err := w.WriteList(d); err != nil {
			return err
		}
	}
	return nil
}

// size is a mail's size as the client is told it.
func (is *imapSession) size(e *store.BoxEntry) int64 {
	if e.Size > 0 {
		return e.Size
	}
	raw, err := is.raw(e)
	if err != nil {
		return 0
	}
	return int64(len(raw))
}

// raw is a mail's raw form.
func (is *imapSession) raw(e *store.BoxEntry) ([]byte, error) {
	m, _, err := is.x.svc.B.Store.BoxMail(is.user.ID, e.Box)
	if err != nil {
		return nil, err
	}
	return crlf(is.x.svc.Render(m)), nil
}

// Status is a folder's counts.
func (is *imapSession) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	is.touch()
	f, err := folderOf(name)
	if err != nil {
		return nil, err
	}
	es, err := is.x.svc.B.Store.FolderEntries(is.user.ID, f)
	if err != nil {
		return nil, err
	}
	d := &imap.StatusData{Mailbox: store.FolderNames[f]}
	n := uint32(len(es))
	if options.NumMessages {
		d.NumMessages = &n
	}
	if options.UIDNext {
		next, err := is.x.svc.B.Store.MailUIDNext()
		if err != nil {
			return nil, err
		}
		d.UIDNext = imap.UID(next)
	}
	if options.UIDValidity {
		d.UIDValidity = is.uidValidity()
	}
	if options.NumUnseen {
		var u uint32
		for _, e := range es {
			if !e.Read {
				u++
			}
		}
		d.NumUnseen = &u
	}
	if options.NumDeleted {
		var del uint32
		for _, e := range es {
			if hasFlag(e, imap.FlagDeleted) {
				del++
			}
		}
		d.NumDeleted = &del
	}
	if options.NumRecent {
		zero := uint32(0)
		d.NumRecent = &zero
	}
	if options.Size {
		var total int64
		for _, e := range es {
			total += is.size(e)
		}
		d.Size = &total
	}
	return d, nil
}

// Append keeps a mail the client puts in a folder: in Sent as the user's
// (once: a mail sent here and put there again is the one already there),
// in Drafts as a draft, else as received.
func (is *imapSession) Append(name string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	is.touch()
	f, err := folderOf(name)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeTryCreate, Text: "No such folder"}
	}
	svc := is.x.svc
	raw, err := io.ReadAll(io.LimitReader(r, svc.Set.MaxMessage+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > svc.Set.MaxMessage {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeTooBig, Text: "Too big"}
	}
	raw = crlf(raw)
	p, err := parse(raw)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeParse, Text: "Not a mail"}
	}
	read, starred, others := splitFlags(options.Flags)
	st := svc.B.Store
	role := store.RoleReceived
	switch f {
	case store.FolderSent:
		role = store.RoleSent
		if p.messageID != "" {
			if box, err := st.SentByMessageID(is.user.ID, p.messageID); err == nil {
				return is.appended(box, f)
			}
		}
	case store.FolderDrafts:
		role = store.RoleDraft
	case store.FolderStarred:
		starred = true
	case store.FolderUnread:
		read = false
	}
	if role != store.RoleReceived {
		read = true
	}
	subject := bbs.Clean(p.subject, bbs.MaxSubjectLen)
	if subject == "" {
		subject = "(no subject)"
	}
	m := &store.Message{Subject: subject, Body: terminalBody(p.text), Raw: raw, MessageID: p.messageID,
		FromAddr: p.fromAddr, ToAddr: addressList(append(p.to, p.cc...)), Created: options.Time}
	m.From = bbs.Clean(shownAddress(p.fromName, p.fromAddr), 200)
	if role != store.RoleReceived {
		m.FromID, m.From, m.FromAddr = is.user.ID, is.user.Name, ""
	}
	m.To = bbs.Clean(addressList(p.to), bbs.MaxMailToLen)
	if m.Created.IsZero() {
		m.Created = p.date
	}
	box, err := st.AppendMail(is.user.ID, role, m, read, starred, f == store.FolderTrash, others)
	if err != nil {
		return nil, err
	}
	return is.appended(box, f)
}

// appended is APPEND's answer for an entry put in a folder.
func (is *imapSession) appended(box int64, f int) (*imap.AppendData, error) {
	uid, err := is.x.svc.B.Store.BoxUID(box, f)
	if err != nil {
		return &imap.AppendData{UIDValidity: is.uidValidity()}, nil
	}
	return &imap.AppendData{UID: imap.UID(uid), UIDValidity: is.uidValidity()}, nil
}

// Poll tells the client what changed in the selected folder since it was
// last told: mail gone (if it may be told now), flags changed, mail come.
func (is *imapSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	sel := is.sel
	if sel == nil {
		return nil
	}
	is.touch()
	fresh, err := is.x.svc.B.Store.FolderEntries(is.user.ID, sel.folder)
	if err != nil {
		return err
	}
	byUID := map[int64]*store.BoxEntry{}
	for _, e := range fresh {
		byUID[e.UID] = e
	}
	if allowExpunge {
		for i := len(sel.msgs) - 1; i >= 0; i-- {
			if byUID[sel.msgs[i].UID] == nil {
				if err := w.WriteExpunge(uint32(i + 1)); err != nil {
					return err
				}
				sel.msgs = append(sel.msgs[:i], sel.msgs[i+1:]...)
			}
		}
	}
	var max int64
	for i, e := range sel.msgs {
		if e.UID > max {
			max = e.UID
		}
		f := byUID[e.UID]
		if f == nil {
			continue
		}
		if !sameFlags(flagsOf(e), flagsOf(f)) {
			if err := w.WriteMessageFlags(uint32(i+1), imap.UID(f.UID), flagsOf(f)); err != nil {
				return err
			}
		}
		sel.msgs[i] = f
	}
	n := len(sel.msgs)
	for _, e := range fresh {
		if e.UID > max {
			sel.msgs = append(sel.msgs, e)
		}
	}
	if len(sel.msgs) != n {
		return w.WriteNumMessages(uint32(len(sel.msgs)))
	}
	return nil
}

func sameFlags(a, b []imap.Flag) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(string(a[i]), string(b[i])) {
			return false
		}
	}
	return true
}

// Idle waits for changes, telling of them, until the client is done.
func (is *imapSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		if err := is.Poll(w, true); err != nil {
			return err
		}
		select {
		case <-stop:
			return nil
		case <-is.wake:
		case <-t.C:
		}
	}
}

// staticNumSet makes "*" and "$" a set's numbers.
func (sel *selection) staticNumSet(numSet imap.NumSet) imap.NumSet {
	if imap.IsSearchRes(numSet) {
		return sel.searchRes
	}
	switch ns := numSet.(type) {
	case imap.SeqSet:
		max := uint32(len(sel.msgs))
		out := make(imap.SeqSet, len(ns))
		copy(out, ns)
		for i := range out {
			staticRange(&out[i].Start, &out[i].Stop, max)
		}
		return out
	case imap.UIDSet:
		var max uint32
		if len(sel.msgs) > 0 {
			max = uint32(sel.msgs[len(sel.msgs)-1].UID)
		}
		out := make(imap.UIDSet, len(ns))
		copy(out, ns)
		for i := range out {
			staticRange((*uint32)(&out[i].Start), (*uint32)(&out[i].Stop), max)
		}
		return out
	}
	return numSet
}

func staticRange(start, stop *uint32, max uint32) {
	dyn := false
	if *start == 0 {
		*start, dyn = max, true
	}
	if *stop == 0 {
		*stop, dyn = max, true
	}
	if dyn && *start > *stop {
		*start, *stop = *stop, *start
	}
}

// each calls f for the selected folder's mail in a set, by sequence
// number.
func (sel *selection) each(numSet imap.NumSet, f func(seq uint32, e *store.BoxEntry) error) error {
	numSet = sel.staticNumSet(numSet)
	for i, e := range sel.msgs {
		seq := uint32(i + 1)
		var in bool
		switch ns := numSet.(type) {
		case imap.SeqSet:
			in = ns.Contains(seq)
		case imap.UIDSet:
			in = ns.Contains(imap.UID(e.UID))
		}
		if in {
			if err := f(seq, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// Fetch answers FETCH; a body fetched (not peeked) marks the mail read.
func (is *imapSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	is.touch()
	sel := is.sel
	markSeen := false
	for _, bs := range options.BodySection {
		markSeen = markSeen || !bs.Peek
	}
	needRaw := options.Envelope || options.BodyStructure != nil || len(options.BodySection) > 0 ||
		len(options.BinarySection) > 0 || len(options.BinarySectionSize) > 0
	return sel.each(numSet, func(seq uint32, e *store.BoxEntry) error {
		var raw []byte
		if needRaw || options.RFC822Size && e.Size == 0 {
			var err error
			if raw, err = is.raw(e); err != nil {
				return nil // gone since: nothing to say of it
			}
		}
		flagsChanged := false
		if markSeen && !e.Read && !sel.readOnly {
			if err := is.x.svc.B.Store.SetBoxFlags(is.user.ID, e.Box, true, e.Starred, e.Flags); err == nil {
				e.Read, flagsChanged = true, true
			}
		}
		rw := w.CreateMessage(seq)
		rw.WriteUID(imap.UID(e.UID))
		if options.Flags || flagsChanged {
			rw.WriteFlags(flagsOf(e))
		}
		if options.InternalDate {
			rw.WriteInternalDate(e.Created)
		}
		if options.RFC822Size {
			if e.Size > 0 {
				rw.WriteRFC822Size(e.Size)
			} else {
				rw.WriteRFC822Size(int64(len(raw)))
			}
		}
		if options.Envelope {
			if h, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(raw))); err == nil {
				rw.WriteEnvelope(imapserver.ExtractEnvelope(h))
			}
		}
		if options.BodyStructure != nil {
			rw.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(raw)))
		}
		for _, bs := range options.BodySection {
			buf := imapserver.ExtractBodySection(bytes.NewReader(raw), bs)
			wc := rw.WriteBodySection(bs, int64(len(buf)))
			_, werr := wc.Write(buf)
			if cerr := wc.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return werr
			}
		}
		for _, bs := range options.BinarySection {
			buf := imapserver.ExtractBinarySection(bytes.NewReader(raw), bs)
			wc := rw.WriteBinarySection(bs, int64(len(buf)))
			_, werr := wc.Write(buf)
			if cerr := wc.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return werr
			}
		}
		for _, bss := range options.BinarySectionSize {
			rw.WriteBinarySectionSize(bss, imapserver.ExtractBinarySectionSize(bytes.NewReader(raw), bss))
		}
		return rw.Close()
	})
}

// Store changes flags.
func (is *imapSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	is.touch()
	sel := is.sel
	if sel.readOnly {
		return errReadOnly
	}
	st := is.x.svc.B.Store
	return sel.each(numSet, func(seq uint32, e *store.BoxEntry) error {
		now := flagsOf(e)
		var next []imap.Flag
		switch flags.Op {
		case imap.StoreFlagsSet:
			next = flags.Flags
		case imap.StoreFlagsAdd:
			next = append(now, flags.Flags...)
		case imap.StoreFlagsDel:
			drop := map[string]bool{}
			for _, f := range flags.Flags {
				drop[strings.ToLower(string(f))] = true
			}
			for _, f := range now {
				if !drop[strings.ToLower(string(f))] {
					next = append(next, f)
				}
			}
		}
		read, starred, others := splitFlags(next)
		if err := st.SetBoxFlags(is.user.ID, e.Box, read, starred, others); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return err
		}
		e.Read, e.Starred, e.Flags = read, starred, others
		if !flags.Silent {
			rw := w.CreateMessage(seq)
			rw.WriteUID(imap.UID(e.UID))
			rw.WriteFlags(flagsOf(e))
			return rw.Close()
		}
		return nil
	})
}

// takeOut takes an entry out of a folder, as EXPUNGE does: out of Starred
// it is unstarred, out of Unread read, out of the trash deleted for good,
// out of any other into the trash.
func (is *imapSession) takeOut(e *store.BoxEntry, folder int) error {
	st := is.x.svc.B.Store
	_, _, others := splitFlags(withoutFlag(flagsOf(e), imap.FlagDeleted))
	switch folder {
	case store.FolderTrash:
		return st.PurgeBox(is.user.ID, e.Box)
	case store.FolderStarred:
		return st.SetBoxFlags(is.user.ID, e.Box, e.Read, false, others)
	case store.FolderUnread:
		return st.SetBoxFlags(is.user.ID, e.Box, true, e.Starred, others)
	}
	if err := st.SetBoxFlags(is.user.ID, e.Box, e.Read, e.Starred, others); err != nil {
		return err
	}
	return st.TrashBox(is.user.ID, e.Box)
}

func withoutFlag(l []imap.Flag, f imap.Flag) []imap.Flag {
	var out []imap.Flag
	for _, g := range l {
		if !strings.EqualFold(string(g), string(f)) {
			out = append(out, g)
		}
	}
	return out
}

// Expunge takes the mail marked \Deleted out of the folder.
func (is *imapSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	is.touch()
	sel := is.sel
	if sel.readOnly {
		return errReadOnly
	}
	for i := len(sel.msgs) - 1; i >= 0; i-- {
		e := sel.msgs[i]
		if uids != nil && !uids.Contains(imap.UID(e.UID)) || !hasFlag(e, imap.FlagDeleted) {
			continue
		}
		if err := is.takeOut(e, sel.folder); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := w.WriteExpunge(uint32(i + 1)); err != nil {
			return err
		}
		sel.msgs = append(sel.msgs[:i], sel.msgs[i+1:]...)
	}
	return nil
}

// putIn puts an entry in a folder, as COPY does; ok false if it cannot go
// there.
func (is *imapSession) putIn(e *store.BoxEntry, folder int) (bool, error) {
	st := is.x.svc.B.Store
	u := is.user.ID
	trashed := !e.Deleted.IsZero()
	switch folder {
	case store.FolderTrash:
		if trashed {
			return true, nil
		}
		return true, st.TrashBox(u, e.Box)
	case store.FolderStarred:
		if err := st.SetBoxFlags(u, e.Box, e.Read, true, e.Flags); err != nil {
			return false, err
		}
		if trashed {
			return true, st.RestoreBox(u, e.Box)
		}
		return true, nil
	case store.FolderUnread:
		if e.Role != store.RoleReceived {
			return false, nil
		}
		if err := st.SetBoxFlags(u, e.Box, false, e.Starred, e.Flags); err != nil {
			return false, err
		}
	case store.FolderInbox:
		if e.Role != store.RoleReceived {
			return false, nil
		}
	case store.FolderSent:
		if e.Role != store.RoleSent {
			return false, nil
		}
	case store.FolderDrafts:
		if e.Role != store.RoleDraft {
			return false, nil
		}
	case store.FolderAll:
		if e.Role == store.RoleDraft {
			return false, nil
		}
	}
	if trashed {
		return true, st.RestoreBox(u, e.Box)
	}
	return true, nil
}

// copyTo puts the mail of a set in a folder: the numbers they have there.
func (is *imapSession) copyTo(numSet imap.NumSet, dest string, each func(seq uint32, e *store.BoxEntry) error) (*imap.CopyData, error) {
	is.touch()
	sel := is.sel
	f, err := folderOf(dest)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeTryCreate, Text: "No such folder"}
	}
	if f == sel.folder {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "It is in that folder already"}
	}
	d := &imap.CopyData{UIDValidity: is.uidValidity()}
	var cannot []string
	err = sel.each(numSet, func(seq uint32, e *store.BoxEntry) error {
		ok, err := is.putIn(e, f)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return err
		}
		if !ok {
			cannot = append(cannot, fmt.Sprint(e.UID))
			return nil
		}
		if uid, err := is.x.svc.B.Store.BoxUID(e.Box, f); err == nil {
			d.SourceUIDs.AddNum(imap.UID(e.UID))
			d.DestUIDs.AddNum(imap.UID(uid))
		}
		if each != nil {
			return each(seq, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(cannot) > 0 && len(d.SourceUIDs) == 0 {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeCannot,
			Text: "Mail goes in " + store.FolderNames[f] + " only by what it is (received, sent, a draft)"}
	}
	return d, nil
}

// Copy puts mail in another folder.
func (is *imapSession) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
	if is.sel.readOnly {
		return nil, errReadOnly
	}
	return is.copyTo(numSet, dest, nil)
}

// Move puts mail in another folder; what is no longer in this one is said
// to be gone.
func (is *imapSession) Move(w *imapserver.MoveWriter, numSet imap.NumSet, dest string) error {
	sel := is.sel
	if sel.readOnly {
		return errReadOnly
	}
	var moved []int64
	d, err := is.copyTo(numSet, dest, func(seq uint32, e *store.BoxEntry) error {
		moved = append(moved, e.Box)
		return nil
	})
	if err != nil {
		return err
	}
	if err := w.WriteCopyData(d); err != nil {
		return err
	}
	st := is.x.svc.B.Store
	gone := map[int64]bool{}
	for _, b := range moved {
		if _, err := st.BoxUID(b, sel.folder); errors.Is(err, store.ErrNotFound) {
			gone[b] = true
		}
	}
	for i := len(sel.msgs) - 1; i >= 0; i-- {
		if gone[sel.msgs[i].Box] {
			if err := w.WriteExpunge(uint32(i + 1)); err != nil {
				return err
			}
			sel.msgs = append(sel.msgs[:i], sel.msgs[i+1:]...)
		}
	}
	return nil
}

// Search answers SEARCH.
func (is *imapSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	is.touch()
	sel := is.sel
	is.staticCriteria(criteria)
	var d imap.SearchData
	var seqs imap.SeqSet
	var uids imap.UIDSet
	for i, e := range sel.msgs {
		seq := uint32(i + 1)
		mm := &matcher{is: is, e: e}
		if !mm.match(seq, criteria) {
			continue
		}
		uids.AddNum(imap.UID(e.UID))
		num := seq
		if kind == imapserver.NumKindUID {
			num = uint32(e.UID)
		} else {
			seqs.AddNum(seq)
		}
		if d.Min == 0 || num < d.Min {
			d.Min = num
		}
		if num > d.Max {
			d.Max = num
		}
		d.Count++
	}
	if kind == imapserver.NumKindUID {
		d.All = uids
	} else {
		d.All = seqs
	}
	if options.ReturnSave {
		sel.searchRes = uids
	}
	return &d, nil
}

// staticCriteria makes the number sets of a search's criteria static.
func (is *imapSession) staticCriteria(c *imap.SearchCriteria) {
	sel := is.sel
	var seqs []imap.SeqSet
	for _, s := range c.SeqNum {
		switch ns := sel.staticNumSet(s).(type) {
		case imap.SeqSet:
			seqs = append(seqs, ns)
		case imap.UIDSet:
			c.UID = append(c.UID, ns)
		}
	}
	c.SeqNum = seqs
	for i := range c.UID {
		c.UID[i] = sel.staticNumSet(c.UID[i]).(imap.UIDSet)
	}
	for i := range c.Not {
		is.staticCriteria(&c.Not[i])
	}
	for i := range c.Or {
		is.staticCriteria(&c.Or[i][0])
		is.staticCriteria(&c.Or[i][1])
	}
}
