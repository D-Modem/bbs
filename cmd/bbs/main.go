// Command bbs is a bulletin board system for telnet, TN3270 and modem
// callers.
//
//	bbs [-config FILE] [serve]         answer callers
//	bbs [-config FILE] check           check the configuration
//	bbs -config FILE config [get|set|unset SECTION KEY [VALUE]]   edit it
//	bbs [-config FILE] init            make the database, with a first area
//	bbs [-config FILE] user add NAME [-sysop]
//	bbs [-config FILE] user passwd|lock|unlock|sysop|unsysop|del NAME
//	bbs [-config FILE] user list
//	bbs [-config FILE] area add NAME [DESCRIPTION] [-sysop-only]
//	bbs [-config FILE] area del NAME
//	bbs [-config FILE] area list
//	bbs [-config FILE] area newsgroup AREA GROUP|-
//	bbs [-config FILE] news sync [PEER]
//	bbs [-config FILE] area echo AREA TAG|-
//	bbs [-config FILE] area group AREA GROUP|-
//	bbs [-config FILE] area aliases AREA yes|no
//	bbs [-config FILE] area keep AREA MESSAGES DAYS
//	bbs [-config FILE] area purge
//	bbs [-config FILE] fido poll|scan|toss [LINK]
//	bbs [-config FILE] fido node ADDRESS | nodes WORDS...
//	bbs [-config FILE] fido areafix|filefix LINK COMMAND...
//	bbs [-config FILE] fido subs LINK | link|unlink|filelink|fileunlink LINK TAG...
//	bbs [-config FILE] user qwknode|unqwknode NAME
//	bbs [-config FILE] qwknet call [NET]
//	bbs [-config FILE] room list|add|del|topic [ROOM] [TOPIC]
//	bbs [-config FILE] token add USER [-scope read,post,...] [-expires DAYS] [-name NAME]
//	bbs [-config FILE] token list [USER]
//	bbs [-config FILE] token revoke ID
//	bbs [-config FILE] filearea add NAME [DESCRIPTION] [-sysop-upload]
//	bbs [-config FILE] filearea del|list [NAME]
//	bbs [-config FILE] filearea echo AREA TAG|-
//	bbs [-config FILE] file add AREA PATH [DESCRIPTION] [-as NAME]
//	bbs [-config FILE] file list AREA
//	bbs [-config FILE] file del AREA NAME
//	bbs [-config FILE] chan board add|set|list|del ... | chan ban add|list|del ...
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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/Neo-Desktop/bbs-sec/internal/api"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/fido"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/finger"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/ftp"
	"github.com/Neo-Desktop/bbs-sec/internal/gopher"
	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/ircd"
	"github.com/Neo-Desktop/bbs-sec/internal/mrc"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/nntp"
	"github.com/Neo-Desktop/bbs-sec/internal/qwknet"
	"github.com/Neo-Desktop/bbs-sec/internal/script"
	"github.com/Neo-Desktop/bbs-sec/internal/server"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  bbs [-config FILE] [serve]         answer callers
  bbs [-config FILE] check           check the configuration
  bbs -config FILE config                         the configuration screens (on this terminal)
  bbs -config FILE config get SECTION [KEY]       a section's keys, or a key's value
  bbs -config FILE config set SECTION KEY VALUE   (SECTION: general, "listen telnet" ...)
  bbs -config FILE config unset SECTION KEY       checked as the BBS would read it; kill -HUP
                                                  (systemctl reload bbs) puts the live keys in force
  bbs [-config FILE] init            make the database, with a first area
  bbs [-config FILE] user add NAME [-sysop]
  bbs [-config FILE] user passwd|lock|unlock|sysop|unsysop|del NAME
  bbs [-config FILE] user list
  bbs [-config FILE] user level NAME N              access level, 0-255
  bbs [-config FILE] user flags NAME +AB -C         access flags (or AB: exactly those)
  bbs [-config FILE] user validate NAME             to validated_level
  bbs [-config FILE] user waiting                   users under validated_level
  bbs [-config FILE] area add NAME [DESCRIPTION] [-sysop-only]
  bbs [-config FILE] area del NAME
  bbs [-config FILE] area list
  bbs [-config FILE] area require AREA READ POST    access requirements ("30 F", sysop, -: none)
  bbs [-config FILE] area newsgroup AREA GROUP|-    (- : not a newsgroup)
  bbs [-config FILE] news sync [PEER]               exchange articles with peers now
  bbs [-config FILE] area echo AREA TAG|-           make an area a FidoNet echo (- : not one)
  bbs [-config FILE] area group AREA GROUP|-        list an area under a group (- : none)
  bbs [-config FILE] area aliases AREA yes|no       whether its posters may use their aliases
  bbs [-config FILE] area keep AREA MESSAGES DAYS   keep the newest MESSAGES, for DAYS (0: no limit)
  bbs [-config FILE] area purge                     take away what areas keep no more, now
  bbs [-config FILE] fido poll [LINK]               call links (with a host) now
  bbs [-config FILE] fido scan|toss [LINK]          pack new mail, or take in what came
  bbs [-config FILE] fido node ADDRESS              a system in the nodelist
  bbs [-config FILE] fido areafix|filefix LINK CMD...  a request to a link's AreaFix (FileFix)
  bbs [-config FILE] fido subs LINK                 the echoes and file echoes a link has
  bbs [-config FILE] fido link|unlink LINK TAG...   link it to echoes (filelink, fileunlink)
  bbs [-config FILE] fido nodes WORDS...            systems found by name, place, sysop
  bbs [-config FILE] user qwknode|unqwknode NAME     an account for another BBS, a QWK network node
  bbs [-config FILE] qwknet call [NET]               call QWK networks' hubs now
  bbs [-config FILE] room list                       permanent chat rooms (IRC channels)
  bbs [-config FILE] room add|topic ROOM [TOPIC]     make a room permanent; set its topic
  bbs [-config FILE] room del ROOM                   a room permanent no more
  bbs [-config FILE] token add USER [-scope read,post,mail,chat,files,sysop]
                     [-expires DAYS] [-name NAME]   an API key (printed once)
  bbs [-config FILE] token list [USER]
  bbs [-config FILE] token revoke ID
  bbs [-config FILE] db copy DATABASE              copy the database into a new one
                     (a path, postgres://, mysql:// or mariadb://user:password@host/db)
  bbs [-config FILE] sshkey add USER FILE|-          SSH keys (authorized_keys lines) to log in with
  bbs [-config FILE] sshkey list [USER]
  bbs [-config FILE] sshkey del ID
  bbs [-config FILE] filearea add NAME [DESCRIPTION] [-sysop-upload]
  bbs [-config FILE] filearea del NAME
  bbs [-config FILE] filearea list
  bbs [-config FILE] filearea echo AREA TAG|-       make it a FidoNet file echo (- : not)
  bbs [-config FILE] filearea require AREA DOWNLOAD UPLOAD
  bbs [-config FILE] file add AREA PATH [DESCRIPTION]   (as the first sysop)
  bbs [-config FILE] file list AREA
  bbs [-config FILE] file del AREA NAME
  bbs [-config FILE] chan board add DIR TITLE [DESCRIPTION] [-nsfw] [-forced-anon] [-text-only] [-locked] [-login] [-oekaki]
                     [-ids] [-flags]
  bbs [-config FILE] chan board set DIR KEY VALUE   (pages, per_page, mods, nsfw, login, captcha, options, files, sound...: files webm,mp4,pdf,swf,flv)
  bbs [-config FILE] chan board list | del DIR      del takes its posts and images too
  bbs [-config FILE] chan ban add ADDRESS DAYS [BOARD|-] [REASON...]   (0 days: for ever)
  bbs [-config FILE] chan ban list | del ID
`)
	os.Exit(2)
}

func main() {
	configPath := flag.String("config", "", "the configuration file (none: the defaults)")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 && args[0] == "config" {
		// before the file is read: it may be one to put right
		configCmd(*configPath, args[1:])
		return
	}

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
		os.Exit(serve(cfg, *configPath))
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
			for _, k := range l.HostKeys {
				fmt.Printf("  host key %s: %s\n", k, server.HostKeyInfo(k))
			}
		}
		fmt.Println("database:", store.Describe(cfg.Database))
		fmt.Println("files:", cfg.FilesDir)
		if cfg.FTP != nil {
			fmt.Printf("ftp: %s, tls %s\n", cfg.FTP.Address, cfg.FTP.TLS)
		}
		fmt.Printf("news: %s\n", cfg.News.PathHost)
		if cfg.NNTP != nil {
			fmt.Printf("nntp: %s, tls %s\n", cfg.NNTP.Address, cfg.NNTP.TLS)
		}
		for _, p := range cfg.Peers {
			fmt.Printf("[peer %s] %s, groups %s, pull %v, push %v, every %v\n", p.Name, p.Address,
				p.Groups, p.Pull, p.Push, p.Interval)
		}
		if f := cfg.Fido; f != nil {
			var as []string
			for _, a := range f.Addrs {
				as = append(as, a.String())
			}
			fmt.Printf("fido: %s, %s\n", strings.Join(as, " "), f.Dir)
			if cfg.Binkp != nil {
				fmt.Printf("binkp: %s\n", cfg.Binkp.Address)
			}
			for _, l := range f.Links {
				fmt.Printf("[link %s] %s, host %q, echoes %s, route %v\n", l.Name, l.Addr, l.Host, l.Echoes, l.Route)
			}
		}
		if cfg.Screens != nil {
			fmt.Println("screens:", cfg.Screens.Dir)
		}
		if cfg.ScriptsDir != "" {
			fmt.Printf("scripts: %s (%s)\n", cfg.ScriptsDir, strings.Join(script.For(cfg.ScriptsDir).Names(), ", "))
		}
		for _, m := range cfg.Menus {
			keys := ""
			for _, it := range m.Items {
				keys += it.Key
			}
			fmt.Printf("[menu %s] %q, keys %s\n", m.Name, m.Title, keys)
		}
		if h := cfg.HTTP; h != nil {
			fmt.Printf("http: %s, tls %v, anonymous %v", h.Address, h.TLS, h.Anonymous)
			if h.Metrics != "" {
				fmt.Printf(", metrics on %s", h.Metrics)
			}
			fmt.Println()
		}
		if m := cfg.MRC; m != nil {
			fmt.Printf("mrc: %s (TLS %v), rooms %s*\n", m.Address, m.TLS, m.Prefix)
		}
		if i := cfg.IRC; i != nil {
			fmt.Printf("irc: %q, TLS %q\n", i.Address, i.TLSAddress)
		}
		if cfg.Finger != nil {
			fmt.Printf("finger: %s (online %v, users %v)\n", cfg.Finger.Address, cfg.Finger.Online, cfg.Finger.Users)
		}
		if g := cfg.Gopher; g != nil {
			fmt.Printf("gopher: %s as %s:%d, areas %q, files %v\n", g.Address, g.Host, g.Port, g.Areas, g.Files)
		}
		for _, d := range cfg.Doors {
			if r := d.Remote; r != nil {
				fmt.Printf("[door %s] %s at %s, %v\n", d.Name, r.Kind, r.Address, d.TimeLimit)
				continue
			}
			fmt.Printf("[door %s] %q, drop files %s in %s, %v\n", d.Name, d.Command,
				strings.Join(d.DropFiles, " "), d.Dir, d.TimeLimit)
		}
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
		if fas, _ := st.FileAreas(); len(fas) == 0 {
			if _, err := st.CreateFileArea("Uploads", "Files from callers", false); err != nil {
				fatal(err)
			}
		}
		fmt.Println("database ready:", store.Describe(cfg.Database))
		fmt.Println("make the sysop's account with: bbs user add NAME -sysop")
	case "user":
		userCmd(cfg, args)
	case "area":
		areaCmd(cfg, args)
	case "news":
		newsCmd(cfg, args)
	case "fido":
		fidoCmd(cfg, args)
	case "qwknet":
		qwkNetCmd(cfg, args)
	case "room":
		roomCmd(cfg, args)
	case "token":
		tokenCmd(cfg, args)
	case "sshkey":
		sshKeyCmd(cfg, args)
	case "db":
		dbCmd(cfg, args)
	case "filearea":
		fileAreaCmd(cfg, args)
	case "file":
		fileCmd(cfg, args)
	case "chan":
		chanCmd(cfg, args)
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

func serve(cfg *server.Config, path string) int {
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
	lg.Printf("database: %s (%s)", store.Describe(cfg.Database), st.Engine())
	b := bbs.New(cfg.Settings, st, lg)
	b.News = cfg.News
	b.Doors = cfg.Doors
	b.Screens, b.Menus, b.Text, b.ScriptsDir = cfg.Screens, cfg.Menus, cfg.Text, cfg.ScriptsDir
	b.Guard = cfg.Guard
	if len(cfg.Events) > 0 {
		b.Events = &events.Scheduler{Events: cfg.Events, Log: b.Log}
	}
	if b.Files, err = files.Open(st, cfg.FilesDir, cfg.MaxFileSize, cfg.UploadQuota); err != nil {
		lg.Printf("files: %v", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	svc := &nntp.Service{B: b, MaxArticle: int(cfg.MaxArticle)}
	if cfg.NNTP != nil {
		ns, err := nntp.NewServer(svc, *cfg.NNTP, cfg.Peers)
		if err != nil {
			lg.Printf("%v", err)
			return 1
		}
		go func() {
			if err := ns.Run(ctx); err != nil {
				lg.Printf("%v", err)
				stop()
			}
		}()
	}
	svc.RunPeers(ctx, cfg.Peers)
	if cfg.MRC != nil {
		mc, err := mrc.New(b, *cfg.MRC)
		if err != nil {
			lg.Printf("%v", err)
			return 1
		}
		b.SetRelay(mc)
		go mc.Run(ctx)
	}
	if cfg.IRC != nil {
		is, err := ircd.New(b, *cfg.IRC)
		if err != nil {
			lg.Printf("%v", err)
			return 1
		}
		go func() {
			if err := is.Run(ctx); err != nil {
				lg.Printf("%v", err)
				stop()
			}
		}()
	}
	if cfg.Finger != nil {
		fs := &finger.Server{B: b, S: *cfg.Finger}
		go func() {
			if err := fs.Run(ctx); err != nil {
				lg.Printf("%v", err)
				stop()
			}
		}()
	}
	if cfg.Gopher != nil {
		gs := &gopher.Server{B: b, S: *cfg.Gopher}
		go func() {
			if err := gs.Run(ctx); err != nil {
				lg.Printf("%v", err)
				stop()
			}
		}()
	}
	var fd *fido.Fido
	if cfg.Fido != nil {
		fd, err = fido.New(b, *cfg.Fido)
		if err != nil {
			lg.Printf("fido: %v", err)
			return 1
		}
		b.Netmail = fd.Netmail
		b.NetmailTo = fd.NetmailTo
		b.Nodelist = fd.Nodelist
		if cfg.Binkp != nil {
			bs := &fido.Server{F: fd, Address: cfg.Binkp.Address, MaxClients: cfg.Binkp.MaxClients,
				MaxPerIP: cfg.Binkp.MaxPerIP}
			go func() {
				if err := bs.Run(ctx); err != nil {
					lg.Printf("%v", err)
					stop()
				}
			}()
		}
		go fd.Run(ctx)
	}
	go func() {
		// areas keep what they say (max_msgs, max_days): looked at hourly
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if _, err := b.PurgeAreas(); err != nil {
				b.Log.Printf("purging: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	if b.Events != nil {
		go b.Events.Run(ctx)
	}
	var qn *qwknet.Runner
	if len(cfg.QWKNets) > 0 {
		qn = &qwknet.Runner{B: b, Nets: cfg.QWKNets}
		go qn.Run(ctx)
	}
	// the image boards, one for the web and the terminals both
	ib := &imageboard.Server{B: b, S: cfg.Chan}
	if cfg.HTTP != nil {
		ib.Secure, ib.TTL = cfg.HTTP.SecureCookies, cfg.HTTP.SessionTTL
	}
	b.ImageBoards = ib
	srv := server.New(b, cfg.Listeners)
	srv.Graphics = cfg.Graphics
	srv.LogoFile = cfg.LogoFile
	var as *api.Server
	if cfg.HTTP != nil {
		as = &api.Server{B: b, S: *cfg.HTTP, Fido: fd, Peers: cfg.Peers, QWKNet: qn, Terminal: srv.WebTerminal, Chan: ib}
		if len(cfg.Peers) > 0 || cfg.NNTP != nil {
			as.News = svc
		}
		go func() {
			if err := as.Run(ctx); err != nil {
				lg.Printf("%v", err)
				stop()
			}
		}()
	}
	if cfg.FTP != nil {
		fs, err := ftp.New(b, *cfg.FTP)
		if err != nil {
			lg.Printf("%v", err)
			return 1
		}
		b.FTPAddress = cfg.FTPAdvertise + map[string]string{
			ftp.TLSRequired: " (FTPS, explicit TLS)", ftp.TLSImplicit: " (FTPS, implicit TLS)",
			ftp.TLSOptional: " (TLS optional)", ftp.TLSNo: " (plain FTP)"}[cfg.FTP.TLS]
		go func() {
			if err := fs.Run(ctx); err != nil {
				lg.Printf("%v", err)
				stop()
			}
		}()
	}
	if path != "" {
		// the file put in force again: by SIGHUP (systemctl reload), or
		// the sysop's Configuration
		rl := server.NewReloader(path, b)
		rl.Chan = ib
		if as != nil {
			rl.Forum = as.ReconfigureForum
		}
		b.Config = &bbs.ConfigTools{Path: path, Check: server.Check, Reload: rl.Reload}
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-hup:
					if _, _, err := rl.Reload(); err != nil {
						lg.Printf("reload: %v: nothing changed", err)
					}
				}
			}
		}()
	}
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
	if len(args) >= 3 && args[0] == "flags" {
		// -C is an edit here, not an option
		st := open(cfg)
		defer st.Close()
		u, err := st.UserByName(args[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", args[1], err))
		}
		fl, err := bbs.EditFlags(u.Flags, strings.Join(args[2:], " "))
		if err != nil {
			fatal(err)
		}
		if err := st.SetFlags(u.ID, fl); err != nil {
			fatal(err)
		}
		fmt.Printf("%s: flags %q\n", u.Name, fl)
		return
	}
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
			if u.Flags != "" {
				tags = append(tags, "flags "+u.Flags)
			}
			fmt.Printf("%-24s level %3d %5d calls %5d posts  %s\n", u.Name, u.Level, u.Logins, u.Posts, strings.Join(tags, " "))
		}
		return
	}
	if words[0] == "level" && len(words) == 3 {
		u, err := st.UserByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		l, err := strconv.Atoi(words[2])
		if err != nil || l < 0 || l > bbs.MaxLevel {
			fatal(fmt.Errorf("a level is 0 to %d", bbs.MaxLevel))
		}
		if err := st.SetLevel(u.ID, l); err != nil {
			fatal(err)
		}
		fmt.Printf("%s: level %d\n", u.Name, l)
		return
	}
	if words[0] == "waiting" {
		us, err := st.UsersBelow(cfg.Settings.ValidatedLevel)
		if err != nil {
			fatal(err)
		}
		for _, u := range us {
			fmt.Printf("%-24s level %3d  since %s  %s\n", u.Name, u.Level, u.Created.Format("2006-01-02"), u.Location)
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
	case "qwknode":
		err = st.SetQWKNode(u.ID, true)
	case "unqwknode":
		err = st.SetQWKNode(u.ID, false)
	case "del":
		err = st.DeleteUser(u.ID)
	case "validate":
		err = st.SetLevel(u.ID, max(u.Level, cfg.Settings.ValidatedLevel))
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
	fmt.Println("done")
}

// limit is n of what, or "no limit".
func limit(n int, what string) string {
	if n == 0 {
		return "any number of " + what
	}
	return strconv.Itoa(n) + " " + what
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
			if a.Newsgroup != "" {
				only += " [" + a.Newsgroup + "]"
			}
			if a.EchoTag != "" {
				only += " [echo " + a.EchoTag + "]"
			}
			if a.ReadReq != "" || a.PostReq != "" {
				only += fmt.Sprintf(" (read %q, post %q)", a.ReadReq, a.PostReq)
			}
			if a.Group != "" {
				only += " {" + a.Group + "}"
			}
			if a.Aliases {
				only += " (aliases)"
			}
			if a.MaxMsgs > 0 || a.MaxDays > 0 {
				only += fmt.Sprintf(" (keeps %d messages, %d days)", a.MaxMsgs, a.MaxDays)
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
	case "require":
		if len(words) != 4 {
			usage()
		}
		a, err := st.AreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		read, post := requireArg(words[2]), requireArg(words[3])
		if err := st.SetAreaRequire(a.ID, read, post); err != nil {
			fatal(err)
		}
		fmt.Printf("%s: read %q, post %q\n", a.Name, read, post)
	case "newsgroup":
		if len(words) != 3 {
			usage()
		}
		a, err := st.AreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		group := words[2]
		if group == "-" {
			group = ""
		} else if !news.ValidGroup(group) {
			fatal(errors.New("a newsgroup name is lower case words with dots: bbs.general"))
		}
		if err := st.SetNewsgroup(a.ID, group); err != nil {
			fatal(err)
		}
		if group == "" {
			fmt.Println(a.Name, "is no longer a newsgroup")
		} else {
			fmt.Println(a.Name, "is", group)
		}
	case "group", "aliases":
		if len(words) != 3 {
			usage()
		}
		a, err := st.AreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		if words[0] == "group" {
			g := bbs.Clean(words[2], 30)
			if g == "-" {
				g = ""
			}
			if err := st.SetAreaGroup(a.ID, g); err != nil {
				fatal(err)
			}
			fmt.Printf("%s: group %q\n", a.Name, g)
			return
		}
		on := map[string]bool{"yes": true, "no": false}
		v, ok := on[strings.ToLower(words[2])]
		if !ok {
			usage()
		}
		if err := st.SetAreaAliases(a.ID, v); err != nil {
			fatal(err)
		}
		fmt.Printf("%s: aliases %s\n", a.Name, words[2])
	case "keep":
		if len(words) != 4 {
			usage()
		}
		a, err := st.AreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		msgs, err1 := strconv.Atoi(words[2])
		days, err2 := strconv.Atoi(words[3])
		if err1 != nil || err2 != nil || msgs < 0 || days < 0 {
			fatal(errors.New("keep: MESSAGES and DAYS are numbers, 0 for no limit"))
		}
		if err := st.SetAreaKeep(a.ID, msgs, days); err != nil {
			fatal(err)
		}
		fmt.Printf("%s: keeps %s, for %s\n", a.Name, limit(msgs, "messages"), limit(days, "days"))
	case "purge":
		b := bbs.New(cfg.Settings, st, log.New(os.Stdout, "", 0))
		n, err := b.PurgeAreas()
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%d messages purged\n", n)
	case "echo":
		if len(words) != 3 {
			usage()
		}
		a, err := st.AreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		tag := strings.ToUpper(words[2])
		if tag == "-" {
			tag = ""
		} else if !ftn.ValidTag(tag) {
			fatal(errors.New("an echo tag is letters, digits and . _ - (FSX_GEN), up to 60"))
		}
		if err := st.SetEchoTag(a.ID, tag); err != nil {
			fatal(err)
		}
		if tag == "" {
			fmt.Println(a.Name, "is no longer an echo")
		} else {
			fmt.Println(a.Name, "is the echo", tag)
		}
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

func fileAreaCmd(cfg *server.Config, args []string) {
	words, flags := split(args)
	if len(words) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	switch words[0] {
	case "list":
		as, err := st.FileAreas()
		if err != nil {
			fatal(err)
		}
		for _, a := range as {
			only := ""
			if a.SysopUpload {
				only = " (sysop uploads only)"
			}
			if a.FileEcho != "" {
				only += " (file echo " + a.FileEcho + ")"
			}
			fmt.Printf("%-20s %5d files %10s  %s%s\n", a.Name, a.Files, files.Size(a.Bytes), a.Description, only)
		}
	case "echo":
		if len(words) != 3 {
			usage()
		}
		a, err := st.FileAreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		tag := strings.ToUpper(words[2])
		if tag == "-" {
			tag = ""
		} else if !ftn.ValidTag(tag) {
			fatal(errors.New("a file echo's tag is letters, digits and . _ - (NODEDIFF), up to 60"))
		}
		if err := st.SetFileEcho(a.ID, tag); errors.Is(err, store.ErrExists) {
			fatal(errors.New("another file area is the file echo " + tag))
		} else if err != nil {
			fatal(err)
		}
		if tag == "" {
			fmt.Println(a.Name, "is no longer a file echo")
		} else {
			fmt.Println(a.Name, "is the file echo", tag)
		}
	case "add":
		if len(words) < 2 || len(words) > 3 {
			usage()
		}
		// an area is a directory over FTP: a file name's rules
		if msg := files.ValidName(words[1]); msg != "" {
			fatal(errors.New(msg))
		}
		desc := ""
		if len(words) == 3 {
			desc = words[2]
		}
		a, err := st.CreateFileArea(words[1], bbs.Clean(desc, 60), flags["sysop-upload"])
		if err != nil {
			fatal(err)
		}
		fmt.Println("added", a.Name)
	case "require":
		if len(words) != 4 {
			usage()
		}
		a, err := st.FileAreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		dl, ul := requireArg(words[2]), requireArg(words[3])
		if err := st.SetFileAreaRequire(a.ID, dl, ul); err != nil {
			fatal(err)
		}
		fmt.Printf("%s: download %q, upload %q\n", a.Name, dl, ul)
	case "del":
		if len(words) != 2 {
			usage()
		}
		a, err := st.FileAreaByName(words[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[1], err))
		}
		if err := st.DeleteFileArea(a.ID); err != nil {
			fatal(err)
		}
		fmt.Println("deleted", a.Name)
	default:
		usage()
	}
}

func fileCmd(cfg *server.Config, args []string) {
	words, _ := split(args)
	if len(words) < 2 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	fst, err := files.Open(st, cfg.FilesDir, cfg.MaxFileSize, 0)
	if err != nil {
		fatal(err)
	}
	a, err := st.FileAreaByName(words[1])
	if err != nil {
		fatal(fmt.Errorf("%s: %w", words[1], err))
	}
	switch words[0] {
	case "list":
		fs, err := st.Files(a.ID)
		if err != nil {
			fatal(err)
		}
		for _, f := range fs {
			fmt.Printf("%-32s %10s %s %-16s %4d  %s\n", f.Name, files.Size(f.Size),
				f.Created.Format("2006-01-02"), f.Uploader, f.Downloads, f.Description)
		}
	case "add":
		if len(words) < 3 || len(words) > 4 {
			usage()
		}
		desc := ""
		if len(words) == 4 {
			desc = words[3]
		}
		sysop := firstSysop(st)
		f, err := fst.Import(sysop, a, words[2], filepath.Base(words[2]), bbs.Clean(desc, 60))
		if err != nil {
			fatal(err)
		}
		fmt.Printf("added %s (%s, sha256 %s)\n", f.Name, files.Size(f.Size), f.SHA256)
	case "del":
		if len(words) != 3 {
			usage()
		}
		f, err := st.FileByName(a.ID, words[2])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", words[2], err))
		}
		if err := fst.Delete(firstSysop(st), f); err != nil {
			fatal(err)
		}
		fmt.Println("deleted", f.Name)
	default:
		usage()
	}
}

// firstSysop is who the command line acts as.
func firstSysop(st *store.Store) *store.User {
	us, err := st.Users()
	if err != nil {
		fatal(err)
	}
	for _, u := range us {
		if u.Sysop {
			return u
		}
	}
	fatal(errors.New("there is no sysop: bbs user add NAME -sysop"))
	return nil
}

func newsCmd(cfg *server.Config, args []string) {
	words, _ := split(args)
	if len(words) == 0 || words[0] != "sync" || len(words) > 2 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	lg := log.New(os.Stderr, "bbs: ", log.LstdFlags)
	b := bbs.New(cfg.Settings, st, lg)
	b.News = cfg.News
	svc := &nntp.Service{B: b, MaxArticle: int(cfg.MaxArticle)}
	n := 0
	for _, p := range cfg.Peers {
		if len(words) == 2 && p.Name != words[1] {
			continue
		}
		n++
		pushed, pulled, err := svc.Sync(p)
		fmt.Printf("%s: %d sent, %d taken", p.Name, pushed, pulled)
		if err != nil {
			fmt.Printf(": %v", err)
		}
		fmt.Println()
	}
	if n == 0 {
		fatal(errors.New("no such peer"))
	}
}

func roomCmd(cfg *server.Config, args []string) {
	words, _ := split(args)
	if len(words) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	b := bbs.New(cfg.Settings, st, log.New(os.Stderr, "bbs: ", log.LstdFlags))
	if words[0] == "list" {
		for _, r := range b.PermanentRooms() {
			fmt.Printf("#%-20s %s\n", r.Name, r.Topic)
		}
		return
	}
	if len(words) < 2 || len(words) > 3 {
		usage()
	}
	name, topic := strings.TrimPrefix(words[1], "#"), ""
	if len(words) == 3 {
		topic = words[2]
	}
	var err error
	switch words[0] {
	case "add":
		err = b.AddPermanentRoom(name, topic, "the bbs command")
	case "del":
		err = b.RemovePermanentRoom(name, "the bbs command")
	case "topic":
		err = b.SetRoomTopic(name, topic, "the bbs command")
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
	fmt.Println("done")
}

func qwkNetCmd(cfg *server.Config, args []string) {
	words, _ := split(args)
	if len(words) == 0 || words[0] != "call" || len(words) > 2 {
		usage()
	}
	if len(cfg.QWKNets) == 0 {
		fatal(errors.New("no [qwknet NAME] section"))
	}
	st := open(cfg)
	defer st.Close()
	b := bbs.New(cfg.Settings, st, log.New(os.Stderr, "bbs: ", log.LstdFlags))
	b.News = cfg.News
	name := ""
	if len(words) == 2 {
		name = words[1]
	}
	r := &qwknet.Runner{B: b, Nets: cfg.QWKNets}
	if err := r.CallNow(name); err != nil {
		fatal(err)
	}
}

func fidoCmd(cfg *server.Config, args []string) {
	words, _ := split(args)
	if len(words) == 0 {
		usage()
	}
	switch words[0] {
	case "node", "nodes", "subs":
		if len(words) < 2 {
			usage()
		}
	case "areafix", "filefix", "link", "unlink", "filelink", "fileunlink":
		if len(words) < 3 {
			usage()
		}
	default:
		if len(words) > 2 {
			usage()
		}
	}
	if cfg.Fido == nil {
		fatal(errors.New("no [fido] section"))
	}
	st := open(cfg)
	defer st.Close()
	lg := log.New(os.Stderr, "bbs: ", log.LstdFlags)
	b := bbs.New(cfg.Settings, st, lg)
	b.News = cfg.News
	fd, err := fido.New(b, *cfg.Fido)
	if err != nil {
		fatal(err)
	}
	switch words[0] {
	case "node", "nodes":
		nodelistCmd(fd, words)
		return
	case "areafix", "filefix", "link", "unlink", "filelink", "fileunlink", "subs":
		fixCmd(fd, words)
		return
	}
	var links []*fido.Link
	for _, l := range cfg.Fido.Links {
		if len(words) == 2 && !strings.EqualFold(l.Name, words[1]) {
			continue
		}
		links = append(links, l)
	}
	if len(links) == 0 {
		fatal(errors.New("no such link"))
	}
	failed := false
	for _, l := range links {
		switch words[0] {
		case "scan":
			n, err := fd.Scan(l)
			fmt.Printf("%s: %d messages packed\n", l.Name, n)
			failed = failed || err != nil
			if err != nil {
				fmt.Println(" ", err)
			}
		case "toss":
			st, err := fd.Toss(l)
			fmt.Printf("%s: %s\n", l.Name, st)
			if err != nil {
				failed = true
				fmt.Println(" ", err)
			}
		case "poll":
			if l.Host == "" {
				if len(words) == 2 {
					fatal(fmt.Errorf("%s has no host to call", l.Name))
				}
				continue
			}
			if err := fd.Call(context.Background(), l); err != nil {
				failed = true
				fmt.Printf("%s: %v\n", l.Name, err)
			} else {
				fmt.Printf("%s: done\n", l.Name)
			}
		default:
			usage()
		}
	}
	if failed {
		os.Exit(1)
	}
}

// fixCmd: requests to a link's AreaFix or FileFix; a link's echoes and
// file echoes, linked and unlinked by hand.
func fixCmd(fd *fido.Fido, words []string) {
	l := fd.Link(words[1])
	if l == nil {
		fatal(fmt.Errorf("no link %s", words[1]))
	}
	kind := fido.Echoes
	if strings.HasPrefix(words[0], "file") {
		kind = fido.FileEchos
	}
	switch words[0] {
	case "areafix", "filefix":
		if err := fd.FixRequest(l, kind, words[2:]); err != nil {
			fatal(err)
		}
		fmt.Printf("queued for %s at %s: sent at the next call\n", map[string]string{fido.Echoes: "AreaFix", fido.FileEchos: "FileFix"}[kind], l.Addr)
	case "subs":
		for _, k := range []string{fido.Echoes, fido.FileEchos} {
			tags, err := fd.Carried(l, k)
			if err != nil {
				fatal(err)
			}
			fmt.Printf("%s %ss (%d): %s\n", l.Name, k, len(tags), strings.Join(tags, " "))
		}
		if !l.AreaFix {
			fmt.Println("(by its echoes and fileechoes: areafix = no)")
		}
	default:
		if !l.AreaFix {
			fatal(fmt.Errorf("%s has areafix = no: its echoes and fileechoes say what it has", l.Name))
		}
		out, err := fd.Subscribe(l, kind, !strings.Contains(words[0], "unlink"), words[2:])
		for _, o := range out {
			fmt.Println(o)
		}
		if err != nil {
			fatal(err)
		}
	}
}

// nodelistCmd: a node of the nodelist, or the nodes found by words.
func nodelistCmd(fd *fido.Fido, words []string) {
	nl := fd.Nodelist()
	if nl.Len() == 0 {
		fatal(errors.New("no nodelist read (set [fido] nodelist)"))
	}
	show := func(n *ftn.Node) {
		kind := ""
		if n.Kind != "" {
			kind = " (" + strings.ToLower(n.Kind) + ")"
		}
		fmt.Printf("%-14s %s%s\n", n.Addr.String4D(), n.Name, kind)
		fmt.Printf("               %s; sysop %s; %s\n", n.Location, n.Sysop, n.Phone)
		if len(n.Flags) > 0 {
			fmt.Printf("               %s\n", strings.Join(n.Flags, ","))
		}
		if h, ok := n.Binkp(); ok {
			fmt.Printf("               binkp at %s\n", h)
		}
	}
	if words[0] == "node" {
		a, err := ftn.ParseAddr(words[1])
		if err != nil {
			fatal(err)
		}
		n, ok := nl.Lookup(a)
		if !ok {
			fatal(fmt.Errorf("%s is not in the nodelist", a))
		}
		show(n)
		return
	}
	found := nl.Search(strings.Join(words[1:], " "), 200)
	for _, n := range found {
		show(n)
	}
	fmt.Printf("%d found (of %d)\n", len(found), nl.Len())
}

func tokenCmd(cfg *server.Config, args []string) {
	if len(args) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	switch args[0] {
	case "add":
		var user, name string
		scopes, days := "read,post,mail,chat,files", 0
		for i := 1; i < len(args); i++ {
			a := args[i]
			val := func() string {
				if i+1 >= len(args) {
					usage()
				}
				i++
				return args[i]
			}
			switch a {
			case "-scope", "-scopes":
				scopes = val()
			case "-expires":
				n, err := strconv.Atoi(val())
				if err != nil || n < 0 {
					fatal(errors.New("-expires: a number of days"))
				}
				days = n
			case "-name":
				name = val()
			default:
				if user != "" || strings.HasPrefix(a, "-") {
					usage()
				}
				user = a
			}
		}
		if user == "" {
			usage()
		}
		u, err := st.UserByName(user)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", user, err))
		}
		sc, err := api.ParseScopes(scopes)
		if err != nil {
			fatal(err)
		}
		if strings.Contains(" "+sc+" ", " sysop ") && !u.Sysop {
			fatal(errors.New("the sysop scope is for sysops"))
		}
		if name == "" {
			name = "key"
		}
		secret, hash := api.NewToken()
		t := &store.Token{UserID: u.ID, Name: bbs.Clean(name, 60), Hash: hash, Scopes: sc}
		if days > 0 {
			t.Expires = time.Now().Add(time.Duration(days) * 24 * time.Hour)
		}
		id, err := st.AddToken(t)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("token %d for %s (%s)", id, u.Name, sc)
		if days > 0 {
			fmt.Printf(", until %s", t.Expires.Format("2006-01-02"))
		}
		fmt.Printf(":\n%s\n(it is shown this once)\n", secret)
	case "list":
		var uid int64
		if len(args) == 2 {
			u, err := st.UserByName(args[1])
			if err != nil {
				fatal(fmt.Errorf("%s: %w", args[1], err))
			}
			uid = u.ID
		}
		ts, err := st.Tokens(uid)
		if err != nil {
			fatal(err)
		}
		for _, t := range ts {
			kind := "key"
			if t.Session {
				kind = "session"
			}
			exp, last := "never", "never"
			if !t.Expires.IsZero() {
				exp = t.Expires.Format("2006-01-02 15:04")
			}
			if !t.LastUsed.IsZero() {
				last = t.LastUsed.Format("2006-01-02 15:04")
			}
			fmt.Printf("%4d  %-16s %-8s %-30s expires %s, used %s  %s\n", t.ID, t.User, kind, t.Scopes, exp, last, t.Name)
		}
	case "revoke":
		if len(args) != 2 {
			usage()
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			usage()
		}
		if err := st.RevokeToken(id, 0); err != nil {
			fatal(fmt.Errorf("token %d: %w", id, err))
		}
		fmt.Println("revoked", id)
	default:
		usage()
	}
}

func sshKeyCmd(cfg *server.Config, args []string) {
	if len(args) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	switch args[0] {
	case "add":
		if len(args) != 3 {
			usage()
		}
		u, err := st.UserByName(args[1])
		if err != nil {
			fatal(fmt.Errorf("%s: %w", args[1], err))
		}
		var b []byte
		if args[2] == "-" {
			b, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		} else {
			b, err = os.ReadFile(args[2])
		}
		if err != nil {
			fatal(err)
		}
		n := 0
		for i, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, err := bbs.ParseSSHKey(line)
			if err != nil {
				fatal(fmt.Errorf("line %d: %w", i+1, err))
			}
			k.UserID = u.ID
			id, err := st.AddSSHKey(k)
			if err != nil {
				fatal(fmt.Errorf("line %d: %w", i+1, err))
			}
			fmt.Printf("key %d for %s: %s %s %s\n", id, u.Name, k.Type, k.Fingerprint, k.Comment)
			n++
		}
		if n == 0 {
			fatal(errors.New("no keys there"))
		}
	case "list":
		var uid int64
		if len(args) == 2 {
			u, err := st.UserByName(args[1])
			if err != nil {
				fatal(fmt.Errorf("%s: %w", args[1], err))
			}
			uid = u.ID
		} else if len(args) != 1 {
			usage()
		}
		ks, err := st.SSHKeys(uid)
		if err != nil {
			fatal(err)
		}
		for _, k := range ks {
			last := "never"
			if !k.LastUsed.IsZero() {
				last = k.LastUsed.Format("2006-01-02 15:04")
			}
			fmt.Printf("%4d  %-16s %-20s %s  used %s  %s\n", k.ID, k.User, k.Type, k.Fingerprint, last, k.Comment)
		}
	case "del":
		if len(args) != 2 {
			usage()
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			usage()
		}
		if err := st.RemoveSSHKey(id, 0); err != nil {
			fatal(fmt.Errorf("key %d: %w", id, err))
		}
		fmt.Println("deleted", id)
	default:
		usage()
	}
}

// requireArg is an access requirement given on the command line ("-":
// none), checked.
func requireArg(s string) string {
	if s == "-" {
		return ""
	}
	r, err := bbs.ParseRequire(s)
	if err != nil {
		fatal(err)
	}
	return r.String()
}

func dbCmd(cfg *server.Config, args []string) {
	if len(args) != 2 || args[0] != "copy" {
		usage()
	}
	src := open(cfg)
	defer src.Close()
	dst, err := store.Open(args[1])
	if err != nil {
		fatal(err)
	}
	defer dst.Close()
	fmt.Printf("copying %s (%s) into %s (%s)\n", store.Describe(cfg.Database), src.Engine(),
		store.Describe(args[1]), dst.Engine())
	if err := store.Copy(src, dst, func(table string, n int) { fmt.Printf("  %-12s %d\n", table, n) }); err != nil {
		fatal(err)
	}
	fmt.Println("done: set database = in the configuration to the new one")
}
