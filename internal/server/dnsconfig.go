package server

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/dnsd"
	"github.com/D-Modem/bbs/internal/vhost"
)

// dnsSection reads [dns]: the DNS server for callers, which answers the
// old services' names with the BBS's address and resolves the rest.  It
// is read after the front ends and listeners, whose names it answers.
func (c *Config) dnsSection(cf *config.File) error {
	s := cf.Get("dns", "")
	if s == nil {
		return nil
	}
	err := func() error {
		d := &dnsd.Settings{Address: s.String("address", ":53")}
		if _, _, err := net.SplitHostPort(d.Address); err != nil {
			return fmt.Errorf("address: %v", err)
		}
		primary, backup := s.String("primary", ""), s.String("backup", "")
		if backup != "" && primary == "" {
			return errors.New("backup: with a primary (or neither, for the host's DNS servers)")
		}
		for _, a := range []struct{ key, v string }{{"primary", primary}, {"backup", backup}} {
			if a.v == "" {
				continue
			}
			sa, err := dnsd.ServerAddress(a.v)
			if err != nil {
				return fmt.Errorf("%s: %v", a.key, err)
			}
			d.Upstreams = append(d.Upstreams, sa)
		}
		if d.Upstreams == nil {
			// the host's: there must be some besides ourselves
			if _, _, _, err := dnsd.HostServers([]string{d.Address}); err != nil {
				c.Warnings = append(c.Warnings, fmt.Sprintf("[dns] %v", err))
			}
		}
		for _, a := range []struct {
			key string
			v4  bool
			ip  *net.IP
		}{{"answer", true, &d.Answer}, {"answer6", false, &d.Answer6}} {
			v := s.String(a.key, "")
			if v == "" {
				continue
			}
			ip := net.ParseIP(v)
			if ip == nil || (ip.To4() != nil) != a.v4 {
				kind := "IPv4"
				if !a.v4 {
					kind = "IPv6"
				}
				return fmt.Errorf("%s: an %s address (the BBS's, as callers reach it)", a.key, kind)
			}
			*a.ip = ip
		}
		on := c.dnsServicesOn()
		switch v := strings.ToLower(strings.TrimSpace(s.String("services", "auto"))); v {
		case "auto", "":
			d.Services = on
		case "none":
		default:
			for _, name := range splitList(v) {
				if _, ok := dnsd.Services[name]; !ok {
					return fmt.Errorf("services: %q is none of %s (or auto, none)", name,
						strings.Join(dnsd.ServiceNames(), ", "))
				}
				if !slices.Contains(on, name) {
					c.Warnings = append(c.Warnings, fmt.Sprintf("[dns] services: %s is not set up here, "+
						"so its clients are sent to a BBS that does not answer them", name))
				}
				if !slices.Contains(d.Services, name) {
					d.Services = append(d.Services, name)
				}
			}
		}
		for _, n := range splitList(s.String("names", "")) {
			n = strings.ToLower(strings.TrimSuffix(n, "."))
			if strings.ContainsAny(n, "/:@") || !strings.Contains(n, ".") || strings.Contains(n[1:], "*") ||
				strings.HasPrefix(n, "*") && !strings.HasPrefix(n, "*.") {
				return fmt.Errorf("names: %q is not a host name (or *.domain)", n)
			}
			d.Names = append(d.Names, n)
		}
		allow := dnsd.DefaultAllow
		if s.Has("allow") {
			allow = splitList(s.String("allow", ""))
		}
		var err error
		if d.Allow, err = dnsd.ParseNetworks(allow); err != nil {
			return fmt.Errorf("allow: %v", err)
		}
		if d.CacheSize, err = s.Int("cache_size", 10000, 0, 10000000); err != nil {
			return err
		}
		if d.MaxTTL, err = s.Seconds("max_ttl", 86400, 1, 7*86400); err != nil {
			return err
		}
		if d.NegTTL, err = s.Seconds("negative_ttl", 300, 0, 86400); err != nil {
			return err
		}
		ttl, err := s.Int("ttl", 60, 0, 86400)
		if err != nil {
			return err
		}
		d.TTL = uint32(ttl)
		if d.Timeout, err = s.Seconds("timeout", 2, 1, 30); err != nil {
			return err
		}
		if d.Rate, err = s.Int("rate", 50, 0, 100000); err != nil {
			return err
		}
		if d.Burst, err = s.Int("burst", 200, 1, 100000); err != nil {
			return err
		}
		if d.Timeout < time.Second {
			d.Timeout = time.Second
		}
		c.DNS = d
		c.dnsWarnings()
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[dns] %w", err)
	}
	return nil
}

