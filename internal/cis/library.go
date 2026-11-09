package cis

import (
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/xfer"
)

// A forum's libraries are the file areas of its group, numbered from 0
// like its sections.  At "LIB !":
//
//	DIR [names] [/DES] [KEY:x] [AGE:n]   the files, with their descriptions
//	BRO ...                              a file at a time
//	SCA ...                              a line each
//	DOW name [/PROTO:B|QB|XMODEM|KERMIT|CAPTURE]
//	UPL name [/PROTO:...] [/TYPE:ASC|BIN]

// library is LIB n: its prompt until the caller goes back to the forum
// (or leaves it).  rest is what followed on the line.
func (s *session) library(st *forumState, num string, rest []string) (func() error, error) {
	if len(st.f.Libs) == 0 {
		s.out("This forum has no libraries.\n")
		return nil, nil
	}
	n := 0
	if num != "" {
		var err error
		if n, err = strconv.Atoi(num); err != nil || n < 0 || n >= len(st.f.Libs) {
			s.outf("There is no library %s.  Libraries are 0 to %d:\n", num, len(st.f.Libs)-1)
			s.libraries(st)
			return nil, nil
		}
	} else if len(st.f.Libs) > 1 {
		s.libraries(st)
		line, err := s.prompt("Enter library number !")
		if err != nil {
			if errors.Is(err, errAbort) {
				return nil, nil
			}
			return nil, err
		}
		if n, err = strconv.Atoi(strings.TrimSpace(line)); err != nil || n < 0 || n >= len(st.f.Libs) {
			s.out("There is no such library.\n")
			return nil, nil
		}
	}
	lib := st.f.Libs[n]
	s.node.Doing("CompuServe: " + st.f.Name + " library " + strconv.Itoa(n))
	pending := rest
	for {
		var line string
		if len(pending) > 0 {
			line, pending = strings.Join(pending, ";"), nil
		} else {
			var err error
			line, err = s.prompt("LIB !")
			if errors.Is(err, errAbort) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		back, leave, err := s.libCommands(st, lib, line)
		if err != nil || leave != nil {
			return leave, err
		}
		if back {
			return nil, nil
		}
	}
}

// libraries lists the forum's libraries.
func (s *session) libraries(st *forumState) {
	s.out("\nLibraries\n")
	for i, a := range st.f.Libs {
		s.outf("%2d %s\n", i, a.Name)
	}
}

// libCommands does a line at "LIB !": back to the forum's prompt, or out of
// the forum.
func (s *session) libCommands(st *forumState, lib *store.FileArea, line string) (bool, func() error, error) {
	cmds := strings.Split(line, ";")
	for i, c := range cmds {
		c = strings.TrimSpace(c)
		verb, arg := splitVerb(c)
		switch {
		case c == "":
		case is(verb, "DIRECTORY", 3):
			s.dir(lib, arg, false)
		case is(verb, "SCAN", 2):
			s.scanLib(lib, arg)
		case is(verb, "BROWSE", 2):
			if leave, err := s.browseLib(st, lib, arg); leave != nil || err != nil {
				return false, leave, err
			}
		case is(verb, "DOWNLOAD", 3):
			if err := s.download(lib, arg); err != nil {
				return false, nil, err
			}
		case is(verb, "UPLOAD", 3):
			if err := s.upload(lib, arg); err != nil {
				return false, nil, err
			}
		case is(verb, "LIBRARY", 3) || strings.HasPrefix(verb, "DL") || strings.HasPrefix(verb, "LIB"):
			// another library
			n := strings.TrimLeft(verb, "LIBRARYD")
			if n == "" {
				n, _, _ = strings.Cut(arg, " ")
			}
			leave, err := s.library(st, strings.TrimSpace(n), cmds[i+1:])
			return true, leave, err
		case verb == "M" || is(verb, "MENU", 4) || is(verb, "EXIT", 2) || verb == "F" || is(verb, "FORUM", 3):
			return true, nil, nil
		case is(verb, "HELP", 1) || verb == "?":
			s.out(`
Library commands:
 DIR [name] [/DES] [KEY:x] [AGE:n]   BRO ... (a file at a time)   SCA ... (a line each)
 DOW name [/PROTO:B|QB|XMODEM|KERMIT|CAPTURE]   UPL name [/PROTO:...] [/TYPE:ASC|BIN]
 LIB n (another library)   M (back to the forum)   OFF
`)
		default:
			// anything else is the forum's
			leave, err := s.forumCommands(st, strings.Join(cmds[i:], ";"))
			if leave != nil || err != nil {
				return false, leave, err
			}
			return true, nil, nil
		}
	}
	return false, nil, nil
}

// libQuery is what DIR, BRO and SCA choose.
type libQuery struct {
	names    []string // patterns, * and ?
	keys     []string
	days     int
	describe bool
	proto    string
}

func parseLibQuery(arg string) libQuery {
	var q libQuery
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ' ' || r == '/' }) {
		k, v, hasV := strings.Cut(f, ":")
		K := strings.ToUpper(k)
		switch {
		case !hasV && (is(K, "DESCRIPTION", 3) || is(K, "LONG", 2)):
			q.describe = true
		case hasV && is(K, "KEYWORDS", 3):
			q.keys = append(q.keys, strings.Split(v, ",")...)
		case hasV && (is(K, "AGE", 3) || K == "DAYS"):
			q.days, _ = strconv.Atoi(v)
		case hasV && is(K, "PROTOCOL", 3):
			q.proto = strings.ToUpper(v)
		case hasV:
		default:
			// a name, [ppn] and all: [74065,16]OASIS
			if i := strings.LastIndex(f, "]"); i >= 0 {
				f = f[i+1:]
			}
			if f != "" {
				q.names = append(q.names, strings.ToUpper(f))
			}
		}
	}
	return q
}

