package ftn

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Node is a system in a nodelist (FTS-5000).
type Node struct {
	Addr     Addr   // zone:net/node
	Kind     string // "Zone", "Region", "Host", "Hub", "Pvt", "Hold", "Down" or "" (a node)
	Name     string // the system's
	Location string
	Sysop    string
	Phone    string // "-Unpublished-" for many
	Flags    []string
}

// Down reports whether the node takes no mail just now.
func (n *Node) Down() bool { return n.Kind == "Down" }

// Flag is a flag's value ("IBN:24555" gives "24555" for "IBN"), and
// whether the node has the flag.
func (n *Node) Flag(name string) (string, bool) {
	for _, f := range n.Flags {
		k, v, _ := strings.Cut(f, ":")
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

// Binkp is where the node answers binkp, by its IBN and INA flags; false
// if the nodelist does not say.
func (n *Node) Binkp() (string, bool) {
	if !n.hasFlag("IBN") {
		return "", false
	}
	host, port := "", "24554"
	for _, f := range n.Flags {
		k, v, _ := strings.Cut(f, ":")
		switch strings.ToUpper(k) {
		case "IBN":
			// IBN, IBN:port, IBN:host, IBN:host:port
			parts := strings.Split(v, ":")
			for _, p := range parts {
				if p == "" {
					continue
				}
				if _, err := strconv.Atoi(p); err == nil {
					port = p
				} else if host == "" {
					host = p
				}
			}
		case "INA":
			if host == "" && v != "" {
				host = v
			}
		}
	}
	if host == "" || strings.ContainsAny(host, " /@") {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

func (n *Node) hasFlag(name string) bool {
	_, ok := n.Flag(name)
	return ok
}

// Nodelist is one or more nodelists read: their nodes by address.
type Nodelist struct {
	nodes map[Addr]*Node
	zones map[int]bool
	order []*Node
}

// NewNodelist is an empty nodelist, to Read into.
func NewNodelist() *Nodelist {
	return &Nodelist{nodes: map[Addr]*Node{}, zones: map[int]bool{}}
}

// MaxNodelistLine is the longest line read; longer ones are skipped.
const MaxNodelistLine = 1024

// Read reads a nodelist (FTS-5000) into l: "Zone", "Region", "Host",
// "Hub", "Pvt", "Hold", "Down" and plain node lines, comments (;)
// skipped, underscores as spaces.  Lines it cannot read are skipped.
func (l *Nodelist) Read(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), MaxNodelistLine)
	sc.Split(scanLinesCR)
	zone, net := 0, 0
	for sc.Scan() {
		line := sc.Bytes()
		if i := bytes.IndexByte(line, 0x1A); i >= 0 {
			line = line[:i] // DOS's end of file
		}
		if len(line) == 0 || line[0] == ';' {
			continue
		}
		f := strings.Split(string(line), ",")
		if len(f) < 7 {
			continue
		}
		num, err := strconv.Atoi(strings.TrimSpace(f[1]))
		if err != nil || num < 0 || num > 65535 {
			continue
		}
		kind := strings.TrimSpace(f[0])
		var a Addr
		switch strings.ToLower(kind) {
		case "zone":
			zone, net = num, num
			a = Addr{Zone: zone, Net: num}
			kind = "Zone"
		case "region":
			net = num
			a = Addr{Zone: zone, Net: num}
			kind = "Region"
		case "host":
			net = num
			a = Addr{Zone: zone, Net: num}
			kind = "Host"
		case "hub", "pvt", "hold", "down", "":
			if zone == 0 {
				continue
			}
			a = Addr{Zone: zone, Net: net, Node: num}
			if kind != "" {
				kind = strings.ToUpper(kind[:1]) + strings.ToLower(kind[1:])
			}
		default:
			continue // Boss, Point and others: not here
		}
		text := func(s string) string {
			s = strings.ReplaceAll(strings.TrimSpace(s), "_", " ")
			return strings.Map(func(r rune) rune {
				if r < 0x20 || r == 0x7f {
					return -1
				}
				return r
			}, s)
		}
		n := &Node{Addr: a, Kind: kind, Name: text(f[2]), Location: text(f[3]), Sysop: text(f[4]), Phone: text(f[5])}
		for _, fl := range f[7:] {
			if fl = strings.TrimSpace(fl); fl != "" {
				n.Flags = append(n.Flags, fl)
			}
		}
		if _, dup := l.nodes[a]; !dup {
			l.order = append(l.order, n)
		}
		l.nodes[a] = n
		l.zones[zone] = true
	}
	return sc.Err()
}

// scanLinesCR splits at CR LF, LF or a lone CR.
func scanLinesCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, c := range data {
		if c == '\n' {
			return i + 1, bytes.TrimRight(data[:i], "\r"), nil
		}
		if c == '\r' && i+1 < len(data) && data[i+1] != '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimRight(data, "\r"), nil
	}
	return 0, nil, nil
}

// Len is how many systems the list has.
func (l *Nodelist) Len() int {
	if l == nil {
		return 0
	}
	return len(l.nodes)
}

// Covers reports whether the list has the zone: an address in a zone it
// does not have is neither found nor missing.
func (l *Nodelist) Covers(zone int) bool { return l != nil && l.zones[zone] }

// Lookup finds the system of an address (a point's: its node's).
func (l *Nodelist) Lookup(a Addr) (*Node, bool) {
	if l == nil {
		return nil, false
	}
	n, ok := l.nodes[Addr{Zone: a.Zone, Net: a.Net, Node: a.Node}]
	return n, ok
}

// Search finds systems whose name, place, sysop or address have all the
// words (any case), in the list's order, at most max.
func (l *Nodelist) Search(words string, max int) []*Node {
	if l == nil {
		return nil
	}
	ws := strings.Fields(strings.ToLower(words))
	var out []*Node
	for _, n := range l.order {
		hay := strings.ToLower(n.Name + " " + n.Location + " " + n.Sysop + " " + n.Addr.String4D())
		ok := true
		for _, w := range ws {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, n)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

// Zones are the zones the list has.
func (l *Nodelist) Zones() []int {
	if l == nil {
		return nil
	}
	var zs []int
	for z := range l.zones {
		zs = append(zs, z)
	}
	sort.Ints(zs)
	return zs
}
