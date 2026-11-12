package dnsd

import (
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
)

// Records are the sysop's own DNS records ([dnsrecord NAME]): answered
// as they are, to anyone, before the services' names and before anything
// is asked upstream.  A name beginning "*." is every name under it.  An
// A or AAAA record's address answers a reverse (PTR) query with its name,
// unless there are PTR records of the sysop's for it.
type Records struct {
	exact  map[string][]dns.RR // fqdn, lower case
	suffix map[string][]dns.RR // ".domain."
}

// NewRecord is a record of the sysop's: name, its type (A, AAAA, CNAME,
// TXT, MX, SRV, ... as a zone file names them; "" for an A or AAAA by the
// value's address), its value as a zone file writes it (an MX's "10
// mail.example.org", a TXT's text), and its TTL.
func NewRecord(name, typ, value string, ttl uint32) (dns.RR, error) {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	typ = strings.ToUpper(strings.TrimSpace(typ))
	if name == "" {
		return nil, fmt.Errorf("no name")
	}
	if strings.ContainsAny(name, " \t/@:") || strings.Contains(strings.TrimPrefix(name, "*."), "*") {
		return nil, fmt.Errorf("name: %q is not a host name (or *.domain)", name)
	}
	if value == "" {
		return nil, fmt.Errorf("no value")
	}
	if typ == "" {
		ip := net.ParseIP(value)
		switch {
		case ip == nil:
			return nil, fmt.Errorf("type: none given, and %q is not an IP address (an A or AAAA record's)", value)
		case ip.To4() != nil:
			typ = "A"
		default:
			typ = "AAAA"
		}
	}
	if _, ok := dns.StringToType[typ]; !ok {
		return nil, fmt.Errorf("type: %q is no DNS record type (A, AAAA, CNAME, TXT, MX, SRV ...)", typ)
	}
	switch typ {
	case "TXT", "SPF":
		if !strings.HasPrefix(value, `"`) {
			value = `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
		}
	case "CNAME", "NS", "PTR", "DNAME":
		value = dns.Fqdn(value)
	case "SOA":
		return nil, fmt.Errorf("type: SOA is not for a record of your own")
	}
	rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", dns.Fqdn(strings.ToLower(name)), ttl, typ, value))
	if err != nil {
		msg := err.Error()
		if pe, ok := err.(*dns.ParseError); ok {
			msg = pe.Error()
		}
		return nil, fmt.Errorf("value: %q is not a %s record's (%s)", value, typ, msg)
	}
	if rr == nil {
		return nil, fmt.Errorf("value: %q is not a %s record's", value, typ)
	}
	return rr, nil
}

// NewRecords is a table of records.
func NewRecords(rrs []dns.RR) *Records {
	r := &Records{exact: map[string][]dns.RR{}, suffix: map[string][]dns.RR{}}
	for _, rr := range rrs {
		n := strings.ToLower(rr.Header().Name)
		if strings.HasPrefix(n, "*.") {
			r.suffix[n[1:]] = append(r.suffix[n[1:]], rr)
			continue
		}
		r.exact[n] = append(r.exact[n], rr)
	}
	// each A and AAAA's address answers back with its name (a PTR), unless
	// the sysop wrote the address's PTRs
	for _, rr := range rrs {
		var ip net.IP
		switch a := rr.(type) {
		case *dns.A:
			ip = a.A
		case *dns.AAAA:
			ip = a.AAAA
		default:
			continue
		}
		name := rr.Header().Name
		rev, err := dns.ReverseAddr(ip.String())
		if err != nil || strings.HasPrefix(name, "*.") || r.wrotePTR(rev, rrs) {
			continue
		}
		r.exact[rev] = append(r.exact[rev], &dns.PTR{Hdr: dns.RR_Header{Name: rev, Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: rr.Header().Ttl}, Ptr: strings.ToLower(name)})
	}
	return r
}

// wrotePTR is whether the sysop's records (rrs) have a PTR for rev.
func (r *Records) wrotePTR(rev string, rrs []dns.RR) bool {
	for _, rr := range rrs {
		if rr.Header().Rrtype == dns.TypePTR && strings.EqualFold(rr.Header().Name, rev) {
			return true
		}
	}
	return false
}

// Len is how many names (and wildcards) have records.
func (r *Records) Len() int {
	if r == nil {
		return 0
	}
	return len(r.exact) + len(r.suffix)
}

// lookup is the records of name (a wildcard's with its name made name's),
// and whether it has any.
func (r *Records) lookup(name string) ([]dns.RR, bool) {
	if r == nil {
		return nil, false
	}
	name = strings.ToLower(dns.Fqdn(name))
	if rrs, ok := r.exact[name]; ok {
		return rrs, true
	}
	for i := 0; i < len(name)-1; i++ {
		if name[i] != '.' {
			continue
		}
		if rrs, ok := r.suffix[name[i:]]; ok {
			out := make([]dns.RR, len(rrs))
			for j, rr := range rrs {
				c := dns.Copy(rr)
				c.Header().Name = name
				out[j] = c
			}
			return out, true
		}
	}
	return nil, false
}

// answer is the records answering name and qtype: those of the type (all,
// for ANY), or else a CNAME and what its target has here; has is whether
// the name has records at all.
func (r *Records) answer(name string, qtype uint16) (ans []dns.RR, has bool) {
	rrs, has := r.lookup(name)
	if !has {
		return nil, false
	}
	for _, rr := range rrs {
		if t := rr.Header().Rrtype; t == qtype || qtype == dns.TypeANY {
			ans = append(ans, rr)
		}
	}
	if len(ans) > 0 || qtype == dns.TypeCNAME {
		return ans, true
	}
	for _, rr := range rrs {
		if c, ok := rr.(*dns.CNAME); ok {
			ans = append(ans, c)
			// its target's, when they are here too (one step: no loops)
			if more, ok := r.lookup(c.Target); ok {
				for _, m := range more {
					if m.Header().Rrtype == qtype {
						ans = append(ans, m)
					}
				}
			}
			return ans, true
		}
	}
	return nil, true
}

// fixName gives rrs the question's own spelling of the name (as the
// client asked: 0x20 case), for the records of that name.
func fixName(rrs []dns.RR, asked string) []dns.RR {
	out := make([]dns.RR, len(rrs))
	for i, rr := range rrs {
		c := dns.Copy(rr)
		if strings.EqualFold(c.Header().Name, asked) {
			c.Header().Name = asked
		}
		out[i] = c
	}
	return out
}
