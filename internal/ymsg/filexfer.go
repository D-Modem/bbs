package ymsg

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// File transfer, with the BBS's web half as the go-between. Files go only
// between Yahoo! Messenger users, and pass through a temporary file.
//
// Y!M 7 and later (FILETRANSFER_15, 0xdc to 0xde, through a relay):
//
//  1. The sender offers the file (0xdc, 222=1); the BBS passes it on.
//  2. The receiver accepts (222=3) or declines (222=4); passed back.
//  3. The sender asks for a relay (0xdd); the BBS gives the receiver the
//     relay's address (250, the web half) and a token for it (251).
//  4. The receiver says it's ready (0xde) and fetches
//     http://<relay>/relay?token=... (HEAD, then GET); the BBS gives the
//     sender the token and the sender POSTs the file to the same URL.
//
// Y!M 5 and 6 (FILETRANSFER, 0x46): the sender POSTs a YMSG packet and the
// file to http://filetransfer.msg.yahoo.com/notifyft. The BBS keeps the
// file and sends the receiver a 0x46 with a URL to fetch it from.

const (
	xferWait = 15 * time.Minute // an offer, until its file is uploaded
	xferKeep = time.Hour        // an uploaded file, until it's fetched
	xferIdle = 2 * time.Minute  // the longest pause in an upload or download
	y7Files  = 12               // the first YMSG version with FILETRANSFER_15
)

// xfer is a file on its way from one user to another.
type xfer struct {
	id       string // the sender's ID for it (key 265; Y!M 7 and later)
	token    string // the relay's or the download URL's
	from, to *store.User
	src, dst *session
	name     string
	size     int64

	expires time.Time // s.mu
	started bool      // s.mu: an upload began

	done chan struct{} // closed when the upload ends
	path string        // the temporary file, once uploaded ("": it failed)
	n    int64
}

// filesOff says why files can't be sent ("": they can).
func (s *Server) filesOff() string {
	if s.S.HTTPAddress == "" || s.S.MaxFileSize <= 0 {
		return "Sending files by Yahoo! Messenger isn't set up here: upload the file to the BBS's file areas instead."
	}
	return ""
}

// newXfer registers a transfer and gives it a token.
func (s *Server) newXfer(x *xfer) {
	x.token = randomString(32)
	x.done = make(chan struct{})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireXfers()
	x.expires = time.Now().Add(xferWait)
	s.xfers[x.token] = x
}

// xferByToken is a transfer by its token (nil: none, or expired).
func (s *Server) xferByToken(t string) *xfer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireXfers()
	return s.xfers[t]
}

// xferByID is a Y!M 7 transfer a session is sending or receiving.
func (s *Server) xferByID(id string, ss *session) *xfer {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.xfers {
		if x.id == id && (x.src == ss || x.dst == ss) {
			return x
		}
	}
	return nil
}

// dropXfer forgets a transfer and removes its file, once any upload of it
// has ended.
func (s *Server) dropXfer(x *xfer) {
	s.mu.Lock()
	_, ok := s.xfers[x.token]
	delete(s.xfers, x.token)
	started := x.started
	x.started = true // no upload may begin now
	s.mu.Unlock()
	if !ok {
		return
	}
	if !started {
		close(x.done)
		return
	}
	go func() {
		<-x.done
		if x.path != "" {
			os.Remove(x.path)
		}
	}()
}

// expireXfers drops transfers past their time (s.mu held).
func (s *Server) expireXfers() {
	now := time.Now()
	for t, x := range s.xfers {
		if now.After(x.expires) {
			delete(s.xfers, t)
			if !x.started {
				x.started = true
				close(x.done)
				continue
			}
			go func() {
				<-x.done
				if x.path != "" {
					os.Remove(x.path)
				}
			}()
		}
	}
}

