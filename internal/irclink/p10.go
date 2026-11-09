package irclink

// P10 is the server protocol of ircu (Undernet and its kin).  The hub
// keeps the network by TS6's names (SIDs and UIDs), so a P10 link is a
// translation at its edge: its lines come in as TS6's and go out as P10's,
// numerics mapped both ways.  A line from one P10 link goes to another as
// it came (modes, bans and G-lines with it); between P10 and TS6 only
// what both have goes across.

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
)

// b64 are P10's base64 digits: A is 0 and ] is 63.
const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789[]"

// toB64 is n in P10's base64, width digits.
func toB64(n, width int) string {
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = b64[n&63]
		n >>= 6
	}
	return string(b)
}

// fromB64 is a P10 base64 number (-1: not one).
func fromB64(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(b64, s[i])
		if d < 0 {
			return -1
		}
		n = n<<6 | d
	}
	return n
}

var (
	p10ProtoRE  = regexp.MustCompile(`^[JP]1[0-9]$`)
	serverNumRE = regexp.MustCompile(`^[A-Za-z0-9\[\]]{2}$`)
	userNumRE   = regexp.MustCompile(`^[A-Za-z0-9\[\]]{5}$`)
)

// base36 digits, as TS6's IDs have them
const b36 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// numerics maps TS6's names to P10's numerics and back: a server's SID
// and its two-digit numeric, a user's UID and their five.  Those of P10
// servers are theirs; those of the others are made as they are needed.
type numerics struct {
	mu      sync.Mutex
	sidNum  map[string]string
	numSID  map[string]string
	uidNum  map[string]string
	numUID  map[string]string
	next    map[string]int       // a server's next client numeric to make
	retired map[string]time.Time // "u"+UID or "s"+SID: gone since
}

func newNumerics() *numerics {
	return &numerics{sidNum: map[string]string{}, numSID: map[string]string{},
		uidNum: map[string]string{}, numUID: map[string]string{}, next: map[string]int{}}
}

// set ties a SID and a server numeric.
func (n *numerics) set(sid, num string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sidNum[sid], n.numSID[num] = num, sid
}

// server is a SID's numeric, made if it has none (a TS6 server's).
func (n *numerics) server(sid string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.serverLocked(sid)
}

func (n *numerics) serverLocked(sid string) string {
	if num, ok := n.sidNum[sid]; ok {
		return num
	}
	h := 0
	for i := 0; i < len(sid); i++ {
		h = h*31 + int(sid[i])
	}
	for i := 0; i < 4096; i++ {
		num := toB64((h+i)%4096, 2)
		if _, used := n.numSID[num]; !used {
			n.sidNum[sid], n.numSID[num] = num, sid
			return num
		}
	}
	return ""
}

// sidOf is a P10 server numeric's SID, made if it has none: one not taken
// (by taken, or another numeric's).
func (n *numerics) sidOf(num string, taken func(string) bool) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if sid, ok := n.numSID[num]; ok {
		return sid
	}
	v := fromB64(num)
	if v < 0 {
		return ""
	}
	for i := 0; i < 10*36*36; i++ {
		k := (v + i) % (10 * 36 * 36)
		sid := string([]byte{b36[k/1296], b36[k/36%36], b36[k%36]})
		if _, used := n.sidNum[sid]; !used && !taken(sid) {
			n.sidNum[sid], n.numSID[num] = num, sid
			return sid
		}
	}
	return ""
}

// knownServer is a server numeric's SID ("": not known).
func (n *numerics) knownServer(num string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.numSID[num]
}

// user is a UID's numeric, made if it has none (one of a TS6 server's).
func (n *numerics) user(uid string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if num, ok := n.uidNum[uid]; ok {
		return num
	}
	if len(uid) != 9 {
		return ""
	}
	srv := n.serverLocked(uid[:3])
	for i := 0; i < 1<<18; i++ {
		c := n.next[srv]
		n.next[srv] = (c + 1) % (1 << 18)
		num := srv + toB64(c, 3)
		if _, used := n.numUID[num]; !used {
			n.uidNum[uid], n.numUID[num] = num, uid
			return num
		}
	}
	return ""
}

// uidOf is a P10 user numeric's UID, made if it has none (its server's
// SID must be known).
func (n *numerics) uidOf(num string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if uid, ok := n.numUID[num]; ok {
		return uid
	}
	sid, ok := n.numSID[num[:2]]
	c := fromB64(num[2:])
	if !ok || c < 0 {
		return ""
	}
	b := []byte(sid + "AAAAAA")
	for i := 8; i > 3; i-- {
		b[i] = b36[c%36]
		c /= 36
	}
	uid := string(b)
	n.uidNum[uid], n.numUID[num] = num, uid
	return uid
}

