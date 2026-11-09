package ymsg

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// A YMSG packet: a 20-byte header, then key/value pairs, each key (a
// decimal number) and each value ending in 0xC0 0x80.  Numbers are big
// endian.
//
//	0  "YMSG"
//	4  version (2), vendor id (2)
//	8  length of what follows the header (2)
//	10 service (2)
//	12 status (4)
//	16 session id (4)

// services
const (
	svcLogon           = 0x01
	svcLogoff          = 0x02
	svcIsAway          = 0x03
	svcIsBack          = 0x04
	svcIdle            = 0x05
	svcMessage         = 0x06
	svcUserStat        = 0x0a
	svcNewMail         = 0x0b
	svcChatInvite      = 0x0c
	svcNewContact      = 0x0f
	svcAddIgnore       = 0x11
	svcPing            = 0x12
	svcSysMessage      = 0x14
	svcConfInvite      = 0x18
	svcConfLogon       = 0x19
	svcConfDecline     = 0x1a
	svcConfLogoff      = 0x1b
	svcConfAddInvite   = 0x1c
	svcConfMsg         = 0x1d
	svcChatLogon       = 0x1e
	svcChatLogoff      = 0x1f
	svcChatMsg         = 0x20
	svcFileTransfer    = 0x46
	svcNotify          = 0x4b
	svcVerify          = 0x4c
	svcP2PFileXfer     = 0x4d
	svcPeerToPeer      = 0x4f
	svcAuthResp        = 0x54
	svcList            = 0x55
	svcAuth            = 0x57
	svcAuthBuddy       = 0x6d
	svcAddBuddy        = 0x83
	svcRemBuddy        = 0x84
	svcIgnoreContact   = 0x85
	svcRejectContact   = 0x86
	svcGroupRename     = 0x89
	svcKeepAlive       = 0x8a
	svcChatOnline      = 0x96
	svcChatGoto        = 0x97
	svcChatJoin        = 0x98
	svcChatLeave       = 0x99
	svcChatExit        = 0x9b
	svcChatAddInvite   = 0x9d
	svcChatLogout      = 0xa0
	svcChatPing        = 0xa1
	svcComment         = 0xa8
	svcVisibleToggle   = 0xc5
	svcStatusUpdate    = 0xc6
	svcAuthReq15       = 0xd6
	svcFileTrans15     = 0xdc
	svcFileTransInfo15 = 0xdd
	svcFileTransAcc15  = 0xde
	svcChangeGroup15   = 0xe7
	svcStatus15        = 0xf0
	svcList15          = 0xf1
	svcMessageAck      = 0xfb
)

// packet statuses
const (
	stOK           = 0
	stServerAck    = 1
	stNotify       = 0x16
	stOffline      = 0x5a55aa56
	stDisconnected = 0xffffffff
)

// limits
const (
	maxData = 64 << 10
)

var sep = []byte{0xc0, 0x80}

var errPacket = errors.New("ymsg: bad packet")

// pair is one key and value.
type pair struct {
	k int
	v string
}

// packet is one YMSG packet.
type packet struct {
	version uint16
	vendor  uint16
	service uint16
	status  uint32
	session uint32
	pairs   []pair
}

// get is the first value of key ("": none).
func (p *packet) get(k int) string {
	for _, x := range p.pairs {
		if x.k == k {
			return x.v
		}
	}
	return ""
}

// has: the key is there.
func (p *packet) has(k int) bool {
	for _, x := range p.pairs {
		if x.k == k {
			return true
		}
	}
	return false
}

// all are every value of key, in order.
func (p *packet) all(k int) []string {
	var out []string
	for _, x := range p.pairs {
		if x.k == k {
			out = append(out, x.v)
		}
	}
	return out
}

// add appends a pair.
func (p *packet) add(k int, v string) *packet {
	p.pairs = append(p.pairs, pair{k, v})
	return p
}

// addInt appends a pair with a number.
func (p *packet) addInt(k int, v int64) *packet { return p.add(k, strconv.FormatInt(v, 10)) }

// encode is the packet's bytes.
func (p *packet) encode() []byte {
	var data []byte
	for _, x := range p.pairs {
		data = strconv.AppendInt(data, int64(x.k), 10)
		data = append(data, sep...)
		data = append(data, x.v...)
		data = append(data, sep...)
	}
	b := make([]byte, 20, 20+len(data))
	copy(b, "YMSG")
	binary.BigEndian.PutUint16(b[4:], p.version)
	binary.BigEndian.PutUint16(b[6:], p.vendor)
	binary.BigEndian.PutUint16(b[8:], uint16(len(data)))
	binary.BigEndian.PutUint16(b[10:], p.service)
	binary.BigEndian.PutUint32(b[12:], p.status)
	binary.BigEndian.PutUint32(b[16:], p.session)
	return append(b, data...)
}

// readPacket reads one packet.
func readPacket(r io.Reader) (*packet, error) {
	var h [20]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	if string(h[:4]) != "YMSG" {
		return nil, errPacket
	}
	p := &packet{version: binary.BigEndian.Uint16(h[4:]), vendor: binary.BigEndian.Uint16(h[6:]),
		service: binary.BigEndian.Uint16(h[10:]), status: binary.BigEndian.Uint32(h[12:]),
		session: binary.BigEndian.Uint32(h[16:])}
	n := int(binary.BigEndian.Uint16(h[8:]))
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	p.pairs = decodePairs(data)
	return p, nil
}

// decodePairs splits data into its pairs (a key that is not a number is
// skipped, with its value).
func decodePairs(data []byte) []pair {
	var fields [][]byte
	for len(data) > 0 {
		i := indexSep(data)
		if i < 0 {
			fields = append(fields, data)
			break
		}
		fields = append(fields, data[:i])
		data = data[i+2:]
	}
	var out []pair
	for i := 0; i+1 < len(fields); i += 2 {
		k, err := strconv.Atoi(string(fields[i]))
		if err != nil {
			continue
		}
		out = append(out, pair{k, string(fields[i+1])})
	}
	return out
}

func indexSep(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0xc0 && b[i+1] == 0x80 {
			return i
		}
	}
	return -1
}

// conn is a client's connection.
type conn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex

	version uint16 // the client's (packets to it go in it)
	session uint32
}

func newConn(c net.Conn) *conn { return &conn{c: c, r: bufio.NewReaderSize(c, 8192)} }

// read is the client's next packet.
func (k *conn) read() (*packet, error) { return readPacket(k.r) }

// send sends a packet of a service, with a status, in the client's
// version and session.
func (k *conn) send(service uint16, status uint32, pairs ...pair) {
	p := &packet{service: service, status: status, pairs: pairs}
	k.write(p)
}

// write sends p, in the client's version and session.
func (k *conn) write(p *packet) {
	k.mu.Lock()
	defer k.mu.Unlock()
	p.version, p.session = k.version, k.session
	if p.version == 0 {
		p.version = 16
	}
	b := p.encode()
	if len(b) > 20+0xffff {
		return
	}
	k.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	k.c.Write(b)
}

func (k *conn) close() { k.c.Close() }

// kv makes a pair.
func kv(k int, v string) pair { return pair{k, v} }

// kn makes a pair with a number.
func kn(k int, v int64) pair { return pair{k, strconv.FormatInt(v, 10)} }