// receiver is the session a user gets files on: their newest signed-in
// Yahoo session that understands FILETRANSFER_15 (y7) or any (!y7). why
// is the reason there's none.
func (s *Server) receiver(t *store.User, y7 bool) (dst *session, why string) {
	any := false
	for _, o := range s.sessionsOf(t.ID) {
		if !o.isOnline() {
			continue
		}
		any = true
		if !y7 || o.ver >= y7Files {
			dst = o
		}
	}
	switch {
	case dst != nil:
		return dst, ""
	case any:
		return nil, idOf(t) + "'s Yahoo! Messenger is too old to take files from yours."
	default:
		return nil, idOf(t) + " isn't on Yahoo! Messenger now. Files go only between Yahoo! Messenger " +
			"users: upload it to the BBS's file areas instead."
	}
}

// fileCheck says why ss may not send a file of size bytes to who ("": it
// may), with the user, or nil when who is ss's own.
func (ss *session) fileCheck(who string, size int64) (*store.User, string) {
	s := ss.s
	if why := s.filesOff(); why != "" {
		return nil, why
	}
	t := s.resolve(who)
	if t == nil {
		return nil, who + " is not a user here."
	}
	if t.ID == ss.u.ID {
		return nil, ""
	}
	if s.B.Blocks(t, ss.u) {
		return nil, bbs.ErrBlocked.Error() + "."
	}
	if size > s.S.MaxFileSize {
		return nil, fmt.Sprintf("That file is too big to send here: the most is %s.", sizeText(s.S.MaxFileSize))
	}
	return t, ""
}

// sizeText is a size in bytes for people.
func sizeText(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return strconv.FormatInt(n>>20, 10) + " MB"
	}
	if n >= 1<<10 {
		return strconv.FormatInt(n>>10, 10) + " KB"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}

// fileTrans15 is FILETRANSFER_15: an offer, or the answer to one.
func (ss *session) fileTrans15(p *packet) {
	s := ss.s
	id := p.get(265)
	if id == "" {
		return
	}
	if p.get(222) == "1" {
		ss.offerFile(p, id)
		return
	}
	x := s.xferByID(id, ss)
	if x == nil || x.dst != ss {
		return
	}
	ans := p.get(222)
	if ans != "3" {
		s.dropXfer(x)
	}
	x.src.k.send(svcFileTrans15, stOK, kv(4, ss.id), kv(5, x.src.id), kv(265, id), kv(222, ans))
}

// offerFile passes a Y!M 7 offer to the receiver, or declines it for them.
func (ss *session) offerFile(p *packet, id string) {
	s := ss.s
	who := p.get(5)
	size, _ := strconv.ParseInt(p.get(28), 10, 64)
	decline := func(why string) {
		ss.k.send(svcFileTrans15, stOK, kv(4, who), kv(5, ss.id), kv(265, id), kv(222, "4"))
		ss.system(why)
	}
	t, why := ss.fileCheck(who, size)
	if why != "" {
		decline(why)
		return
	}
	if t == nil || s.xferByID(id, ss) != nil {
		return
	}
	dst, why := s.receiver(t, true)
	if dst == nil {
		decline(why)
		return
	}
	x := &xfer{id: id, from: ss.u, to: t, src: ss, dst: dst, name: p.get(27), size: size}
	s.newXfer(x)
	out := &packet{service: svcFileTrans15, status: stOK}
	out.add(4, ss.id)
	out.add(5, dst.id)
	out.add(265, id)
	out.add(222, "1")
	for _, kv := range p.pairs {
		switch kv.k {
		case 1, 4, 5, 222, 265:
		case 14:
			out.add(14, dst.toClient(fromYahoo(kv.v, p.get(97) == "1")))
		default:
			out.add(kv.k, kv.v)
		}
	}
	dst.k.write(out)
}