// knownUser is a user numeric's UID ("": not known).
func (n *numerics) knownUser(num string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.numUID[num]
}

// retireUser lets a user's numeric go in a while: lines about them (their
// QUIT, a KILL) may follow their leaving.
func (n *numerics) retireUser(uid string) { n.retire("u" + uid) }

// retireServer lets a server's numeric go in a while.
func (n *numerics) retireServer(sid string) { n.retire("s" + sid) }

// retireAfter is how long a numeric is kept after its user or server is gone.
const retireAfter = 5 * time.Minute

func (n *numerics) retire(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	if n.retired == nil {
		n.retired = map[string]time.Time{}
	}
	n.retired[key] = now
	for k, t := range n.retired {
		if now.Sub(t) < retireAfter {
			continue
		}
		delete(n.retired, k)
		id := k[1:]
		if k[0] == 'u' {
			if num, ok := n.uidNum[id]; ok {
				delete(n.uidNum, id)
				delete(n.numUID, num)
			}
		} else if num, ok := n.sidNum[id]; ok {
			delete(n.sidNum, id)
			delete(n.numSID, num)
			delete(n.next, num)
		}
	}
}

// back: a user or server is on again; its numeric is not to go.
func (n *numerics) back(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.retired, key)
}

// id is a TS6 name (a SID or UID) as a P10 numeric; anything else is as
// it is.
func (n *numerics) id(s string) string {
	switch {
	case len(s) == 3 && sidRE.MatchString(s):
		return n.server(s)
	case len(s) == 9 && sidRE.MatchString(s[:3]):
		return n.user(s)
	}
	return s
}

// p10ID is a P10 numeric as a TS6 name; anything else is as it is.
func (n *numerics) p10ID(s string) string {
	switch {
	case serverNumRE.MatchString(s):
		if sid := n.knownServer(s); sid != "" {
			return sid
		}
	case userNumRE.MatchString(s):
		if uid := n.knownUser(s); uid != "" {
			return uid
		}
	}
	return s
}

// p10Line makes a P10 line: source, token and parameters, the last as
// trailing when it must be, or text is.
func p10Line(source, token string, trailing bool, params ...string) string {
	var b strings.Builder
	if source != "" {
		b.WriteString(source + " ")
	}
	b.WriteString(token)
	for i, p := range params {
		b.WriteByte(' ')
		if i == len(params)-1 && (trailing || p == "" || strings.ContainsRune(p, ' ') || p[0] == ':') {
			b.WriteByte(':')
		}
		b.WriteString(p)
	}
	s := b.String()
	if len(s) > maxLine-2 {
		s = s[:maxLine-2]
	}
	return s
}

// parseP10 splits a P10 line: its source (a numeric), token and
// parameters.
func parseP10(raw string) (source, token string, params []string) {
	s := strings.TrimLeft(raw, " ")
	if strings.HasPrefix(s, ":") {
		// an old-style :name prefix
		i := strings.IndexByte(s, ' ')
		if i < 0 {
			return "", "", nil
		}
		source, s = s[1:i], s[i+1:]
	}
	for s != "" {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			break
		}
		if s[0] == ':' {
			params = append(params, s[1:])
			break
		}
		f, rest, _ := strings.Cut(s, " ")
		params = append(params, f)
		s = rest
	}
	if len(params) == 0 {
		return "", "", nil
	}
	if source == "" && len(params) > 1 && (serverNumRE.MatchString(params[0]) || userNumRE.MatchString(params[0])) {
		source, params = params[0], params[1:]
	}
	return source, params[0], params[1:]
}

// ipToB64 is an IP address in P10's base64 (an IPv6 one as none: the BBS
// does not ask its peers for those).
func ipToB64(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Unmap().Is4() {
		return "AAAAAA"
	}
	b := a.Unmap().As4()
	return toB64(int(b[0])<<24|int(b[1])<<16|int(b[2])<<8|int(b[3]), 6)
}

