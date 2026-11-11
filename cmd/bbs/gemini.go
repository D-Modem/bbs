package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/gemini"
	"github.com/D-Modem/bbs/internal/server"
)

// geminiCmd is bbs gemini: cert makes the Gemini server's certificate,
// asking the sysop what goes in it.
func geminiCmd(cfg *server.Config, args []string) {
	if len(args) == 0 || args[0] != "cert" {
		usage()
	}
	fs := flag.NewFlagSet("gemini cert", flag.ExitOnError)
	force := fs.Bool("force", false, "replace a certificate there already (clients will warn)")
	fs.Parse(args[1:])
	g := cfg.Gemini
	if g == nil {
		fatal(errors.New("no [gemini] section in the configuration"))
	}
	if certs.IsCA(g.CertFile) {
		fatal(errors.New("[gemini] cert_file = ca: the BBS's CA signs it when the BBS starts (bbs ca show)"))
	}
	if err := geminiCert(g, cfg.Settings.Name, *force, os.Stdin, os.Stderr); err != nil {
		fatal(err)
	}
}

// geminiCert asks on in (prompts to out) for the host names, organization
// and years of a new certificate, shows what it will be, and makes it
// once the sysop says yes.
func geminiCert(g *gemini.Settings, org string, force bool, in io.Reader, out io.Writer) error {
	_, cerr := os.Stat(g.CertFile)
	_, kerr := os.Stat(g.KeyFile)
	there := cerr == nil || kerr == nil
	if there && !force {
		return fmt.Errorf("there is a certificate already (%s: %s); -force to replace it, and clients that know it will warn",
			g.CertFile, gemini.CertInfo(g.CertFile))
	}
	r := bufio.NewReader(in)
	ask := func(q, def string) (string, error) {
		if def != "" {
			fmt.Fprintf(out, "%s [%s]: ", q, def)
		} else {
			fmt.Fprintf(out, "%s: ", q)
		}
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no answer")
		}
		if line = strings.TrimSpace(line); line != "" {
			return line, nil
		}
		return def, nil
	}
	fmt.Fprintln(out, "A certificate for the Gemini server, made here and signed by itself.")
	fmt.Fprintln(out, "Clients trust it the first time they connect and warn if it ever changes:")
	fmt.Fprintln(out, "make it once, and keep it (and its key) with your backups.")
	var req gemini.CertRequest
	for {
		names, err := ask("Host names callers use, by commas", g.Host)
		if err != nil {
			return err
		}
		req.Names = nil
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				req.Names = append(req.Names, strings.ToLower(n))
			}
		}
		if req.Org, err = ask("Organization (- for none)", org); err != nil {
			return err
		}
		if req.Org == "-" {
			req.Org = ""
		}
		years, err := ask("Years it is good for", "20")
		if err != nil {
			return err
		}
		if req.Years, err = strconv.Atoi(years); err != nil {
			req.Years = 0
		}
		if err := req.Check(); err != nil {
			fmt.Fprintln(out, err)
			continue
		}
		break
	}
	fmt.Fprintf(out, "\nFor:      %s\n", strings.Join(req.Names, ", "))
	if req.Org != "" {
		fmt.Fprintf(out, "Org:      %s\n", req.Org)
	}
	fmt.Fprintf(out, "Until:    %s\n", time.Now().AddDate(req.Years, 0, 0).Format("2006-01-02"))
	fmt.Fprintf(out, "Files:    %s, %s\n", g.CertFile, g.KeyFile)
	if there {
		fmt.Fprintln(out, "Replaces: "+gemini.CertInfo(g.CertFile)+" (kept as .old)")
	}
	yes, err := ask("Make it? (yes/no)", "no")
	if err != nil {
		return err
	}
	if y := strings.ToLower(yes); y != "y" && y != "yes" {
		return errors.New("not made")
	}
	if there {
		for _, f := range []string{g.CertFile, g.KeyFile} {
			if err := os.Rename(f, f+".old"); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	if err := gemini.MakeCert(g.CertFile, g.KeyFile, req); err != nil {
		return err
	}
	fmt.Fprintln(out, "Made: "+gemini.CertInfo(g.CertFile))
	fmt.Fprintln(out, "The BBS serves it from its next start.")
	return nil
}
