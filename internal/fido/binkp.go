package fido

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
)

// binkp (FTS-1026) commands.
const (
	mNUL = iota
	mADR
	mPWD
	mFILE
	mOK
	mEOB
	mGOT
	mERR
	mBSY
	mGET
	mSKIP
)

// Limits of a session.
const (
	binkpTimeout  = 5 * time.Minute // without a frame
	binkpSession  = 2 * time.Hour
	maxFrame      = 32767
	blockSize     = 16384
	maxInFile     = 64 << 20
	maxInSession  = 512 << 20
	maxSessionCmd = 10000 // command frames: enough for many files
)

// ErrBusy: the link is in another session.
var ErrBusy = errors.New("busy: already in a session with this link")

type frame struct {
	cmd  bool
	id   byte
	data []byte
}

func (fr frame) arg() string { return strings.TrimRight(string(fr.data), "\x00") }

// session is one binkp session, either side.
type session struct {
	f      *Fido
	c      net.Conn
	r      *bufio.Reader
	w      *bufio.Writer
	link   *Link
	caller bool
	who    string

	frames chan frame
	rerr   error
}

func (s *session) send(fr frame) error {
	if len(fr.data)+1 > maxFrame {
		return errors.New("frame too long")
	}
	n := len(fr.data)
	var h [2]byte
	if fr.cmd {
		n++
		binary.BigEndian.PutUint16(h[:], uint16(n)|0x8000)
	} else {
		binary.BigEndian.PutUint16(h[:], uint16(n))
	}
	s.c.SetWriteDeadline(time.Now().Add(binkpTimeout))
	s.w.Write(h[:])
	if fr.cmd {
		s.w.WriteByte(fr.id)
	}
	s.w.Write(fr.data)
	return s.w.Flush()
}

func (s *session) cmd(id byte, arg string) error {
	return s.send(frame{cmd: true, id: id, data: []byte(arg)})
}

// readFrames reads frames into s.frames until an error, which is in rerr
// when the channel closes.
func (s *session) readFrames() {
	defer close(s.frames)
	for {
		s.c.SetReadDeadline(time.Now().Add(binkpTimeout))
		var h [2]byte
		if _, err := io.ReadFull(s.r, h[:]); err != nil {
			s.rerr = err
			return
		}
		v := binary.BigEndian.Uint16(h[:])
		fr := frame{cmd: v&0x8000 != 0}
		n := int(v & 0x7fff)
		buf := make([]byte, n)
		if _, err := io.ReadFull(s.r, buf); err != nil {
			s.rerr = err
			return
		}
		if fr.cmd {
			if n == 0 {
				s.rerr = errors.New("empty command frame")
				return
			}
			fr.id, fr.data = buf[0], buf[1:]
		} else {
			fr.data = buf
		}
		s.frames <- fr
	}
}

// next is the next frame.
func (s *session) next() (frame, error) {
	fr, ok := <-s.frames
	if !ok {
		if s.rerr == nil || errors.Is(s.rerr, io.EOF) {
			return frame{}, errors.New("the other side hung up")
		}
		return frame{}, s.rerr
	}
	return fr, nil
}

func (s *session) logf(format string, a ...any) {
	s.f.B.Log.Printf("binkp %s: "+format, append([]any{s.who}, a...)...)
}

// hello is our M_NULs and M_ADR.
func (s *session) hello(extra ...string) error {
	f := s.f
	info := []string{"SYS " + f.S.System, "ZYZ " + f.S.Sysop, "LOC " + f.S.Location,
		"NDL 115200,TCP,BINKP", "TIME " + time.Now().Format(time.RFC1123Z), "VER bbs-sec/1.0 binkp/1.0"}
	for _, i := range append(info, extra...) {
		if err := s.cmd(mNUL, i); err != nil {
			return err
		}
	}
	var addrs []string
	for _, a := range f.S.Addrs {
		addrs = append(addrs, a.String())
	}
	return s.cmd(mADR, strings.Join(addrs, " "))
}

