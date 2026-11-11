package confui

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/dnsd"
	"github.com/D-Modem/bbs/internal/feeds"
	"github.com/D-Modem/bbs/internal/screens"
)

// What a key may be, offered from what the file (and the machine) has,
// so that a sysop picks a value rather than typing it: the certificates
// [acme] gets and the files the other servers name, this machine's
// addresses, the access levels, the screens, the feeds' catalogue...

// offer is the values a key is offered, a note beside each.
type offer struct {
	values []string
	notes  []string
	open   bool // a value of one's own may be typed too
	multi  bool // some of them, by commas
}

// add puts v (with its note) on the offer, once.
func (o *offer) add(v, note string) {
	if slices.Contains(o.values, v) {
		return
	}
	o.values = append(o.values, v)
	o.notes = append(o.notes, note)
}

// more are the closed lists (by "kind.key") beside choices: the values
// the BBS takes and no others.  "" is the default.
var more = map[string][]string{
	"listen.frontend":    {"", "bbs", "compuserve", "marvel"},
	"acme.challenge":     {"", "http", "tls-alpn", "dns"},
	"acme.dns_check":     {"", "authoritative", "resolvers", "no"},
	"acme.key_type":      {"", "ec256", "ec384", "rsa2048", "rsa3072", "rsa4096"},
	"mail.outbound":      {"", "direct", "smarthost", "none"},
	"smarthost.tls":      {"", "starttls", "implicit", "no"},
	"pop3.tls":           {"", "required", "implicit", "optional", "no"},
	"imap.tls":           {"", "required", "implicit", "optional", "no"},
	"smtp.tls":           {"", "required", "implicit", "optional", "no"},
	"mx.tls":             {"", "optional", "required", "implicit", "no"},
	"lmtp.tls":           {"", "no", "optional", "required", "implicit"},
	"door.door32_comm":   {"", "serial", "telnet", "local"},
	"ircpeer.protocol":   {"", "ts6", "hybrid", "p10"},
	"quotes.alpaca_feed": {"", "iex", "sip", "delayed_sip"},
	"ptp.timestamping":   {"", "software", "hardware"},
}

// notes are words beside some of the closed lists' values.
var notes = map[string]map[string]string{
	"listen.frontend": {"bbs": "the BBS", "compuserve": "CompuServe (CIS)", "marvel": "MSN 1995, dial-in"},
	"acme.challenge": {"http": "a token on port 80", "tls-alpn": "a certificate on port 443",
		"dns": "a TXT record (the only one for *.wildcards)"},
	"acme.dns_check": {"authoritative": "the zone's own servers", "resolvers": "dns_resolvers", "no": "just wait dns_wait"},
	"acme.key_type":  {"rsa2048": "Windows XP's clients know only RSA"},
	"mail.outbound": {"direct": "to each domain's MX, port 25", "smarthost": "through [smarthost]",
		"none": "local mail only"},
	"ptp.timestamping":   {"software": "the kernel's stamps", "hardware": "the network card's"},
	"quotes.alpaca_feed": {"iex": "the free plan", "sip": "every exchange (paid)"},
	"ircpeer.protocol":   {"ts6": "Solanum, charybdis, ratbox", "hybrid": "ircd-hybrid 8", "p10": "ircu"},
}