// b64ToIP is a P10 base64 IP address as text ("0": none).
func b64ToIP(s string) string {
	if len(s) == 6 {
		v := fromB64(s)
		if v <= 0 {
			return "0"
		}
		return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}).String()
	}
	// IPv6: groups of three digits, _ for the longest run of zeros
	var g [8]uint16
	pos := 0
	for i := 0; i < len(s) && pos < 8; {
		if s[i] == '_' {
			left := (len(s) - i - 1) / 3
			pos = 8 - left
			i++
			continue
		}
		if i+3 > len(s) {
			return "0"
		}
		v := fromB64(s[i : i+3])
		if v < 0 {
			return "0"
		}
		g[pos] = uint16(v)
		pos++
		i += 3
	}
	var b [16]byte
	for i, v := range g {
		b[2*i], b[2*i+1] = byte(v>>8), byte(v)
	}
	return netip.AddrFrom16(b).String()
}

// p10Tokens are TS6's commands as P10's tokens, for those sent as they
// are (their numerics mapped).
var p10Tokens = map[string]string{
	"PRIVMSG": "P", "NOTICE": "O", "QUIT": "Q", "AWAY": "A", "WHOIS": "W",
	"VERSION": "V", "MOTD": "MO", "ADMIN": "AD", "INFO": "F", "TIME": "TI",
	"LUSERS": "LU", "STATS": "R", "LINKS": "LI", "TRACE": "TR", "PART": "L",
	"KICK": "K", "KILL": "D", "ERROR": "Y",
}

// p10Commands are P10's tokens as TS6's commands, for those taken as
// they come (their numerics mapped).
var p10Commands = map[string]string{
	"P": "PRIVMSG", "O": "NOTICE", "W": "WHOIS", "V": "VERSION", "MO": "MOTD",
	"AD": "ADMIN", "F": "INFO", "TI": "TIME", "LU": "LUSERS", "R": "STATS",
	"LI": "LINKS", "TR": "TRACE", "PRIVMSG": "PRIVMSG", "NOTICE": "NOTICE",
}

