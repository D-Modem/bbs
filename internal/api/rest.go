package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// routes is the API's endpoints, by method and path, each with the scope
// it needs (the OpenAPI document's ops must list the same ones).
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
		"GET /api/v1/callers":           {scope: "read", h: s.callers},

		"GET /api/v1/areas":                 {scope: "read", h: s.areas},
		"GET /api/v1/areas/{id}":            {scope: "read", h: s.area},
		"GET /api/v1/areas/{id}/messages":   {scope: "read", h: s.messages},
		"POST /api/v1/areas/{id}/messages":  {scope: "post", h: s.post},
		"PUT /api/v1/areas/{id}/lastread":   {scope: "read", h: s.lastRead},
		"GET /api/v1/messages/{id}":         {scope: "read", h: s.message},
		"GET /api/v1/messages/{id}/thread":  {scope: "read", h: s.thread},
		"POST /api/v1/messages/{id}/vote":   {scope: "post", h: s.vote},
		"POST /api/v1/messages/{id}/answer": {scope: "post", h: s.answerPoll},

		"GET /api/v1/mail":         {scope: "mail", h: s.mail},
		"GET /api/v1/mail/{id}":    {scope: "mail", h: s.mailOne},
		"DELETE /api/v1/mail/{id}": {scope: "mail", h: s.mailDelete},
		"POST /api/v1/mail":        {scope: "mail", h: s.sendMail},
		"GET /api/v1/qwk":          {scope: "mail", h: s.qwkPacket},
		"POST /api/v1/qwk/rep":     {scope: "mail", h: s.qwkREP},

		"GET /api/v1/fileareas":             {scope: "files", h: s.fileAreas},
		"GET /api/v1/fileareas/{id}/files":  {scope: "files", h: s.files},
		"POST /api/v1/fileareas/{id}/files": {scope: "files", h: s.upload},
		"GET /api/v1/files/{id}":            {scope: "files", h: s.file},
		"GET /api/v1/files":                 {scope: "files", h: s.findFiles},
		"GET /api/v1/files/{id}/listing":    {scope: "files", h: s.listing},
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
		"GET /api/v1/admin/bans":                 {scope: "sysop", h: s.bans},
		"DELETE /api/v1/admin/bans/{ip}":         {scope: "sysop", h: s.unban},
		"GET /api/v1/admin/hacklog":              {scope: "sysop", h: s.hackLog},
		"GET /api/v1/admin/events":               {scope: "sysop", h: s.scheduled},
		"POST /api/v1/admin/events/{name}/run":   {scope: "sysop", h: s.runEvent},
		"PUT /api/v1/admin/available":            {scope: "sysop", h: s.setAvailable},
		"GET /api/v1/admin/fido":                 {scope: "sysop", h: s.fido},
		"POST /api/v1/admin/fido/poll":           {scope: "sysop", h: s.fidoPoll},
		"GET /api/v1/admin/qwknet":               {scope: "sysop", h: s.qwkNets},
		"POST /api/v1/admin/qwknet/call":         {scope: "sysop", h: s.qwkNetCall},
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
	QWKNode   bool       `json:"qwk_node,omitempty"` // to sysops
	About     string     `json:"about,omitempty"`
}

// userView is a user as the request's user may see them: their access
// only to sysops and themselves.
func (s *Server) userView(r *http.Request, u *store.User) userJSON {
	j := s.userOut(u)
	if me := whoOf(r).user; me != nil && (me.Sysop || me.ID == u.ID) {
		level := u.Level
		j.Level, j.Flags = &level, u.Flags
		j.QWKNode = u.QWKNode
	}
	return j
}

// userOut is u as anyone may see them (no level, flags or QWK node: see
// userView), with the node they are on now, if any.
func (s *Server) userOut(u *store.User) userJSON {
	j := userJSON{Name: u.Name, Location: u.Location, Sysop: u.Sysop, Created: u.Created,
		Calls: u.Logins, Posts: u.Posts, Locked: u.Locked, About: u.About}
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
	// the caller's country (ISO 3166-1), if known
	Country string `json:"country,omitempty"`
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
	Group       string `json:"group,omitempty"`
	Aliases     bool   `json:"aliases,omitempty"`
	MaxMsgs     int    `json:"max_messages,omitempty"`
	MaxDays     int    `json:"max_days,omitempty"`
}

