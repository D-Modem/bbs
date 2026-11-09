package msn

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// limits
const (
	maxLine    = 2048     // a command line's bytes
	maxPayload = 64 << 10 // a payload's (MSG, QRY)
)

// cmd is a command a client sent: its name, transaction id and arguments,
// and its payload, for those with one.
type cmd struct {
	name    string
	trid    string
	args    []string
	payload []byte
}

// arg is the i'th argument ("": none).
func (c *cmd) arg(i int) string {
	if i < len(c.args) {
		return c.args[i]
	}
	return ""
}

// payloadCmds are those a client sends with a payload, its length last.
var payloadCmds = map[string]bool{"MSG": true, "QRY": true, "UUX": true, "ADL": true, "RML": true,
	"UUN": true, "UUM": true, "PAG": true, "SDC": true, "NOT": true, "FQY": true}

var errLine = errors.New("msn: bad command line")

// conn is a client's connection: commands in, lines out.
type conn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex // writes
}

func newConn(c net.Conn, r *bufio.Reader) *conn { return &conn{c: c, r: r} }

// read is the next command.
func (k *conn) read() (*cmd, error) {
	line, err := k.r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxLine {
		return nil, errLine
	}
	if err != nil {
		return nil, err
	}
	f := strings.Fields(strings.TrimRight(string(line), "\r\n"))
	if len(f) == 0 {
		return &cmd{}, nil
	}
	c := &cmd{name: strings.ToUpper(f[0])}
	if len(f) > 1 {
		c.trid = f[1]
		c.args = f[2:]
	}
	if payloadCmds[c.name] && len(f) >= 3 {
		n, err := strconv.Atoi(f[len(f)-1])
		if err != nil || n < 0 || n > maxPayload {
			return nil, errLine
		}
		c.payload = make([]byte, n)
		if _, err := readFull(k.r, c.payload); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// send writes a line (its words joined by spaces).
func (k *conn) send(words ...string) error {
	return k.write([]byte(strings.Join(words, " ") + "\r\n"))
}

// sendPayload writes a line ending in the payload's length, and the
// payload.
func (k *conn) sendPayload(payload []byte, words ...string) error {
	line := strings.Join(append(words, strconv.Itoa(len(payload))), " ") + "\r\n"
	return k.write(append([]byte(line), payload...))
}

// sendErr writes an error's number for a transaction.
func (k *conn) sendErr(code int, trid string) error { return k.send(strconv.Itoa(code), trid) }

func (k *conn) write(b []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err := k.c.Write(b)
	return err
}

func (k *conn) close() { k.c.Close() }

// error numbers
const (
	errSyntax       = 200
	errBadParam     = 201
	errNoSuchUser   = 205
	errNoDomain     = 206
	errBadHandle    = 208
	errBadFriendly  = 209
	errListFull     = 210
	errAlreadyThere = 215
	errNotOnList    = 216
	errNotOnline    = 217
	errInOpposite   = 219
	errBadGroup     = 224
	errNotInGroup   = 225
	errGroupName    = 228
	errNoSB         = 280
	errNotLoggedIn  = 302
	errInternal     = 500
	errDisabled     = 502
	errBusy         = 600
	errRate         = 800
	errAuth         = 911
	errNotAllowed   = 913
)

// encode is a name as MSNP carries it: URL encoded (a space %20, every
// byte that is not printable ASCII, and %).
func encode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= ' ' || c >= 0x7f || c == '%' {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// decode is a URL-encoded name in the clear (as it was, if it is not
// well encoded).
func decode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b = append(b, byte(v))
				i += 2
				continue
			}
		}
		b = append(b, s[i])
	}
	return string(b)
}

// mime is a MSG payload: its headers, by lower-case name, and its body.
type mime struct {
	headers map[string]string
	body    string
}

// parseMIME reads a MSG payload.
func parseMIME(p []byte) mime {
	m := mime{headers: map[string]string{}}
	s := string(p)
	head, body, ok := strings.Cut(s, "\r\n\r\n")
	if !ok {
		head, body, _ = strings.Cut(s, "\n\n")
	}
	m.body = body
	for _, l := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(strings.TrimRight(l, "\r"), ":")
		if ok {
			m.headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return m
}

// contentType is the payload's media type, lower case, without its
// parameters.
func (m mime) contentType() string {
	t, _, _ := strings.Cut(m.headers["content-type"], ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// textPayload is a plain text message's payload.
func textPayload(text string) []byte {
	return []byte("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n" +
		"X-MMS-IM-Format: FN=MS%20Sans%20Serif; EF=; CO=0; CS=0; PF=0\r\n\r\n" + text)
}