// suggested are open lists: values most use, others typed.
var suggested = map[string][][2]string{
	"general.log": {{"syslog", "the daemon facility"}, {"stderr", "systemd's journal"}, {"none", ""},
		{"/var/log/bbs/bbs.log", "a file"}},
	"acme.ca": {{"letsencrypt", "Let's Encrypt"}, {"letsencrypt-staging", "for trying it out"}, {"zerossl", "ZeroSSL"},
		{"buypass", "Buypass"}, {"google", "Google Trust Services (needs EAB)"}, {"sslcom", "SSL.com"}},
	"acme.dns": {{"cloudflare", ""}, {"route53", "Amazon"}, {"digitalocean", ""}, {"hetzner", ""}, {"gandiv5", ""},
		{"ovh", ""}, {"namecheap", ""}, {"porkbun", ""}, {"godaddy", ""}, {"desec", ""}, {"duckdns", ""},
		{"pdns", "PowerDNS"}, {"rfc2136", "nsupdate"}, {"acme-dns", ""}, {"httpreq", ""},
		{"exec:/usr/local/bin/dns-hook", "a program of your own"}, {"acmesh:dns_cf", "an acme.sh dnsapi script"}},
	"general.page_command": {{"curl -s -d \"{user} on node {node}: {reason}\" https://ntfy.sh/my-bbs-pages", "ntfy.sh"}},
	"dns.primary": {{"1.1.1.1", "Cloudflare"}, {"8.8.8.8", "Google"}, {"9.9.9.9", "Quad9"},
		{"208.67.222.222", "OpenDNS"}},
	"dns.backup": {{"8.8.8.8", "Google"}, {"1.1.1.1", "Cloudflare"}, {"9.9.9.9", "Quad9"},
		{"208.67.220.220", "OpenDNS"}},
	"listen.socket_mode": {{"0660", "its group"}, {"0666", "anyone on this machine"}, {"0600", "the BBS's user only"}},
	"general.phone_country": {{"US", "North America (+1)"}, {"GB", "the UK"}, {"IE", "Ireland"}, {"AU", "Australia"},
		{"NZ", "New Zealand"}, {"DE", "Germany"}, {"FR", "France"}, {"NL", "the Netherlands"}, {"SE", "Sweden"}, {"JP", "Japan"}},
	"general.geoip_credit_url": {{"https://db-ip.com", "DB-IP"}, {"https://www.maxmind.com", "MaxMind"}},
	"general.geoip_credit":     {{"IP geolocation by DB-IP", ""}, {"This product includes GeoLite2 data created by MaxMind", ""}},
}

// multiples are lists of some at once.
var multiples = map[string][][2]string{
	"time.servers": {{"nts time.cloudflare.com", "Cloudflare, NTS"}, {"nts nts.netnod.se", "Netnod, NTS"},
		{"nts ptbtime1.ptb.de", "PTB, NTS"}, {"nts time.nist.gov", ""}, {"ntp pool.ntp.org", "the NTP pool"},
		{"ntp time.google.com", "Google (smeared leap seconds)"}},
	"mx.dnsbl": {{"zen.spamhaus.org", "Spamhaus"}, {"bl.spamcop.net", "SpamCop"}, {"b.barracudacentral.org", "Barracuda"},
		{"psbl.surriel.com", "PSBL"}},
}

// listening are the kinds whose address keys are where the BBS listens
// (others' are where it calls).
var listening = map[string]bool{
	"listen": true, "ftp": true, "nntp": true, "pop3": true, "imap": true, "smtp": true, "mx": true, "lmtp": true,
	"binkp": true, "irc": true, "irclink": true, "oscar": true, "aol": true, "msn": true, "marvel": true, "ymsg": true,
	"dns": true, "finger": true, "gopher": true, "gemini": true, "ntp": true, "nts": true, "ptp": true, "http": true,
	"acme": true,
}

