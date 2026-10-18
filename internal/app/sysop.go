package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/confui"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// The sysop's tools: the nodes (watching, chat, kicking), the users, the
// statistics, the hack log and bans, the scheduled events, the
// configuration, and whether pages reach them.

// sysopMenu is the sysop's tools until Back, or nothing if the caller is no
// longer a sysop: watching a node (on a text terminal), chat, kicking, the
// users, statistics, the hack log, the events and configuration where there are
// any, and whether pages reach the sysop.
func (s *Session) sysopMenu() error {
	for {
		u := s.refresh()
		if !u.Sysop {
			return nil
		}
		s.N.Doing("Sysop tools")
		avail := "yes"
		if !s.B.SysopAvailable() {
			avail = "no"
		}
		var items []view.Item
		if s.UI.Kind() == Text {
			items = append(items, view.Item{Key: "W", Label: "Watch a node"})
		}
		items = append(items, view.Item{Key: "C", Label: "Chat with a node"},
			view.Item{Key: "K", Label: "Kick a node"},
			view.Item{Key: "U", Label: "Users"},
			view.Item{Key: "S", Label: "Statistics"},
			view.Item{Key: "H", Label: "Hack log and bans"})
		if s.B.Events != nil {
			items = append(items, view.Item{Key: "E", Label: "Scheduled events"})
		}
		if s.B.Config != nil {
			items = append(items, view.Item{Key: "F", Label: "Configuration"})
		}
		items = append(items, view.Item{Key: "A", Label: "Available: " + avail},
			view.Item{Key: "Q", Label: "Back"})
		k, err := s.UI.Menu(&view.Menu{Title: "Sysop tools", Items: items, Brief: s.expert()})
		if err != nil {
			return err
		}
		switch strings.ToUpper(k) {
		case "Q", view.Back:
			return nil
		case "W":
			if n, ok, err := s.pickNode("Watch which node?"); err != nil || !ok {
				if err != nil {
					return err
				}
			} else if err := s.UI.Watch(n); err != nil {
				return err
			}
		case "C":
			if err := s.chatWith(); err != nil {
				return err
			}
		case "K":
			n, ok, err := s.pickNode("Kick which node?")
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if yes, err := s.UI.Confirm(fmt.Sprintf("Throw node %d's caller off?", n), false); err != nil || !yes {
				if err != nil {
					return err
				}
				continue
			}
			if err := s.B.Kick(n); err != nil {
				s.UI.Say(view.Bad, capital(err.Error())+".")
			} else {
				s.B.Log.Printf("node %d: %s kicked node %d", s.N.Num, u.Name, n)
				s.UI.Say(view.Good, fmt.Sprintf("Node %d is off.", n))
			}
		case "U":
			err = s.userEditor()
		case "S":
			err = s.statistics()
		case "H":
			err = s.hackLog()
		case "E":
			if s.B.Events != nil {
				err = s.eventsScreen()
			}
		case "F":
			if s.B.Config != nil {
				s.N.Doing("Configuration")
				err = (&confui.Editor{UI: s.UI, Tools: *s.B.Config, Who: u.Name, Log: s.B.Log.Printf,
					Open: func() (*bbs.BBS, error) { return s.B, nil }}).Run()
			}
		case "A":
			s.B.SetSysopAvailable(!s.B.SysopAvailable())
		}
		if err != nil {
			return err
		}
	}
}

// pickNode: a node chosen from those online but the sysop's own.
func (s *Session) pickNode(prompt string) (int, bool, error) {
	t := &view.Table{Title: "Nodes", Pick: "Node", Prompt: prompt + " (its number, Enter for none): ",
		Empty: "No one else is on.",
		Columns: []view.Column{{Title: "User", Width: 20}, {Title: "Via", Width: 16}, {Title: "From", Width: 24},
			{Title: "Doing", Wide: true}}}
	for _, n := range s.B.Nodes() {
		if n.Num == s.N.Num {
			continue
		}
		name := n.User
		if name == "" {
			name = "(logging in)"
		}
		t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(n.Num), Cells: []string{name, n.Via, placeOf(n.Place), n.Activity}})
	}
	c, err := s.UI.Table(t)
	if err != nil || c.Action != view.Pick {
		return 0, false, err
	}
	n, err := strconv.Atoi(c.Num)
	if err != nil {
		s.UI.Say(view.Bad, "That is not a node's number.")
		return 0, false, nil
	}
	return n, true, nil
}