func cramDigest(password, challenge string) (string, error) {
	ch, err := hex.DecodeString(challenge)
	if err != nil || len(ch) == 0 {
		return "", errors.New("bad CRAM-MD5 challenge")
	}
	m := hmac.New(md5.New, []byte(password))
	m.Write(ch)
	return hex.EncodeToString(m.Sum(nil)), nil
}

// Call calls a link, exchanges mail, and tosses what came.
func (f *Fido) Call(ctx context.Context, l *Link) error {
	if l.Host == "" {
		return errors.New("no host to call")
	}
	mu := f.lock(l)
	if !mu.TryLock() {
		return ErrBusy
	}
	defer mu.Unlock()
	if _, err := f.scan(l); err != nil {
		f.B.Log.Printf("fido: scan for %s: %v", l.Name, err)
	}
	d := net.Dialer{Timeout: time.Minute}
	c, err := d.DialContext(ctx, "tcp", l.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	s := &session{f: f, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c), link: l, caller: true,
		who: l.Name + " (" + l.Host + ")", frames: make(chan frame, 16)}
	c.SetDeadline(time.Now().Add(binkpSession))
	go s.readFrames()
	defer func() {
		c.Close()
		for range s.frames {
		}
	}()
	if err := s.hello(); err != nil {
		return err
	}
	challenge := ""
	for {
		fr, err := s.next()
		if err != nil {
			return err
		}
		if !fr.cmd {
			continue
		}
		switch fr.id {
		case mNUL:
			if opt, ok := strings.CutPrefix(fr.arg(), "OPT "); ok {
				for _, o := range strings.Fields(opt) {
					if c, ok := strings.CutPrefix(o, "CRAM-MD5-"); ok {
						challenge = c
					}
				}
			}
			continue
		case mERR, mBSY:
			return fmt.Errorf("refused: %s", fr.arg())
		case mADR:
		default:
			continue
		}
		// its addresses: the link's among them
		found := false
		for _, a := range strings.Fields(fr.arg()) {
			if ad, err := ftn.ParseAddr(a); err == nil && ad.Same(l.Addr) {
				found = true
			}
		}
		if !found {
			s.cmd(mERR, "You are not "+l.Addr.String())
			return fmt.Errorf("it says it is %s, not %s", fr.arg(), l.Addr)
		}
		break
	}
	pwd := "-"
	if l.Password != "" {
		switch {
		case challenge != "":
			dg, err := cramDigest(l.Password, challenge)
			if err != nil {
				return err
			}
			pwd = "CRAM-MD5-" + dg
		case l.PlainOK:
			pwd = l.Password
		default:
			s.cmd(mERR, "CRAM-MD5 required")
			return errors.New("it offers no CRAM-MD5: the password is not sent in the clear (plain_password = yes allows it)")
		}
	}
	if err := s.cmd(mPWD, pwd); err != nil {
		return err
	}
	for {
		fr, err := s.next()
		if err != nil {
			return err
		}
		if !fr.cmd {
			continue
		}
		switch fr.id {
		case mOK:
			s.logf("logged in (%s)", fr.arg())
			return s.transfer()
		case mERR, mBSY:
			return fmt.Errorf("refused: %s", fr.arg())
		}
	}
}

// Server answers binkp calls.
type Server struct {
	F          *Fido
	Address    string
	MaxClients int
	MaxPerIP   int

	mu      sync.Mutex
	clients int
	perIP   map[string]int
	conns   map[net.Conn]bool
}

// Run listens until ctx is done.
func (sv *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", sv.Address)
	if err != nil {
		return fmt.Errorf("binkp: %w", err)
	}
	return sv.Serve(ctx, ln)
}

// Serve answers on ln until ctx is done.
func (sv *Server) Serve(ctx context.Context, ln net.Listener) error {
	sv.perIP = map[string]int{}
	sv.conns = map[net.Conn]bool{}
	sv.F.B.Log.Printf("binkp: listening on %s", ln.Addr())
	go func() {
		<-ctx.Done()
		ln.Close()
		sv.mu.Lock()
		for c := range sv.conns {
			c.Close()
		}
		sv.mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go sv.serve(c)
	}
}

