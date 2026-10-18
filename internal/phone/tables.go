package phone

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// The tables are libphonenumber's (metadata.go, places.gz: go run
// ./internal/phone/gen), read here.

// Kind is what a number is, as its country's plan says.
type Kind string

const (
	FixedLine  Kind = "fixed line"
	Mobile     Kind = "mobile"
	TollFree   Kind = "toll-free"
	Premium    Kind = "premium rate"
	SharedCost Kind = "shared cost"
	VoIP       Kind = "VoIP"
	Personal   Kind = "personal"
	Pager      Kind = "pager"
	UAN        Kind = "UAN"
	Voicemail  Kind = "voicemail"
	// FixedLineOrMobile: a plan whose fixed and mobile numbers are alike
	// (the NANP's)
	FixedLineOrMobile Kind = "fixed line or mobile"
)

// A place's kinds: a number that is one is somewhere.
func (k Kind) placed() bool { return k == FixedLine || k == Mobile || k == FixedLineOrMobile }

type kindPattern struct {
	kind    Kind
	pattern string
}

type numberFormat struct {
	pattern, leading, format, intl string
}

// territory is a country's (or a non-geographic code's, id "001")
// numbering plan.
type territory struct {
	id, code  string
	main      bool   // the code's own country (the first of those sharing it)
	intl      string // the prefix for calls abroad (a pattern)
	national  string // the trunk prefix
	parsing   string // the national prefix as it is taken off (a pattern; "": national)
	transform string // and what replaces it, if anything
	leading   string // a shared code's: how its numbers begin
	general   string // any number's pattern
	kinds     []kindPattern
	formats   []numberFormat

	// compiled, when first wanted
	once                                    sync.Once
	intlRE, parsingRE, leadingRE, generalRE *regexp.Regexp
	kindRE                                  []*regexp.Regexp
	formatRE                                [][2]*regexp.Regexp // pattern, leading
}

var (
	compileOnce sync.Once
	byID        map[string]*territory
	byCode      map[string][]*territory // the main country first
)

// prefixRE is a pattern that matches at the start.
func prefixRE(p string) *regexp.Regexp {
	if p == "" {
		return nil
	}
	return regexp.MustCompile(`^(?:` + p + `)`)
}

// wholeRE is a pattern that matches the whole.
func wholeRE(p string) *regexp.Regexp { return regexp.MustCompile(`^(?:` + p + `)$`) }

// compile indexes the territories, once: byID (not 001, the
// non-geographic ones) and byCode, each code's main country first. Their
// patterns are compiled later, by prepare.
func compile() {
	compileOnce.Do(func() {
		byID, byCode = map[string]*territory{}, map[string][]*territory{}
		for i := range territories {
			t := &territories[i]
			if t.id != "001" {
				byID[t.id] = t
			}
			if t.main {
				byCode[t.code] = append([]*territory{t}, byCode[t.code]...)
			} else {
				byCode[t.code] = append(byCode[t.code], t)
			}
		}
	})
}

// prepare compiles a territory's patterns.
func (t *territory) prepare() *territory {
	t.once.Do(func() {
		t.intlRE, t.leadingRE, t.generalRE = prefixRE(t.intl), prefixRE(t.leading), wholeRE(t.general)
		switch {
		case t.parsing != "":
			t.parsingRE = prefixRE(t.parsing)
		case t.national != "":
			t.parsingRE = prefixRE(regexp.QuoteMeta(t.national))
		}
		for _, k := range t.kinds {
			t.kindRE = append(t.kindRE, wholeRE(k.pattern))
		}
		for _, f := range t.formats {
			t.formatRE = append(t.formatRE, [2]*regexp.Regexp{wholeRE(f.pattern), prefixRE(f.leading)})
		}
	})
	return t
}

// kindOf is what a national number is in the plan ("": none of its).
func (t *territory) kindOf(n string) Kind {
	t.prepare()
	if !t.generalRE.MatchString(n) {
		return ""
	}
	var fixed, mobile bool
	for i, k := range t.kinds {
		if !t.kindRE[i].MatchString(n) {
			continue
		}
		switch k.kind {
		case FixedLine:
			fixed = true
		case Mobile:
			mobile = true
		default:
			return k.kind
		}
	}
	switch {
	case fixed && mobile:
		return FixedLineOrMobile
	case fixed:
		return FixedLine
	case mobile:
		return Mobile
	}
	return ""
}

// stripNational takes the trunk prefix off a number dialed at home, as
// libphonenumber does: not where the number was whole without it.
func (t *territory) stripNational(d string) string {
	t.prepare()
	if t.parsingRE == nil {
		return d
	}
	m := t.parsingRE.FindStringSubmatchIndex(d)
	if m == nil {
		return d
	}
	viable := t.generalRE.MatchString(d)
	groups := len(m)/2 - 1
	var out string
	if t.transform == "" || groups == 0 || m[2*groups] < 0 {
		out = d[m[1]:]
	} else {
		out = string(t.parsingRE.ExpandString(nil, t.transform, d, m)) + d[m[1]:]
	}
	if viable && !t.generalRE.MatchString(out) {
		return d
	}
	return out
}

// formatIntl is a national number as it is written abroad ("" if the
// plan has no way).
func (t *territory) formatIntl(n string) string {
	t.prepare()
	for i, f := range t.formats {
		re := t.formatRE[i]
		if f.intl == "NA" || (re[1] != nil && !re[1].MatchString(n)) || !re[0].MatchString(n) {
			continue
		}
		return re[0].ReplaceAllString(n, dollars.ReplaceAllString(f.intl, "$${$1}"))
	}
	return ""
}

var dollars = regexp.MustCompile(`\$(\d)`)

// nonGeographic names the codes that are no country's.
var nonGeographic = map[string]string{
	"800": "international freephone",
	"808": "international shared cost",
	"870": "Inmarsat",
	"878": "universal personal",
	"881": "global satellite",
	"882": "international networks",
	"883": "international networks",
	"888": "humanitarian",
	"979": "international premium",
}

// nanpArea is a North American area code as NANPA has it (nanpa.go):
// whose, where, what for if it is no place's, and from when if it is not
// in service yet ("YYYY-MM-DD").
type nanpArea struct {
	country, region, service, from string
}

// nanpAreaOf is an area code in service on the day.
func nanpAreaOf(code string, day time.Time) (nanpArea, bool) {
	a, ok := nanpAreas[code]
	if !ok || a.from > day.Format("2006-01-02") {
		return nanpArea{}, false
	}
	return a, true
}

// NANPADate is the day of the NANPA report the area codes are from.
func NANPADate() string { return nanpaDate }

// Version is the libphonenumber the tables are from.
func Version() string { return strings.TrimSpace(dataVersion) }