// toP10 is a TS6 line as P10's, for a P10 link ("": it has none).
func (h *Hub) toP10(raw string) string {
	m, err := ircmsg.ParseLine(raw)
	if err != nil {
		return ""
	}
	p := func(i int) string {
		if i < len(m.Params) {
			return m.Params[i]
		}
		return ""
	}
	cmd := strings.ToUpper(m.Command)
	if m.Source == "" {
		switch cmd {
		case "ERROR", "PASS", "SERVER":
			return raw // before a link is taken: as it is
		}
		return ""
	}
	n := h.nums
	src := n.id(m.Source)
	switch cmd {
	case "PING":
		return p10Line(src, "G", true, p(0))
	case "PONG", "SVINFO", "CAPAB", "EOB", "SAVE", "ENCAP", "CHGHOST", "MODE":
		return ""
	case "SID":
		// SID name hops sid :desc
		return p10Line(src, "S", true, p(0), p(1), "0", strconv.FormatInt(time.Now().Unix(), 10), "P10",
			n.server(p(2))+"]]]", "+", p(3))
	case "SQUIT":
		return p10Line(src, "SQ", true, n.id(p(0)), "0", p(1))
	case "UID", "EUID":
		var nick, hops, ts, umodes, ident, host, ip, uid, account, real string
		switch {
		case cmd == "EUID" && len(m.Params) >= 11:
			nick, hops, ts, umodes, ident, host, ip, uid, account, real = p(0), p(1), p(2), p(3), p(4), p(5), p(6), p(7), p(9), p(10)
		case len(m.Params) >= 11: // hybrid 8's UID
			nick, hops, ts, umodes, ident, host, ip, uid, account, real = p(0), p(1), p(2), p(3), p(4), p(5), p(7), p(8), p(9), p(10)
		case len(m.Params) >= 9:
			nick, hops, ts, umodes, ident, host, ip, uid, real = p(0), p(1), p(2), p(3), p(4), p(5), p(6), p(7), p(8)
		default:
			return ""
		}
		modes := "+"
		for _, r := range umodes {
			if strings.ContainsRune("iwd", r) {
				modes += string(r)
			}
		}
		params := []string{nick, hops, ts, ident, host}
		if account != "" && account != "*" && account != "0" {
			params = append(params, modes+"r", account)
		} else if modes != "+" {
			params = append(params, modes)
		}
		params = append(params, ipToB64(ip), n.user(uid), real)
		return p10Line(src, "N", true, params...)
	case "NICK":
		return p10Line(src, "N", false, p(0), p(1))
	case "JOIN":
		if p(0) == "0" {
			return p10Line(src, "J", false, "0")
		}
		return p10Line(src, "J", false, p(1), p(0))
	case "SJOIN":
		// SJOIN ts channel modes [args...] :members
		if len(m.Params) < 4 {
			return ""
		}
		params := []string{p(1), p(0)}
		if modes := m.Params[2 : len(m.Params)-1]; modes[0] != "+" && modes[0] != "" {
			params = append(params, modes...)
		}
		var plain, voiced, opped, both []string
		for _, mem := range strings.Fields(m.Params[len(m.Params)-1]) {
			uid := strings.TrimLeft(mem, "@+%~&!")
			num := n.user(uid)
			if num == "" {
				continue
			}
			prefix := mem[:len(mem)-len(uid)]
			op := strings.ContainsAny(prefix, "@~&!%")
			voice := strings.ContainsRune(prefix, '+')
			switch {
			case op && voice:
				both = append(both, num)
			case op:
				opped = append(opped, num)
			case voice:
				voiced = append(voiced, num)
			default:
				plain = append(plain, num)
			}
		}
		var list []string
		for _, g := range []struct {
			mark string
			nums []string
		}{{"", plain}, {":v", voiced}, {":o", opped}, {":ov", both}} {
			for i, num := range g.nums {
				if i == 0 {
					num += g.mark
				}
				list = append(list, num)
			}
		}
		if len(list) == 0 {
			return ""
		}
		params = append(params, strings.Join(list, ","))
		return p10Line(src, "B", false, params...)
	case "TOPIC":
		return p10Line(src, "T", true, p(0), p(len(m.Params)-1))
	case "TB":
		// TB channel topic-ts [setter] :topic
		setter := h.me.name
		if len(m.Params) >= 4 {
			setter = p(2)
		}
		return p10Line(src, "T", true, p(0), "0", p(1), setter, p(len(m.Params)-1))
	case "TBURST", "ETB":
		// TBURST channel-ts channel topic-ts setter :topic
		if len(m.Params) < 5 {
			return ""
		}
		return p10Line(src, "T", true, p(1), "0", p(2), p(3), p(len(m.Params)-1))
	case "TMODE":
		// TMODE ts channel modes args...
		if len(m.Params) < 3 {
			return ""
		}
		params := []string{p(1)}
		for _, a := range m.Params[2:] {
			params = append(params, n.id(a))
		}
		return p10Line(src, "M", false, params...)
	case "BMASK":
		// BMASK ts channel type :masks
		if p(2) != "b" {
			return ""
		}
		masks := strings.Fields(p(3))
		var lines []string
		for len(masks) > 0 {
			k := min(len(masks), 6)
			lines = append(lines, p10Line(src, "M", false, append([]string{p(1), "+" + strings.Repeat("b", k)}, masks[:k]...)...))
			masks = masks[k:]
		}
		return strings.Join(lines, "\r\n")
	case "AWAY":
		if p(0) == "" {
			return p10Line(src, "A", false)
		}
		return p10Line(src, "A", true, p(0))
	}
	if len(cmd) == 3 && cmd[0] >= '0' && cmd[0] <= '9' {
		params := append([]string{}, m.Params...)
		if len(params) > 0 {
			params[0] = n.id(params[0])
		}
		return p10Line(src, cmd, len(params) > 1, params...)
	}
	token, ok := p10Tokens[cmd]
	if !ok {
		return ""
	}
	if (cmd == "PRIVMSG" || cmd == "NOTICE") && strings.ContainsAny(p(0)[:min(len(p(0)), 1)], "@+=") {
		return "" // to a channel's ops or voiced: P10 has none such
	}
	params := append([]string{}, m.Params...)
	text := false
	switch cmd {
	case "PRIVMSG", "NOTICE", "KILL", "QUIT", "AWAY", "ERROR":
		text = true
	case "PART":
		text = len(params) > 1
	case "KICK":
		text = len(params) > 2
	}
	for i, a := range params {
		if !(text && i == len(params)-1) {
			params[i] = n.id(a)
		}
	}
	return p10Line(src, token, text, params...)
}

// p10In is one line come in by a P10 link: as TS6's, and as the P10 one
// for the other P10 links (ts6 "": it goes only to them).
type p10In struct{ ts6, p10 string }