// offerFor is what key of s is offered, if anything.
func (e *Editor) offerFor(s confedit.Section, key string) (offer, bool) {
	kk := s.Kind + "." + key
	var o offer
	switch {
	case more[kk] != nil:
		for _, v := range more[kk] {
			o.add(v, notes[kk][v])
		}
	case suggested[kk] != nil:
		o.open = true
		o.add("", "")
		for _, v := range suggested[kk] {
			o.add(v[0], v[1])
		}
	case multiples[kk] != nil:
		o.multi, o.open = true, true
		for _, v := range multiples[kk] {
			o.add(v[0], v[1])
		}
	case s.Kind == "menu" && e.cat.Kind("menu").IsFree(key):
		o = e.menuItemOffer()
	case s.Kind == "qwknet" && e.cat.Kind("qwknet").IsFree(key):
		o = e.areaOffer(false)
		o.values, o.notes = o.values[1:], o.notes[1:]
	case isCertKey(key):
		o = e.certOffer(s)
	case isKeyKey(key):
		o = e.keyOffer(s, key)
	case listening[s.Kind] && isAddressKey(key):
		o = e.addressOffer(s, key)
	case kk == "dns.answer" || kk == "dns.answer6" || s.Kind == "dns" && strings.HasSuffix(key, "_answer"):
		o = e.answerOffer(key == "answer6")
	case kk == "dns.services":
		o.multi, o.open = true, true
		for _, n := range dnsd.ServiceNames() {
			note := "not set up here"
			if e.serviceOn(n) {
				note = "set up here"
			}
			o.add(n, note)
		}
	case kk == "dns.primary" || kk == "dns.backup":
		o.open = true
		o.add("", "the host's own servers")
		for _, ns := range resolvConf() {
			o.add(ns, "this host's")
		}
		for _, v := range suggested[kk] {
			o.add(v[0], v[1])
		}
	case kk == "general.new_user_level" || kk == "general.validated_level" || kk == "mrc.require" || kk == "menu.require":
		o = e.levelOffer()
	case kk == "link.aka":
		o.open = true
		o.add("", "ours in its zone")
		v, _ := e.doc.Get(sec("fido"), "address")
		for _, a := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
			o.add(a, "")
		}
	case kk == "feeds.catalog":
		o.multi, o.open = true, true
		o.add("all", "every feed of the catalogue")
		for _, g := range catalogGroups() {
			o.add(strings.ToLower(g), "the group")
		}
		for _, c := range feeds.Catalog {
			o.add(c.Name, c.Area)
		}
	case kk == "feed.group":
		o.open = true
		o.add("", "")
		for _, g := range catalogGroups() {
			o.add(g, "")
		}
		for _, f := range e.instances("feed", nil) {
			if g, _ := e.doc.Get(f, "group"); g != "" {
				o.add(g, "")
			}
		}
	case kk == "feed.area" || kk == "gopher.areas" || kk == "gemini.areas":
		o = e.areaOffer(kk != "feed.area")
	case kk == "menu.screen":
		o = e.screenOffer()
	case kk == "quotes.interface" || kk == "ptp.interface":
		o.open = true
		o.add("", "")
		for _, a := range localAddrs() {
			o.add(a.iface, "")
		}
	case kk == "general.user":
		o = sysNames("/etc/passwd")
	case kk == "general.group" || kk == "listen.socket_group":
		o = sysNames("/etc/group")
	case kk == "mail.postmaster":
		o.open = true
		o.add("", "the sysop")
		if e.db != nil {
			for _, u := range e.db.users {
				if u.Sysop {
					o.add(u.Name, "a sysop")
				}
			}
		}
	default:
		return offer{}, false
	}
	if len(o.values) == 0 || len(o.values) == 1 && o.values[0] == "" {
		return offer{}, false
	}
	return o, true
}

// caSigned are the kinds whose certificate the BBS's own CA may sign
// (tls_cert = ca, cert_file = ca).
var caSigned = map[string]bool{"msn": true, "ymsg": true, "oscar": true, "gemini": true}

// isCertKey is whether a key names a certificate.
func isCertKey(key string) bool { return key == "tls_cert" || key == "cert_file" }

// isKeyKey is whether a key names a certificate's private key.
func isKeyKey(key string) bool { return key == "tls_key" || key == "key_file" }

// keyOf is the key key of a certificate key ("tls_cert": "tls_key").
func keyOf(certKey string) string {
	if certKey == "tls_cert" {
		return "tls_key"
	}
	return "key_file"
}

// certOf is the certificate key of a key key.
func certOf(keyKey string) string {
	if keyKey == "tls_key" {
		return "tls_cert"
	}
	return "cert_file"
}

// isAddressKey is whether a key is an address the BBS listens on.
func isAddressKey(key string) bool {
	return key == "address" || key == "metrics" || strings.HasSuffix(key, "_address")
}