// fileTransInfo15 is the sender asking for a relay: the receiver is told
// where it is and the token for it.
func (ss *session) fileTransInfo15(p *packet) {
	x := ss.s.xferByID(p.get(265), ss)
	if x == nil || x.src != ss {
		return
	}
	relay := ss.s.S.Host
	if relay == "" {
		relay = p.get(250)
	}
	if relay == "" {
		relay, _, _ = net.SplitHostPort(ss.k.c.LocalAddr().String())
	}
	x.dst.k.send(svcFileTransInfo15, stOK, kv(4, ss.id), kv(5, x.dst.id), kv(265, x.id), kv(27, x.name),
		kv(249, "3"), kv(250, relay), kv(251, x.token))
}

// fileTransAcc15 is the receiver ready at the relay: the sender is given
// the token to upload with.
func (ss *session) fileTransAcc15(p *packet) {
	x := ss.s.xferByID(p.get(265), ss)
	if x == nil || x.dst != ss {
		return
	}
	x.src.k.send(svcFileTransAcc15, stOK, kv(4, ss.id), kv(5, x.src.id), kv(265, x.id), kv(27, x.name),
		kv(249, "3"), kv(251, x.token))
}

// The web half's file transfer URLs.

// relay is /relay: the Y!M 7 relay. The sender POSTs, the receiver HEADs
// and GETs.
func (s *Server) relay(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	x := s.xferByToken(q.Get("token"))
	if x == nil || x.id == "" {
		http.NotFound(w, r)
		return
	}
	if !s.is(q.Get("sender"), x.from) || !s.is(q.Get("recver"), x.to) {
		http.Error(w, "not yours", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := s.upload(x, r.Body, w); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Write([]byte("OK"))
	case http.MethodHead, http.MethodGet:
		s.download(x, w, r)
	default:
		http.Error(w, "no", http.StatusMethodNotAllowed)
	}
}

// is says whether a Yahoo ID in a URL is u's ("" is anyone's).
func (s *Server) is(id string, u *store.User) bool {
	if id == "" {
		return true
	}
	o := s.resolve(id)
	return o != nil && o.ID == u.ID
}

// upload stores a transfer's file from r, once.
func (s *Server) upload(x *xfer, r io.Reader, w http.ResponseWriter) (err error) {
	s.mu.Lock()
	if x.started {
		s.mu.Unlock()
		return errors.New("already sent")
	}
	x.started = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		x.expires = time.Now().Add(xferKeep)
		s.mu.Unlock()
		close(x.done)
	}()
	f, err := os.CreateTemp("", "bbs-ymsg-*")
	if err != nil {
		s.B.Log.Printf("ymsg: file transfer: %v", err)
		return errors.New("no room")
	}
	rc := http.NewResponseController(w)
	n, err := io.Copy(f, &idle{r: io.LimitReader(r, s.S.MaxFileSize+1), rc: rc})
	if err == nil && n > s.S.MaxFileSize {
		err = errors.New("too big")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	x.path, x.n = f.Name(), n
	return nil
}

// download serves a transfer's file once it's uploaded, then drops it.
func (s *Server) download(x *xfer, w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Now().Add(xferWait + xferIdle))
	t := time.NewTimer(xferWait)
	defer t.Stop()
	select {
	case <-x.done:
	case <-t.C:
		http.NotFound(w, r)
		return
	case <-r.Context().Done():
		return
	}
	if x.path == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(x.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(x.n, 10))
	if r.Method == http.MethodHead {
		return
	}
	n, err := io.Copy(&idle{w: w, rc: rc}, f)
	if err == nil && n == x.n {
		s.dropXfer(x)
	}
}

// idle reads or writes, giving each step xferIdle.
type idle struct {
	r  io.Reader
	w  io.Writer
	rc *http.ResponseController
}

func (d *idle) Read(b []byte) (int, error) {
	t := time.Now().Add(xferIdle)
	d.rc.SetReadDeadline(t)
	d.rc.SetWriteDeadline(t) // for the answer, after a long upload
	return d.r.Read(b)
}

func (d *idle) Write(b []byte) (int, error) {
	d.rc.SetWriteDeadline(time.Now().Add(xferIdle))
	return d.w.Write(b)
}

