package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func (s *Server) routes() map[string]route {
	return map[string]route{
		"POST /api/v1/login":  {h: s.login},
		"POST /api/v1/logout": {scope: "any", h: s.logout},
		"GET /api/v1/system":  {anon: true, h: s.system},
		"GET /api/v1/who":     {anon: true, h: s.who},

		"GET /api/v1/me":                {scope: "read", h: s.me},
		"PATCH /api/v1/me":              {scope: "read", h: s.patchMe},
		"GET /api/v1/me/tokens":         {scope: "any", h: s.myTokens},
		"DELETE /api/v1/me/tokens/{id}": {scope: "any", h: s.revokeMine},
		"GET /api/v1/me/keys":           {scope: "read", h: s.myKeys},
		"POST /api/v1/me/keys":          {scope: "any", h: s.addKey},
		"DELETE /api/v1/me/keys/{id}":   {scope: "any", h: s.delKey},
		"GET /api/v1/users":             {scope: "read", h: s.users},
		"GET /api/v1/users/{name}":      {scope: "read", h: s.user},

		"GET /api/v1/areas":                {scope: "read", h: s.areas},
		"GET /api/v1/areas/{id}":           {scope: "read", h: s.area},
		"GET /api/v1/areas/{id}/messages":  {scope: "read", h: s.messages},
		"POST /api/v1/areas/{id}/messages": {scope: "post", h: s.post},
		"PUT /api/v1/areas/{id}/lastread":  {scope: "read", h: s.lastRead},
		"GET /api/v1/messages/{id}":        {scope: "read", h: s.message},

		"GET /api/v1/mail":         {scope: "mail", h: s.mail},
		"GET /api/v1/mail/{id}":    {scope: "mail", h: s.mailOne},
		"DELETE /api/v1/mail/{id}": {scope: "mail", h: s.mailDelete},
		"POST /api/v1/mail":        {scope: "mail", h: s.sendMail},

		"GET /api/v1/fileareas":             {scope: "files", h: s.fileAreas},
		"GET /api/v1/fileareas/{id}/files":  {scope: "files", h: s.files},
		"POST /api/v1/fileareas/{id}/files": {scope: "files", h: s.upload},
		"GET /api/v1/files/{id}":            {scope: "files", h: s.file},
		"GET /api/v1/files/{id}/content":    {scope: "files", h: s.download},

		"GET /api/v1/chat/rooms":         {scope: "chat", h: s.rooms},
		"POST /api/v1/nodes/{n}/message": {scope: "chat", h: s.nodeMessage},
		"POST /api/v1/page":              {scope: "chat", h: s.page},
		"GET /api/v1/events":             {scope: "any", h: s.events},
		"GET /api/v1/ws":                 {h: s.wsJSON},
		"GET /api/v1/ws/proto":           {h: s.wsProto},

		"GET /api/v1/admin/pages":                {scope: "sysop", h: s.pages},
		"POST /api/v1/admin/pages/{node}/answer": {scope: "sysop", h: s.answer},
		"POST /api/v1/admin/users":               {scope: "sysop", h: s.addUser},
		"PATCH /api/v1/admin/users/{name}":       {scope: "sysop", h: s.patchUser},
		"POST /api/v1/admin/areas":               {scope: "sysop", h: s.addArea},
		"PATCH /api/v1/admin/areas/{id}":         {scope: "sysop", h: s.patchArea},
		"POST /api/v1/admin/nodes/{n}/kick":      {scope: "sysop", h: s.kick},
		"GET /api/v1/admin/fido":                 {scope: "sysop", h: s.fido},
		"POST /api/v1/admin/fido/poll":           {scope: "sysop", h: s.fidoPoll},
		"GET /api/v1/admin/news":                 {scope: "sysop", h: s.news},
		"POST /api/v1/admin/news/sync":           {scope: "sysop", h: s.newsSync},

		"GET /api/v1/openapi.json": {h: s.openapi},
	}
}

// --- shapes

