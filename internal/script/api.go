package script

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// Limits on what scripts keep.
const (
	maxKey        = 100      // a name's characters
	maxValue      = 64 << 10 // a value's bytes, as JSON
	maxGlobalKeys = 1000     // names a script keeps for everyone
	maxUserKeys   = 100      // and for each user
	maxList       = 200      // messages a list gives
)

// bind sets up the script's world.
func (r *run) bind() error {
	vm := r.vm
	set := func(name string, v any) {
		if err := vm.Set(name, v); err != nil {
			panic(err)
		}
	}
	set("ui", r.uiObject())
	set("bbs", r.bbsObject())
	set("user", r.userObject())
	set("node", r.nodeObject())
	set("msg", r.msgObject())
	data := r.dataObject(0)
	if u := r.n.User(); u != nil {
		data.Set("user", r.dataObject(u.ID))
	}
	set("data", data)
	set("exit", func() { vm.Interrupt(errExit) })
	console := vm.NewObject()
	console.Set("log", func(c goja.FunctionCall) goja.Value {
		var parts []string
		for _, a := range c.Arguments {
			parts = append(parts, a.String())
		}
		r.b.Log.Printf("node %d: script %s: %s", r.n.Num, r.name, bbs.Clean(strings.Join(parts, " "), 400))
		return goja.Undefined()
	})
	set("console", console)
	return nil
}

// --- reading what a script gives

// obj is a script's object as a map (nil if it is not one).
func obj(v goja.Value) map[string]any {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	m, _ := v.Export().(map[string]any)
	return m
}

// str is m[k] as a string: "" for none, a date in RFC 3339, anything else as
// fmt prints it.
func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

