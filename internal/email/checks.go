package email

import (
	"bytes"
	"context"
	"net"
	"strings"
	"time"

	"blitiri.com.ar/go/spf"
	"github.com/emersion/go-msgauth/authres"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/emersion/go-msgauth/dmarc"
)

// verdict is what the checks of a mail from elsewhere found: the
// Authentication-Results field to put on it, and why it is refused ("":
// it is not).
type verdict struct {
	field  string
	refuse string
}

// orgDomain is a domain's organizational domain, roughly: its last two
// labels, or three where the second is a short one under a country's
// (example.co.uk).  (The public suffix list would be exact.)
func orgDomain(d string) string {
	l := strings.Split(strings.ToLower(strings.TrimSuffix(d, ".")), ".")
	switch {
	case len(l) <= 2:
		return strings.Join(l, ".")
	case len(l[len(l)-1]) == 2 && len(l[len(l)-2]) <= 3:
		return strings.Join(l[len(l)-3:], ".")
	}
	return strings.Join(l[len(l)-2:], ".")
}

// aligned reports whether two domains are aligned as DMARC means it.
func aligned(a, b string, mode dmarc.AlignmentMode) bool {
	a, b = strings.ToLower(strings.TrimSuffix(a, ".")), strings.ToLower(strings.TrimSuffix(b, "."))
	if mode == dmarc.AlignmentStrict {
		return a == b
	}
	return orgDomain(a) == orgDomain(b)
}

// domainOf is an address's domain.
func domainOf(addr string) string {
	_, d, _ := strings.Cut(addr, "@")
	return strings.ToLower(d)
}

// check checks a mail from elsewhere: SPF for the address it came from,
// its DKIM signatures, and DMARC for its From.  A mail is refused if its
// From's domain asks for that (p=reject) and it fails, or, for a domain
// with no DMARC, if SPF fails and no signature of the domain holds.
func (s *Service) check(ip net.IP, helo, mailFrom string, raw []byte) verdict {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := s.Set.Resolver
	var results []authres.Result

	spfFor := mailFrom
	if spfFor == "" {
		spfFor = "postmaster@" + helo
	}
	spfRes, _ := spf.CheckHostWithSender(ip, helo, spfFor, spf.WithResolver(r), spf.WithContext(ctx))
	results = append(results, &authres.SPFResult{Value: authres.ResultValue(spfRes), From: mailFrom, Helo: helo})

	lookup := func(d string) ([]string, error) { return r.LookupTXT(ctx, d) }
	verifs, _ := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{LookupTXT: lookup, MaxVerifications: 5})
	var passed []string
	for _, v := range verifs {
		var val authres.ResultValue = authres.ResultPass
		switch {
		case v.Err == nil:
			passed = append(passed, v.Domain)
		case dkim.IsTempFail(v.Err):
			val = authres.ResultTempError
		case dkim.IsPermFail(v.Err):
			val = authres.ResultPermError
		default:
			val = authres.ResultFail
		}
		results = append(results, &authres.DKIMResult{Value: val, Domain: v.Domain})
	}
	if len(verifs) == 0 {
		results = append(results, &authres.DKIMResult{Value: authres.ResultNone})
	}

	var refuse string
	from := ""
	if p, err := parse(raw); err == nil {
		from = domainOf(p.fromAddr)
	}
	var rec *dmarc.Record
	if from != "" {
		rec, _ = dmarc.LookupWithOptions(from, &dmarc.LookupOptions{LookupTXT: lookup})
		if rec == nil && orgDomain(from) != from {
			rec, _ = dmarc.LookupWithOptions(orgDomain(from), &dmarc.LookupOptions{LookupTXT: lookup})
		}
	}
	if rec != nil {
		pass := spfRes == spf.Pass && aligned(domainOf(mailFrom), from, rec.SPFAlignment)
		for _, d := range passed {
			pass = pass || aligned(d, from, rec.DKIMAlignment)
		}
		val := authres.ResultValue(authres.ResultPass)
		if !pass {
			val = authres.ResultFail
			if rec.Policy == dmarc.PolicyReject {
				refuse = "DMARC: " + from + " says to refuse mail from it that fails"
			}
		}
		results = append(results, &authres.DMARCResult{Value: val, From: from})
	} else if spfRes == spf.Fail {
		ok := false
		for _, d := range passed {
			ok = ok || aligned(d, from, dmarc.AlignmentRelaxed)
		}
		if !ok {
			refuse = "SPF: " + domainOf(spfFor) + " does not let " + ip.String() + " send its mail"
		}
	}
	return verdict{field: "Authentication-Results: " + authres.Format(s.Set.Hostname, results) + "\r\n", refuse: refuse}
}