type userJSON struct {
	Name      string     `json:"name"`
	Location  string     `json:"location,omitempty"`
	Sysop     bool       `json:"sysop"`
	Created   time.Time  `json:"created"`
	LastLogin *time.Time `json:"last_login,omitempty"`
	Calls     int        `json:"calls"`
	Posts     int        `json:"posts"`
	Node      int        `json:"node,omitempty"` // online on, now
	Locked    bool       `json:"locked,omitempty"`
	Level     *int       `json:"level,omitempty"` // to sysops and the user
	Flags     string     `json:"flags,omitempty"`
}

// userView is a user as the request's user may see them: their access
// only to sysops and themselves.
func (s *Server) userView(r *http.Request, u *store.User) userJSON {
	j := s.userOut(u)
	if me := whoOf(r).user; me != nil && (me.Sysop || me.ID == u.ID) {
		level := u.Level
		j.Level, j.Flags = &level, u.Flags
	}
	return j
}

func (s *Server) userOut(u *store.User) userJSON {
	j := userJSON{Name: u.Name, Location: u.Location, Sysop: u.Sysop, Created: u.Created,
		Calls: u.Logins, Posts: u.Posts, Locked: u.Locked}
	if !u.LastLogin.IsZero() {
		t := u.LastLogin
		j.LastLogin = &t
	}
	for _, n := range s.B.Nodes() {
		if strings.EqualFold(n.User, u.Name) {
			j.Node = n.Num
		}
	}
	return j
}

type nodeJSON struct {
	Node  int       `json:"node"`
	User  string    `json:"user"`
	Via   string    `json:"via"`
	Since time.Time `json:"since"`
	Doing string    `json:"doing"`
	Term  string    `json:"terminal,omitempty"`
}

type areaJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SysopOnly   bool   `json:"sysop_only"`
	Newsgroup   string `json:"newsgroup,omitempty"`
	EchoTag     string `json:"echo_tag,omitempty"`
	Messages    int    `json:"messages"`
	FirstID     int64  `json:"first_id"`
	LastID      int64  `json:"last_id"`
	Unread      int    `json:"unread"`
	LastRead    int64  `json:"last_read"`
	CanPost     bool   `json:"can_post"`
	ReadReq     string `json:"read_require,omitempty"`
	PostReq     string `json:"post_require,omitempty"`
}

type messageJSON struct {
	ID        int64     `json:"id"`
	Area      int64     `json:"area,omitempty"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body,omitempty"`
	ReplyTo   int64     `json:"reply_to,omitempty"`
	Created   time.Time `json:"created"`
	MessageID string    `json:"message_id,omitempty"`
	FTNMsgID  string    `json:"ftn_msgid,omitempty"`
	Read      *bool     `json:"read,omitempty"` // mail
}

func msgOut(m *store.Message, body bool) messageJSON {
	j := messageJSON{ID: m.ID, Area: m.AreaID, From: m.From, To: m.To, Subject: m.Subject,
		ReplyTo: m.ReplyTo, Created: m.Created, MessageID: m.MessageID, FTNMsgID: m.FTNMsgID}
	if body {
		j.Body = m.Body
	}
	return j
}

func mailOut(m *store.Message, body bool) messageJSON {
	j := msgOut(m, body)
	j.Area, j.MessageID = 0, ""
	r := m.Read
	j.Read = &r
	return j
}

