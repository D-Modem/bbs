// Package ftn is FidoNet's formats: addresses, Type 2+ packets
// (FTS-0001, FSC-0039), the packed messages in them and the kludge
// lines, SEEN-BY and PATH of echomail (FTS-0004, FTS-4000 and friends).
package ftn

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Addr is an FTN address: zone:net/node.point@domain.
type Addr struct {
	Zone, Net, Node, Point int
	Domain                 string // "" if not said
}

// ErrAddr: not an FTN address.
var ErrAddr = errors.New("not an FTN address (zone:net/node[.point])")

// ParseAddr reads zone:net/node[.point][@domain].
func ParseAddr(s string) (Addr, error) {
	var a Addr
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '@'); i >= 0 {
		a.Domain = strings.ToLower(s[i+1:])
		s = s[:i]
		if a.Domain == "" || len(a.Domain) > 20 || strings.ContainsAny(a.Domain, " :/.@") {
			return Addr{}, ErrAddr
		}
	}
	z, rest, ok := strings.Cut(s, ":")
	if !ok {
		return Addr{}, ErrAddr
	}
	n, rest, ok := strings.Cut(rest, "/")
	if !ok {
		return Addr{}, ErrAddr
	}
	f, p, hasPoint := strings.Cut(rest, ".")
	var err error
	num := func(s string, max int) int {
		if err != nil {
			return 0
		}
		v, e := strconv.Atoi(s)
		if e != nil || v < 0 || v > max || s == "" || s[0] == '+' {
			err = ErrAddr
		}
		return v
	}
	a.Zone, a.Net, a.Node = num(z, 32767), num(n, 32767), num(f, 32767)
	if hasPoint {
		a.Point = num(p, 32767)
	}
	if err != nil || a.Zone == 0 {
		return Addr{}, ErrAddr
	}
	return a, nil
}

// String is the 4D address (5D with the domain).
func (a Addr) String() string {
	s := a.String4D()
	if a.Domain != "" {
		s += "@" + a.Domain
	}
	return s
}

// String4D is zone:net/node, with .point for a point.
func (a Addr) String4D() string {
	s := fmt.Sprintf("%d:%d/%d", a.Zone, a.Net, a.Node)
	if a.Point != 0 {
		s += fmt.Sprintf(".%d", a.Point)
	}
	return s
}

// String3D is zone:net/node.
func (a Addr) String3D() string { return fmt.Sprintf("%d:%d/%d", a.Zone, a.Net, a.Node) }

// String2D is net/node, as in SEEN-BY and PATH.
func (a Addr) String2D() string { return fmt.Sprintf("%d/%d", a.Net, a.Node) }

// Same compares addresses without their domains.
func (a Addr) Same(b Addr) bool {
	return a.Zone == b.Zone && a.Net == b.Net && a.Node == b.Node && a.Point == b.Point
}

// Boss is the node a point belongs to (a node itself).
func (a Addr) Boss() Addr { a.Point = 0; return a }

// Net2D is a net/node pair, as SEEN-BY and PATH have them.
type Net2D struct{ Net, Node int }

// ParseNet2D reads a SEEN-BY or PATH line's addresses: "5020/1 2 3
// 5030/4" (a bare number is a node of the net before).  Points (".5")
// are dropped; what does not read is skipped.
func ParseNet2D(s string) []Net2D {
	var out []Net2D
	net := -1
	for _, f := range strings.Fields(s) {
		if i := strings.IndexByte(f, '.'); i >= 0 {
			f = f[:i]
		}
		if n, nd, ok := strings.Cut(f, "/"); ok {
			v, err := strconv.Atoi(n)
			if err != nil || v < 0 || v > 32767 {
				continue
			}
			net = v
			f = nd
		}
		v, err := strconv.Atoi(f)
		if err != nil || v < 0 || v > 32767 || net < 0 {
			continue
		}
		out = append(out, Net2D{net, v})
	}
	return out
}

// FormatNet2D writes addresses as SEEN-BY or PATH lines do, each line up
// to width characters after the prefix ("SEEN-BY: ", "\x01PATH: ").
func FormatNet2D(prefix string, addrs []Net2D, width int) []string {
	var lines []string
	line := ""
	net := -1
	for _, a := range addrs {
		var w string
		if a.Net == net && line != "" {
			w = strconv.Itoa(a.Node)
		} else {
			w = fmt.Sprintf("%d/%d", a.Net, a.Node)
		}
		if line != "" && len(line)+1+len(w) > width {
			lines = append(lines, prefix+line)
			line = ""
			w = fmt.Sprintf("%d/%d", a.Net, a.Node)
		}
		if line != "" {
			line += " "
		}
		line += w
		net = a.Net
	}
	if line != "" {
		lines = append(lines, prefix+line)
	}
	return lines
}

// ValidTag checks an echo tag: letters, digits and ._-!$&+, upper case, up
// to 60.
func ValidTag(tag string) bool {
	if tag == "" || len(tag) > 60 {
		return false
	}
	for _, c := range tag {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-!$&+", c)) {
			return false
		}
	}
	return true
}
