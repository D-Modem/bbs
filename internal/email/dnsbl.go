package email

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// reversed is an IP's DNSBL query name under a list: 1.2.0.192.list for
// 192.0.2.1, and for IPv6 its 32 nibbles backwards.
func reversed(ip net.IP, list string) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.%s", v4[3], v4[2], v4[1], v4[0], list)
	}
	const hex = "0123456789abcdef"
	var b strings.Builder
	for i := len(ip) - 1; i >= 0; i-- {
		b.WriteByte(hex[ip[i]&0xf])
		b.WriteByte('.')
		b.WriteByte(hex[ip[i]>>4])
		b.WriteByte('.')
	}
	return b.String() + list
}

// unlisted: addresses no list has (this machine's, a private network's).
var unlisted = func(ip net.IP) bool { return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() }

// listedBy is the first of the lists that has an IP on it, and the reason it
// gives (its TXT record), or "" if none does.  A list that cannot be asked
// (no answer in time, or a refusal: 127.255.255.x, as Spamhaus answers
// queries through public resolvers) does not count: mail is not refused
// because a list is down.
func (s *Service) listedBy(ip net.IP, lists []string) (list, why string) {
	if ip == nil || unlisted(ip) || len(lists) == 0 {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := s.Set.Resolver
	hits := make([]string, len(lists))
	var wg sync.WaitGroup
	for i, l := range lists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := reversed(ip, l)
			addrs, err := r.LookupIPAddr(ctx, name)
			if err != nil {
				return
			}
			for _, a := range addrs {
				v4 := a.IP.To4()
				if v4 == nil || v4[0] != 127 {
					continue
				}
				if v4[1] == 255 && v4[2] == 255 {
					s.B.Log.Printf("mx: dnsbl %s refused the query (%s): use a resolver of your own for it", l, a.IP)
					return
				}
				hits[i] = name
				return
			}
		}()
	}
	wg.Wait()
	for i, name := range hits {
		if name == "" {
			continue
		}
		if txt, err := r.LookupTXT(ctx, name); err == nil && len(txt) > 0 {
			why = txt[0]
		}
		return lists[i], why
	}
	return "", ""
}

// printable is a list's reason fit for an SMTP reply: printable ASCII, on
// one line, not too long.
func printable(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r > '~' {
			return ' '
		}
		return r
	}, s)
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}
