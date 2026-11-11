package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbsca"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/server"
)

// caCmd is bbs ca: the BBS's own certificate authority.
//
//	bbs ca [show]                       where it is, its fingerprint, what it has signed
//	bbs ca init [-sha1]                 make it (-sha1: for Windows before XP SP3)
//	bbs ca subca NAME                   a sub-CA of the root's for a program beside
//	                                    the BBS (a TLS proxy's: proxy), made if none
//	bbs ca issue [-ca NAME] [-out DIR] [-days N] NAME...
//	                                    a certificate signed by a sub-CA (services)
//	bbs ca package [-out FILE]          the zip that installs it on callers' machines
//
// The root is the one callers' machines trust; it signs only sub-CAs, each
// with its own key: services signs the BBS's own servers' certificates,
// and others (proxy) programs beside it, which are given only their own.
func caCmd(cfg *server.Config, args []string) {
	dir := bbsca.Dir(cfg.DataDir())
	if len(args) == 0 {
		args = []string{"show"}
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("ca init", flag.ExitOnError)
		sha1 := fs.Bool("sha1", false, "sign with SHA-1, for Windows 95 to 2000 and XP before SP3 (which know no SHA-256)")
		fs.Parse(args[1:])
		ca, err := bbsca.Init(dir, cfg.Settings.Name, *sha1)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("made %s\n", filepath.Join(dir, "ca.crt"))
		caShow(ca, cfg)
		zip := caPackage(ca, cfg, "")
		fmt.Printf("\nfor callers' machines: %s\n", zip)
	case "show":
		ca, err := bbsca.Load(dir)
		if err != nil {
			fatal(err)
		}
		caShow(ca, cfg)
	case "subca":
		if len(args) != 2 {
			fatal(errors.New("ca subca: its name (proxy, say)"))
		}
		sub, made, err := caLoad(dir, cfg).Sub(args[1])
		if err != nil {
			fatal(err)
		}
		if made {
			fmt.Printf("made %s\n", sub.Cert.Subject.CommonName)
		}
		fmt.Printf("sub-CA:      %s, fingerprint %s\n", sub.Cert.Subject.CommonName, sub.Thumbprint())
		fmt.Printf("certificate: %s\n", filepath.Join(dir, args[1]+"-ca.crt"))
		fmt.Printf("key:         %s (keep it private: it signs for any name)\n", filepath.Join(dir, args[1]+"-ca.key"))
		fmt.Printf("root:        %s (send the sub-CA's certificate after each one it signs)\n", filepath.Join(dir, "ca.crt"))
	case "issue":
		fs := flag.NewFlagSet("ca issue", flag.ExitOnError)
		name := fs.String("ca", bbsca.Services, "the sub-CA that signs it")
		out := fs.String("out", ".", "the directory the certificate and key are written in")
		days := fs.Int("days", bbsca.LeafDays, "days it is good for")
		fs.Parse(args[1:])
		names := fs.Args()
		if len(names) == 0 {
			fatal(errors.New("ca issue: the names (or IP addresses) it is for"))
		}
		ca, _, err := caLoad(dir, cfg).Sub(*name)
		if err != nil {
			fatal(err)
		}
		cp, kp, err := ca.Issue(names, *days, nil)
		if err != nil {
			fatal(err)
		}
		base := filepath.Join(*out, strings.TrimPrefix(names[0], "*."))
		if err := os.WriteFile(base+".key", kp, 0o600); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(base+".crt", cp, 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("%s.crt (with the %s CA's after it) and %s.key, for %s\n", base, *name, base, strings.Join(names, ", "))
	case "package":
		fs := flag.NewFlagSet("ca package", flag.ExitOnError)
		out := fs.String("out", "", "the zip to write (default: in the CA's directory)")
		fs.Parse(args[1:])
		fmt.Println(caPackage(caLoad(dir, cfg), cfg, *out))
	default:
		usage()
	}
}

// caLoad is the CA, made first if there is none (said so).
func caLoad(dir string, cfg *server.Config) *bbsca.CA {
	ca, made, err := bbsca.LoadOrInit(dir, cfg.Settings.Name)
	if err != nil {
		fatal(err)
	}
	if made {
		fmt.Fprintf(os.Stderr, "bbs: made a certificate authority in %s\n", dir)
		caPackage(ca, cfg, "")
	}
	return ca
}

// caPackage writes the CA's zip for callers' machines (out "": NAME-CA.zip
// in its directory), and says where.
func caPackage(ca *bbsca.CA, cfg *server.Config, out string) string {
	z, err := ca.Package(cfg.Settings.Name)
	if err != nil {
		fatal(err)
	}
	if out == "" {
		out = caZipPath(ca, cfg)
	}
	if err := os.WriteFile(out, z, 0o644); err != nil {
		fatal(err)
	}
	return out
}

func caZipPath(ca *bbsca.CA, cfg *server.Config) string {
	return filepath.Join(ca.Dir, "install-ca.zip")
}

func caShow(ca *bbsca.CA, cfg *server.Config) {
	fmt.Printf("CA:          %s (%s)\n", ca.Cert.Subject.CommonName, filepath.Join(ca.Dir, "ca.crt"))
	fmt.Printf("fingerprint: %s (SHA-1)\n", ca.Thumbprint())
	fmt.Printf("until:       %s, signing with %v\n", ca.Cert.NotAfter.Format(time.DateOnly), ca.Cert.SignatureAlgorithm)
	for _, n := range ca.Subs() {
		if sub, _, err := ca.Sub(n); err == nil {
			fmt.Printf("%-12s %s (%s)\n", "sub-CA:", sub.Cert.Subject.CommonName, filepath.Join(ca.Dir, n+"-ca.crt"))
		}
	}
	for _, l := range caLeaves(cfg) {
		fmt.Printf("%-12s %s (tls_cert = ca)\n", l.label+":", strings.Join(l.names, ", "))
	}
	if _, err := os.Stat(caZipPath(ca, cfg)); err == nil {
		fmt.Printf("callers' machines: %s\n", caZipPath(ca, cfg))
	}
}

// caLeaf is a server whose certificate the CA signs (tls_cert = ca).
type caLeaf struct {
	label string
	names []string
	days  int // 0: bbsca.LeafDays
	set   func(certFile, keyFile string)
}

// caLeaves are the servers set to tls_cert = ca, with the names their
// clients ask for: the old services' HTTPS names (and an IP of their own,
// for a client given that), the Gemini server's host name.
func caLeaves(cfg *server.Config) []caLeaf {
	var out []caLeaf
	ip := func(addr string) []string {
		h, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil
		}
		if p := net.ParseIP(h); p != nil && !p.IsUnspecified() && !p.IsLoopback() {
			return []string{p.String()}
		}
		return nil
	}
	if m := cfg.MSN; m != nil && certs.IsCA(m.CertFile) {
		out = append(out, caLeaf{"msn", append([]string{"login.passport.com", "nexus.passport.com",
			"loginnet.passport.com", "login.live.com"}, ip(m.HTTPSAddress)...), 0,
			func(c, k string) { m.CertFile, m.KeyFile = c, k }})
	}
	if y := cfg.YMSG; y != nil && certs.IsCA(y.CertFile) {
		out = append(out, caLeaf{"ymsg", append([]string{"login.yahoo.com"}, ip(y.HTTPSAddress)...), 0,
			func(c, k string) { y.CertFile, y.KeyFile = c, k }})
	}
	if o := cfg.OSCAR; o != nil && certs.IsCA(o.TLSCert) {
		names := []string{"api.screenname.aol.com", "api.oscar.aol.com", "kdc.uas.aol.com",
			"slogin.oscar.aol.com", "login.oscar.aol.com", "login.messaging.aol.com", "slogin.icq.com", "login.icq.com"}
		if h, _, err := net.SplitHostPort(o.TLSHost); err == nil && h != "" && net.ParseIP(h) == nil {
			names = append(names, h)
		}
		out = append(out, caLeaf{"oscar", append(names, ip(o.TLSAddress)...), 0,
			func(c, k string) { o.TLSCert, o.TLSKey = c, k }})
	}
	if g := cfg.Gemini; g != nil && certs.IsCA(g.CertFile) {
		// Gemini clients pin what they first see: good for as long as the
		// CA, its key kept
		out = append(out, caLeaf{"gemini", []string{strings.ToLower(g.Host)}, 20 * 365,
			func(c, k string) { g.CertFile, g.KeyFile = c, k }})
	}
	return out
}