// chatWith calls a caller into chat with the sysop.
func (s *Session) chatWith() error {
	n, ok, err := s.pickNode("Chat with which node?")
	if err != nil || !ok {
		return err
	}
	room, err := s.N.Summon(n)
	if err != nil {
		s.UI.Say(view.Bad, capital(err.Error())+".")
		return nil
	}
	return s.UI.Chat(room, fmt.Sprintf("Node %d's caller is called here at their next menu.", n))
}

// summoned takes the caller into chat if the sysop has called them.
func (s *Session) summoned() error {
	if room := s.N.Summoned(); room != "" {
		return s.UI.Chat(room, "The sysop would like a word.")
	}
	return nil
}

// --- users

// userEditor is the users, with their levels, flags and last calls, until Back;
// one picked is edited.
func (s *Session) userEditor() error {
	for {
		us, err := s.B.Store.Users()
		if err != nil {
			return err
		}
		t := &view.Table{Title: "Users", Pick: "Number to edit",
			Columns: []view.Column{{Title: "User", Width: 22}, {Title: "Level", Width: 5, Right: true},
				{Title: "Flags", Width: 8}, {Title: "", Width: 7}, {Title: "Last call", Width: 10, Wide: true}}}
		for i, u := range us {
			state := ""
			switch {
			case u.Locked:
				state = "locked"
			case u.Sysop:
				state = "sysop"
			}
			last := "never"
			if !u.LastLogin.IsZero() {
				last = u.LastLogin.Format("2006-01-02")
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Hot: u.Sysop,
				Cells: []string{u.Name, strconv.Itoa(u.Level), u.Flags, state, last}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		if c.Action != view.Pick || c.Row < 0 {
			continue
		}
		if err := s.editUser(us[c.Row].ID); err != nil {
			return err
		}
	}
}

// editUser is user id's panel for a sysop, until Back: the level and flags,
// locking and sysop rights (not for the sysop's own account), and a new
// password (not for an external account). Each change is logged. A user that
// cannot be read ends it quietly.
func (s *Session) editUser(id int64) error {
	by := fmt.Sprintf("node %d: %s", s.N.Num, s.User.Name)
	for {
		u, err := s.B.Store.UserByID(id)
		if err != nil {
			return nil
		}
		lvl := s.B.LevelOf(u)
		p := &view.Panel{Title: u.Name, Lines: []view.Line{
			view.V("From:", u.Location),
			view.V("Member:", fmt.Sprintf("since %s, %d calls, %d posts", u.Created.Format("2006-01-02"), u.Logins, u.Posts)),
			view.V("Access:", fmt.Sprintf("level %d (%s), flags %q", u.Level, lvl.Name, u.Flags)),
			view.V("Files:", fmt.Sprintf("%d downloaded, %d uploaded", u.Downloads, u.Uploads)),
			view.V("State:", map[bool]string{true: "locked", false: "may log in"}[u.Locked]+
				map[bool]string{true: ", a sysop", false: ""}[u.Sysop])},
			Actions: []view.Action{{ID: "level", Label: "Level", Key: 'L', PF: 5},
				{ID: "flags", Label: "Flags", Key: 'F', PF: 6}}}
		if u.ID != s.User.ID {
			p.Actions = append(p.Actions,
				view.Action{ID: "lock", Label: map[bool]string{true: "Unlock", false: "Lock"}[u.Locked], Key: 'K', PF: 9},
				view.Action{ID: "sysop", Label: map[bool]string{true: "Sysop no more", false: "Sysop"}[u.Sysop], Key: 'S', PF: 10})
		}
		if !u.External {
			p.Actions = append(p.Actions, view.Action{ID: "password", Label: "Password", Key: 'P', PF: 11})
		}
		act, err := s.UI.Panel(p)
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "level":
			v, ok, err := s.UI.Form(&view.Form{Title: "Level for " + u.Name, Submit: "Set", Fields: []view.Field{
				{Name: "level", Label: "Level (0-255)", Width: 3, Value: strconv.Itoa(u.Level)}}})
			if err != nil {
				return err
			}
			l, e := strconv.Atoi(strings.TrimSpace(v["level"]))
			if !ok {
				continue
			}
			if e != nil || l < 0 || l > bbs.MaxLevel {
				s.UI.Say(view.Bad, "That is not a level.")
				continue
			}
			if err := s.B.Store.SetLevel(u.ID, l); err != nil {
				return err
			}
			s.B.Log.Printf("%s set %s's level to %d", by, u.Name, l)
		case "flags":
			v, ok, err := s.UI.Form(&view.Form{Title: "Flags for " + u.Name, Submit: "Set", Fields: []view.Field{
				{Name: "flags", Label: "Flags", Prompt: "Flags (letters; +AB -C adds and takes): ", Width: 30}}})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			fl, err := bbs.EditFlags(u.Flags, v["flags"])
			if err != nil {
				s.UI.Say(view.Bad, capital(err.Error())+".")
				continue
			}
			if err := s.B.Store.SetFlags(u.ID, fl); err != nil {
				return err
			}
			s.B.Log.Printf("%s set %s's flags to %q", by, u.Name, fl)
		case "lock":
			if u.ID == s.User.ID {
				continue
			}
			if err := s.B.Store.SetLocked(u.ID, !u.Locked); err != nil {
				return err
			}
			s.B.Log.Printf("%s %s %s", by, map[bool]string{true: "unlocked", false: "locked"}[u.Locked], u.Name)
		case "sysop":
			if u.ID == s.User.ID {
				continue
			}
			if ok, err := s.UI.Confirm(map[bool]string{true: "Take " + u.Name + "'s sysop rights away?",
				false: "Make " + u.Name + " a sysop?"}[u.Sysop], false); err != nil || !ok {
				if err != nil {
					return err
				}
				continue
			}
			if err := s.B.Store.SetSysop(u.ID, !u.Sysop); err != nil {
				return err
			}
			s.B.Log.Printf("%s made %s %s", by, u.Name, map[bool]string{true: "no longer a sysop", false: "a sysop"}[u.Sysop])
		case "password":
			v, ok, err := s.UI.Form(&view.Form{Title: "A new password for " + u.Name, Submit: "Set", Fields: []view.Field{
				{Name: "pw", Label: "New password", Width: 32, Secret: true},
				{Name: "again", Label: "Again", Width: 32, Secret: true}}})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if v["pw"] != v["again"] {
				s.UI.Say(view.Bad, "They differ.")
				continue
			}
			if msg := bbs.ValidPassword(v["pw"], u.Name); msg != "" {
				s.UI.Say(view.Bad, msg)
				continue
			}
			if err := s.B.Store.SetPassword(u.ID, v["pw"]); err != nil {
				return err
			}
			s.B.Log.Printf("%s set a new password for %s", by, u.Name)
			s.UI.Say(view.Good, "Set.")
		}
	}
}

// --- statistics

// days is how many days the statistics go back.
const days = 14

// statistics shows the totals, the counts since the BBS started, and the calls,
// new users, posts and uploads of each of the last 14 (days) days, as bars against
// the busiest day.
func (s *Session) statistics() error {
	s.N.Doing("Statistics")
	totals, err := s.B.Store.Totals()
	if err != nil {
		return err
	}
	lines := []view.Line{
		view.V("Users:", strconv.Itoa(totals["users"])),
		view.V("Messages:", strconv.Itoa(totals["messages"])),
		view.V("Mail:", strconv.Itoa(totals["mail"])),
		view.V("Files:", strconv.Itoa(totals["files"])),
		view.V("Since start:", fmt.Sprintf("%d logins, %d posts, %d pages, %d failed logins",
			s.B.Stats.Logins.Load(), s.B.Stats.Posts.Load(), s.B.Stats.Pages.Load(), s.B.Stats.LoginFailures.Load())),
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	for _, what := range []struct{ key, title string }{{"calls", "Calls"}, {"users", "New users"},
		{"posts", "Posts"}, {"uploads", "Uploads"}} {
		ts, err := s.B.Store.Times(what.key, start)
		if err != nil {
			return err
		}
		per := make([]int, days)
		for _, t := range ts {
			if d := int(t.In(now.Location()).Sub(start).Hours() / 24); d >= 0 && d < days {
				per[d]++
			}
		}
		most, total := 0, 0
		for _, n := range per {
			most, total = max(most, n), total+n
		}
		lines = append(lines, view.L(""), view.T(view.Bright, fmt.Sprintf("%s by day (%d in %d days)", what.title, total, days)))
		for d := days - 1; d >= 0; d-- {
			share := 0.0
			if most > 0 {
				share = float64(per[d]) / float64(most)
			}
			lines = append(lines, view.Line{Label: start.AddDate(0, 0, d).Format("Mon 01-02"),
				Text: strconv.Itoa(per[d]), Bar: share})
		}
	}
	return s.UI.Show("Statistics", lines)
}

// --- the hack log and bans

// hackLog is the banned addresses until Back: one picked has its ban lifted
// (and that put in the hack log); Log shows the hack log's last 100 lines.
func (s *Session) hackLog() error {
	for {
		s.N.Doing("Hack log")
		bans := s.B.Guard.Bans()
		ips := make([]string, 0, len(bans))
		for ip := range bans {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		t := &view.Table{Title: "Bans", Empty: "No address is banned.", Pick: "Number to lift its ban",
			Columns: []view.Column{{Title: "Address", Width: 39}, {Title: "Until", Width: 16}},
			Actions: []view.Action{{ID: "log", Label: "Log", Key: 'L', PF: 5}}}
		for i, ip := range ips {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
				Cells: []string{ip, bans[ip].Format("2006-01-02 15:04")}})
		}
		if s.B.Guard == nil || s.B.Guard.HackLog == "" {
			t.Note = []view.Line{view.T(view.Dim, "There is no hack log: [security] hack_log in bbs.conf.")}
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		switch {
		case c.Action == "log":
			var ls []view.Line
			for _, l := range s.B.Guard.Tail(100) {
				ls = append(ls, view.L(l))
			}
			if len(ls) == 0 {
				ls = []view.Line{view.L("The hack log is empty.")}
			}
			if err := s.UI.Show("Hack log", ls); err != nil {
				return err
			}
		case c.Action == view.Pick && c.Row >= 0:
			if s.B.Guard.Unban(ips[c.Row]) {
				s.B.Guard.Hack("%s ban lifted by %s", ips[c.Row], s.User.Name)
				s.UI.Say(view.Good, "The ban on "+ips[c.Row]+" is lifted.")
			}
		}
	}
}

// --- scheduled events

// eventsScreen is the scheduled events until Back, with their schedules and
// last runs (failures marked); one picked shows its command and last output,
// and may be run now.
func (s *Session) eventsScreen() error {
	for {
		s.N.Doing("Scheduled events")
		st := s.B.Events.Status()
		t := &view.Table{Title: "Scheduled events", Pick: "Number to see (and run)",
			Columns: []view.Column{{Title: "Event", Width: 14}, {Title: "When", Width: 18},
				{Title: "Next", Width: 11}, {Title: "Last", Wide: true}}}
		for i, e := range st {
			last := "not yet"
			if e.Running {
				last = "running"
			} else if e.Last != nil {
				last = e.Last.Describe()
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: e.Last != nil && e.Last.Err != "",
				Cells: []string{e.Name, e.Schedule(), e.Next.Format("01-02 15:04"), last}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		if c.Action != view.Pick || c.Row < 0 {
			continue
		}
		e := st[c.Row]
		lines := []view.Line{view.V("Command:", strings.Join(e.Command, " ")), view.V("When:", e.Schedule())}
		if e.Last != nil {
			lines = append(lines, view.V("Last:", e.Last.Describe()), view.L(""))
			for _, l := range strings.Split(strings.TrimRight(e.Last.Output, "\n"), "\n") {
				lines = append(lines, view.L(bbs.Clean(l, 200)))
			}
		}
		act, err := s.UI.Panel(&view.Panel{Title: "Event " + e.Name, Lines: lines,
			Actions: []view.Action{{ID: "run", Label: "Run it now", Key: 'R', PF: 5, Short: "Run"}}})
		if err != nil {
			return err
		}
		if act == "run" {
			s.UI.Say(view.Plain, "Running "+e.Name+"...")
			r, err := s.B.Events.RunNow(context.Background(), e.Name, s.User.Name)
			switch {
			case errors.Is(err, events.ErrRunning):
				s.UI.Say(view.Bad, e.Name+" is running already.")
			case err != nil:
				s.UI.Say(view.Bad, e.Name+": "+err.Error()+".")
			case r.Err != "":
				s.UI.Say(view.Bad, e.Name+": "+r.Err+".")
			default:
				s.UI.Say(view.Good, e.Name+": done.")
			}
		}
	}
}

// placeOf is where a caller is, as the sysop sees it: the number a modem
// caller called from (and that it is a mobile's), and the place.
func placeOf(p bbs.Place) string {
	where := p.String()
	num := p.Number
	if num != "" && p.Kind == "mobile" {
		num += " (mobile)"
	}
	switch {
	case num == "":
		return where
	case where == "":
		return num
	}
	return num + ", " + where
}