// certOffer: the [acme] certificates, and the certificate files other
// sections name.
func (e *Editor) certOffer(s confedit.Section) offer {
	o := offer{open: true}
	acme := e.doc.Has(sec("acme"))
	switch {
	case acme && s.Kind != "gemini" && s.Kind != "ymsg":
		o.add("", "the [acme] certificate for the name asked")
	default:
		o.add("", "none")
	}
	if acme {
		d, _ := e.doc.Get(sec("acme"), "domains")
		o.add(certs.ACME, noteOr(d, "[acme]'s own"))
	}
	for _, a := range e.instances("acme", func(x confedit.Section) bool { return x.Name != "" }) {
		d, _ := e.doc.Get(a, "domains")
		o.add(certs.ACME+":"+a.Name, noteOr(d, "[acme "+a.Name+"]"))
	}
	if caSigned[s.Kind] {
		o.add(certs.CA, "the BBS's own CA signs it for the old names (bbs ca)")
	}
	for _, f := range e.files(isCertKey) {
		o.add(f.value, "as "+f.where)
	}
	return o
}

// keyOffer: the key that goes with the certificate chosen (where another
// section has them together), then the other key files named.
func (e *Editor) keyOffer(s confedit.Section, key string) offer {
	o := offer{open: true}
	o.add("", "")
	cert, _ := e.doc.Get(s, certOf(key))
	if cert != "" && !certs.IsACME(cert) && !certs.IsCA(cert) {
		for _, x := range e.doc.Sections() {
			for _, ck := range []string{"tls_cert", "cert_file"} {
				if c, _ := e.doc.Get(x, ck); c == cert {
					if k, _ := e.doc.Get(x, keyOf(ck)); k != "" {
						o.add(k, "goes with "+filepath.Base(cert))
					}
				}
			}
		}
		// beside it: name.crt, name.key; cert.pem, key.pem
		dir, base := filepath.Dir(cert), filepath.Base(cert)
		ext := filepath.Ext(base)
		for _, guess := range []string{strings.TrimSuffix(base, ext) + ".key",
			strings.Replace(base, "cert", "key", 1), strings.Replace(base, "crt", "key", 1)} {
			if guess == base {
				continue
			}
			p := filepath.Join(dir, guess)
			if _, err := os.Stat(p); err == nil {
				o.add(p, "beside the certificate")
			}
		}
	}
	for _, f := range e.files(isKeyKey) {
		o.add(f.value, "as "+f.where)
	}
	return o
}

// named is a value the file has, and where.
type named struct{ value, where string }

// files are the values of the keys keep says, in every section, once
// each.
func (e *Editor) files(keep func(string) bool) []named {
	var out []named
	seen := map[string]bool{}
	for _, x := range e.doc.Sections() {
		for _, k := range e.doc.Keys(x) {
			if !keep(k) {
				continue
			}
			v, _ := e.doc.Get(x, k)
			if v == "" || certs.IsACME(v) || certs.IsCA(v) || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, named{v, x.String()})
		}
	}
	return out
}

// noteOr is s, or else if s is "".
func noteOr(s, orElse string) string {
	if s == "" {
		return orElse
	}
	return s
}

// ifAddr is an address of this machine's.
type ifAddr struct {
	iface string
	ip    net.IP
}

// localAddrs are this machine's interfaces' addresses (a var for the
// tests).
var localAddrs = func() []ifAddr {
	var out []ifAddr
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 {
			continue
		}
		as, _ := i.Addrs()
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLinkLocalUnicast() {
				out = append(out, ifAddr{i.Name, n.IP})
			}
		}
		if len(as) == 0 {
			out = append(out, ifAddr{iface: i.Name})
		}
	}
	return out
}