// caServe signs the certificates of the servers set to tls_cert = ca (the
// CA made first if there is none) and names their files in cfg, before
// they start; then keeps them signed, looking daily, until ctx is done
// (a server reads its files again when they change).
func caServe(ctx context.Context, cfg *server.Config, lg *log.Logger) error {
	leaves := caLeaves(cfg)
	if len(leaves) == 0 {
		return nil
	}
	dir := bbsca.Dir(cfg.DataDir())
	root, made, err := bbsca.LoadOrInit(dir, cfg.Settings.Name)
	if err != nil {
		return fmt.Errorf("ca: %w", err)
	}
	if made {
		lg.Printf("ca: made a certificate authority in %s (fingerprint %s)", dir, root.Thumbprint())
	}
	if z, err := root.Package(cfg.Settings.Name); err == nil {
		if err := os.WriteFile(caZipPath(root, cfg), z, 0o644); err != nil {
			lg.Printf("ca: %v", err)
		}
	}
	// the root signs only sub-CAs: the services one signs the servers'
	ca, _, err := root.Sub(bbsca.Services)
	if err != nil {
		return fmt.Errorf("ca: %w", err)
	}
	ensure := func() error {
		for _, l := range leaves {
			c, k, renewed, err := ca.Ensure(l.label, l.names, l.days)
			if err != nil {
				return fmt.Errorf("ca: %s: %w", l.label, err)
			}
			if renewed {
				lg.Printf("ca: signed %s's certificate for %s", l.label, strings.Join(l.names, ", "))
			}
			l.set(c, k)
		}
		return nil
	}
	if err := ensure(); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := ensure(); err != nil {
					lg.Printf("%v", err)
				}
			}
		}
	}()
	return nil
}