func (sv *Server) admit(c net.Conn) bool {
	h, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.MaxClients > 0 && sv.clients >= sv.MaxClients || sv.MaxPerIP > 0 && sv.perIP[h] >= sv.MaxPerIP {
		return false
	}
	sv.clients++
	sv.perIP[h]++
	sv.conns[c] = true
	return true
}

func (sv *Server) release(c net.Conn) {
	h, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.clients--
	if sv.perIP[h]--; sv.perIP[h] <= 0 {
		delete(sv.perIP, h)
	}
	delete(sv.conns, c)
}

func (sv *Server) serve(c net.Conn) {
	defer c.Close()
	f := sv.F
	if f.B.Refuse("binkp", c.RemoteAddr()) {
		return
	}
	s := &session{f: f, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c),
		who: c.RemoteAddr().String(), frames: make(chan frame, 16)}
	if !sv.admit(c) {
		s.cmd(mBSY, "Too many sessions; call again later")
		return
	}
	defer sv.release(c)
	defer func() {
		if r := recover(); r != nil {
			f.B.Log.Printf("binkp %s: panic: %v", s.who, r)
		}
	}()
	c.SetDeadline(time.Now().Add(binkpSession))
	go s.readFrames()
	defer func() {
		c.Close()
		for range s.frames {
		}
	}()
	if err := s.answer(); err != nil {
		s.logf("%v", err)
	}
}

// answer is the answering side's session.
func (s *session) answer() error {
	f := s.f
	var ch [16]byte
	rand.Read(ch[:])
	challenge := hex.EncodeToString(ch[:])
	if err := s.hello("OPT CRAM-MD5-" + challenge); err != nil {
		return err
	}
	var l *Link
	for l == nil {
		fr, err := s.next()
		if err != nil {
			return err
		}
		if !fr.cmd {
			continue
		}
		switch fr.id {
		case mERR, mBSY:
			return fmt.Errorf("it gave up: %s", fr.arg())
		case mADR:
		default:
			continue
		}
		for _, a := range strings.Fields(fr.arg()) {
			if ad, err := ftn.ParseAddr(a); err == nil {
				if l = f.linkByAddr(ad); l != nil {
					break
				}
			}
		}
		if l == nil {
			s.cmd(mERR, "Unknown address: this system talks to its links only")
			return fmt.Errorf("not a link: %s", fr.arg())
		}
	}
	s.who = l.Name + " (" + s.c.RemoteAddr().String() + ")"
	var pwd string
	for {
		fr, err := s.next()
		if err != nil {
			return err
		}
		if fr.cmd && fr.id == mPWD {
			pwd = fr.arg()
			break
		}
		if fr.cmd && (fr.id == mERR || fr.id == mBSY) {
			return fmt.Errorf("it gave up: %s", fr.arg())
		}
	}
	ok := false
	switch {
	case l.Password == "":
		ok = true
	case strings.HasPrefix(pwd, "CRAM-MD5-"):
		want, _ := cramDigest(l.Password, challenge)
		ok = subtle.ConstantTimeCompare([]byte(strings.ToLower(pwd[9:])), []byte(want)) == 1
	case l.PlainOK:
		ok = subtle.ConstantTimeCompare([]byte(pwd), []byte(l.Password)) == 1
	}
	if !ok {
		time.Sleep(2 * time.Second)
		s.cmd(mERR, "Bad password")
		return errors.New("bad password")
	}
	mu := f.lock(l)
	if !mu.TryLock() {
		s.cmd(mBSY, "Already in a session with you")
		return ErrBusy
	}
	defer mu.Unlock()
	if _, err := f.scan(l); err != nil {
		f.B.Log.Printf("fido: scan for %s: %v", l.Name, err)
	}
	s.link = l
	secure := "secure"
	if l.Password == "" {
		secure = "non-secure"
	}
	if err := s.cmd(mOK, secure); err != nil {
		return err
	}
	s.logf("logged in (%s)", secure)
	return s.transfer()
}