// addressOffer: every address, this machine only, each of its addresses
// (at the port it has, or the sample's), and which other section is on
// each already.
func (e *Editor) addressOffer(s confedit.Section, key string) offer {
	o := offer{open: true}
	cur, _ := e.doc.Get(s, key)
	port := ""
	if _, p, err := net.SplitHostPort(cur); err == nil {
		port = p
	}
	help := ""
	if x := e.cat.Key(s.Kind, key); x != nil {
		help = x.Help
		for _, ex := range x.Examples {
			if port != "" {
				break
			}
			if strings.HasPrefix(ex, "/") {
				o.add("", "")
				o.add(ex, "a unix socket")
				continue
			}
			if _, p, err := net.SplitHostPort(ex); err == nil {
				port = p
			}
		}
	}
	if port == "" {
		return o
	}
	used := e.usedAddresses(s, key)
	note := func(addr, n string) string {
		if who, ok := used[addr]; ok {
			return "taken by " + who
		}
		if _, p, _ := net.SplitHostPort(addr); used[":"+p] != "" && addr != ":"+p {
			return "taken by " + used[":"+p] + " (every address)"
		}
		return n
	}
	o.add("", "")
	o.add(":"+port, note(":"+port, "every address"))
	o.add("127.0.0.1:"+port, note("127.0.0.1:"+port, "this machine only"))
	for _, a := range localAddrs() {
		if a.ip == nil || a.ip.IsLoopback() {
			continue
		}
		addr := net.JoinHostPort(a.ip.String(), port)
		o.add(addr, note(addr, a.iface))
	}
	if strings.Contains(help, "= none") {
		o.add("none", "not on")
	}
	return o
}

// usedAddresses are the addresses other keys listen on: address, whose.
func (e *Editor) usedAddresses(s confedit.Section, key string) map[string]string {
	out := map[string]string{}
	for _, x := range e.doc.Sections() {
		if !listening[x.Kind] {
			continue
		}
		for _, k := range e.doc.Keys(x) {
			if !isAddressKey(k) || x == s && k == key {
				continue
			}
			if v, _ := e.doc.Get(x, k); v != "" && v != "none" {
				if _, ok := out[v]; !ok {
					out[v] = x.String() + " " + k
				}
			}
		}
	}
	return out
}

// answerOffer: this machine's addresses (IPv6 ones for answer6), and the
// IPs the file's services listen on.
func (e *Editor) answerOffer(v6 bool) offer {
	o := offer{open: true}
	o.add("", "as each caller reached the BBS")
	fits := func(ip net.IP) bool { return ip != nil && (ip.To4() == nil) == v6 && !ip.IsUnspecified() }
	for _, x := range e.doc.Sections() {
		if !listening[x.Kind] || x.Kind == "dns" {
			continue
		}
		for _, k := range e.doc.Keys(x) {
			if !isAddressKey(k) {
				continue
			}
			v, _ := e.doc.Get(x, k)
			if h, _, err := net.SplitHostPort(v); err == nil {
				if ip := net.ParseIP(h); fits(ip) && !ip.IsLoopback() {
					o.add(ip.String(), "["+x.String()+"] "+k)
				}
			}
		}
	}
	for _, a := range localAddrs() {
		if fits(a.ip) && !a.ip.IsLoopback() {
			o.add(a.ip.String(), a.iface)
		}
	}
	return o
}

// serviceOn is whether a service [dns] answers for is set up here.
func (e *Editor) serviceOn(name string) bool {
	if name == "compuserve" {
		return len(e.instances("listen", e.ofFrontEnd("compuserve"))) > 0
	}
	return e.doc.Has(sec(name))
}

// resolvConf is the host's name servers (not a local stub's).
func resolvConf() []string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == "nameserver" {
			if ip := net.ParseIP(fs[1]); ip != nil && !ip.IsLoopback() {
				out = append(out, fs[1])
			}
		}
	}
	return out
}

// levelOffer: the [level N]s, their names beside them.
func (e *Editor) levelOffer() offer {
	o := offer{open: true}
	type lv struct {
		n    int
		name string
	}
	var ls []lv
	for _, s := range e.instances("level", nil) {
		n, err := strconv.Atoi(s.Name)
		if err != nil {
			continue
		}
		name, _ := e.doc.Get(s, "name")
		ls = append(ls, lv{n, name})
	}
	if len(ls) == 0 {
		return o
	}
	sort.Slice(ls, func(i, j int) bool { return ls[i].n < ls[j].n })
	o.add("", "")
	for _, l := range ls {
		o.add(strconv.Itoa(l.n), noteOr(l.name, "[level "+strconv.Itoa(l.n)+"]"))
	}
	return o
}

// catalogGroups are the feeds' catalogue's groups, in its order.
func catalogGroups() []string {
	var out []string
	for _, c := range feeds.Catalog {
		if c.Group != "" && !slices.Contains(out, c.Group) {
			out = append(out, c.Group)
		}
	}
	return out
}