// dnsServicesOn are the front ends set up here, whose old servers' names
// [dns] services = auto answers.
func (c *Config) dnsServicesOn() []string {
	var on []string
	if c.MSN != nil {
		on = append(on, "msn")
	}
	marvel, cis := c.Marvel != nil, false
	for _, l := range c.Listeners {
		marvel = marvel || l.Frontend == "marvel"
		cis = cis || l.Frontend == "compuserve"
	}
	if marvel {
		on = append(on, "marvel")
	}
	if c.OSCAR != nil {
		on = append(on, "oscar")
	}
	if c.AOL != nil {
		on = append(on, "aol")
	}
	if c.YMSG != nil {
		on = append(on, "ymsg")
	}
	if cis {
		on = append(on, "compuserve")
	}
	return on
}

// dnsWarnings: the clients sent here by name come to the old services'
// ports, so a front end listening elsewhere is not found.
func (c *Config) dnsWarnings() {
	want := func(service, key, addr, port, who string) {
		if !slices.Contains(c.DNS.Services, service) {
			return
		}
		if addr == "" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[%s] has no %s: %s", service, key, who))
			return
		}
		if _, p, _ := net.SplitHostPort(addr); p != port {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[%s] %s is %s: clients sent here by [dns] come to port %s (%s)",
				service, key, addr, port, who))
		}
	}
	if m := c.MSN; m != nil {
		want("msn", "address", m.Address, "1863", "MSN Messenger")
		want("msn", "https_address", m.HTTPSAddress, "443", "MSN Messenger 5.0 to 7.5's Passport login")
	}
	if y := c.YMSG; y != nil {
		want("ymsg", "address", y.Address, "5050", "Yahoo! Messenger")
		want("ymsg", "http_address", y.HTTPAddress, "80", "Yahoo! Messenger 9's pager lookup, the room list and file transfer")
		want("ymsg", "https_address", y.HTTPSAddress, "443", "Yahoo! Messenger 9's login")
	}
	if o := c.OSCAR; o != nil {
		want("oscar", "address", o.Address, "5190", "AIM and ICQ")
	}
	if a := c.AOL; a != nil {
		want("aol", "address", a.Address, "5190", "AOL's client")
	}
	if m := c.Marvel; m != nil {
		want("marvel", "address", m.Address, "569", "the MSN client of 1995")
	}
}

// SharedPort is a port two or more front ends serve, by the names their
// clients ask for (internal/vhost).
type SharedPort struct {
	Address string
	TLS     bool
	Members []string // msn, ymsg, oscar: in the order of the routes (the first's certificate for clients without SNI)
}

// SharedPorts are the ports the front ends' web servers share: MSN's
// Passport, Yahoo!'s login and AIM's TLS on 443, say.  A port one front
// end has alone is its own as before.
func (c *Config) SharedPorts() ([]SharedPort, error) {
	type use struct {
		addr, who string
		tls       bool
	}
	var uses []use
	if m := c.MSN; m != nil {
		uses = append(uses, use{m.HTTPSAddress, "msn", true}, use{m.HTTPAddress, "msn", false})
	}
	if y := c.YMSG; y != nil {
		uses = append(uses, use{y.HTTPSAddress, "ymsg", true}, use{y.HTTPAddress, "ymsg", false})
	}
	if o := c.OSCAR; o != nil {
		uses = append(uses, use{o.TLSAddress, "oscar", true})
	}
	var out []SharedPort
	byAddr := map[string]int{}
	for _, u := range uses {
		if u.addr == "" {
			continue
		}
		a := vhost.Normal(u.addr)
		i, ok := byAddr[a]
		if !ok {
			byAddr[a] = len(out)
			out = append(out, SharedPort{Address: u.addr, TLS: u.tls, Members: []string{u.who}})
			continue
		}
		if out[i].TLS != u.tls {
			return nil, fmt.Errorf("%s is both a TLS port and a plain one ([%s] and [%s])", u.addr,
				out[i].Members[0], u.who)
		}
		out[i].Members = append(out[i].Members, u.who)
	}
	var shared []SharedPort
	for _, p := range out {
		if len(p.Members) > 1 {
			shared = append(shared, p)
		}
	}
	return shared, nil
}

// sharedPortsCheck: the shared ports can be shared, and a TLS one's
// clients without SNI (Windows XP's) get a certificate for every name.
func (c *Config) sharedPortsCheck() error {
	ports, err := c.SharedPorts()
	if err != nil {
		return err
	}
	for _, p := range ports {
		if !p.TLS {
			continue
		}
		var files []string
		for _, m := range p.Members {
			switch m {
			case "msn":
				files = append(files, c.MSN.CertFile)
			case "ymsg":
				files = append(files, c.YMSG.CertFile)
			case "oscar":
				files = append(files, c.OSCAR.TLSCert)
			}
		}
		slices.Sort(files)
		if len(slices.Compact(files)) > 1 {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s is shared by %s with certificates of their own: "+
				"clients that name no server (Windows XP's) all get %s's, so give them one certificate with all their names",
				p.Address, strings.Join(p.Members, ", "), p.Members[0]))
		}
	}
	return nil
}