type fileJSON struct {
	ID          int64     `json:"id"`
	Area        int64     `json:"area"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	Uploader    string    `json:"uploader"`
	Created     time.Time `json:"created"`
	Downloads   int       `json:"downloads"`
}

func fileOut(f *store.File) fileJSON {
	return fileJSON{f.ID, f.AreaID, f.Name, f.Description, f.Size, f.SHA256, f.Uploader, f.Created, f.Downloads}
}

// --- helpers

func pathID(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil && n > 0
}

func (s *Server) agent(r *http.Request) *bbs.Node {
	return s.B.Agent(whoOf(r).user, "api")
}

// lines splits a body into lines for the BBS's editor's checks.
func lines(body string) []string {
	return strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
}

// --- login

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Scopes   string `json:"scopes"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	u, err := s.B.Authenticate("http", "ip:"+hostOf(r), req.Name, req.Password)
	if err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	scopes := "read post mail chat files"
	if u.Sysop {
		scopes += " sysop"
	}
	if req.Scopes != "" {
		if scopes, err = ParseScopes(req.Scopes); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	secret, hash := NewToken()
	exp := time.Now().Add(s.S.SessionTTL)
	if _, err := s.B.Store.AddToken(&store.Token{UserID: u.ID, Name: "login from " + hostOf(r), Hash: hash,
		Scopes: scopes, Session: true, Expires: exp}); err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	s.B.Store.LoggedIn(u.ID)
	s.B.Stats.Logins.Add(1)
	s.B.Log.Printf("http: %s logged in from %s", u.Name, hostOf(r))
	writeJSON(w, http.StatusOK, map[string]any{"token": secret, "expires": exp, "user": u.Name,
		"scopes": strings.Fields(scopes)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	wh := whoOf(r)
	if !wh.token.Session {
		fail(w, http.StatusBadRequest, "an API key is revoked with DELETE /api/v1/me/tokens/{id}, not logout")
		return
	}
	s.B.Store.RevokeToken(wh.token.ID, wh.user.ID)
	w.WriteHeader(http.StatusNoContent)
}

// --- the system

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	us, _ := s.B.Store.Users()
	areas, _ := s.B.Store.Areas()
	msgs := 0
	for _, a := range areas {
		msgs += a.Messages
	}
	on := 0
	for _, n := range s.B.Nodes() {
		if n.User != "" {
			on++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": s.B.Name, "sysop": s.B.Sysop, "location": s.B.Location, "motd": s.B.MOTDText(),
		"users": len(us), "messages": msgs, "areas": len(areas), "online": on,
		"max_nodes": s.B.MaxNodes, "new_users": s.B.NewUsers,
		"features": map[string]bool{"files": s.B.Files != nil, "fido": s.Fido != nil, "news": s.News != nil,
			"doors": len(s.B.Doors) > 0, "netmail": s.B.Netmail != nil},
		"version": "bbs-sec/1"})
}

func (s *Server) who(w http.ResponseWriter, r *http.Request) {
	out := []nodeJSON{}
	for _, n := range s.B.Nodes() {
		if n.User == "" {
			continue
		}
		out = append(out, nodeJSON{n.Num, n.User, n.Via, n.Since, n.Activity, n.Terminal})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- me and users

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.B.Store.UserByID(whoOf(r).user.ID)
	if err != nil {
		fail(w, http.StatusNotFound, "no such user")
		return
	}
	j := s.userView(r, u)
	unread, _ := s.B.Store.UnreadMail(u.ID)
	lvl := s.B.LevelOf(u)
	access := map[string]any{"level": u.Level, "name": lvl.Name, "flags": u.Flags,
		"downloads_today": u.DownloadsToday(), "minutes_today": u.TimeToday() / 60,
		"post": lvl.Post, "mail": lvl.Mail, "chat": lvl.Chat, "page": lvl.Page,
		"download": lvl.Download, "upload": lvl.Upload, "doors": lvl.Doors,
		"validated": u.Sysop || u.Level >= s.B.ValidatedLevel}
	if lvl.Downloads > 0 {
		access["downloads_per_day"] = lvl.Downloads
	}
	if lvl.Time > 0 {
		access["minutes_per_day"] = int(lvl.Time.Minutes())
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": j, "unread_mail": unread, "access": access,
		"scopes": strings.Fields(whoOf(r).token.Scopes)})
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location    *string `json:"location"`
		OldPassword string  `json:"old_password"`
		NewPassword string  `json:"new_password"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	u := whoOf(r).user
	if req.Location != nil {
		s.B.Store.SetLocation(u.ID, bbs.Clean(*req.Location, bbs.MaxLocationLen))
	}
	if req.NewPassword != "" {
		if err := s.agent(r).ChangePassword(req.OldPassword, req.NewPassword); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.me(w, r)
}

func (s *Server) myTokens(w http.ResponseWriter, r *http.Request) {
	ts, _ := s.B.Store.Tokens(whoOf(r).user.ID)
	out := []map[string]any{}
	for _, t := range ts {
		m := map[string]any{"id": t.ID, "name": t.Name, "scopes": strings.Fields(t.Scopes),
			"session": t.Session, "created": t.Created, "current": t.ID == whoOf(r).token.ID}
		if !t.Expires.IsZero() {
			m["expires"] = t.Expires
		}
		if !t.LastUsed.IsZero() {
			m["last_used"] = t.LastUsed
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeMine(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || s.B.Store.RevokeToken(id, whoOf(r).user.ID) != nil {
		fail(w, http.StatusNotFound, "no such token of yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sshKeyJSON(k *store.SSHKey) map[string]any {
	m := map[string]any{"id": k.ID, "type": k.Type, "fingerprint": k.Fingerprint, "comment": k.Comment,
		"key": k.Key, "created": k.Created}
	if !k.LastUsed.IsZero() {
		m["last_used"] = k.LastUsed
	}
	return m
}

func (s *Server) myKeys(w http.ResponseWriter, r *http.Request) {
	ks, err := s.B.Store.SSHKeys(whoOf(r).user.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	out := []map[string]any{}
	for _, k := range ks {
		out = append(out, sshKeyJSON(k))
	}
	writeJSON(w, http.StatusOK, out)
}

// addKey: a key to log in by SSH with.  It opens the account to whoever
// holds the key, so only a login's session may add one, not an API key.
func (s *Server) addKey(w http.ResponseWriter, r *http.Request) {
	wh := whoOf(r)
	if !wh.token.Session {
		fail(w, http.StatusForbidden, "SSH keys are added from a login's session, not with an API key")
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	k, err := s.B.AddSSHKey(wh.user, req.Key)
	switch {
	case errors.Is(err, store.ErrKeyTaken):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		fail(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusCreated, sshKeyJSON(k))
	}
}

func (s *Server) delKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || s.B.Store.RemoveSSHKey(id, whoOf(r).user.ID) != nil {
		fail(w, http.StatusNotFound, "no such key of yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	us, err := s.B.Store.Users()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	sysop := whoOf(r).user.Sysop
	out := []userJSON{}
	for _, u := range us {
		if !u.Locked || sysop {
			out = append(out, s.userView(r, u))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	u, err := s.B.Store.UserByName(r.PathValue("name"))
	if err != nil || u.Locked && !whoOf(r).user.Sysop {
		fail(w, http.StatusNotFound, "no such user")
		return
	}
	writeJSON(w, http.StatusOK, s.userView(r, u))
}

// --- message areas

func (s *Server) areaOut(r *http.Request, a *store.Area) areaJSON {
	u := whoOf(r).user
	unread, _ := s.B.Store.NewCount(u.ID, a.ID)
	last, _ := s.B.Store.LastRead(u.ID, a.ID)
	return areaJSON{a.ID, a.Name, a.Description, a.SysopOnly, a.Newsgroup, a.EchoTag, a.Messages,
		a.FirstID, a.LastID, unread, last, s.B.CanPost(u, a), a.ReadReq, a.PostReq}
}

func (s *Server) areas(w http.ResponseWriter, r *http.Request) {
	as, err := s.B.Store.Areas()
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	out := []areaJSON{}
	for _, a := range as {
		if s.B.CanRead(whoOf(r).user, a) {
			out = append(out, s.areaOut(r, a))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) areaByPath(w http.ResponseWriter, r *http.Request) *store.Area {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, http.StatusNotFound, "no such area")
		return nil
	}
	a, err := s.B.Store.Area(id)
	if err != nil || !s.B.CanRead(whoOf(r).user, a) {
		fail(w, http.StatusNotFound, "no such area")
		return nil
	}
	return a
}

func (s *Server) area(w http.ResponseWriter, r *http.Request) {
	if a := s.areaByPath(w, r); a != nil {
		writeJSON(w, http.StatusOK, s.areaOut(r, a))
	}
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	a := s.areaByPath(w, r)
	if a == nil {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 200 {
		limit = 50
	}
	body := r.URL.Query().Get("body") != "no"
	ms, err := s.B.Store.Messages(a.ID, max(after, 0), limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	out := []messageJSON{}
	for _, m := range ms {
		out = append(out, msgOut(m, body))
	}
	resp := map[string]any{"messages": out}
	if len(ms) == limit {
		resp["next"] = fmt.Sprintf("/api/v1/areas/%d/messages?after=%d&limit=%d", a.ID, ms[len(ms)-1].ID, limit)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, http.StatusNotFound, "no such message")
		return
	}
	m, err := s.B.Store.Message(id)
	if err == nil {
		if a, err2 := s.B.Store.Area(m.AreaID); err2 != nil || !s.B.CanRead(whoOf(r).user, a) {
			err = store.ErrNotFound
		}
	}
	if err != nil {
		fail(w, http.StatusNotFound, "no such message")
		return
	}
	writeJSON(w, http.StatusOK, msgOut(m, true))
}

func (s *Server) post(w http.ResponseWriter, r *http.Request) {
	a := s.areaByPath(w, r)
	if a == nil {
		return
	}
	var req struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		ReplyTo int64  `json:"reply_to"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if req.ReplyTo != 0 {
		if p, err := s.B.Store.Message(req.ReplyTo); err != nil || p.AreaID != a.ID {
			fail(w, http.StatusBadRequest, "reply_to is not a message of this area")
			return
		}
	}
	if !s.B.CanPost(whoOf(r).user, a) {
		fail(w, http.StatusForbidden, "only the sysop posts here")
		return
	}
	id, err := s.agent(r).Post(a, req.To, req.Subject, lines(req.Body), req.ReplyTo)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	m, _ := s.B.Store.Message(id)
	w.Header().Set("Location", fmt.Sprintf("/api/v1/messages/%d", id))
	writeJSON(w, http.StatusCreated, msgOut(m, true))
}

func (s *Server) lastRead(w http.ResponseWriter, r *http.Request) {
	a := s.areaByPath(w, r)
	if a == nil {
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if req.ID < 0 || req.ID > a.LastID {
		fail(w, http.StatusBadRequest, "id: from 0 to the area's last")
		return
	}
	s.B.Store.SetLastRead(whoOf(r).user.ID, a.ID, req.ID)
	writeJSON(w, http.StatusOK, s.areaOut(r, a))
}

// --- mail

func (s *Server) mail(w http.ResponseWriter, r *http.Request) {
	in, err := s.B.Store.Inbox(whoOf(r).user.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	out := []messageJSON{}
	for _, m := range in {
		out = append(out, mailOut(m, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) mailOne(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	u := whoOf(r).user
	m, err := s.B.Store.Mail(u.ID, id)
	if !ok || err != nil {
		fail(w, http.StatusNotFound, "no such mail")
		return
	}
	s.B.Store.MarkMailRead(u.ID, id)
	m.Read = true
	writeJSON(w, http.StatusOK, mailOut(m, true))
}

func (s *Server) mailDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || s.B.Store.DeleteMail(whoOf(r).user.ID, id) != nil {
		fail(w, http.StatusNotFound, "no such mail")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sendMail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		ReplyTo int64  `json:"reply_to"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.agent(r).SendMail(req.To, req.Subject, lines(req.Body), req.ReplyTo); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sent": true})
}

// --- files

func (s *Server) noFiles(w http.ResponseWriter) bool {
	if s.B.Files == nil {
		fail(w, http.StatusNotFound, "there are no file areas")
		return true
	}
	return false
}

func (s *Server) fileAreas(w http.ResponseWriter, r *http.Request) {
	if s.noFiles(w) {
		return
	}
	fas, _ := s.B.Store.FileAreas()
	out := []map[string]any{}
	u := whoOf(r).user
	for _, fa := range fas {
		if !s.B.CanDownload(u, fa) && !s.B.CanUpload(u, fa) {
			continue
		}
		out = append(out, map[string]any{"id": fa.ID, "name": fa.Name, "description": fa.Description,
			"sysop_upload": fa.SysopUpload, "files": fa.Files,
			"can_download": s.B.CanDownload(u, fa), "can_upload": s.B.CanUpload(u, fa)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) fileAreaByPath(w http.ResponseWriter, r *http.Request) *store.FileArea {
	if s.noFiles(w) {
		return nil
	}
	id, ok := pathID(r, "id")
	fa, err := s.B.Store.FileArea(id)
	u := whoOf(r).user
	if !ok || err != nil || !s.B.CanDownload(u, fa) && !s.B.CanUpload(u, fa) {
		fail(w, http.StatusNotFound, "no such file area")
		return nil
	}
	return fa
}

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	fa := s.fileAreaByPath(w, r)
	if fa == nil {
		return
	}
	fs, _ := s.B.Store.Files(fa.ID)
	out := []fileJSON{}
	for _, f := range fs {
		out = append(out, fileOut(f))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) fileByPath(w http.ResponseWriter, r *http.Request) *store.File {
	if s.noFiles(w) {
		return nil
	}
	id, ok := pathID(r, "id")
	f, err := s.B.Store.FileByID(id)
	if ok && err == nil {
		fa, err2 := s.B.Store.FileArea(f.AreaID)
		if u := whoOf(r).user; err2 != nil || !s.B.CanDownload(u, fa) && !s.B.CanUpload(u, fa) {
			err = store.ErrNotFound
		}
	}
	if !ok || err != nil {
		fail(w, http.StatusNotFound, "no such file")
		return nil
	}
	return f
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	if f := s.fileByPath(w, r); f != nil {
		writeJSON(w, http.StatusOK, fileOut(f))
	}
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	f := s.fileByPath(w, r)
	if f == nil {
		return
	}
	u := whoOf(r).user
	if fa, err := s.B.Store.FileArea(f.AreaID); err != nil {
		fail(w, http.StatusNotFound, "no such file")
		return
	} else if r.Header.Get("Range") == "" {
		if err := s.B.MayDownload(u, fa); err != nil {
			fail(w, http.StatusForbidden, err.Error())
			return
		}
	} else if !s.B.CanDownload(u, fa) {
		fail(w, http.StatusForbidden, bbs.ErrNotAllowed.Error())
		return
	}
	fh, err := s.B.Files.OpenFile(f)
	if err != nil {
		fail(w, http.StatusNotFound, "the file is missing")
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	w.Header().Set("Digest", "sha-256="+f.SHA256)
	if r.Header.Get("Range") == "" {
		s.B.Downloaded(u, f)
	}
	http.ServeContent(w, r, f.Name, f.Created, fh)
}

// upload: the body is the file; ?name= and &description=.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	fa := s.fileAreaByPath(w, r)
	if fa == nil {
		return
	}
	u := whoOf(r).user
	if !s.B.CanUpload(u, fa) {
		fail(w, http.StatusForbidden, "your access does not allow uploading here")
		return
	}
	up, err := s.B.Files.Upload(u, fa, r.URL.Query().Get("name"), bbs.Clean(r.URL.Query().Get("description"), 60))
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, files.ErrDenied):
			status = http.StatusForbidden
		case errors.Is(err, files.ErrExists):
			status = http.StatusConflict
		case errors.Is(err, files.ErrQuota):
			status = http.StatusTooManyRequests
		}
		fail(w, status, err.Error())
		return
	}
	if _, err := io.Copy(up, r.Body); err != nil {
		up.Abort()
		status := http.StatusBadRequest
		if errors.Is(err, files.ErrTooLarge) || errors.Is(err, files.ErrQuota) {
			status = http.StatusRequestEntityTooLarge
		}
		fail(w, status, err.Error())
		return
	}
	f, err := up.Commit()
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	s.B.Log.Printf("http: %s uploaded %q (%d bytes) to %s", u.Name, f.Name, f.Size, fa.Name)
	w.Header().Set("Location", fmt.Sprintf("/api/v1/files/%d", f.ID))
	writeJSON(w, http.StatusCreated, fileOut(f))
}

// --- chat (what needs a node needs an open stream: /events or /ws)

func (s *Server) streamNode(w http.ResponseWriter, r *http.Request) *bbs.Node {
	s.mu.Lock()
	ns := s.streams[whoOf(r).token.ID]
	s.mu.Unlock()
	if len(ns) == 0 {
		fail(w, http.StatusConflict, "open a stream first (/api/v1/events or /api/v1/ws): it is your node")
		return nil
	}
	return ns[len(ns)-1]
}

func (s *Server) rooms(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, ro := range s.B.Agent(whoOf(r).user, "api").Rooms() {
		out = append(out, map[string]any{"name": ro.Name, "users": ro.Users})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) nodeMessage(w http.ResponseWriter, r *http.Request) {
	n := s.streamNode(w, r)
	if n == nil {
		return
	}
	to, err := strconv.Atoi(r.PathValue("n"))
	var req struct {
		Text string `json:"text"`
	}
	if err != nil || !s.decode(w, r, &req) {
		if err != nil {
			fail(w, http.StatusNotFound, "no such node")
		}
		return
	}
	if err := n.Message(to, req.Text); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, bbs.ErrNoNode) {
			status = http.StatusNotFound
		} else if errors.Is(err, bbs.ErrFlood) {
			status = http.StatusTooManyRequests
		}
		fail(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sent": true})
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	n := s.streamNode(w, r)
	if n == nil {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	room, told, err := n.PageSysop(req.Reason)
	if err != nil {
		fail(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if told > 0 {
		n.JoinRoom(room)
	} else {
		n.CancelPage()
	}
	writeJSON(w, http.StatusOK, map[string]any{"told": told, "room": room, "waiting": told > 0})
}

// --- admin

func (s *Server) pages(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, p := range s.B.Pages() {
		out = append(out, map[string]any{"node": p.Node, "user": p.User, "reason": p.Reason, "since": p.Since})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	n := s.streamNode(w, r)
	if n == nil {
		return
	}
	num, _ := strconv.Atoi(r.PathValue("node"))
	_, node, err := n.Answer(num)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": node, "room": n.Room()})
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Location string `json:"location"`
		Sysop    bool   `json:"sysop"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if msg := bbs.ValidName(req.Name); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if msg := bbs.ValidPassword(req.Password, req.Name); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.B.Store.CreateUser(req.Name, req.Password, bbs.Clean(req.Location, bbs.MaxLocationLen), req.Sysop)
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "that name is taken")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	s.B.Log.Printf("http: %s added user %s", whoOf(r).user.Name, u.Name)
	writeJSON(w, http.StatusCreated, s.userView(r, u))
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.B.Store.UserByName(r.PathValue("name"))
	if err != nil {
		fail(w, http.StatusNotFound, "no such user")
		return
	}
	var req struct {
		Locked   *bool   `json:"locked"`
		Sysop    *bool   `json:"sysop"`
		Password *string `json:"password"`
		Location *string `json:"location"`
		Level    *int    `json:"level"`
		Flags    *string `json:"flags"` // "AB", or an edit: "+A -B"
	}
	if !s.decode(w, r, &req) {
		return
	}
	if req.Password != nil {
		if msg := bbs.ValidPassword(*req.Password, u.Name); msg != "" {
			fail(w, http.StatusBadRequest, msg)
			return
		}
		s.B.Store.SetPassword(u.ID, *req.Password)
	}
	if req.Locked != nil {
		s.B.Store.SetLocked(u.ID, *req.Locked)
	}
	if req.Sysop != nil {
		s.B.Store.SetSysop(u.ID, *req.Sysop)
	}
	if req.Location != nil {
		s.B.Store.SetLocation(u.ID, bbs.Clean(*req.Location, bbs.MaxLocationLen))
	}
	if req.Level != nil {
		if *req.Level < 0 || *req.Level > bbs.MaxLevel {
			fail(w, http.StatusBadRequest, "a level is 0 to 255")
			return
		}
		s.B.Store.SetLevel(u.ID, *req.Level)
	}
	if req.Flags != nil {
		fl, err := bbs.EditFlags(u.Flags, *req.Flags)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		s.B.Store.SetFlags(u.ID, fl)
	}
	s.B.Log.Printf("http: %s changed user %s", whoOf(r).user.Name, u.Name)
	u, _ = s.B.Store.UserByID(u.ID)
	writeJSON(w, http.StatusOK, s.userView(r, u))
}

func (s *Server) addArea(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SysopOnly   bool   `json:"sysop_only"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	name := bbs.Clean(req.Name, 30)
	if name == "" {
		fail(w, http.StatusBadRequest, "a name")
		return
	}
	a, err := s.B.Store.CreateArea(name, bbs.Clean(req.Description, 60), req.SysopOnly)
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "there is an area of that name")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "the system is in trouble")
		return
	}
	a, _ = s.B.Store.Area(a.ID)
	writeJSON(w, http.StatusCreated, s.areaOut(r, a))
}

func (s *Server) patchArea(w http.ResponseWriter, r *http.Request) {
	a := s.areaByPath(w, r)
	if a == nil {
		return
	}
	var req struct {
		Newsgroup *string `json:"newsgroup"`
		EchoTag   *string `json:"echo_tag"`
		ReadReq   *string `json:"read_require"`
		PostReq   *string `json:"post_require"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if req.Newsgroup != nil {
		if *req.Newsgroup != "" && !news.ValidGroup(*req.Newsgroup) {
			fail(w, http.StatusBadRequest, "a newsgroup name is lower case words with dots: bbs.general")
			return
		}
		if err := s.B.Store.SetNewsgroup(a.ID, *req.Newsgroup); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
	}
	if req.EchoTag != nil {
		tag := strings.ToUpper(*req.EchoTag)
		if tag != "" && !ftn.ValidTag(tag) {
			fail(w, http.StatusBadRequest, "an echo tag is letters, digits and . _ - (FSX_GEN), up to 60")
			return
		}
		if err := s.B.Store.SetEchoTag(a.ID, tag); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
	}
	if req.ReadReq != nil || req.PostReq != nil {
		read, post := a.ReadReq, a.PostReq
		if req.ReadReq != nil {
			read = *req.ReadReq
		}
		if req.PostReq != nil {
			post = *req.PostReq
		}
		for _, q := range []string{read, post} {
			if _, err := bbs.ParseRequire(q); err != nil {
				fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if err := s.B.Store.SetAreaRequire(a.ID, read, post); err != nil {
			fail(w, http.StatusInternalServerError, "the system is in trouble")
			return
		}
	}
	a, _ = s.B.Store.Area(a.ID)
	writeJSON(w, http.StatusOK, s.areaOut(r, a))
}

func (s *Server) kick(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || s.B.Kick(n) != nil {
		fail(w, http.StatusNotFound, "no one is on that node")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) fido(w http.ResponseWriter, r *http.Request) {
	if s.Fido == nil {
		fail(w, http.StatusNotFound, "the BBS is not on FidoNet")
		return
	}
	var addrs []string
	for _, a := range s.Fido.S.Addrs {
		addrs = append(addrs, a.String())
	}
	links := []map[string]any{}
	for _, l := range s.Fido.S.Links {
		links = append(links, map[string]any{"name": l.Name, "address": l.Addr.String(), "host": l.Host,
			"echoes": l.Echoes, "route": l.Route, "waiting": s.Fido.Outbound(l)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": addrs, "links": links})
}

func (s *Server) fidoPoll(w http.ResponseWriter, r *http.Request) {
	if s.Fido == nil {
		fail(w, http.StatusNotFound, "the BBS is not on FidoNet")
		return
	}
	called := []string{}
	for _, l := range s.Fido.S.Links {
		if l.Host != "" {
			called = append(called, l.Name)
			go func() {
				if err := s.Fido.Call(s.Ctx, l); err != nil {
					s.B.Log.Printf("fido: calling %s: %v", l.Name, err)
				}
			}()
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"calling": called})
}

func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	if s.News == nil {
		fail(w, http.StatusNotFound, "no news")
		return
	}
	peers := []map[string]any{}
	for _, p := range s.Peers {
		peers = append(peers, map[string]any{"name": p.Name, "address": p.Address, "groups": p.Groups,
			"pull": p.Pull, "push": p.Push})
	}
	groups := []string{}
	if as, err := s.B.Store.Newsgroups(); err == nil {
		for _, a := range as {
			groups = append(groups, a.Newsgroup)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": peers, "newsgroups": groups})
}

func (s *Server) newsSync(w http.ResponseWriter, r *http.Request) {
	if s.News == nil {
		fail(w, http.StatusNotFound, "no news")
		return
	}
	names := []string{}
	for _, p := range s.Peers {
		names = append(names, p.Name)
		go s.News.Sync(p)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"syncing": names})
}