type messageJSON struct {
	ID        int64      `json:"id"`
	Area      int64      `json:"area,omitempty"`
	From      string     `json:"from"`
	To        string     `json:"to"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body,omitempty"`
	ReplyTo   int64      `json:"reply_to,omitempty"`
	Created   time.Time  `json:"created"`
	MessageID string     `json:"message_id,omitempty"`
	FTNMsgID  string     `json:"ftn_msgid,omitempty"`
	Read      *bool      `json:"read,omitempty"` // mail
	Votes     *votesJSON `json:"votes,omitempty"`
	Poll      *pollJSON  `json:"poll,omitempty"`
}

type votesJSON struct {
	Up   int `json:"up"`
	Down int `json:"down"`
	Mine int `json:"mine"`
}

type pollJSON struct {
	Answers []string `json:"answers"`
	Counts  []int    `json:"counts"`
	Mine    int      `json:"mine"` // -1: not answered
}

// msgOut is m as JSON, its body only if body.
func msgOut(m *store.Message, body bool) messageJSON {
	j := messageJSON{ID: m.ID, Area: m.AreaID, From: m.From, To: m.To, Subject: m.Subject,
		ReplyTo: m.ReplyTo, Created: m.Created, MessageID: m.MessageID, FTNMsgID: m.FTNMsgID}
	if body {
		j.Body = m.Body
	}
	return j
}

// mailOut is a mail as JSON (no area or Message-ID; whether it is read).
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
	LongDesc    string    `json:"long_description,omitempty"`
}

// fileOut is f as JSON.
func fileOut(f *store.File) fileJSON {
	return fileJSON{f.ID, f.AreaID, f.Name, f.Description, f.Size, f.SHA256, f.Uploader, f.Created, f.Downloads, f.LongDesc}
}

// --- helpers

// pathID is the path's value name as an id; false unless it is a number
// above 0.
func pathID(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil && n > 0
}

// agent is the request's user as a node that takes no line, for acting
// through the BBS as a caller would (see bbs.BBS.Agent).
func (s *Server) agent(r *http.Request) *bbs.Node {
	return s.B.Agent(whoOf(r).user, "api")
}

// lines splits a body into lines for the BBS's editor's checks.
// --- login

// login: a session token for a name and password, checked and throttled
// as any login; its scopes all but sysop's (a sysop's: all), or those
// asked for.
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

// logout: the session's token revoked; an API key is refused (it is
// revoked by its id).
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

// system: the BBS's name, sysop, counts and features.
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
		"name": s.B.Conf().Name, "sysop": s.B.Conf().Sysop, "location": s.B.Conf().Location, "motd": s.B.MOTDText(),
		"users": len(us), "messages": msgs, "areas": len(areas), "online": on,
		"max_nodes": s.B.Conf().MaxNodes, "new_users": s.B.Conf().NewUsers,
		"features": map[string]bool{"files": s.B.Files != nil, "fido": s.Fido != nil, "news": s.News != nil,
			"doors": len(s.B.Doors) > 0, "netmail": s.B.Netmail != nil},
		"version": "bbs-sec/1"})
}

// who: the nodes with a caller logged in.
func (s *Server) who(w http.ResponseWriter, r *http.Request) {
	out := []nodeJSON{}
	for _, n := range s.B.Nodes() {
		if n.User == "" {
			continue
		}
		out = append(out, nodeJSON{n.Num, n.User, n.Via, n.Since, n.Activity, n.Terminal, n.Place.Country})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- me and users

// me: the user's account, unread mail, access (level, limits, what they
// may do) and settings, and the token's scopes.
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
		"validated": u.Sysop || u.Level >= s.B.Conf().ValidatedLevel}
	if lvl.Downloads > 0 {
		access["downloads_per_day"] = lvl.Downloads
	}
	if lvl.Ratio > 0 {
		access["ratio"], access["ratio_free"] = lvl.Ratio, lvl.RatioFree
	}
	access["downloads"], access["uploads"] = u.Downloads, u.Uploads
	if left, _ := s.B.DownloadsLeft(u); left >= 0 {
		access["downloads_left"] = left
	}
	if lvl.Time > 0 {
		access["minutes_per_day"] = int(lvl.Time.Minutes())
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": j, "unread_mail": unread, "access": access,
		"prefs": bbs.ParsePrefs(u.Prefs), "scopes": strings.Fields(whoOf(r).token.Scopes)})
}

