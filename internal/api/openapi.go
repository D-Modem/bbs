package api

import (
	"net/http"
	"strconv"
	"strings"
)

// op describes an endpoint for the OpenAPI document.
type op struct {
	method, path, summary, scope string
	body, resp                   string // schema names ("": none)
	status                       int
}

var ops = []op{
	{"POST", "/api/v1/login", "Log in: a session token", "", "Login", "Session", 200},
	{"POST", "/api/v1/logout", "End this session", "any", "", "", 204},
	{"GET", "/api/v1/system", "The system: name, sysop, counts, features", "anonymous", "", "System", 200},
	{"GET", "/api/v1/who", "Who is online", "anonymous", "", "Nodes", 200},
	{"GET", "/api/v1/me", "Your account", "read", "", "Me", 200},
	{"PATCH", "/api/v1/me", "Change your location, password or settings", "read", "PatchMe", "Me", 200},
	{"GET", "/api/v1/me/tokens", "Your tokens (keys and sessions)", "any", "", "Tokens", 200},
	{"DELETE", "/api/v1/me/tokens/{id}", "Revoke a token of yours", "any", "", "", 204},
	{"GET", "/api/v1/me/keys", "Your SSH keys", "read", "", "SSHKeys", 200},
	{"POST", "/api/v1/me/keys", "Add an SSH key (from a login's session only)", "any", "NewSSHKey", "SSHKey", 201},
	{"DELETE", "/api/v1/me/keys/{id}", "Delete an SSH key of yours", "any", "", "", 204},
	{"GET", "/api/v1/users", "The users", "read", "", "Users", 200},
	{"GET", "/api/v1/users/{name}", "A user", "read", "", "User", 200},
	{"GET", "/api/v1/areas", "Message areas, with your unread", "read", "", "Areas", 200},
	{"GET", "/api/v1/areas/{id}", "A message area", "read", "", "Area", 200},
	{"GET", "/api/v1/areas/{id}/messages", "Messages after an id (?after=, ?limit= up to 200, ?body=no)", "read", "", "MessagePage", 200},
	{"POST", "/api/v1/areas/{id}/messages", "Post a message", "post", "NewMessage", "Message", 201},
	{"PUT", "/api/v1/areas/{id}/lastread", "Mark read up to a message", "read", "LastRead", "Area", 200},
	{"GET", "/api/v1/messages/{id}", "A message", "read", "", "Message", 200},
	{"GET", "/api/v1/mail", "Your inbox", "mail", "", "Messages", 200},
	{"GET", "/api/v1/mail/{id}", "A mail of yours (marked read)", "mail", "", "Message", 200},
	{"DELETE", "/api/v1/mail/{id}", "Delete a mail of yours", "mail", "", "", 204},
	{"POST", "/api/v1/mail", "Send mail (to a user, or Name@zone:net/node)", "mail", "NewMessage", "Sent", 202},
	{"GET", "/api/v1/fileareas", "File areas", "files", "", "FileAreas", 200},
	{"GET", "/api/v1/fileareas/{id}/files", "An area's files", "files", "", "Files", 200},
	{"POST", "/api/v1/fileareas/{id}/files", "Upload: the body is the file (?name=, ?description=)", "files", "Binary", "File", 201},
	{"GET", "/api/v1/files/{id}", "A file's details", "files", "", "File", 200},
	{"GET", "/api/v1/files/{id}/content", "Download (Range works)", "files", "", "Binary", 200},
	{"GET", "/api/v1/chat/rooms", "Chat rooms in use", "chat", "", "Rooms", 200},
	{"POST", "/api/v1/nodes/{n}/message", "A line to one node (needs an open stream)", "chat", "Text", "Sent", 202},
	{"POST", "/api/v1/page", "Page the sysop (needs an open stream)", "chat", "PageReq", "PageRes", 200},
	{"GET", "/api/v1/events", "Server-sent events: the stream's node's events", "chat or read", "", "EventStream", 200},
	{"GET", "/api/v1/ws", "WebSocket, JSON (subprotocol bbs.v1.json): see proto/bbs.proto", "chat", "", "", 101},
	{"GET", "/api/v1/ws/proto", "WebSocket, protobuf (subprotocol bbs.v1.proto): see proto/bbs.proto", "chat", "", "", 101},
	{"GET", "/api/v1/admin/pages", "Callers paging the sysop", "sysop", "", "Pages", 200},
	{"POST", "/api/v1/admin/pages/{node}/answer", "Answer a page (needs an open stream; node 0: the longest waiting)", "sysop", "", "PageRes", 200},
	{"POST", "/api/v1/admin/users", "Add a user", "sysop", "NewUser", "User", 201},
	{"PATCH", "/api/v1/admin/users/{name}", "Lock, unlock, make sysop, set password, location, level or flags", "sysop", "PatchUser", "User", 200},
	{"POST", "/api/v1/admin/areas", "Add a message area", "sysop", "NewArea", "Area", 201},
	{"PATCH", "/api/v1/admin/areas/{id}", "Set an area's newsgroup, echo tag or access requirements (\"\": none)", "sysop", "PatchArea", "Area", 200},
	{"POST", "/api/v1/admin/nodes/{n}/kick", "Throw a caller off", "sysop", "", "", 204},
	{"GET", "/api/v1/admin/fido", "FidoNet: addresses, links, files waiting", "sysop", "", "Object", 200},
	{"POST", "/api/v1/admin/fido/poll", "Call the FidoNet links now", "sysop", "", "Object", 202},
	{"GET", "/api/v1/admin/news", "News: peers and newsgroups", "sysop", "", "Object", 200},
	{"POST", "/api/v1/admin/news/sync", "Sync with the news peers now", "sysop", "", "Object", 202},
	{"GET", "/api/v1/openapi.json", "This document", "", "", "Object", 200},
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

var (
	str  = map[string]any{"type": "string"}
	num  = map[string]any{"type": "integer"}
	boo  = map[string]any{"type": "boolean"}
	date = map[string]any{"type": "string", "format": "date-time"}
)

func ref(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
func arr(name string) map[string]any { return map[string]any{"type": "array", "items": ref(name)} }

var schemas = map[string]any{
	"Error":       obj(map[string]any{"error": str, "code": str}, "error", "code"),
	"Login":       obj(map[string]any{"name": str, "password": str, "scopes": str}, "name", "password"),
	"Session":     obj(map[string]any{"token": str, "expires": date, "user": str, "scopes": map[string]any{"type": "array", "items": str}}),
	"System":      obj(map[string]any{"name": str, "sysop": str, "location": str, "motd": str, "users": num, "messages": num, "areas": num, "online": num, "features": map[string]any{"type": "object"}}),
	"Node":        obj(map[string]any{"node": num, "user": str, "via": str, "since": date, "doing": str, "terminal": str}),
	"Nodes":       arr("Node"),
	"User":        obj(map[string]any{"name": str, "location": str, "sysop": boo, "created": date, "last_login": date, "calls": num, "posts": num, "node": num, "locked": boo, "level": num, "flags": str}),
	"Users":       arr("User"),
	"Me":          obj(map[string]any{"user": ref("User"), "unread_mail": num, "access": map[string]any{"type": "object"}, "prefs": ref("Prefs"), "scopes": map[string]any{"type": "array", "items": str}}),
	"PatchMe":     obj(map[string]any{"location": str, "old_password": str, "new_password": str, "prefs": ref("Prefs")}),
	"Prefs":       map[string]any{"type": "object", "description": "settings: ansi, utf8, sixel, rip (on, off), width (40, 80), editor (full, line); \"\" forgets one", "additionalProperties": str},
	"SSHKey":      obj(map[string]any{"id": num, "type": str, "fingerprint": str, "comment": str, "key": str, "created": date, "last_used": date}),
	"SSHKeys":     arr("SSHKey"),
	"NewSSHKey":   obj(map[string]any{"key": str}, "key"),
	"Tokens":      map[string]any{"type": "array", "items": obj(map[string]any{"id": num, "name": str, "scopes": map[string]any{"type": "array", "items": str}, "session": boo, "created": date, "expires": date, "last_used": date, "current": boo})},
	"Area":        obj(map[string]any{"id": num, "name": str, "description": str, "sysop_only": boo, "newsgroup": str, "echo_tag": str, "messages": num, "first_id": num, "last_id": num, "unread": num, "last_read": num, "can_post": boo, "read_require": str, "post_require": str}),
	"Areas":       arr("Area"),
	"Message":     obj(map[string]any{"id": num, "area": num, "from": str, "to": str, "subject": str, "body": str, "reply_to": num, "created": date, "message_id": str, "ftn_msgid": str, "read": boo}),
	"Messages":    arr("Message"),
	"MessagePage": obj(map[string]any{"messages": arr("Message"), "next": str}),
	"NewMessage":  obj(map[string]any{"to": str, "subject": str, "body": str, "reply_to": num}, "subject", "body"),
	"LastRead":    obj(map[string]any{"id": num}, "id"),
	"Sent":        obj(map[string]any{"sent": boo}),
	"FileAreas":   map[string]any{"type": "array", "items": obj(map[string]any{"id": num, "name": str, "description": str, "sysop_upload": boo, "files": num, "can_upload": boo})},
	"File":        obj(map[string]any{"id": num, "area": num, "name": str, "description": str, "size": num, "sha256": str, "uploader": str, "created": date, "downloads": num}),
	"Files":       arr("File"),
	"Binary":      map[string]any{"type": "string", "format": "binary"},
	"Rooms":       map[string]any{"type": "array", "items": obj(map[string]any{"name": str, "users": map[string]any{"type": "array", "items": str}})},
	"Text":        obj(map[string]any{"text": str}, "text"),
	"PageReq":     obj(map[string]any{"reason": str}, "reason"),
	"PageRes":     obj(map[string]any{"told": num, "room": str, "waiting": boo, "node": num}),
	"Pages":       map[string]any{"type": "array", "items": obj(map[string]any{"node": num, "user": str, "reason": str, "since": date})},
	"NewUser":     obj(map[string]any{"name": str, "password": str, "location": str, "sysop": boo}, "name", "password"),
	"PatchUser":   obj(map[string]any{"locked": boo, "sysop": boo, "password": str, "location": str, "level": num, "flags": str}),
	"NewArea":     obj(map[string]any{"name": str, "description": str, "sysop_only": boo}, "name"),
	"PatchArea":   obj(map[string]any{"newsgroup": str, "echo_tag": str, "read_require": str, "post_require": str}),
	"EventStream": map[string]any{"type": "string", "description": "text/event-stream: events welcome, said, emoted, joined, left, private, paged, notice, mail; data is the Event of proto/bbs.proto in its JSON"},
	"Object":      map[string]any{"type": "object"},
}

// document is the OpenAPI 3.1 description.
func document() map[string]any {
	paths := map[string]any{}
	for _, o := range ops {
		p, _ := paths[o.path].(map[string]any)
		if p == nil {
			p = map[string]any{}
			paths[o.path] = p
		}
		resp := map[string]any{"description": http.StatusText(o.status)}
		if o.resp != "" {
			ct := "application/json"
			if o.resp == "Binary" {
				ct = "application/octet-stream"
			} else if o.resp == "EventStream" {
				ct = "text/event-stream"
			}
			resp["content"] = map[string]any{ct: map[string]any{"schema": ref(o.resp)}}
		}
		e := map[string]any{"summary": o.summary,
			"responses": map[string]any{
				itoa(o.status): resp,
				"default":      map[string]any{"description": "an error", "content": map[string]any{"application/json": map[string]any{"schema": ref("Error")}}},
			}}
		switch o.scope {
		case "", "anonymous":
			e["security"] = []any{map[string]any{}, map[string]any{"bearer": []any{}}}
		default:
			e["security"] = []any{map[string]any{"bearer": []any{}}}
			e["description"] = "Scope: " + o.scope + "."
		}
		if o.body != "" {
			ct := "application/json"
			if o.body == "Binary" {
				ct = "application/octet-stream"
			}
			e["requestBody"] = map[string]any{"required": true, "content": map[string]any{ct: map[string]any{"schema": ref(o.body)}}}
		}
		var params []any
		for _, seg := range strings.Split(o.path, "/") {
			if strings.HasPrefix(seg, "{") {
				params = append(params, map[string]any{"name": strings.Trim(seg, "{}"), "in": "path", "required": true, "schema": str})
			}
		}
		if params != nil {
			e["parameters"] = params
		}
		p[strings.ToLower(o.method)] = e
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "bbs-sec API", "version": "1",
			"description": "The BBS over HTTP.  Tokens: POST /api/v1/login, or bbs token add.  WebSockets: proto/bbs.proto."},
		"paths":      paths,
		"components": map[string]any{"schemas": schemas, "securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}}},
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, document())
}
