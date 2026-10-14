package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/server"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// chanCmd runs the image boards' commands: boards and bans.
func chanCmd(cfg *server.Config, args []string) {
	if len(args) < 2 {
		usage()
	}
	switch args[0] {
	case "board":
		chanBoardCmd(cfg, args[1:])
	case "ban":
		chanBanCmd(cfg, args[1:])
	default:
		usage()
	}
}

func chanBoardCmd(cfg *server.Config, args []string) {
	words, flags := split(args)
	if len(words) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	switch words[0] {
	case "list":
		bs, err := st.Boards()
		if err != nil {
			fatal(err)
		}
		for _, b := range bs {
			var opts []string
			for name, on := range map[string]bool{"nsfw": !b.Worksafe, "forced anon": b.ForcedAnon, "locked": b.Locked,
				"users post": b.LoginToPost && !b.LoginToRead, "users only": b.LoginToRead, "text only": b.TextOnly,
				"oekaki": b.Oekaki, "captcha " + b.Captcha: b.Captcha != "", "options " + b.Options: b.Options != ""} {
				if on {
					opts = append(opts, name)
				}
			}
			n, _ := st.ChanThreadCount(b.ID)
			fmt.Printf("/%s/ %-20s %4d threads, %6d posts, %d pages of %d  %s\n", b.Dir, b.Title, n, b.LastNo, b.Pages,
				b.PerPage, strings.Join(opts, ", "))
		}
	case "add":
		if len(words) < 3 || len(words) > 4 {
			usage()
		}
		if msg := imageboard.ValidDir(words[1]); msg != "" {
			fatal(errors.New(msg))
		}
		b := imageboard.NewBoard(words[1], bbs.Clean(words[2], 60))
		if len(words) == 4 {
			b.Description = bbs.Clean(words[3], 200)
		}
		b.Worksafe = !flags["nsfw"]
		b.ForcedAnon, b.Locked, b.LoginToPost, b.TextOnly = flags["forced-anon"], flags["locked"], flags["login"], flags["text-only"]
		b.Oekaki = flags["oekaki"]
		if _, err := st.CreateBoard(b); errors.Is(err, store.ErrExists) {
			fatal(fmt.Errorf("there is a /%s/ already", b.Dir))
		} else if err != nil {
			fatal(err)
		}
		fmt.Printf("added /%s/ - %s\n", b.Dir, b.Title)
	case "set":
		if len(words) != 4 {
			usage()
		}
		b, err := st.Board(words[1])
		if err != nil {
			fatal(fmt.Errorf("/%s/: %w", words[1], err))
		}
		if err := setBoard(b, words[2], words[3]); err != nil {
			fatal(err)
		}
		if err := st.UpdateBoard(b); err != nil {
			fatal(err)
		}
		fmt.Printf("/%s/: %s = %s\n", b.Dir, words[2], words[3])
	case "del":
		if len(words) != 2 {
			usage()
		}
		b, err := st.Board(words[1])
		if err != nil {
			fatal(fmt.Errorf("/%s/: %w", words[1], err))
		}
		srv := &imageboard.Server{B: &bbs.BBS{Store: st}, S: cfg.Chan}
		if err := srv.RemoveBoard(b); err != nil {
			fatal(err)
		}
		fmt.Printf("deleted /%s/, its posts and its images\n", b.Dir)
	default:
		usage()
	}
}