// num is m[k] as an int: a number (a fraction cut off), or a string of digits;
// 0 for anything else.
func num(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// flag is m[k] if it is a bool; false otherwise.
func flag(m map[string]any, k string) bool {
	v, _ := m[k].(bool)
	return v
}

// list is v if it is an array; nil otherwise.
func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// key is an action's key: its first character.
func key(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

var toneNames = map[string]view.Tone{"": view.Plain, "plain": view.Plain, "dim": view.Dim,
	"bright": view.Bright, "good": view.Good, "bad": view.Bad, "warn": view.Warn}

// tone is the view.Tone a script names, in any case; Plain for a name it does
// not know.
func tone(s string) view.Tone { return toneNames[strings.ToLower(s)] }

// lines are a script's lines: strings, or {label, text, tone, bar}.
func lines(v any) []view.Line {
	var out []view.Line
	for _, l := range list(v) {
		switch l := l.(type) {
		case string:
			for _, s := range strings.Split(l, "\n") {
				out = append(out, view.L(bbs.Clean(s, 400)))
			}
		case map[string]any:
			line := view.Line{Label: bbs.Clean(str(l, "label"), 40), Text: bbs.Clean(str(l, "text"), 400),
				Tone: tone(str(l, "tone")), Bar: -1}
			if b, ok := l["bar"].(float64); ok {
				line.Bar = min(max(b, 0), 1)
			} else if b, ok := l["bar"].(int64); ok {
				line.Bar = min(max(float64(b), 0), 1)
			}
			out = append(out, line)
		}
	}
	return out
}

// actions are a script's actions, {id, label, key, pf, short}, cleaned: the key
// in upper case, a PF key past 0 to 24 none. What is not an object is skipped.
func actions(v any) []view.Action {
	var out []view.Action
	for _, a := range list(v) {
		m, _ := a.(map[string]any)
		if m == nil {
			continue
		}
		pf := num(m, "pf")
		if pf < 0 || pf > 24 {
			pf = 0
		}
		out = append(out, view.Action{ID: str(m, "id"), Label: bbs.Clean(str(m, "label"), 40),
			Key: key(strings.ToUpper(str(m, "key"))), PF: pf, Short: bbs.Clean(str(m, "short"), 20)})
	}
	return out
}

// clean is a script's text made one line safe to show, at most 200 runes.
func clean(s string) string { return bbs.Clean(s, 200) }

// prompt is a prompt cleaned, keeping the space it ends in.
func prompt(s string) string {
	if c := clean(s); c != "" && strings.HasSuffix(s, " ") {
		return c + " "
	}
	return clean(s)
}

// --- ui

// uiObject is the script's ui: the terminal's kind, page size and narrowness,
// the choices' names (BACK, PICK, ...), and menu, table, message, form, panel,
// edit, confirm, say, show and screen, drawing the shared screens with what the
// script gives cleaned. A form left or a draft not sent is null.
func (r *run) uiObject() *goja.Object {
	vm := r.vm
	o := vm.NewObject()
	o.Set("kind", r.ui.Kind())
	o.Set("pageSize", r.ui.PageSize())
	o.Set("narrow", r.ui.Narrow())
	for k, v := range map[string]string{"BACK": view.Back, "PICK": view.Pick, "MORE": view.More,
		"LESS": view.Less, "CMD": view.Cmd} {
		o.Set(k, v)
	}

	o.Set("menu", func(c goja.FunctionCall) goja.Value {
		m := obj(c.Argument(0))
		vmenu := &view.Menu{Title: clean(str(m, "title")), Intro: lines(m["intro"]), Prompt: prompt(str(m, "prompt")),
			Screen: str(m, "screen")}
		for _, it := range list(m["items"]) {
			im, _ := it.(map[string]any)
			if k := strings.ToUpper(str(im, "key")); k != "" {
				vmenu.Items = append(vmenu.Items, view.Item{Key: string(key(k)), Label: clean(str(im, "label"))})
			}
		}
		if len(vmenu.Items) == 0 {
			r.throw("ui.menu: no items")
		}
		var k string
		r.wait(func() (err error) { k, err = r.ui.Menu(vmenu); return })
		return vm.ToValue(k)
	})

	o.Set("table", func(c goja.FunctionCall) goja.Value {
		m := obj(c.Argument(0))
		t := &view.Table{Title: clean(str(m, "title")), Note: lines(m["note"]), Empty: clean(str(m, "empty")),
			Pick: clean(str(m, "pick")), Prompt: prompt(str(m, "prompt")), Actions: actions(m["actions"]),
			More: flag(m, "more"), Less: flag(m, "less"), Command: clean(str(m, "command"))}
		for _, col := range list(m["columns"]) {
			cm, _ := col.(map[string]any)
			t.Columns = append(t.Columns, view.Column{Title: clean(str(cm, "title")),
				Width: min(max(num(cm, "width"), 0), 200), Right: flag(cm, "right"), Wide: flag(cm, "wide")})
		}
		for i, row := range list(m["rows"]) {
			var vr view.Row
			switch row := row.(type) {
			case []any: // the cells alone
				for _, cell := range row {
					vr.Cells = append(vr.Cells, clean(fmt.Sprint(cell)))
				}
			case map[string]any:
				vr = view.Row{Num: clean(str(row, "num")), Mark: flag(row, "mark"), Hot: flag(row, "hot")}
				for _, cell := range list(row["cells"]) {
					vr.Cells = append(vr.Cells, clean(fmt.Sprint(cell)))
				}
			}
			if vr.Num == "" && t.Pick != "" {
				vr.Num = strconv.Itoa(i + 1)
			}
			t.Rows = append(t.Rows, vr)
		}
		var ch view.Choice
		r.wait(func() (err error) { ch, err = r.ui.Table(t); return })
		res := vm.NewObject()
		res.Set("action", ch.Action)
		res.Set("row", ch.Row)
		res.Set("num", ch.Num)
		res.Set("text", ch.Text)
		return res
	})

	o.Set("message", func(c goja.FunctionCall) goja.Value {
		m := obj(c.Argument(0))
		vmsg := &view.Message{Where: clean(str(m, "where")), From: clean(str(m, "from")), To: clean(str(m, "to")),
			Subject: clean(str(m, "subject")), Body: bbs.CleanBody(strings.Split(str(m, "body"), "\n")),
			Actions: actions(m["actions"]), Default: str(m, "default"), Quit: str(m, "quit")}
		if d, ok := m["date"].(time.Time); ok {
			vmsg.Date = d
		} else if d, err := time.Parse(time.RFC3339, str(m, "date")); err == nil {
			vmsg.Date = d
		} else {
			vmsg.Date = time.Now()
		}
		var act string
		r.wait(func() (err error) { act, err = r.ui.Message(vmsg); return })
		return vm.ToValue(act)
	})

	o.Set("form", func(c goja.FunctionCall) goja.Value {
		m := obj(c.Argument(0))
		f := &view.Form{Title: clean(str(m, "title")), Intro: lines(m["intro"]), Submit: clean(str(m, "submit"))}
		for _, fd := range list(m["fields"]) {
			fm, _ := fd.(map[string]any)
			name := str(fm, "name")
			if name == "" {
				r.throw("ui.form: a field without a name")
			}
			w := num(fm, "width")
			if w <= 0 || w > 2048 {
				w = 40
			}
			label := clean(str(fm, "label"))
			if label == "" {
				label = name
			}
			f.Fields = append(f.Fields, view.Field{Name: name, Label: label, Prompt: prompt(str(fm, "prompt")),
				Width: w, Secret: flag(fm, "secret"), Value: clean(str(fm, "value"))})
		}
		if len(f.Fields) == 0 {
			r.throw("ui.form: no fields")
		}
		var vals map[string]string
		var ok bool
		r.wait(func() (err error) { vals, ok, err = r.ui.Form(f); return })
		if !ok {
			return goja.Null()
		}
		res := vm.NewObject()
		for _, fd := range f.Fields {
			res.Set(fd.Name, bbs.Clean(vals[fd.Name], fd.Width))
		}
		return res
	})

	o.Set("panel", func(c goja.FunctionCall) goja.Value {
		m := obj(c.Argument(0))
		p := &view.Panel{Title: clean(str(m, "title")), Lines: lines(m["lines"]), Actions: actions(m["actions"])}
		var act string
		r.wait(func() (err error) { act, err = r.ui.Panel(p); return })
		return vm.ToValue(act)
	})

	o.Set("edit", func(c goja.FunctionCall) goja.Value {
		m := obj(c.Argument(0))
		var to, subject string
		var body []string
		d := &view.Draft{Title: clean(str(m, "title")), To: clean(str(m, "to")), Subject: clean(str(m, "subject")),
			Send: func(t, s string, b []string) error { to, subject, body = t, s, b; return nil }}
		if b := str(m, "body"); b != "" {
			d.Body = strings.Split(b, "\n")
		}
		if q := str(m, "quote"); q != "" {
			d.Quote = strings.Split(q, "\n")
		}
		if d.Title == "" {
			d.Title = "Write"
		}
		var sent bool
		r.wait(func() (err error) { sent, err = r.ui.Edit(d); return })
		if !sent {
			return goja.Null()
		}
		res := vm.NewObject()
		res.Set("to", bbs.Clean(to, bbs.MaxMailToLen))
		res.Set("subject", bbs.Clean(subject, bbs.MaxSubjectLen))
		res.Set("body", bbs.CleanBody(body))
		return res
	})

	o.Set("confirm", func(question string, def bool) bool {
		var ok bool
		r.wait(func() (err error) { ok, err = r.ui.Confirm(clean(question), def); return })
		return ok
	})
	o.Set("say", func(text, tn string) {
		r.ui.Say(tone(tn), bbs.Clean(text, 400))
	})
	o.Set("show", func(c goja.FunctionCall) goja.Value {
		title := clean(c.Argument(0).String())
		ls := lines(c.Argument(1).Export())
		r.wait(func() error { return r.ui.Show(title, ls) })
		return goja.Undefined()
	})
	o.Set("screen", func(name string) bool {
		var shown bool
		r.wait(func() (err error) { shown, err = r.ui.ShowScreen(name, true); return })
		return shown
	})
	return o
}

// --- the BBS, the caller, their node

// bbsObject is the script's bbs: the system's name, sysop and location, and
// log, a line in the BBS's log.
func (r *run) bbsObject() *goja.Object {
	o := r.vm.NewObject()
	o.Set("name", r.b.Conf().Name)
	o.Set("sysop", r.b.Conf().Sysop)
	o.Set("location", r.b.Conf().Location)
	o.Set("log", func(text string) {
		r.b.Log.Printf("node %d: script %s: %s", r.n.Num, r.name, bbs.Clean(text, 400))
	})
	return o
}

// userObject is the script's user: the caller as they were when the script
// began (empty before a login), and has, whether they meet an access
// requirement (bbs.ParseRequire's) now.
func (r *run) userObject() *goja.Object {
	o := r.vm.NewObject()
	u := r.n.Refresh()
	if u == nil {
		return o
	}
	o.Set("id", u.ID)
	o.Set("name", u.Name)
	o.Set("location", u.Location)
	o.Set("level", u.Level)
	o.Set("flags", u.Flags)
	o.Set("sysop", u.Sysop)
	o.Set("calls", u.Logins)
	o.Set("posts", u.Posts)
	o.Set("since", u.Created.Format(time.RFC3339))
	if !u.LastLogin.IsZero() {
		o.Set("lastCall", u.LastLogin.Format(time.RFC3339))
	}
	o.Set("has", func(req string) bool {
		rq, err := bbs.ParseRequire(req)
		if err != nil {
			r.throw("user.has: %v", err)
		}
		return rq.Allows(r.n.Refresh())
	})
	return o
}

// nodeObject is the script's node: its number, doing (what who's online shows),
// who (the nodes in use), and message, a node message to another.
func (r *run) nodeObject() *goja.Object {
	o := r.vm.NewObject()
	o.Set("number", r.n.Num)
	o.Set("doing", func(text string) { r.n.Doing(bbs.Clean(text, 60)) })
	o.Set("who", func() []map[string]any {
		var out []map[string]any
		for _, n := range r.b.Nodes() {
			out = append(out, map[string]any{"node": n.Num, "user": n.User, "via": n.Via,
				"minutes": int(time.Since(n.Since).Minutes()), "doing": n.Activity})
		}
		return out
	})
	o.Set("message", func(num int, text string) {
		if err := r.n.Message(num, text); err != nil {
			r.throw("%v", err)
		}
	})
	return o
}

// --- messages and mail, with the caller's access

// messageOut is a message as a script sees it.
func messageOut(m *store.Message) map[string]any {
	return map[string]any{"id": m.ID, "area": m.AreaID, "from": m.From, "to": m.To, "subject": m.Subject,
		"body": m.Body, "date": m.Created.Format(time.RFC3339), "replyTo": m.ReplyTo}
}

// area is area id if the caller may read it; otherwise it throws, as if there
// were no such area.
func (r *run) area(id int64) *store.Area {
	a, err := r.b.Store.Area(id)
	if err != nil || !r.b.CanRead(r.n.User(), a) {
		r.throw("there is no area %d", id)
	}
	return a
}

// body is a script's body: a string, or lines.
func body(v any) []string {
	switch v := v.(type) {
	case string:
		return strings.Split(v, "\n")
	case []any:
		var out []string
		for _, l := range v {
			out = append(out, fmt.Sprint(l))
		}
		return out
	}
	return nil
}

// msgObject is the script's msg, with the caller's access: the areas they may
// read, a page of an area's messages, a message by its id (null if they may not
// read it), and posting and mail as the caller.
func (r *run) msgObject() *goja.Object {
	o := r.vm.NewObject()
	o.Set("areas", func() []map[string]any {
		as, err := r.b.AreasFor(r.n.User())
		if err != nil {
			r.throw("%v", err)
		}
		out := []map[string]any{}
		for _, a := range as {
			out = append(out, map[string]any{"id": a.ID, "name": a.Name, "description": a.Description,
				"messages": a.Messages, "new": a.New, "canPost": r.b.CanPost(r.n.User(), a.Area)})
		}
		return out
	})
	o.Set("list", func(area int64, after int64, limit int) []map[string]any {
		a := r.area(area)
		if limit <= 0 || limit > maxList {
			limit = 50
		}
		ms, err := r.b.Store.Messages(a.ID, after, limit)
		if err != nil {
			r.throw("%v", err)
		}
		out := []map[string]any{}
		for _, m := range ms {
			out = append(out, messageOut(m))
		}
		return out
	})
	o.Set("get", func(id int64) goja.Value {
		m, err := r.b.Store.Message(id)
		if err != nil {
			return goja.Null()
		}
		if a, err := r.b.Store.Area(m.AreaID); err != nil || !r.b.CanRead(r.n.User(), a) {
			return goja.Null()
		}
		return r.vm.ToValue(messageOut(m))
	})
	o.Set("post", func(area int64, msg goja.Value) int64 {
		a := r.area(area)
		m := obj(msg)
		id, err := r.n.Post(a, str(m, "to"), str(m, "subject"), body(m["body"]), int64(num(m, "replyTo")))
		if err != nil {
			r.throw("%v", err)
		}
		return id
	})
	o.Set("mail", func(msg goja.Value) {
		m := obj(msg)
		if err := r.n.SendMail(str(m, "to"), str(m, "subject"), body(m["body"]), 0); err != nil {
			r.throw("%v", err)
		}
	})
	return o
}

// --- data the script keeps: for everyone (user 0), or for a user

// checkKey throws unless k is a name a script may keep data under: 1 to maxKey
// characters of valid UTF-8.
func (r *run) checkKey(k string) {
	if k == "" || utf8.RuneCountInString(k) > maxKey || !utf8.ValidString(k) {
		r.throw("a name is 1 to %d characters", maxKey)
	}
}

// dataObject is the script's data, kept for everyone (user 0) or for that user:
// get (undefined if there is none), set as JSON (up to maxValue bytes, and a
// limit on the names), remove, and keys, the names with a prefix.
func (r *run) dataObject(user int64) *goja.Object {
	o := r.vm.NewObject()
	most := maxGlobalKeys
	if user != 0 {
		most = maxUserKeys
	}
	o.Set("get", func(k string) goja.Value {
		r.checkKey(k)
		v, ok, err := r.b.Store.ScriptGet(r.name, user, k)
		if err != nil {
			r.throw("%v", err)
		}
		if !ok {
			return goja.Undefined()
		}
		var x any
		if json.Unmarshal([]byte(v), &x) != nil {
			return goja.Undefined()
		}
		return r.vm.ToValue(x)
	})
	o.Set("set", func(k string, v goja.Value) {
		r.checkKey(k)
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v.Export()); err != nil {
			r.throw("data.set: %v", err)
		}
		b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		if len(b) > maxValue {
			r.throw("data.set: the value is over %d bytes", maxValue)
		}
		if _, had, _ := r.b.Store.ScriptGet(r.name, user, k); !had {
			if n, _ := r.b.Store.ScriptCount(r.name, user); n >= most {
				r.throw("data.set: that is the %d names a script keeps", most)
			}
		}
		if err := r.b.Store.ScriptSet(r.name, user, k, string(b)); err != nil {
			r.throw("%v", err)
		}
	})
	o.Set("remove", func(k string) {
		r.checkKey(k)
		if err := r.b.Store.ScriptDelete(r.name, user, k); err != nil {
			r.throw("%v", err)
		}
	})
	o.Set("keys", func(prefix string) []string {
		ks, err := r.b.Store.ScriptNames(r.name, user, prefix, most)
		if err != nil {
			r.throw("%v", err)
		}
		if ks == nil {
			ks = []string{}
		}
		return ks
	})
	return o
}
