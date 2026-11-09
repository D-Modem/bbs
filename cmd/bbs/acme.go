package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/D-Modem/bbs/internal/acme"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/server"
	"github.com/D-Modem/bbs/internal/sockets"
)

// acmeCmd is bbs acme: list shows the ACME client's certificates, renew
// gets them now (those due, or -force all, or those named).
func acmeCmd(cfg *server.Config, args []string) {
	if cfg.ACME == nil {
		fatal(errors.New("no [acme] section in the configuration"))
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	lg := log.New(os.Stderr, "", 0)
	m, err := acme.New(*cfg.ACME, lg)
	if err != nil {
		fatal(err)
	}
	switch args[0] {
	case "list":
		acmeList(m, os.Stdout)
	case "renew":
		fs := flag.NewFlagSet("acme renew", flag.ExitOnError)
		force := fs.Bool("force", false, "get them even if they are not due")
		fs.Parse(args[1:])
		if err := acmeRenew(m, *force, fs.Args()); err != nil {
			fatal(err)
		}
	default:
		usage()
	}
}

// acmeList prints each certificate: its names, how it is proved, and how
// long it has.
func acmeList(m *acme.Manager, w io.Writer) {
	fmt.Fprintf(w, "CA %s, account %s\n", m.S.CA, or(m.S.Email, "(no email)"))
	for _, st := range m.Status() {
		how := st.Challenge
		if how == acme.DNS01 {
			how += " (" + st.DNS + ")"
		}
		fmt.Fprintf(w, "%s: %s, by %s, key %s\n", st.Name, strings.Join(st.Domains, ", "), how, st.KeyType)
		if st.Have {
			fmt.Fprintf(w, "  from %s until %s (%d days)\n", st.Issuer, st.NotAfter.Format(time.DateOnly),
				int(time.Until(st.NotAfter).Hours()/24))
		}
		if st.Due {
			fmt.Fprintf(w, "  due: %s\n", st.Why)
		}
		fmt.Fprintf(w, "  %s, %s\n", st.CertFile, st.KeyFile)
	}
}

// acmeRenew gets certificates now. HTTP-01 is answered on http_address
// (or :80) while it runs, or, with that port bbs serve's, by bbs serve:
// the challenges are put in the acme directory for it, TLS-ALPN-01's
// too. A running bbs serve reads what it gets within a minute.
func acmeRenew(m *acme.Manager, force bool, names []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	certs.SetManager(m)
	for _, n := range names {
		if !m.Has(n) {
			return fmt.Errorf("no certificate %s (%s)", n, strings.Join(m.S.Names(), ", "))
		}
	}
	http01 := false
	for _, c := range m.S.Certs {
		http01 = http01 || c.Challenge == acme.HTTP01
	}
	if http01 {
		addr := or(m.S.HTTPAddress, ":80")
		ln, err := sockets.Listen("tcp", addr)
		if err != nil {
			// a running bbs serve answers for this process (the
			// challenges go through the acme directory)
			fmt.Fprintf(os.Stderr, "bbs: %s is in use, so HTTP-01 is left to bbs serve (%v)\n", addr, err)
		} else {
			hs := &http.Server{Handler: m, ReadHeaderTimeout: 10 * time.Second}
			go hs.Serve(ln)
			defer hs.Close()
		}
	}
	failed := 0
	for _, c := range m.S.Certs {
		if len(names) > 0 && !slices.Contains(names, c.Name) {
			continue
		}
		due, why := m.Due(c.Name)
		if !due && !force && len(names) == 0 {
			fmt.Printf("%s: not due\n", c.Name)
			continue
		}
		if why == "" {
			why = "asked for"
		}
		fmt.Printf("%s: getting a certificate for %s (%s)\n", c.Name, strings.Join(c.Domains, ", "), why)
		if err := m.Obtain(ctx, c.Name); err != nil {
			fmt.Fprintf(os.Stderr, "bbs: %s: %v\n", c.Name, err)
			failed++
			continue
		}
		crt, _ := m.Files(c.Name)
		fmt.Printf("%s: got it: %s\n", c.Name, crt)
	}
	if failed > 0 {
		return fmt.Errorf("%d certificate(s) not got", failed)
	}
	return nil
}
