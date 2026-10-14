// Package geoip finds the country of an address, from a database the
// sysop has: a MaxMind DB file (.mmdb: MaxMind's GeoLite2 and GeoIP2
// Country and City, DB-IP's IP to Country Lite, IPinfo's and others in its
// format), or a CSV of ranges (DB-IP's "start,end,CC", IP2Location LITE's
// "from","to","CC","Country", by addresses or by their numbers), plain
// or gzipped.  It reads them itself: nothing is fetched, nothing else is
// needed.
package geoip

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"os"
	"sort"
	"strings"
)

// Reader is a country database.
type Reader struct {
	mmdb   *mmdb
	ranges []span // a CSV's, by start
}

// span is a CSV's range of addresses (as 16 bytes) and its country.
type span struct {
	start, end [16]byte
	code, name string
}

// maxFile is the largest database read (a City database is some 70 MB).
const maxFile = 512 << 20

// Open reads a database file.
func Open(path string) (*Reader, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxFile {
		return nil, fmt.Errorf("%s: bigger than %d MB", path, maxFile>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if data, err = io.ReadAll(io.LimitReader(zr, maxFile+1)); err != nil {
			return nil, err
		}
		if len(data) > maxFile {
			return nil, fmt.Errorf("%s: bigger than %d MB unpacked", path, maxFile>>20)
		}
	}
	if bytes.Contains(data[max(0, len(data)-128<<10):], metaMarker) {
		m, err := openMMDB(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return &Reader{mmdb: m}, nil
	}
	ranges, err := readCSV(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Reader{ranges: ranges}, nil
}

// Country is an address's country: its ISO 3166 code (upper case) and
// name in English; ok false if the database does not say.
func (r *Reader) Country(ip net.IP) (code, name string, ok bool) {
	if r == nil || ip == nil {
		return "", "", false
	}
	if r.mmdb != nil {
		v, err := r.mmdb.lookup(ip)
		if err != nil || v == nil {
			return "", "", false
		}
		code, name = countryOf(v)
	} else {
		code, name = r.lookupCSV(ip)
	}
	code = strings.ToUpper(code)
	if unknown[code] || len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return "", "", false
	}
	if name == "" {
		name = Names[code]
	}
	if name == "" {
		name = code
	}
	return code, name, true
}

// unknown are the codes that say no country: ZZ (DB-IP's reserved and
// unknown ranges), XX, and --.
var unknown = map[string]bool{"ZZ": true, "XX": true, "--": true}

// countryOf is a record's country (or, without one, its registered
// country's): GeoIP2's and its likes' shape.
func countryOf(v any) (string, string) {
	rec, _ := v.(map[string]any)
	for _, key := range []string{"country", "registered_country"} {
		c, _ := rec[key].(map[string]any)
		code, _ := c["iso_code"].(string)
		if code == "" {
			continue
		}
		names, _ := c["names"].(map[string]any)
		name, _ := names["en"].(string)
		return code, name
	}
	// flat ones: {"country_code": "NZ", "country_name": "..."} (IPinfo's)
	for _, key := range []string{"country_code", "country", "iso_code"} {
		if code, _ := rec[key].(string); code != "" {
			name, _ := rec["country_name"].(string)
			return code, name
		}
	}
	return "", ""
}

// Flag is a country's flag as an emoji: its code's two regional
// indicator symbols ("" for no code).
func Flag(code string) string {
	if len(code) != 2 {
		return ""
	}
	code = strings.ToUpper(code)
	return string([]rune{0x1F1E6 + rune(code[0]-'A'), 0x1F1E6 + rune(code[1]-'A')})
}

// ---- MaxMind DB ----

var metaMarker = []byte("\xab\xcd\xefMaxMind.com")

type mmdb struct {
	tree       []byte
	data       []byte
	nodeCount  uint
	recordSize uint
	ipVersion  uint
	ipv4Start  uint // the node the IPv4 addresses begin at, in an IPv6 tree
}

func openMMDB(b []byte) (*mmdb, error) {
	at := bytes.LastIndex(b, metaMarker)
	if at < 0 {
		return nil, errors.New("not a MaxMind DB")
	}
	d := decoder{buf: b[at+len(metaMarker):]}
	mv, _, err := d.decode(0, 0)
	if err != nil {
		return nil, fmt.Errorf("its metadata: %w", err)
	}
	meta, ok := mv.(map[string]any)
	if !ok {
		return nil, errors.New("its metadata is not a map")
	}
	num := func(k string) uint {
		v, _ := meta[k].(uint64)
		return uint(v)
	}
	m := &mmdb{nodeCount: num("node_count"), recordSize: num("record_size"), ipVersion: num("ip_version")}
	if m.recordSize != 24 && m.recordSize != 28 && m.recordSize != 32 {
		return nil, fmt.Errorf("a record size of %d", m.recordSize)
	}
	if m.ipVersion != 4 && m.ipVersion != 6 {
		return nil, fmt.Errorf("IP version %d", m.ipVersion)
	}
	treeSize := m.nodeCount * m.recordSize / 4
	if treeSize+16 > uint(at) || m.nodeCount == 0 {
		return nil, errors.New("its search tree is bigger than it is")
	}
	m.tree = b[:treeSize]
	m.data = b[treeSize+16 : at]
	if m.ipVersion == 6 {
		// IPv4 is ::a.b.c.d: 96 zero bits in
		node := uint(0)
		for i := 0; i < 96 && node < m.nodeCount; i++ {
			node = m.record(node, 0)
		}
		m.ipv4Start = node
	}
	return m, nil
}

// record is a node's left (0) or right (1) record.
func (m *mmdb) record(node, bit uint) uint {
	switch m.recordSize {
	case 24:
		o := node*6 + bit*3
		b := m.tree[o : o+3]
		return uint(b[0])<<16 | uint(b[1])<<8 | uint(b[2])
	case 28:
		o := node * 7
		b := m.tree[o : o+7]
		if bit == 0 {
			return uint(b[3]&0xf0)<<20 | uint(b[0])<<16 | uint(b[1])<<8 | uint(b[2])
		}
		return uint(b[3]&0x0f)<<24 | uint(b[4])<<16 | uint(b[5])<<8 | uint(b[6])
	default:
		o := node*8 + bit*4
		return uint(binary.BigEndian.Uint32(m.tree[o : o+4]))
	}
}

func (m *mmdb) lookup(ip net.IP) (any, error) {
	var bits []byte
	node := uint(0)
	if v4 := ip.To4(); v4 != nil {
		bits = v4
		if m.ipVersion == 6 {
			node = m.ipv4Start
		}
	} else if m.ipVersion == 4 {
		return nil, nil // an IPv4 database: no IPv6 in it
	} else {
		bits = ip.To16()
	}
	for i := 0; i < len(bits)*8 && node < m.nodeCount; i++ {
		node = m.record(node, uint(bits[i/8]>>(7-i%8)&1))
	}
	switch {
	case node == m.nodeCount:
		return nil, nil // not there
	case node < m.nodeCount:
		return nil, errors.New("the search tree ends in a node")
	}
	off := node - m.nodeCount - 16
	if off >= uint(len(m.data)) {
		return nil, errors.New("a record past the data")
	}
	d := decoder{buf: m.data}
	v, _, err := d.decode(off, 0)
	return v, err
}

// decoder reads the MaxMind DB data section's values.
type decoder struct{ buf []byte }

var errData = errors.New("the data is broken")

const maxDepth = 32

// decode reads the value at off: it, and where the next begins.
func (d decoder) decode(off uint, depth int) (any, uint, error) {
	if depth > maxDepth || off >= uint(len(d.buf)) {
		return nil, 0, errData
	}
	ctrl := d.buf[off]
	off++
	typ := uint(ctrl >> 5)
	if typ == 1 { // a pointer
		ss, vvv := uint(ctrl>>3&3), uint(ctrl&7)
		n := ss + 1
		if off+n > uint(len(d.buf)) {
			return nil, 0, errData
		}
		var p uint
		switch ss {
		case 0:
			p = vvv<<8 | uint(d.buf[off])
		case 1:
			p = (vvv<<16 | uint(d.buf[off])<<8 | uint(d.buf[off+1])) + 2048
		case 2:
			p = (vvv<<24 | uint(d.buf[off])<<16 | uint(d.buf[off+1])<<8 | uint(d.buf[off+2])) + 526336
		default:
			p = uint(binary.BigEndian.Uint32(d.buf[off : off+4]))
		}
		v, _, err := d.decode(p, depth+1)
		return v, off + n, err
	}
	if typ == 0 { // extended
		if off >= uint(len(d.buf)) {
			return nil, 0, errData
		}
		typ = 7 + uint(d.buf[off])
		off++
	}
	size := uint(ctrl & 0x1f)
	if size >= 29 {
		n := size - 28
		if off+n > uint(len(d.buf)) {
			return nil, 0, errData
		}
		switch size {
		case 29:
			size = 29 + uint(d.buf[off])
		case 30:
			size = 285 + (uint(d.buf[off])<<8 | uint(d.buf[off+1]))
		default:
			size = 65821 + (uint(d.buf[off])<<16 | uint(d.buf[off+1])<<8 | uint(d.buf[off+2]))
		}
		off += n
	}
	bytesOf := func() ([]byte, error) {
		if off+size > uint(len(d.buf)) {
			return nil, errData
		}
		return d.buf[off : off+size], nil
	}
	switch typ {
	case 2, 4: // a string, bytes
		b, err := bytesOf()
		if err != nil {
			return nil, 0, err
		}
		if typ == 2 {
			return string(b), off + size, nil
		}
		return append([]byte(nil), b...), off + size, nil
	case 3, 15: // a double, a float
		b, err := bytesOf()
		if err != nil || (typ == 3 && size != 8) || (typ == 15 && size != 4) {
			return nil, 0, errData
		}
		if typ == 3 {
			return math.Float64frombits(binary.BigEndian.Uint64(b)), off + size, nil
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), off + size, nil
	case 5, 6, 8, 9, 10: // unsigned (and signed 32) integers
		b, err := bytesOf()
		if err != nil || size > 16 {
			return nil, 0, errData
		}
		var v uint64
		for _, c := range b {
			v = v<<8 | uint64(c)
		}
		return v, off + size, nil
	case 7: // a map
		m := make(map[string]any, min(size, 64))
		for i := uint(0); i < size; i++ {
			k, next, err := d.decode(off, depth+1)
			if err != nil {
				return nil, 0, err
			}
			ks, ok := k.(string)
			if !ok {
				return nil, 0, errData
			}
			v, next2, err := d.decode(next, depth+1)
			if err != nil {
				return nil, 0, err
			}
			m[ks] = v
			off = next2
		}
		return m, off, nil
	case 11: // an array
		a := make([]any, 0, min(size, 64))
		for i := uint(0); i < size; i++ {
			v, next, err := d.decode(off, depth+1)
			if err != nil {
				return nil, 0, err
			}
			a = append(a, v)
			off = next
		}
		return a, off, nil
	case 14: // a boolean, in its size
		return size != 0, off, nil
	}
	return nil, 0, fmt.Errorf("a value of type %d", typ)
}

// embedded is the IPv4 address an IPv6 one carries, where it is one that
// does (6to4, 2002::/16; Teredo, 2001::/32, the client's inverted), as a
// MaxMind DB's tree has them; else the address as it is.
func embedded(ip net.IP) net.IP {
	if ip.To4() != nil {
		return ip
	}
	b := ip.To16()
	switch {
	case b[0] == 0x20 && b[1] == 0x02:
		return net.IPv4(b[2], b[3], b[4], b[5])
	case b[0] == 0x20 && b[1] == 0x01 && b[2] == 0 && b[3] == 0:
		return net.IPv4(^b[12], ^b[13], ^b[14], ^b[15])
	}
	return ip
}

// ---- CSV ----

// readCSV reads ranges: "start,end,CC[,name...]", by addresses or by
// their numbers, quoted or not; lines that are not are passed over.
func readCSV(b []byte) ([]span, error) {
	var out []span
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ",")
		if len(f) < 3 {
			continue
		}
		for i := range f {
			f[i] = strings.Trim(strings.TrimSpace(f[i]), `"`)
		}
		start, ok1 := address(f[0])
		end, ok2 := address(f[1])
		if !ok1 || !ok2 || len(f[2]) != 2 || f[2] == "-" {
			continue
		}
		sp := span{start: start, end: end, code: strings.ToUpper(f[2])}
		if len(f) >= 4 && f[3] != "" && f[3] != "-" {
			sp.name = strings.Join(f[3:], ",")
		}
		out = append(out, sp)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("not a MaxMind DB, nor a CSV of ranges (start,end,country)")
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].start[:], out[j].start[:]) < 0 })
	return out, nil
}

// address is an address, or an address's number (IPv4 under 2^32, else
// IPv6), as 16 bytes.
func address(s string) ([16]byte, bool) {
	var a [16]byte
	if ip := net.ParseIP(s); ip != nil {
		copy(a[:], ip.To16())
		return a, true
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 128 {
		return a, false
	}
	if n.BitLen() <= 32 && !strings.HasPrefix(s, "00") {
		ip := net.IPv4(byte(n.Uint64()>>24), byte(n.Uint64()>>16), byte(n.Uint64()>>8), byte(n.Uint64()))
		copy(a[:], ip.To16())
		return a, true
	}
	n.FillBytes(a[:])
	return a, true
}

func (r *Reader) lookupCSV(ip net.IP) (string, string) {
	ip = embedded(ip)
	var a [16]byte
	copy(a[:], ip.To16())
	i := sort.Search(len(r.ranges), func(i int) bool { return bytes.Compare(r.ranges[i].start[:], a[:]) > 0 })
	if i == 0 {
		return "", ""
	}
	sp := r.ranges[i-1]
	if bytes.Compare(a[:], sp.end[:]) > 0 {
		return "", ""
	}
	return sp.code, sp.name
}
