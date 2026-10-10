// Command bbs is a bulletin board system for telnet, TN3270 and modem
// callers.
//
//	bbs [-config FILE] [serve]         answer callers
//	bbs [-config FILE] check           check the configuration
//	bbs [-config FILE] init            make the database, with a first area
//	bbs [-config FILE] user add NAME [-sysop]
//	bbs [-config FILE] user passwd|lock|unlock|sysop|unsysop|del NAME
//	bbs [-config FILE] user list
//	bbs [-config FILE] area add NAME [DESCRIPTION] [-sysop-only]
//	bbs [-config FILE] area del NAME
//	bbs [-config FILE] area list
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/syslog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/server"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  bbs [-config FILE] [serve]         answer callers
  bbs [-config FILE] check           check the configuration
  bbs [-config FILE] init            make the database, with a first area
  bbs [-config FILE] user add NAME [-sysop]
  bbs [-config FILE] user passwd|lock|unlock|sysop|unsysop|del NAME
  bbs [-config FILE] user list
  bbs [-config FILE] area add NAME [DESCRIPTION] [-sysop-only]
  bbs [-config FILE] area del NAME
  bbs [-config FILE] area list
`)
	os.Exit(2)
}

func main() {
	configPath := flag.String("config", "", "the configuration file (none: the defaults)")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()

	cfg, err := server.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		os.Exit(serve(cfg))
	case "check":
		for _, w := range cfg.Warnings {
			fmt.Println("warning:", w)
		}
		for _, l := range cfg.Listeners {
			extra := ""
			if l.Kind == "telnet" && l.TN3270 {
				extra = " + tn3270"
			}
			if l.Header {
				extra += ", header"
			}
			if l.TrustUser {
				extra += ", trust_user"
			}
			fmt.Printf("[listen %s] %s %s%s\n", l.Name, l.Kind, l.Address, extra)
		}
		fmt.Println("database:", cfg.Database)
		fmt.Println("configuration OK")
	case "init":
		st := open(cfg)
		defer st.Close()
		if areas, _ := st.Areas(); len(areas) == 0 {
			if _, err := st.CreateArea("General", "Talk about anything", false); err != nil {
				fatal(err)
			}
			if _, err := st.CreateArea("Announcements", "News from the sysop", true); err != nil {
				fatal(err)
			}
		}
		fmt.Println("database ready:", cfg.Database)
		fmt.Println("make the sysop's account with: bbs user add NAME -sysop")
	case "user":
		userCmd(cfg, args)
	case "area":
		areaCmd(cfg, args)
	default:
		usage()
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bbs:", err)
	os.Exit(1)
}

func open(cfg *server.Config) *store.Store {
	st, err := store.Open(cfg.Database)
	if err != nil {
		fatal(err)
	}
	return st
}

func logger(spec string) (*log.Logger, error) {
	switch spec {
	case "", "stderr":
		return log.New(os.Stderr, "bbs: ", log.LstdFlags), nil
	case "none":
		return log.New(io.Discard, "", 0), nil
	case "syslog":
		w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "bbs")
		if err != nil {
			return nil, err
		}
		return log.New(w, "", 0), nil
	}
	f, err := os.OpenFile(spec, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}
	return log.New(f, "bbs: ", log.LstdFlags), nil
}

func serve(cfg *server.Config) int {
	lg, err := logger(cfg.Log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bbs: log:", err)
		return 1
	}
	for _, w := range cfg.Warnings {
		lg.Printf("warning: %s", w)
	}
	st := open(cfg)
	defer st.Close()
	b := bbs.New(cfg.Settings, st, lg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	srv := server.New(b, cfg.Listeners)
	srv.Graphics = cfg.Graphics
	srv.LogoFile = cfg.LogoFile
	if err := srv.Run(ctx); err != nil {
		lg.Printf("%v", err)
		return 1
	}
	return 0
}

// readPassword reads a new password twice from the terminal, or once from
// a pipe.
func readPassword(name string) string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			fatal(errors.New("no password on stdin"))
		}
		pw := strings.TrimRight(line, "\r\n")
		if msg := bbs.ValidPassword(pw, name); msg != "" {
			fatal(errors.New(msg))
		}
		return pw
	}
	for {
		fmt.Fprint(os.Stderr, "Password for "+name+": ")
		a, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fatal(err)
		}
		if msg := bbs.ValidPassword(string(a), name); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
			continue
		}
		fmt.Fprint(os.Stderr, "Again: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fatal(err)
		}
		if string(a) != string(b) {
			fmt.Fprintln(os.Stderr, "They differ.")
			continue
		}
		return string(a)
	}
}

// flags split from words: -sysop and -sysop-only may come anywhere
func split(args []string) (words []string, flags map[string]bool) {
	flags = map[string]bool{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags[strings.TrimLeft(a, "-")] = true
		} else {
			words = append(words, a)
		}
	}
	return words, flags
}

func userCmd(cfg *server.Config, args []string) {
	words, flags := split(args)
	if len(words) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	if words[0] == "list" {
		us, err := st.Users()
		if err != nil {
			fatal(err)
		}
		for _, u := range us {
			var tags []string
			if u.Sysop {
				tags = append(tags, "sysop")
			}
			if u.Locked {
				tags = append(tags, "locked")
			}
			if u.External {
				tags = append(tags, "external")
			}
			fmt.Printf("%-24s %5d calls %5d posts  %s\n", u.Name, u.Logins, u.Posts, strings.Join(tags, " "))
		}
		return
	}
	if len(words) != 2 {
		usage()
	}
	name := words[1]
	if words[0] == "add" {
		// a sysop may take a reserved name such as "sysop"
		if msg := bbs.ValidName(name); msg != "" && !(flags["sysop"] && bbs.Reserved(name)) {
			fatal(errors.New(msg))
		}
		pw := readPassword(name)
		u, err := st.CreateUser(name, pw, "", flags["sysop"])
		if err != nil {
			fatal(err)
		}
		fmt.Printf("added %s%s\n", u.Name, map[bool]string{true: " (sysop)"}[u.Sysop])
		return
	}
	u, err := st.UserByName(name)
	if err != nil {
		fatal(fmt.Errorf("%s: %w", name, err))
	}
	switch words[0] {
	case "passwd":
		err = st.SetPassword(u.ID, readPassword(u.Name))
	case "lock":
		err = st.SetLocked(u.ID, true)
	case "unlock":
		err = st.SetLocked(u.ID, false)
	case "sysop":
		err = st.SetSysop(u.ID, true)
	case "unsysop":
		err = st.SetSysop(u.ID, false)
	case "del":
		err = st.DeleteUser(u.ID)
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
	fmt.Println("done")
}

func areaCmd(cfg *server.Config, args []string) {
	words, flags := split(args)
	if len(words) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	switch words[0] {
	case "list":
		as, err := st.Areas()
		if err != nil {
			fatal(err)
		}
		for _, a := range as {
			only := ""
			if a.SysopOnly {
				only = " (sysop posts only)"
			}
			fmt.Printf("%-20s %5d messages  %s%s\n", a.Name, a.Messages, a.Description, only)
		}
	case "add":
		if len(words) < 2 || len(words) > 3 {
			usage()
		}
		desc := ""
		if len(words) == 3 {
			desc = words[2]
		}
		a, err := st.CreateArea(bbs.Clean(words[1], 30), bbs.Clean(desc, 60), flags["sysop-only"])
		if err != nil {
			fatal(err)
		}
		fmt.Println("added", a.Name)
	case "del":
		if len(words) != 2 {
			usage()
		}
		a, err := st.AreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		if err := st.DeleteArea(a.ID); err != nil {
			fatal(err)
		}
		fmt.Println("deleted", a.Name, "and its", a.Messages, "messages")
	default:
		usage()
	}
}