// notifyFT is /notifyft: a Y!M 5 or 6 upload. The body is a FILETRANSFER
// packet (0 the sender, 5 the receiver, 14 a message, 27 the file's name,
// 28 its size) ending with key 29, then the file.
func (s *Server) notifyFT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "no", http.StatusMethodNotAllowed)
		return
	}
	ss := s.byCookie(r.Header.Get("Cookie"))
	if ss == nil {
		http.Error(w, "not signed in", http.StatusForbidden)
		return
	}
	br := bufio.NewReader(r.Body)
	p, err := readFTHeader(br)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	who := p.get(5)
	size, _ := strconv.ParseInt(p.get(28), 10, 64)
	t, why := ss.fileCheck(who, size)
	var dst *session
	if why == "" && t != nil {
		dst, why = s.receiver(t, false)
	}
	if why != "" || t == nil {
		if why != "" {
			ss.system(why)
		}
		http.Error(w, "not sent", http.StatusForbidden)
		return
	}
	x := &xfer{from: ss.u, to: t, src: ss, dst: dst, name: p.get(27), size: size}
	s.newXfer(x)
	if err := s.upload(x, br, w); err != nil {
		s.dropXfer(x)
		ss.system("Your file " + x.name + " didn't reach " + who + ": " + err.Error() + ".")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	host := r.Host
	if host == "" {
		host = s.pagerHost(r)
	}
	u := "http://" + host + "/ft/" + x.token + "/" + url.PathEscape(x.name)
	exp := time.Now().Add(xferKeep).Unix()
	dst.k.send(svcFileTransfer, stOK, kv(4, ss.id), kv(5, dst.id),
		kv(14, dst.toClient(fromYahoo(p.get(14), p.get(97) == "1"))), kv(20, u), kn(38, exp),
		kv(27, x.name), kn(28, x.n))
	w.Write([]byte("OK"))
}

// readFTHeader reads /notifyft's packet, up to and including key 29.
func readFTHeader(br *bufio.Reader) (*packet, error) {
	var h [20]byte
	if _, err := io.ReadFull(br, h[:]); err != nil || string(h[:4]) != "YMSG" {
		return nil, errors.New("not a YMSG packet")
	}
	p := &packet{}
	var total int
	field := func() (string, error) {
		var b []byte
		for {
			c, err := br.ReadByte()
			if err != nil {
				return "", err
			}
			if total++; total > 8192 {
				return "", errors.New("packet too long")
			}
			if c == 0x80 && len(b) > 0 && b[len(b)-1] == 0xc0 {
				return string(b[:len(b)-1]), nil
			}
			b = append(b, c)
		}
	}
	for {
		k, err := field()
		if err != nil {
			return nil, err
		}
		if k == "29" {
			return p, nil
		}
		v, err := field()
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(k)
		if err != nil {
			return nil, errors.New("bad key")
		}
		p.add(n, v)
	}
}

// ft is /ft/<token>/<name>: a Y!M 5 or 6 upload's file, for its receiver.
func (s *Server) ft(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/ft/")
	tok, _, _ := strings.Cut(rest, "/")
	x := s.xferByToken(tok)
	if x == nil || x.id != "" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	s.download(x, w, r)
}

// byCookie is the signed-in session a Cookie header's T cookie was given to.
func (s *Server) byCookie(h string) *session {
	var t string
	for _, part := range strings.Split(h, ";") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "T="); ok {
			t = v
		}
	}
	if t == "" {
		return nil
	}
	for _, ss := range s.all() {
		ss.mu.Lock()
		ok := false
		for _, c := range ss.tcookies {
			if c == t {
				ok = true
			}
		}
		ss.mu.Unlock()
		if ok && ss.isOnline() {
			return ss
		}
	}
	return nil
}

// noteCookie remembers a T cookie a session was given (the last few).
func (ss *session) noteCookie(t string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.tcookies = append(ss.tcookies, t)
	if len(ss.tcookies) > 4 {
		ss.tcookies = ss.tcookies[1:]
	}
}