// setBoard sets one of a board's settings.
func setBoard(b *store.Board, key, val string) error {
	num := func(min, max int) (int, error) {
		n, err := strconv.Atoi(val)
		if err != nil || n < min || n > max {
			return 0, fmt.Errorf("%s: a number from %d to %d", key, min, max)
		}
		return n, nil
	}
	yes := func() (bool, error) {
		switch strings.ToLower(val) {
		case "yes", "on", "true", "1":
			return true, nil
		case "no", "off", "false", "0":
			return false, nil
		}
		return false, fmt.Errorf("%s: yes or no", key)
	}
	var err error
	switch key {
	case "title":
		b.Title = bbs.Clean(val, 60)
	case "description":
		b.Description = bbs.Clean(val, 200)
	case "default_name":
		b.DefaultName = bbs.Clean(val, 75)
	case "mods":
		b.Mods = bbs.Clean(val, 500)
	case "pages":
		b.Pages, err = num(1, 100)
	case "per_page":
		b.PerPage, err = num(1, 50)
	case "bump_limit":
		b.BumpLimit, err = num(1, 100000)
	case "image_limit":
		b.ImageLimit, err = num(0, 100000)
	case "sort":
		b.Sort, err = num(-1000, 1000)
	case "max_file":
		var n int64
		if n, err = sizeArg(val); err == nil && (n < 0 || n > 1<<30) {
			err = errors.New("max_file: up to 1G (0: [chan]'s)")
		}
		b.MaxFile = n
	case "worksafe":
		b.Worksafe, err = yes()
	case "nsfw":
		var v bool
		v, err = yes()
		b.Worksafe = !v
	case "forced_anon":
		b.ForcedAnon, err = yes()
	case "locked":
		b.Locked, err = yes()
	case "login":
		// no, post (only users post), all (only users see it)
		switch strings.ToLower(val) {
		case "no", "off", "false", "0":
			b.LoginToPost, b.LoginToRead = false, false
		case "post", "yes", "on", "true", "1":
			b.LoginToPost, b.LoginToRead = true, false
		case "all":
			b.LoginToPost, b.LoginToRead = true, true
		default:
			err = errors.New("login: no, post (only the BBS's users post) or all (only they see the board)")
		}
	case "captcha", "options":
		v := strings.ToLower(val)
		switch v {
		case "default", "":
			v = ""
		case "yes", "no":
		default:
			err = fmt.Errorf("%s: yes, no, or default ([chan]'s)", key)
		}
		if key == "captcha" {
			b.Captcha = v
		} else {
			b.Options = v
		}
	case "text_only":
		b.TextOnly, err = yes()
	case "oekaki":
		b.Oekaki, err = yes()
	default:
		return fmt.Errorf("%s: not a board's setting (title, description, default_name, mods, pages, per_page, "+
			"bump_limit, image_limit, max_file, sort, nsfw, forced_anon, locked, login, text_only, oekaki, captcha, options)", key)
	}
	return err
}

func chanBanCmd(cfg *server.Config, args []string) {
	if len(args) == 0 {
		usage()
	}
	st := open(cfg)
	defer st.Close()
	switch args[0] {
	case "list":
		bans, err := st.Bans()
		if err != nil {
			fatal(err)
		}
		for _, b := range bans {
			board, exp := "every board", "never"
			if b.Board != "" {
				board = "/" + b.Board + "/"
			}
			if !b.Expires.IsZero() {
				exp = b.Expires.Format("2006-01-02 15:04")
			}
			fmt.Printf("%4d %-20s %-12s until %-16s by %-10s %s\n", b.ID, b.IP, board, exp, b.By, b.Reason)
		}
	case "add":
		// ban add ADDRESS DAYS [BOARD|-] [REASON...]
		if len(args) < 3 {
			usage()
		}
		ip := args[1]
		if net.ParseIP(ip) == nil && !strings.HasSuffix(ip, ".") && !strings.HasSuffix(ip, ":") {
			fatal(errors.New("an address (192.0.2.7), or a range ending in . or : (192.0.2.)"))
		}
		days, err := strconv.Atoi(args[2])
		if err != nil || days < 0 || days > 3650 {
			fatal(errors.New("a ban is for 0 (for ever) to 3650 days"))
		}
		ban := &store.Ban{IP: ip, By: "sysop"}
		if len(args) > 3 && args[3] != "-" {
			if _, err := st.Board(args[3]); err != nil {
				fatal(fmt.Errorf("/%s/: %w", args[3], err))
			}
			ban.Board = args[3]
		}
		if len(args) > 4 {
			ban.Reason = bbs.Clean(strings.Join(args[4:], " "), 200)
		}
		if days > 0 {
			ban.Expires = time.Now().Add(time.Duration(days) * 24 * time.Hour)
		}
		id, err := st.AddBan(ban)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("ban %d: %s\n", id, ip)
	case "del":
		if len(args) != 2 {
			usage()
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			usage()
		}
		if err := st.DeleteBan(id); err != nil {
			fatal(err)
		}
		fmt.Println("lifted ban", id)
	default:
		usage()
	}
}

// sizeArg is a size: bytes, or with K or M.
func sizeArg(v string) (int64, error) {
	v = strings.ToUpper(strings.TrimSpace(v))
	mult := int64(1)
	switch {
	case strings.HasSuffix(v, "K"):
		mult, v = 1<<10, strings.TrimSuffix(v, "K")
	case strings.HasSuffix(v, "M"):
		mult, v = 1<<20, strings.TrimSuffix(v, "M")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > 1<<30 {
		return 0, errors.New("max_file: a size, as 4M or 512K (0: [chan]'s)")
	}
	return n * mult, nil
}