// libFiles are lib's files that q chooses, newest first.
func (s *session) libFiles(lib *store.FileArea, q libQuery) []*store.File {
	fs, err := s.b.Store.Files(lib.ID)
	if err != nil {
		return nil
	}
	var cut time.Time
	if q.days > 0 {
		cut = s.now().AddDate(0, 0, -q.days)
	}
	var out []*store.File
	for _, f := range fs {
		if !cut.IsZero() && f.Created.Before(cut) {
			continue
		}
		if len(q.names) > 0 {
			ok := false
			for _, n := range q.names {
				if m, _ := path.Match(n, strings.ToUpper(f.Name)); m || n == strings.ToUpper(f.Name) {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		if len(q.keys) > 0 {
			hay := strings.ToUpper(f.Name + " " + f.Description + " " + f.LongDesc)
			ok := false
			for _, k := range q.keys {
				if k = strings.ToUpper(strings.TrimSpace(k)); k != "" && strings.Contains(hay, k) {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, f)
	}
	// newest first, as CompuServe listed them
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// textFile is a file CompuServe would have called ASCII: by its extension.
func textFile(name string) bool {
	switch strings.ToUpper(path.Ext(name)) {
	case ".TXT", ".DOC", ".ASC", ".ANS", ".MSG", ".THD", ".CAP", ".LST", ".ME", ".1ST", ".HLP", ".BAS", ".C", ".H", ".PAS", ".NFO", ".DIZ":
		return true
	}
	return false
}

// entryName is a file's name as a library shows it: /binary after a
// binary file's.
func entryName(f *store.File) string {
	n := strings.ToUpper(f.Name)
	if !textFile(f.Name) {
		n += "/binary"
	}
	return n
}

// uploaderPPN is the uploader's User ID, in brackets.
func uploaderPPN(f *store.File) string {
	if f.UploaderID == 0 {
		return "[" + f.Uploader + "]"
	}
	return "[" + PPN(f.UploaderID) + "]"
}

// keywords is a file's keywords, kept in its long description's first
// line ("Keywords: ...") by an upload here.
func keywords(f *store.File) (string, string) {
	d := f.LongDesc
	if k, ok := strings.CutPrefix(d, "Keywords: "); ok {
		k, rest, _ := strings.Cut(k, "\n")
		return k, rest
	}
	return "", d
}

// entry is a file as DIR shows it:
//
//	[74065,16]
//	OASIS/binary              03-Oct-87 73856              Accesses: 160
//
//	    Title   : ...
//	    Keywords: OASIS FINDER SHAREWARE
//
//	    Here it is, the latest version of Oasis...
func (s *session) entry(f *store.File, describe bool) {
	s.out("\n" + uploaderPPN(f) + "\n")
	s.outf("%-25s %s %-12d Accesses: %d\n", entryName(f), f.Created.Format("02-Jan-06"), f.Size, f.Downloads)
	if !describe {
		return
	}
	k, rest := keywords(f)
	s.out("\n")
	if f.Description != "" {
		s.out("    Title   : " + f.Description + "\n")
	}
	if k != "" {
		s.out("    Keywords: " + k + "\n")
	}
	if rest = strings.TrimSpace(rest); rest != "" {
		s.out("\n")
		for _, l := range strings.Split(rest, "\n") {
			s.out("    " + strings.TrimRight(l, "\r") + "\n")
		}
	}
}

// dir is DIR.
func (s *session) dir(lib *store.FileArea, arg string, _ bool) {
	q := parseLibQuery(arg)
	fs := s.libFiles(lib, q)
	if len(fs) == 0 {
		s.out("No files found\n")
		return
	}
	for _, f := range fs {
		s.entry(f, q.describe || len(q.names) > 0)
	}
}

// scanLib is SCAN: a line or two a file.
//
//	GUILIB.LHA/Bin  Bytes: 175965
//	  Title   : ...
func (s *session) scanLib(lib *store.FileArea, arg string) {
	q := parseLibQuery(arg)
	fs := s.libFiles(lib, q)
	if len(fs) == 0 {
		s.out("No files found\n")
		return
	}
	for _, f := range fs {
		name := strings.ToUpper(f.Name)
		if !textFile(f.Name) {
			name += "/Bin"
		}
		s.outf("%s  Bytes: %d\n", name, f.Size)
		if f.Description != "" {
			s.out("  Title   : " + f.Description + "\n")
		}
		if q.describe {
			if k, _ := keywords(f); k != "" {
				s.out("  Keywords: " + k + "\n")
			}
		}
	}
}

// browseLib is BROWSE: each file described, then a choice.
func (s *session) browseLib(st *forumState, lib *store.FileArea, arg string) (func() error, error) {
	q := parseLibQuery(arg)
	fs := s.libFiles(lib, q)
	if len(fs) == 0 {
		s.out("No files found\n")
		return nil, nil
	}
	for _, f := range fs {
		s.entry(f, true)
		for {
			line, err := s.prompt("\nEnter command, N for next file\nor <CR> for disposition menu !")
			if errors.Is(err, errAbort) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			v, a := splitVerb(strings.TrimSpace(line))
			switch {
			case v == "N" || is(v, "NEXT", 3):
			case v == "":
				s.out(" 1 (DOW) Download this file\n 2 (N)   Next file\n 3 (M)   Back to the library\n")
				c, err := s.prompt("Enter choice !")
				if err != nil {
					return nil, s.abortOK0(err)
				}
				switch strings.ToUpper(strings.TrimSpace(c)) {
				case "1", "DOW":
					if err := s.download(lib, f.Name+" "+a); err != nil {
						return nil, err
					}
				case "2", "N", "":
				default:
					return nil, nil
				}
			case is(v, "DOWNLOAD", 3):
				if err := s.download(lib, f.Name+" "+a); err != nil {
					return nil, err
				}
			case is(v, "READ", 3) || is(v, "DESCRIPTION", 3):
				s.entry(f, true)
				continue
			default:
				return nil, nil
			}
			break
		}
	}
	return nil, nil
}

func (s *session) abortOK0(err error) error {
	if errors.Is(err, errAbort) {
		return nil
	}
	return err
}

// the download protocols, as CompuServe's menu numbered them
var protocols = []struct {
	num, key, name string
}{
	{"1", "XMODEM", "XMODEM (MODEM7) protocol"},
	{"2", "B", "CompuServe 'B' protocol"},
	{"4", "CAPTURE", "DC2/DC4 CAPTURE protocol"},
	{"5", "KERMIT", "Kermit"},
	{"6", "QB", "CompuServe Quick 'B' protocol"},
}

// chooseProtocol asks which protocol (or takes /PROTO:); "" to abort.
func (s *session) chooseProtocol(given string) (string, error) {
	switch given {
	case "B", "CISB", "BPLUS", "B+":
		return "B", nil
	case "QB", "QUICKB":
		return "QB", nil
	case "X", "XMO", "XMODEM", "XMODEM1K":
		return "XMODEM", nil
	case "K", "KER", "KERMIT":
		return "KERMIT", nil
	case "CAP", "CAPTURE", "ASCII", "DC2":
		return "CAPTURE", nil
	}
	if given == "" {
		// the permanent protocol preference, if it is one of these
		if k, ok := map[string]string{"b": "B", "qb": "QB", "xmodem": "XMODEM", "kermit": "KERMIT"}[s.keptSetting(setProtocol)]; ok {
			return k, nil
		}
	}
	for {
		for _, p := range protocols {
			s.outf(" %s %s\n", p.num, p.name)
		}
		s.out(" 0 Abort transfer request\n")
		c, err := s.prompt("Enter choice !")
		if err != nil {
			return "", s.abortOK0(err)
		}
		c = strings.TrimSpace(c)
		if c == "0" || c == "" {
			return "", nil
		}
		for _, p := range protocols {
			if c == p.num {
				return p.key, nil
			}
		}
	}
}

// port is the caller's line for a transfer protocol: reading through the
// session's buffer, so nothing read ahead is lost.
type port struct{ s *session }

func (p port) Read(b []byte) (int, error) {
	if len(p.s.pushed) > 0 {
		n := copy(b, p.s.pushed)
		p.s.pushed = p.s.pushed[n:]
		return n, nil
	}
	if p.s.r.Buffered() > 0 {
		return p.s.r.Read(b[:min(len(b), p.s.r.Buffered())])
	}
	return p.s.r.Read(b)
}

func (p port) Write(b []byte) (int, error)       { return p.s.line.Write(b) }
func (p port) SetReadDeadline(t time.Time) error { return p.s.line.SetReadDeadline(t) }

// waitCR is "press <CR> when the transfer is complete": what comes before
// the CR (a protocol's leftovers) is dropped.
func (s *session) waitCR() {
	deadline := 2 * time.Minute
	for {
		c, err := s.getc(deadline)
		if err != nil || c == '\r' {
			s.cr = true
			return
		}
	}
}

// download is DOW name [/PROTO:x]: the file by the protocol.
func (s *session) download(lib *store.FileArea, arg string) error {
	q := parseLibQuery(arg)
	if len(q.names) == 0 {
		line, err := s.prompt("Filename: ")
		if err != nil {
			return s.abortOK0(err)
		}
		q.names = parseLibQuery(line).names
		if len(q.names) == 0 {
			return nil
		}
	}
	f, err := s.b.Store.FileByName(lib.ID, q.names[0])
	if err != nil {
		// names are kept as given; CompuServe's are capitals
		f = nil
		fs, _ := s.b.Store.Files(lib.ID)
		for _, c := range fs {
			if strings.EqualFold(c.Name, q.names[0]) {
				f = c
			}
		}
	}
	if f == nil {
		s.outf("%%File %s not found\n", q.names[0])
		return nil
	}
	if err := s.b.MayDownload(s.user, lib); err != nil {
		s.out("You may not download this file: " + err.Error() + ".\n")
		return nil
	}
	proto, err := s.chooseProtocol(q.proto)
	if err != nil || proto == "" {
		return err
	}
	in, err := s.b.Files.OpenFile(f)
	if err != nil {
		s.out("The file cannot be read now.\n")
		s.b.Log.Printf("node %d: CompuServe: %s: %v", s.node.Num, f.Name, err)
		return nil
	}
	defer in.Close()
	s.node.Doing("CompuServe: downloading " + f.Name)
	if err := s.send(proto, xfer.Outgoing{Name: f.Name, Size: f.Size, ModTime: f.Created, Data: in}, !textFile(f.Name)); err != nil {
		return nil
	}
	s.b.Downloaded(s.user, f)
	s.b.Log.Printf("node %d: %s downloaded %q by %s (%d bytes)", s.node.Num, s.user.Name, f.Name, proto, f.Size)
	return nil
}

// send sends a file by a protocol of chooseProtocol's, and says how it
// went: an error is the transfer's, already told and logged.
func (s *session) send(proto string, o xfer.Outgoing, binary bool) error {
	p := port{s}
	var err error
	switch proto {
	case "B", "QB":
		k := xfer.NewBLink(p)
		if err = k.Start(xfer.HostParams); err == nil {
			_, err = k.SendFile(strings.ToUpper(o.Name), binary, o.Data)
		}
		if errors.Is(err, xfer.ErrTimeout) && !k.Plus {
			s.out("\nYour terminal does not support the B protocol.\n")
		}
	case "XMODEM":
		s.out("Starting XMODEM transfer\n\nPlease initiate XMODEM transfer\nand press <CR> when the transfer\nis complete.\n")
		err = xfer.SendXMODEM(p, o, false)
		s.waitCR()
	case "KERMIT":
		s.out("Starting KERMIT transfer\n\nPlease initiate KERMIT transfer\nand press <CR> when the transfer\nis complete.\n")
		err = xfer.SendKermit(p, []xfer.Outgoing{o})
		s.waitCR()
	case "CAPTURE":
		if binary {
			s.out("Only ASCII files can be captured.\n")
			return errors.New("binary capture")
		}
		s.line.Write([]byte{0x12}) // DC2: capture on
		var b []byte
		if b, err = io.ReadAll(io.LimitReader(o.Data, 4<<20)); err == nil {
			s.out(strings.ReplaceAll(string(b), "\r\n", "\n"))
		}
		s.line.Write([]byte{0x14}) // DC4: capture off
		s.out("\n")
	}
	if err != nil {
		s.b.Log.Printf("node %d: CompuServe: %s download of %s failed: %v", s.node.Num, proto, o.Name, err)
		s.out("\nTransfer aborted.\n")
		return err
	}
	if proto != "B" && proto != "QB" {
		s.out("Transfer complete.\n")
	}
	return nil
}

// incoming is an upload for a protocol.
type incoming struct {
	w      io.Writer
	commit func() error
	abort  func()
}

func (in *incoming) Write(p []byte) (int, error) { return in.w.Write(p) }
func (in *incoming) Commit() error               { return in.commit() }
func (in *incoming) Abort()                      { in.abort() }

// upload is UPL name [/PROTO:x] [/TYPE:ASC|BIN]: the file, then its
// title, keywords and description.
func (s *session) upload(lib *store.FileArea, arg string) error {
	if !s.b.CanUpload(s.user, lib) {
		s.out("Uploads are not allowed in this library.\n")
		return nil
	}
	q := parseLibQuery(arg)
	binary := true
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ' ' || r == '/' }) {
		if k, v, ok := strings.Cut(f, ":"); ok && is(strings.ToUpper(k), "TYPE", 3) {
			binary = !strings.HasPrefix(strings.ToUpper(v), "A")
		}
	}
	if len(q.names) == 0 {
		line, err := s.prompt("Filename: ")
		if err != nil {
			return s.abortOK0(err)
		}
		q.names = parseLibQuery(line).names
		if len(q.names) == 0 {
			return nil
		}
	}
	name := q.names[0]
	proto, err := s.chooseProtocol(q.proto)
	if err != nil || proto == "" {
		return err
	}
	if proto == "CAPTURE" {
		s.out("Uploads are by XMODEM, Kermit or the B protocol.\n")
		return nil
	}
	up, err := s.b.Files.Upload(s.user, lib, name, "")
	if err != nil {
		s.out("The file cannot be uploaded: " + err.Error() + ".\n")
		return nil
	}
	var got *store.File
	in := &incoming{w: up, abort: up.Abort, commit: func() error {
		f, err := up.Commit()
		got = f
		return err
	}}
	p := port{s}
	s.node.Doing("CompuServe: uploading " + name)
	switch proto {
	case "B", "QB":
		k := xfer.NewBLink(p)
		if err = k.Start(xfer.HostParams); err == nil {
			_, err = k.ReceiveFile(name, binary, in, 0)
		}
		if err == nil {
			err = in.Commit()
		} else {
			in.Abort()
		}
	case "XMODEM", "KERMIT":
		s.outf("Starting %s transfer\n\nPlease initiate %s transfer\nand press <CR> when the transfer\nis complete.\n", proto, proto)
		sink := func(string, int64) (xfer.Incoming, error) { return in, nil }
		if proto == "XMODEM" {
			err = xfer.ReceiveXMODEM(p, sink)
		} else {
			err = xfer.ReceiveKermit(p, sink)
		}
		if got == nil && err == nil {
			err = errors.New("nothing came")
		}
		if err != nil && got == nil {
			in.Abort()
		}
		s.waitCR()
	}
	if err != nil || got == nil {
		if err == nil {
			err = errors.New("nothing came")
		}
		s.b.Log.Printf("node %d: CompuServe: %s upload of %s failed: %v", s.node.Num, proto, name, err)
		s.out("\nTransfer aborted.\n")
		return nil
	}
	s.b.Log.Printf("node %d: %s uploaded %q by %s (%d bytes)", s.node.Num, s.user.Name, got.Name, proto, got.Size)
	s.out("\n")
	for {
		title, err := s.prompt("Title: ")
		if err != nil {
			return s.abortOK0(err)
		}
		keys, err := s.prompt("Keywords: ")
		if err != nil {
			return s.abortOK0(err)
		}
		s.out("Please enter description (465 characters maximum)\n")
		desc, _, err := s.edit()
		if err != nil {
			return s.abortOK0(err)
		}
		d := strings.Join(desc, "\n")
		if len(d) > 465 {
			d = d[:465]
		}
		s.outf("\n%s\nTitle   : %s\nKeywords: %s\n\n%s\n", entryName(got), title, strings.ToUpper(keys), d)
		ok, err := s.prompt("Is this okay (Y or N)? ")
		if err != nil {
			return s.abortOK0(err)
		}
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ok)), "Y") {
			continue
		}
		s.b.Store.SetFileDescription(got.ID, strings.TrimSpace(title))
		long := d
		if k := strings.ToUpper(strings.TrimSpace(keys)); k != "" {
			long = "Keywords: " + k + "\n" + d
		}
		s.b.Store.SetFileLongDesc(got.ID, long)
		s.outf("File %s stored in library\n", strings.ToUpper(got.Name))
		return nil
	}
}