// outFile is a file being sent.
type outFile struct {
	path, name string
	size, time int64
	fh         *os.File
	off        int64
}

func (o *outFile) desc() string { return fmt.Sprintf("%s %d %d", o.name, o.size, o.time) }

// inFile is a file being received.
type inFile struct {
	name, tmp  string
	size, time int64
	fh         *os.File
	got        int64
}

// safeName is a received file's name as it is kept: its base, of
// characters that do no harm.
func safeName(n string) string {
	if i := strings.LastIndexAny(n, "/\\:"); i >= 0 {
		n = n[i+1:]
	}
	b := []byte(n)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_') {
			b[i] = '_'
		}
	}
	n = strings.TrimLeft(string(b), ".")
	if len(n) > 64 {
		n = n[len(n)-64:]
	}
	if n == "" {
		n = "file"
	}
	return n
}

// transfer sends the link's outbound and takes what it sends, then
// tosses (FTS-1026, binkp/1.0: one batch each way).
func (s *session) transfer() error {
	f, l := s.f, s.link
	defer func() {
		if _, err := f.toss(l); err != nil {
			f.B.Log.Printf("fido: toss for %s: %v", l.Name, err)
		}
	}()
	var queue []*outFile
	ents, _ := os.ReadDir(f.outbound(l))
	for _, e := range ents {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		queue = append(queue, &outFile{path: filepath.Join(f.outbound(l), e.Name()), name: e.Name(),
			size: fi.Size(), time: fi.ModTime().Unix()})
	}
	var cur *outFile
	waiting := map[string]*outFile{}
	var in *inFile
	sentEOB, gotEOB := false, false
	var total int64
	cmds := 0
	sent, received := 0, 0
	defer func() {
		if cur != nil && cur.fh != nil {
			cur.fh.Close()
		}
		if in != nil {
			in.fh.Close()
			os.Remove(in.tmp)
		}
	}()

	start := func(o *outFile, off int64) error {
		if o.fh == nil {
			fh, err := os.Open(o.path)
			if err != nil {
				return err
			}
			o.fh = fh
		}
		if _, err := o.fh.Seek(off, io.SeekStart); err != nil {
			return err
		}
		o.off = off
		cur = o
		return s.cmd(mFILE, fmt.Sprintf("%s %d", o.desc(), off))
	}
	find := func(arg string) *outFile {
		name := strings.Fields(arg + " ")[0]
		if cur != nil && cur.name == name {
			return cur
		}
		if o := waiting[name]; o != nil {
			return o
		}
		return nil
	}
	handle := func(fr frame) error {
		if !fr.cmd {
			if in == nil {
				return nil
			}
			if in.got+int64(len(fr.data)) > in.size {
				return fmt.Errorf("%s: more data than its size", in.name)
			}
			if _, err := in.fh.Write(fr.data); err != nil {
				return err
			}
			in.got += int64(len(fr.data))
			total += int64(len(fr.data))
			if in.got == in.size {
				return finish(s, f, l, &in, &received)
			}
			return nil
		}
		if cmds++; cmds > maxSessionCmd {
			return errors.New("too many commands")
		}
		arg := fr.arg()
		switch fr.id {
		case mFILE:
			if in != nil {
				in.fh.Close()
				os.Remove(in.tmp)
				in = nil
			}
			p := strings.Fields(arg)
			if len(p) < 4 {
				return fmt.Errorf("bad M_FILE %q", arg)
			}
			size, err1 := strconv.ParseInt(p[1], 10, 64)
			tm, err2 := strconv.ParseInt(p[2], 10, 64)
			off, err3 := strconv.ParseInt(p[3], 10, 64)
			if err1 != nil || err2 != nil || err3 != nil || size < 0 {
				return fmt.Errorf("bad M_FILE %q", arg)
			}
			if size > maxInFile || total+size > maxInSession {
				s.logf("%s: too big (%d bytes): skipped", p[0], size)
				return s.cmd(mSKIP, fmt.Sprintf("%s %d %d", p[0], size, tm))
			}
			if off != 0 {
				// we take files whole (and NR mode's -1 asks where from)
				return s.cmd(mGET, fmt.Sprintf("%s %d %d 0", p[0], size, tm))
			}
			fh, err := os.CreateTemp(f.inbound(l), ".part-*")
			if err != nil {
				return err
			}
			in = &inFile{name: p[0], tmp: fh.Name(), size: size, time: tm, fh: fh}
			if size == 0 {
				return finish(s, f, l, &in, &received)
			}
		case mGOT, mSKIP:
			o := find(arg)
			if o == nil {
				return nil
			}
			if o.fh != nil {
				o.fh.Close()
				o.fh = nil
			}
			if o == cur {
				cur = nil
			}
			delete(waiting, o.name)
			if fr.id == mGOT {
				os.Remove(o.path)
				sent++
			} else {
				s.logf("it skipped %s: kept for later", o.name)
			}
		case mGET:
			p := strings.Fields(arg)
			o := find(arg)
			if o == nil || len(p) < 4 {
				return nil
			}
			off, err := strconv.ParseInt(p[3], 10, 64)
			if err != nil || off < 0 || off > o.size {
				return fmt.Errorf("bad M_GET %q", arg)
			}
			delete(waiting, o.name)
			return start(o, off)
		case mEOB:
			gotEOB = true
		case mERR:
			return fmt.Errorf("it gave up: %s", arg)
		case mBSY:
			return fmt.Errorf("it is busy: %s", arg)
		}
		return nil
	}

	buf := make([]byte, blockSize)
	for {
		// a frame that came, without waiting while there is sending to do
		var fr frame
		have := false
		if cur != nil || len(queue) > 0 || !sentEOB {
			select {
			case x, ok := <-s.frames:
				if !ok {
					_, err := s.next()
					return err
				}
				fr, have = x, true
			default:
			}
		} else {
			if gotEOB && len(waiting) == 0 && in == nil {
				s.logf("sent %d files, received %d", sent, received)
				return nil
			}
			x, err := s.next()
			if err != nil {
				return err
			}
			fr, have = x, true
		}
		if have {
			if err := handle(fr); err != nil {
				s.cmd(mERR, err.Error())
				return err
			}
			continue
		}
		switch {
		case cur != nil:
			n, err := cur.fh.Read(buf[:min(int64(len(buf)), cur.size-cur.off)])
			if n > 0 {
				if err := s.send(frame{data: buf[:n]}); err != nil {
					return err
				}
				cur.off += int64(n)
			}
			if cur.off >= cur.size || err != nil {
				if cur.off < cur.size {
					return fmt.Errorf("%s: %v", cur.name, err)
				}
				waiting[cur.name] = cur
				cur = nil
			}
		case len(queue) > 0:
			o := queue[0]
			queue = queue[1:]
			if err := start(o, 0); err != nil {
				s.logf("%s: %v", o.name, err)
				cur = nil
			}
		case !sentEOB:
			sentEOB = true
			if err := s.cmd(mEOB, ""); err != nil {
				return err
			}
		}
	}
}

// finish keeps a file received whole and says so.
func finish(s *session, f *Fido, l *Link, inp **inFile, received *int) error {
	in := *inp
	*inp = nil
	if err := in.fh.Close(); err != nil {
		os.Remove(in.tmp)
		return err
	}
	name := safeName(in.name)
	dst := filepath.Join(f.inbound(l), name)
	for i := 1; exists(dst); i++ {
		dst = filepath.Join(f.inbound(l), fmt.Sprintf("%d-%s", i, name))
	}
	if err := os.Rename(in.tmp, dst); err != nil {
		os.Remove(in.tmp)
		return err
	}
	if in.time > 0 {
		t := time.Unix(in.time, 0)
		os.Chtimes(dst, t, t)
	}
	*received++
	return s.cmd(mGOT, fmt.Sprintf("%s %d %d", in.name, in.size, in.time))
}