// areaOffer: the message areas (when the database has been read); some
// of them at once, for a wildmat, if multi.
func (e *Editor) areaOffer(multi bool) offer {
	o := offer{open: true, multi: multi}
	if !multi {
		o.add("", "its name")
	}
	if e.db == nil {
		if e.dbErr != nil {
			return o
		}
		if _, e.dbErr = e.data(); e.dbErr != nil {
			return o
		}
	}
	for _, a := range e.db.areas {
		o.add(a.Name, a.Description)
	}
	return o
}

// screenOffer: the display files in screens_dir, by name.
func (e *Editor) screenOffer() offer {
	o := offer{open: true}
	dir, _ := e.doc.Get(sec("general"), "screens_dir")
	if dir == "" {
		return o
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return o
	}
	o.add("", "")
	for _, d := range ents {
		n := d.Name()
		switch ext := filepath.Ext(n); ext {
		case ".ans", ".asc", ".txt", ".rip", ".3270", ".seq":
			o.add(strings.TrimSuffix(n, ext), "")
		}
	}
	return o
}

// sysNames are the accounts (or groups) of this machine a BBS would run
// as: the people's (1000 and up), and those named for it.
func sysNames(file string) offer {
	o := offer{open: true}
	f, err := os.Open(file)
	if err != nil {
		return o
	}
	defer f.Close()
	o.add("", "")
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Split(sc.Text(), ":")
		if len(fs) < 3 {
			continue
		}
		id, err := strconv.Atoi(fs[2])
		if err != nil {
			continue
		}
		known := strings.Contains(fs[0], "bbs") || fs[0] == "dialout" || fs[0] == "asterisk" || fs[0] == "uucp"
		if known || id >= 1000 && id < 65534 {
			o.add(fs[0], "")
		}
	}
	return o
}

// builtins are a menu item's actions of the BBS's own, with a label each.
var builtins = [][2]string{
	{"areas", "Message areas"}, {"mail", "E-mail"}, {"files", "File areas"}, {"doors", "Doors"},
	{"chat", "Chat"}, {"page", "Page the sysop"}, {"who", "Who's online"}, {"users", "User list"},
	{"profile", "Your profile"}, {"info", "System info"}, {"callers", "Last callers"}, {"boards", "Image boards"},
	{"feedback", "Feedback to the sysop"}, {"qwk", "QWK offline mail"}, {"terminal", "Your terminal"},
	{"sysop", "Sysop's menu"}, {"back", "Back"}, {"logoff", "Goodbye"},
}

// menuItemOffer: a menu item's actions, each with a label to begin from:
// the BBS's own, then the doors, the other menus, the screens, the scripts
// and the areas the file and the database have.
func (e *Editor) menuItemOffer() offer {
	o := offer{open: true}
	for _, b := range builtins {
		o.add(b[0]+" "+b[1], "")
	}
	for _, d := range e.instances("door", nil) {
		desc, _ := e.doc.Get(d, "description")
		o.add("door:"+d.Name+" "+noteOr(desc, d.Name), "a door")
	}
	for _, m := range e.instances("menu", func(s confedit.Section) bool { return s.Name != "main" }) {
		t, _ := e.doc.Get(m, "title")
		o.add("menu:"+m.Name+" "+noteOr(screens.StripTags(t), m.Name), "a menu")
	}
	for _, sc := range e.screenOffer().values {
		if sc != "" {
			o.add("screen:"+sc+" "+sc, "a screen")
		}
	}
	if dir, _ := e.doc.Get(sec("general"), "scripts_dir"); dir != "" {
		if ents, err := os.ReadDir(dir); err == nil {
			for _, d := range ents {
				if n, ok := strings.CutSuffix(d.Name(), ".js"); ok && n != "logon" {
					o.add("script:"+n+" "+n, "a script")
				}
			}
		}
	}
	if e.db != nil {
		for _, a := range e.db.areas {
			o.add("area:"+a.Name+" "+a.Name, "an area")
		}
	}
	return o
}