// fromP10 is a line from a P10 link as TS6's (nil: taken here, or
// dropped).  The hub's lock is held.
func (h *Hub) fromP10(l *link, raw string) []p10In {
	source, token, params := parseP10(raw)
	if token == "" {
		return nil
	}
	p := func(i int) string {
		if i >= 0 && i < len(params) {
			return params[i]
		}
		return ""
	}
	last := p(len(params) - 1)
	n := h.nums
	if source == "" {
		source = n.server(l.srv.sid)
	}
	src := n.p10ID(source)
	if src == source {
		switch {
		case h.names[fold(source)] != nil: // :name, as old servers had it
			src = h.names[fold(source)].sid
		case h.nicks[fold(source)] != nil:
			src = h.nicks[fold(source)].uid
		default:
			return nil // a source not known
		}
	}
	me := n.server(h.me.sid)
	one := func(command string, ps ...string) []p10In {
		return []p10In{{line(src, command, ps...), raw}}
	}
	onlyP10 := []p10In{{"", raw}}
	switch token {
	case "G", "PING":
		// G origin [dest [ts]]: AsLL's, answered as ircu answers
		if len(params) >= 3 {
			l.write(p10Line(me, "Z", false, me, p(0), p(2), "0", p(2)))
		} else {
			l.write(p10Line(me, "Z", true, p(0), source))
		}
		return nil
	case "Z", "PONG":
		return nil
	case "EB", "END_OF_BURST":
		if h.servers[src] == l.srv && !l.bursted {
			h.burst(l) // with its EA
		}
		return onlyP10
	case "EA", "END_OF_BURST_ACK":
		return onlyP10
	case "Y", "ERROR":
		return []p10In{{line("", "ERROR", last), raw}}
	case "S", "SERVER":
		// S name hops start link proto numeric+mask +flags :desc
		if len(params) < 7 || len(p(5)) < 2 {
			return nil
		}
		num := p(5)[:2]
		sid := n.sidOf(num, func(s string) bool { return h.servers[s] != nil })
		if sid == "" {
			return nil
		}
		return one("SID", p(0), p(1), sid, last)
	case "N", "NICK":
		if len(src) == 9 {
			// a nick change: N nick ts
			ts := p(1)
			if ts == "" {
				ts = strconv.FormatInt(time.Now().Unix(), 10)
			}
			return one("NICK", p(0), ts)
		}
		// N nick hops ts ident host [+modes [args]] ip numeric :info
		if len(params) < 8 {
			return nil
		}
		num := params[len(params)-2]
		if !userNumRE.MatchString(num) || num[:2] != source {
			return nil
		}
		uid := n.uidOf(num)
		if uid == "" {
			return nil
		}
		umodes, account := "+", "*"
		if len(params) >= 9 && strings.HasPrefix(p(5), "+") {
			args := params[6 : len(params)-3]
			for _, r := range p(5)[1:] {
				switch r {
				case 'r', 'h':
					if len(args) > 0 {
						if r == 'r' {
							account, _, _ = strings.Cut(args[0], ":")
						}
						args = args[1:]
					}
				case 'i', 'w', 'o', 'd':
					umodes += string(r)
				}
			}
		}
		return one("EUID", p(0), p(1), p(2), umodes, p(3), p(4), b64ToIP(params[len(params)-3]), uid, "*", account, last)
	case "Q", "QUIT":
		return one("QUIT", last)
	case "D", "KILL":
		return one("KILL", n.p10ID(p(0)), last)
	case "SQ", "SQUIT":
		target := p(0)
		if sid := n.knownServer(target); sid != "" {
			target = sid
		}
		return one("SQUIT", target, last)
	case "B", "BURST":
		return h.p10Burst(src, raw, params)
	case "C", "CREATE":
		var out []p10In
		for _, ch := range strings.Split(p(0), ",") {
			if ch == "" || len(src) != 9 {
				continue
			}
			out = append(out, p10In{line(src[:3], "SJOIN", p(1), ch, "+", "@"+src), p10Line(source, "C", false, ch, p(1))})
		}
		return out
	case "J", "JOIN":
		if p(0) == "0" {
			return one("JOIN", "0")
		}
		var out []p10In
		for _, ch := range strings.Split(p(0), ",") {
			if ch == "" {
				continue
			}
			ts := p(1)
			if ts == "" {
				if c := h.chans[fold(ch)]; c != nil {
					ts = strconv.FormatInt(c.ts, 10)
				} else {
					ts = strconv.FormatInt(time.Now().Unix(), 10)
				}
			}
			out = append(out, p10In{line(src, "JOIN", ts, ch, "+"), p10Line(source, "J", false, ch, ts)})
		}
		return out
	case "L", "PART":
		var out []p10In
		for _, ch := range strings.Split(p(0), ",") {
			if ch == "" {
				continue
			}
			ps := []string{ch}
			if len(params) > 1 {
				ps = append(ps, last)
			}
			out = append(out, p10In{line(src, "PART", ps...), p10Line(source, "L", len(ps) > 1, ps...)})
		}
		return out
	case "K", "KICK":
		return one("KICK", p(0), n.p10ID(p(1)), last)
	case "T", "TOPIC":
		if len(params) < 2 {
			return nil
		}
		return one("TOPIC", p(0), last)
	case "A", "AWAY":
		if len(params) == 0 || last == "" {
			return one("AWAY")
		}
		return one("AWAY", last)
	case "AC", "ACCOUNT":
		// AC target account [...] (older: AC target R account)
		account := p(1)
		if (account == "R" || account == "M") && len(params) > 2 {
			account = p(2)
		}
		if uid := n.knownUser(p(0)); uid != "" {
			if u := h.users[uid]; u != nil && u.srv.link == l {
				u.account = account
			}
		}
		return onlyP10
	case "M", "MODE", "OM", "OPMODE":
		ch := h.chans[fold(p(0))]
		if ch == nil || len(params) < 2 {
			return onlyP10
		}
		args := []string{strconv.FormatInt(ch.ts, 10), ch.name}
		for _, a := range params[1:] {
			args = append(args, n.p10ID(a))
		}
		if len(src) == 3 && len(args) > 3 {
			if _, err := strconv.Atoi(args[len(args)-1]); err == nil && !strings.ContainsAny(p(1), "lk") {
				args = args[:len(args)-1] // a server's MODE ends with the channel's time
			}
		}
		return []p10In{{line(src, "TMODE", args...), raw}}
	case "CP", "CPRIVMSG", "CN", "CNOTICE":
		// CP target #channel :text
		command := "PRIVMSG"
		if token == "CN" || token == "CNOTICE" {
			command = "NOTICE"
		}
		return one(command, n.p10ID(p(0)), last)
	}
	if len(token) == 3 && token[0] >= '0' && token[0] <= '9' {
		ps := append([]string{}, params...)
		if len(ps) > 0 {
			ps[0] = n.p10ID(ps[0])
		}
		return one(token, ps...)
	}
	command, ok := p10Commands[token]
	if !ok {
		return onlyP10 // modes, G-lines, jupes, ...: to the other P10 links
	}
	ps := append([]string{}, params...)
	for i := range ps {
		if i == len(ps)-1 && (command == "PRIVMSG" || command == "NOTICE" || command == "WHOIS") {
			break // text, or a nick
		}
		ps[i] = n.p10ID(ps[i])
	}
	return one(command, ps...)
}