// patchMe: the user's location, about, settings or password changed (a
// new password needs the old); then their account, as me.  A refused
// password leaves the other changes made.
func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location    *string           `json:"location"`
		OldPassword string            `json:"old_password"`
		NewPassword string            `json:"new_password"`
		Prefs       map[string]string `json:"prefs"`
		About       *string           `json:"about"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	u := whoOf(r).user
	if req.Prefs != nil {
		fresh, err := s.B.Store.UserByID(u.ID)
		if err != nil {
			fail(w, http.StatusNotFound, "no such user")
			return
		}
		prefs, err := bbs.ChangePrefs(fresh.Prefs, req.Prefs)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		s.B.Store.SetPrefs(u.ID, prefs)
	}
	if req.Location != nil {
		s.B.Store.SetLocation(u.ID, bbs.Clean(*req.Location, bbs.MaxLocationLen))
	}
	if req.About != nil {
		s.B.Store.SetAbout(u.ID, bbs.Clean(*req.About, 60))
	}
	if req.NewPassword != "" {
		if err := s.agent(r).ChangePassword(req.OldPassword, req.NewPassword); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.me(w, r)
}

// callers: the last callers, newest first
func (s *Server) callers(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 || n > 200 {
		n = 20
	}
	cs, err := s.B.Store.LastCalls(n)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []map[string]any{}
	for _, c := range cs {
		out = append(out, map[string]any{"user": c.Name, "location": c.Location, "via": c.Via,
			"node": c.Node, "at": c.At})
	}
	writeJSON(w, http.StatusOK, out)
}

// myTokens: the user's tokens, sessions and API keys, the one in use
// marked current.
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

// revokeMine: a token of the user's revoked; 404 if it is no token of
// theirs.
func (s *Server) revokeMine(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || s.B.Store.RevokeToken(id, whoOf(r).user.ID) != nil {
		fail(w, http.StatusNotFound, "no such token of yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sshKeyJSON is k as JSON.
func sshKeyJSON(k *store.SSHKey) map[string]any {
	m := map[string]any{"id": k.ID, "type": k.Type, "fingerprint": k.Fingerprint, "comment": k.Comment,
		"key": k.Key, "created": k.Created}
	if !k.LastUsed.IsZero() {
		m["last_used"] = k.LastUsed
	}
	return m
}

// myKeys: the user's SSH keys.
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

// delKey: an SSH key of the user's deleted; 404 if it is no key of
// theirs.
func (s *Server) delKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || s.B.Store.RemoveSSHKey(id, whoOf(r).user.ID) != nil {
		fail(w, http.StatusNotFound, "no such key of yours")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// users: the users as the user may see them (the locked ones only to a
// sysop).
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

// user: one user by name, as the user may see them; a locked user is 404
// but to a sysop.
func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	u, err := s.B.Store.UserByName(r.PathValue("name"))
	if err != nil || u.Locked && !whoOf(r).user.Sysop {
		fail(w, http.StatusNotFound, "no such user")
		return
	}
	writeJSON(w, http.StatusOK, s.userView(r, u))
}

// --- message areas

// areaOut is a as JSON for the request's user: their unread, last read,
// and whether they may post.
func (s *Server) areaOut(r *http.Request, a *store.Area) areaJSON {
	u := whoOf(r).user
	unread, _ := s.B.Store.NewCount(u.ID, a.ID)
	last, _ := s.B.Store.LastRead(u.ID, a.ID)
	return areaJSON{a.ID, a.Name, a.Description, a.SysopOnly, a.Newsgroup, a.EchoTag, a.Messages,
		a.FirstID, a.LastID, unread, last, s.B.CanPost(u, a), a.ReadReq, a.PostReq,
		a.Group, a.Aliases, a.MaxMsgs, a.MaxDays}
}

// areas: the message areas the user may read.
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

// areaByPath is the path's area; nil, answered 404, if there is none or
// the user may not read it.
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

// area: one message area.
func (s *Server) area(w http.ResponseWriter, r *http.Request) {
	if a := s.areaByPath(w, r); a != nil {
		writeJSON(w, http.StatusOK, s.areaOut(r, a))
	}
}

// messages: an area's messages after ?after=, ?limit= of them (50, up to
// 200), without bodies if ?body=no; next is the following page's path
// when the page is full.
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

// readable is the path's message, if the user may read its area.
func (s *Server) readable(w http.ResponseWriter, r *http.Request) *store.Message {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, http.StatusNotFound, "no such message")
		return nil
	}
	m, err := s.B.Store.Message(id)
	if err == nil {
		if a, err2 := s.B.Store.Area(m.AreaID); err2 != nil || !s.B.CanRead(whoOf(r).user, a) {
			err = store.ErrNotFound
		}
	}
	if err != nil {
		fail(w, http.StatusNotFound, "no such message")
		return nil
	}
	return m
}

// message: one message, its votes and poll with the user's own, if any.
func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	m := s.readable(w, r)
	if m == nil {
		return
	}
	j := msgOut(m, true)
	u := whoOf(r).user
	if up, down, mine, err := s.B.Store.Votes(m.ID, u.ID); err == nil && up+down+mine != 0 {
		j.Votes = &votesJSON{up, down, mine}
	}
	if ans, counts, mine, err := s.B.Store.Poll(m.ID, u.ID); err == nil && ans != nil {
		j.Poll = &pollJSON{ans, counts, mine}
	}
	writeJSON(w, http.StatusOK, j)
}

// vote: {"value": 1, -1 or 0}
func (s *Server) vote(w http.ResponseWriter, r *http.Request) {
	m := s.readable(w, r)
	if m == nil {
		return
	}
	var req struct {
		Value int `json:"value"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	u := whoOf(r).user
	if req.Value < -1 || req.Value > 1 || req.Value != 0 && m.FromID == u.ID {
		fail(w, http.StatusBadRequest, "value is 1, -1 or 0, and not on your own message")
		return
	}
	if err := s.B.Store.Vote(m.ID, u.ID, req.Value); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.message(w, r)
}

