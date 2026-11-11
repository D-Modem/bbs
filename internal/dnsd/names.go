package dnsd

import (
	"slices"
	"strings"
)

// Services are the front ends whose old servers' names the DNS server can
// answer with the BBS's address, and those names.  A name beginning "*."
// is every name under it (not the name itself).
var Services = map[string][]string{
	// MSN Messenger 1.0 to 7.5: the messaging server, and Passport's
	// nexus and logins (MSN 5.0 and later)
	"msn": {
		"messenger.hotmail.com",
		"nexus.passport.com",
		"login.passport.com",
		"loginnet.passport.com",
		"login.live.com",
	},
	// The Microsoft Network of 1995 to 1998 (MSN Classic): the gateway in
	// its Dial-Up Networking settings
	"marvel": {
		"gateway.moswest.msn.net",
	},
	// AIM and ICQ: BUCP and the later logins (Kerberos, clientLogin and
	// startOSCARSession)
	"oscar": {
		"login.oscar.aol.com",
		"slogin.oscar.aol.com",
		"login.messaging.aol.com",
		"kdc.uas.aol.com",
		"api.screenname.aol.com",
		"api.oscar.aol.com",
		"login.icq.com",
		"slogin.icq.com",
	},
	// AOL's own client (3.0 and later, by TCP)
	"aol": {
		"americaonline.aol.com",
	},
	// Yahoo! Messenger: the pager, the web half and the HTTPS login
	"ymsg": {
		"scs.msg.yahoo.com",
		"scsa.msg.yahoo.com",
		"scsb.msg.yahoo.com",
		"scsc.msg.yahoo.com",
		"cs.yahoo.com",
		"vcs1.msg.yahoo.com",
		"vcs2.msg.yahoo.com",
		"insider.msg.yahoo.com",
		"relay.msg.yahoo.com",
		"relay.yahoo.com",
		"filetransfer.msg.yahoo.com",
		"login.yahoo.com",
	},
	// CompuServe: the host CIM and terminal programs reached it at over
	// the internet
	"compuserve": {
		"compuserve.com",
		"cis.compuserve.com",
	},
}

// ServiceNames are Services' keys, sorted.
func ServiceNames() []string {
	var out []string
	for k := range Services {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Table is the names answered with the BBS's address.
type Table struct {
	exact  map[string]string // name: whose
	suffix map[string]string // ".domain": whose
}

// NewTable is a table of the services' names and extra names of the
// sysop's (given as "sysop").
func NewTable(services []string, extra []string) *Table {
	t := &Table{exact: map[string]string{}, suffix: map[string]string{}}
	add := func(name, whose string) {
		name = canonical(name)
		if strings.HasPrefix(name, "*.") {
			t.suffix[name[1:]] = whose
			return
		}
		t.exact[name] = whose
	}
	for _, s := range services {
		for _, n := range Services[s] {
			add(n, s)
		}
	}
	for _, n := range extra {
		add(n, "sysop")
	}
	return t
}

// Match is whose name is (the service, "sysop", or "": not answered here).
func (t *Table) Match(name string) string {
	if t == nil {
		return ""
	}
	name = canonical(name)
	if w, ok := t.exact[name]; ok {
		return w
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			if w, ok := t.suffix[name[i:]]; ok {
				return w
			}
		}
	}
	return ""
}

// Len is how many names (and wildcards) the table has.
func (t *Table) Len() int { return len(t.exact) + len(t.suffix) }

// canonical is a name in lower case without its final dot.
func canonical(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}