// p10Burst is BURST: a channel's modes and members, as SJOIN (bans go
// only to the other P10 links).
func (h *Hub) p10Burst(src, raw string, params []string) []p10In {
	if len(params) < 2 || len(src) != 3 {
		return nil
	}
	name, ts := params[0], params[1]
	rest := params[2:]
	modes := []string{"+"}
	if len(rest) > 0 && strings.HasPrefix(rest[0], "+") {
		modes = []string{rest[0]}
		want := 0
		for _, r := range rest[0] {
			if strings.ContainsRune("klAUL", r) {
				want++
			}
		}
		rest = rest[1:]
		for ; want > 0 && len(rest) > 0; want-- {
			modes = append(modes, rest[0])
			rest = rest[1:]
		}
	}
	var members []string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "%") {
		prefix := ""
		for _, m := range strings.Split(rest[0], ",") {
			num, mode, ok := strings.Cut(m, ":")
			if ok {
				prefix = ""
				if strings.ContainsAny(mode, "o0123456789") {
					prefix += "@"
				}
				if strings.ContainsRune(mode, 'v') {
					prefix += "+"
				}
			}
			if uid := h.nums.knownUser(num); uid != "" {
				members = append(members, prefix+uid)
			}
		}
	}
	if len(members) == 0 {
		return []p10In{{"", raw}}
	}
	ps := append([]string{ts, name}, modes...)
	ps = append(ps, strings.Join(members, " "))
	return []p10In{{line(src, "SJOIN", ps...), raw}}
}

// p10Handshake is the BBS's PASS and SERVER, for a P10 link.
func (l *link) p10Handshake() {
	h := l.h
	l.write("PASS :" + l.peer.SendPassword)
	l.write(p10Line("", "SERVER", true, h.me.name, "1", strconv.FormatInt(h.started.Unix(), 10),
		strconv.FormatInt(time.Now().Unix(), 10), "J10", h.nums.server(h.me.sid)+"]]]", "+h", h.me.desc))
}