// answerPoll: {"answer": the answer's index}
func (s *Server) answerPoll(w http.ResponseWriter, r *http.Request) {
	m := s.readable(w, r)
	if m == nil {
		return
	}
	var req struct {
		Answer *int `json:"answer"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	u := whoOf(r).user
	ans, _, _, err := s.B.Store.Poll(m.ID, u.ID)
	if err != nil || ans == nil {
		fail(w, http.StatusBadRequest, "that message is no poll")
		return
	}
	if req.Answer == nil || *req.Answer < 0 || *req.Answer >= len(ans) {
		fail(w, http.StatusBadRequest, fmt.Sprintf("answer is 0 to %d", len(ans)-1))
		return
	}
	if err := s.B.Store.PollVote(m.ID, u.ID, *req.Answer); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.message(w, r)
}

// thread: the message's thread, from its start, with each one's depth
func (s *Server) thread(w http.ResponseWriter, r *http.Request) {
	m := s.readable(w, r)
	if m == nil {
		return
	}
	ms, depth, err := s.B.Store.Thread(m)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []map[string]any{}
	for i, x := range ms {
		out = append(out, map[string]any{"message": msgOut(x, false), "depth": depth[i]})
	}
	writeJSON(w, http.StatusOK, out)
}

// post: a message posted to the area as the user (as their alias, or a
// poll, if asked), as a caller's post is checked and cleaned; a reply
// must be to a message of the same area.
func (s *Server) post(w http.ResponseWriter, r *http.Request) {
	a := s.areaByPath(w, r)
	if a == nil {
		return
	}
	var req struct {
		To      string   `json:"to"`
		Subject string   `json:"subject"`
		Body    string   `json:"body"`
		ReplyTo int64    `json:"reply_to"`
		Alias   bool     `json:"alias"`
		Poll    []string `json:"poll"`
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
	id, err := s.agent(r).PostWith(a, req.To, req.Subject, bbs.WrapBody(req.Body), req.ReplyTo,
		bbs.PostOpts{Alias: req.Alias, Poll: req.Poll})
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	m, _ := s.B.Store.Message(id)
	w.Header().Set("Location", fmt.Sprintf("/api/v1/messages/%d", id))
	writeJSON(w, http.StatusCreated, msgOut(m, true))
}

// lastRead: the user's last read in the area set to an id (0 up to its
// last message); then the area.
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

// mail: the user's inbox, without bodies.
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

// mailOne: a mail of the user's, now marked read; 404 if it is no mail of
// theirs.
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

// mailDelete: a mail of the user's deleted; 404 if it is no mail of
// theirs.
func (s *Server) mailDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || s.B.Store.DeleteMail(whoOf(r).user.ID, id) != nil {
		fail(w, http.StatusNotFound, "no such mail")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendMail: mail sent as the user, to a user or by netmail, as a caller's
// is checked.
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
	if err := s.agent(r).SendMail(req.To, req.Subject, bbs.WrapBody(req.Body), req.ReplyTo); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sent": true})
}

// --- files

// noFiles says whether the BBS has no file areas, answering 404 if so.
func (s *Server) noFiles(w http.ResponseWriter) bool {
	if s.B.Files == nil {
		fail(w, http.StatusNotFound, "there are no file areas")
		return true
	}
	return false
}

// fileAreas: the file areas the user may download from or upload to.
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

// findFiles: files by words (?q=) or since a time (?since=), in the
// areas the user may see, newest first.
func (s *Server) findFiles(w http.ResponseWriter, r *http.Request) {
	if s.noFiles(w) {
		return
	}
	fas, _ := s.B.Store.FileAreas()
	u := whoOf(r).user
	var ids []int64
	for _, fa := range fas {
		if s.B.CanDownload(u, fa) || s.B.CanUpload(u, fa) {
			ids = append(ids, fa.ID)
		}
	}
	q, since := strings.TrimSpace(r.URL.Query().Get("q")), r.URL.Query().Get("since")
	var fs []*store.File
	var err error
	switch {
	case q != "":
		if len([]rune(q)) < 2 {
			fail(w, http.StatusBadRequest, "q is two characters or more")
			return
		}
		fs, err = s.B.Store.SearchFiles(ids, q, 200)
	case since != "":
		t, perr := time.Parse(time.RFC3339, since)
		if perr != nil {
			fail(w, http.StatusBadRequest, "since is a time as 2006-01-02T15:04:05Z")
			return
		}
		fs, err = s.B.Store.FilesSince(ids, t, 200)
	default:
		fail(w, http.StatusBadRequest, "give q (words) or since (a time)")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []fileJSON{}
	for _, f := range fs {
		out = append(out, fileOut(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// listing: what is in a file that is an archive
func (s *Server) listing(w http.ResponseWriter, r *http.Request) {
	f := s.fileByPath(w, r)
	if f == nil {
		return
	}
	es, err := s.B.Files.Listing(f)
	if errors.Is(err, files.ErrNotArchive) {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []map[string]any{}
	for _, e := range es {
		m := map[string]any{"name": e.Name, "size": e.Size, "dir": e.Dir}
		if !e.Modified.IsZero() {
			m["modified"] = e.Modified
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// fileAreaByPath is the path's file area; nil, answered 404, if there is
// none or the user may neither download from it nor upload to it.
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

// files: a file area's files.
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

// fileByPath is the path's file; nil, answered 404, if there is none or
// the user may neither download from its area nor upload to it.
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

// file: one file's details.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	if f := s.fileByPath(w, r); f != nil {
		writeJSON(w, http.StatusOK, fileOut(f))
	}
}

// download: a file's content (Range works), if the user may download it
// and has downloads left today; counted as a download unless it is asked
// for by Range.
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

// --- QWK offline mail

// qwkPacket: the user's packet (new messages and unread mail), which then
// count as read, unless ?mark=no.
func (s *Server) qwkPacket(w http.ResponseWriter, r *http.Request) {
	out, err := s.B.Agent(whoOf(r).user, "api").QWKPacket()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+out.Name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(out.Data)))
	w.Header().Set("X-QWK-Messages", strconv.Itoa(out.Messages))
	if _, err := w.Write(out.Data); err != nil {
		return // not all of it went: nothing counts as read
	}
	if r.URL.Query().Get("mark") != "no" {
		out.Done()
	}
}

// qwkREP: the body is a REP packet; what came of it.
func (s *Server) qwkREP(w http.ResponseWriter, r *http.Request) {
	if !whoOf(r).has("post") {
		fail(w, http.StatusForbidden, "the token has no post scope")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, qwk.MaxREP))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "a REP packet is at most 16 MB")
		return
	}
	res, err := s.B.Agent(whoOf(r).user, "api").ImportREP(data)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if res.Errors == nil {
		res.Errors = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"posted": res.Posted, "mailed": res.Mailed,
		"duplicates": res.Duplicates, "errors": res.Errors})
}

// --- chat (what needs a node needs an open stream: /events or /ws)

// streamNode is the node of the token's newest open stream (/events or
// /ws); nil, answered 409, if it has none.
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

// rooms: the chat rooms in use and who is in each.
func (s *Server) rooms(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, ro := range s.B.Agent(whoOf(r).user, "api").Rooms() {
		out = append(out, map[string]any{"name": ro.Name, "users": ro.Users})
	}
	writeJSON(w, http.StatusOK, out)
}

// nodeMessage: a line from the user's stream's node to node n.
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

// page: the sysop paged from the user's stream's node, which joins the
// page's room to wait if anyone was told; if no one was, the page is
// called off.
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

// pages: the callers paging the sysop.
func (s *Server) pages(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, p := range s.B.Pages() {
		out = append(out, map[string]any{"node": p.Node, "user": p.User, "reason": p.Reason, "since": p.Since})
	}
	writeJSON(w, http.StatusOK, out)
}

// answer: a page answered from the sysop's stream's node (node 0: the
// longest waiting), which joins the caller's room.
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

// addUser: a user added, their name and password checked as a new
// caller's are.
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

// patchUser: a user's lock, sysop, QWK node, password, location, level or
// flags set; then the user.  The changes are made one by one, so one
// refused leaves those before it made.
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
		QWKNode  *bool   `json:"qwk_node"`
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
	if req.QWKNode != nil {
		s.B.Store.SetQWKNode(u.ID, *req.QWKNode)
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

// addArea: a message area added.
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

// patchArea: an area's newsgroup, echo tag, requirements, group, aliases
// or limits set; then the area.  As patchUser, one refused leaves those
// before it made.
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
		Group     *string `json:"group"`
		Aliases   *bool   `json:"aliases"`
		MaxMsgs   *int    `json:"max_messages"`
		MaxDays   *int    `json:"max_days"`
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
	if req.Group != nil {
		s.B.Store.SetAreaGroup(a.ID, bbs.Clean(*req.Group, 30))
	}
	if req.Aliases != nil {
		s.B.Store.SetAreaAliases(a.ID, *req.Aliases)
	}
	if req.MaxMsgs != nil || req.MaxDays != nil {
		msgs, days := a.MaxMsgs, a.MaxDays
		if req.MaxMsgs != nil {
			msgs = *req.MaxMsgs
		}
		if req.MaxDays != nil {
			days = *req.MaxDays
		}
		if msgs < 0 || days < 0 {
			fail(w, http.StatusBadRequest, "max_messages and max_days are 0 (no limit) or more")
			return
		}
		s.B.Store.SetAreaKeep(a.ID, msgs, days)
	}
	a, _ = s.B.Store.Area(a.ID)
	writeJSON(w, http.StatusOK, s.areaOut(r, a))
}

// kick: the caller on node n thrown off.
func (s *Server) kick(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || s.B.Kick(n) != nil {
		fail(w, http.StatusNotFound, "no one is on that node")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bans: the banned addresses and until when, by address.
func (s *Server) bans(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for ip, until := range s.B.Guard.Bans() {
		out = append(out, map[string]any{"ip": ip, "until": until})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["ip"].(string) < out[j]["ip"].(string) })
	writeJSON(w, http.StatusOK, out)
}

// unban: an address's ban lifted, in the log and the hack log.
func (s *Server) unban(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if !s.B.Guard.Unban(ip) {
		fail(w, http.StatusNotFound, "that address is not banned")
		return
	}
	s.B.Log.Printf("api: %s lifted the ban on %s", whoOf(r).user.Name, ip)
	s.B.Guard.Hack("%s ban lifted by %s", ip, whoOf(r).user.Name)
	w.WriteHeader(http.StatusNoContent)
}

// hackLog: the hack log's last ?lines= lines (100, up to 1000).
func (s *Server) hackLog(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("lines"))
	if err != nil || n <= 0 || n > 1000 {
		n = 100
	}
	lines := s.B.Guard.Tail(n)
	if lines == nil {
		lines = []string{}
	}
	writeJSON(w, http.StatusOK, lines)
}

// resultOut is an event's run as JSON.
func resultOut(r *events.Result) map[string]any {
	m := map[string]any{"start": r.Start, "seconds": r.Took.Seconds(), "ok": r.Err == "", "output": r.Output, "by": r.By}
	if r.Err != "" {
		m["error"] = r.Err
	}
	return m
}

// scheduled: the scheduled events
func (s *Server) scheduled(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	if s.B.Events != nil {
		for _, e := range s.B.Events.Status() {
			m := map[string]any{"name": e.Name, "command": e.Command, "schedule": e.Schedule(), "next": e.Next,
				"running": e.Running}
			if e.Last != nil {
				m["last"] = resultOut(e.Last)
			}
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// runEvent: an event run now, by the user, waited for; how it went.
func (s *Server) runEvent(w http.ResponseWriter, r *http.Request) {
	if s.B.Events == nil {
		fail(w, http.StatusNotFound, "there are no events")
		return
	}
	res, err := s.B.Events.RunNow(r.Context(), r.PathValue("name"), whoOf(r).user.Name)
	switch {
	case errors.Is(err, events.ErrRunning):
		fail(w, http.StatusConflict, "it is running already")
	case err != nil:
		fail(w, http.StatusNotFound, err.Error())
	default:
		writeJSON(w, http.StatusOK, resultOut(res))
	}
}

// setAvailable: whether pages reach the sysop, set.
func (s *Server) setAvailable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Available *bool `json:"available"`
	}
	if !s.decode(w, r, &req) {
		return
	}
	if req.Available == nil {
		fail(w, http.StatusBadRequest, `give {"available": true} or false`)
		return
	}
	s.B.SetSysopAvailable(*req.Available)
	writeJSON(w, http.StatusOK, map[string]any{"available": s.B.SysopAvailable()})
}

// fido: the BBS's FidoNet addresses, and its links with what waits for
// each.
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

// fidoPoll: the FidoNet links that have a host called, in the background;
// which they are.
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

// qwkNets: the QWK networks, their hubs and conferences.
func (s *Server) qwkNets(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	if s.QWKNet != nil {
		for _, n := range s.QWKNet.Nets {
			confs := map[string]string{}
			for c, a := range n.Confs {
				confs[strconv.Itoa(c)] = a
			}
			out = append(out, map[string]any{"name": n.Name, "address": n.Address, "user": n.User,
				"every_minutes": int(n.Every / time.Minute), "conferences": confs})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// qwkNetCall calls the hubs (?net=: one) now, in the background.
func (s *Server) qwkNetCall(w http.ResponseWriter, r *http.Request) {
	if s.QWKNet == nil {
		fail(w, http.StatusNotFound, "the BBS is in no QWK network")
		return
	}
	name := r.URL.Query().Get("net")
	go func() {
		if err := s.QWKNet.CallNow(name); err != nil {
			s.B.Log.Printf("qwknet: %v", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"calling": name})
}

// news: the news peers, and the areas' newsgroups.
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

// newsSync: every news peer synced with, in the background; which they
// are.
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
