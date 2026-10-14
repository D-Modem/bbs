package api

import (
	"net"
	"net/http"
	"strings"
)

// Behind a reverse proxy (nginx, Caddy, a load balancer) every request
// comes from the proxy's address.  The proxies named in trusted_proxies
// are believed when they say whose request it is, by X-Forwarded-For (or
// Forwarded, or X-Real-IP); no one else is, since anyone can send those
// headers.

// ParseProxies reads trusted_proxies: addresses and CIDR ranges, by spaces
// or commas.
func ParseProxies(s string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if _, n, err := net.ParseCIDR(f); err == nil {
			out = append(out, n)
			continue
		}
		ip := net.ParseIP(f)
		if ip == nil {
			return nil, &net.ParseError{Type: "address or CIDR range", Text: f}
		}
		bits := 128
		if ip.To4() != nil {
			ip, bits = ip.To4(), 32
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}

// trusted says whether an address is a trusted proxy's.
func (s *Server) trusted(ip net.IP) bool {
	for _, n := range s.S.TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// parseHopIP is an address as a forwarding header gives it: bare,
// with a port, or in brackets ("[2001:db8::1]:443").
func parseHopIP(s string) net.IP {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return net.ParseIP(strings.Trim(s, "[]"))
}

// forwardedFor is the hops a request came by, as its proxies say, the
// first (the client) first.
func forwardedFor(r *http.Request) []string {
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	if len(hops) > 0 {
		return hops
	}
	// RFC 7239: Forwarded: for=192.0.2.60;proto=http, for="[2001:db8::1]"
	for _, h := range r.Header.Values("Forwarded") {
		for _, elem := range strings.Split(h, ",") {
			for _, pair := range strings.Split(elem, ";") {
				k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
				if ok && strings.EqualFold(k, "for") {
					hops = append(hops, v)
				}
			}
		}
	}
	if len(hops) > 0 {
		return hops
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return []string{v}
	}
	return nil
}

// clientIP is whom a trusted proxy's request is for: the last hop, from
// the end, that is not a trusted proxy (the ones before it could be
// anyone's invention); nil if the headers do not say.
func (s *Server) clientIP(r *http.Request) net.IP {
	hops := forwardedFor(r)
	var last net.IP
	for i := len(hops) - 1; i >= 0; i-- {
		ip := parseHopIP(hops[i])
		if ip == nil {
			break // "unknown", an obfuscated name: no further back
		}
		last = ip
		if !s.trusted(ip) {
			return ip
		}
	}
	return last
}

// realAddr has requests from trusted proxies come from whom they are
// for, before anything else sees them: rate limits, logins, the logs,
// the forum and the image boards, the web terminal.  The caller's
// address is then held to the [security] deny lists as a connection's
// is.
func (s *Server) realAddr(h http.Handler) http.Handler {
	if len(s.S.TrustedProxies) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err == nil && ip != nil && s.trusted(ip) {
			if c := s.clientIP(r); c != nil {
				if s.B.Refuse("http", &net.TCPAddr{IP: c}) {
					http.Error(w, "refused", http.StatusForbidden)
					return
				}
				r.RemoteAddr = net.JoinHostPort(c.String(), port)
			}
		}
		h.ServeHTTP(w, r)
	})
}
