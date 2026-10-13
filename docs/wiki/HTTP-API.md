The BBS has an HTTP API for programs and web front ends: REST for
accounts, message areas, mail and files; server-sent events; and
WebSockets for chat, in JSON or protobuf.  A web client is a node like
any other: it shows in *Who's online*, chats with terminal callers, gets
node messages and can page the sysop.

It is on when `bbs.conf` has an `[http]` section:

```ini
[http]
address = :8443
cert_file = /etc/bbs/tls/cert.pem
key_file = /etc/bbs/tls/key.pem
;cors = https://www.example.org
;metrics = 127.0.0.1:9100
```

| setting | default | |
|---|---|---|
| `address` | `:8443` | where it listens |
| `tls` | `yes` | TLS needs `cert_file` and `key_file`.  `tls = no` is for `127.0.0.1` behind a proxy that has TLS; `bbs check` warns about it on any other address |
| `anonymous` | `yes` | `/system` and `/who` without a token |
| `cors` | (none) | one browser origin allowed to call the API (no `*`); WebSockets are accepted only from it and the API's own host |
| `metrics` | (none) | an address for Prometheus metrics, `/metrics` (keep it private) |
| `max_per_ip` | `20` | connections at once from one address |
| `rate`, `burst` | `10`, `20` | requests a second per token (per address without one), and how many may come at once |
| `session_hours` | `24` | how long a login's token lasts |
| `terminal` | `yes` | the web terminal page (below) |
| `forum`, `forum_guests`, `forum_theme` | `yes`, `yes`, `ipb3` | the [web forum](Web-Forum), guests reading it, and its theme (`ipb3`, `ipb2`, `ipb1`, `ikon`, `phpbb2` or `phpbb1`) |

The OpenAPI 3.1 description is at `/api/v1/openapi.json`.

## Tokens and scopes

Every request but `/login`, `/openapi.json` and (when `anonymous = yes`)
`/system` and `/who` needs a token: `Authorization: Bearer bbs_...`.

- A **session**: `POST /api/v1/login` with `{"name": ..., "password": ...}`
  (and `"scopes"`, to have fewer) returns `{"token": ..., "expires": ...}`.
  `POST /api/v1/logout` ends it.
- An **API key**, for a program: `bbs token add alice -scope read,post
  -expires 90 -name "my bot"` prints it once.  `bbs token list [USER]` and
  `bbs token revoke ID` manage them, and users see and revoke their own at
  `/api/v1/me/tokens`.

Only a hash of a token is kept.  A locked user's tokens stop working.

| scope | allows |
|---|---|
| `read` | the account, users, message areas and messages, last read |
| `post` | posting |
| `mail` | reading, sending and deleting mail |
| `chat` | the event stream, WebSockets, node messages, paging |
| `files` | file areas, uploading and downloading |
| `sysop` | `/admin/...` (only for a sysop's tokens) |

## REST

All under `/api/v1`, JSON in and out (but uploads and downloads).
Errors are `{"error": "...", "code": "..."}`.

| | | scope |
|---|---|---|
| `POST /login`, `POST /logout` | a session token; ending it | (none), any |
| `GET /system` | the name, sysop, counts, features | anonymous |
| `GET /who` | who is online (with each `terminal`, as it said what it is) | anonymous |
| `GET /callers?limit=` | the last callers, newest first | read |
| `GET`, `PATCH /me` | your account, access and settings; change your location, a line about yourself (`about`), password or settings (`{"prefs": {"ansi": "off"}}`; `""` forgets one) | read |
| `GET /me/tokens`, `DELETE /me/tokens/{id}` | your tokens; revoke one | any |
| `GET /me/keys` | your SSH keys | read |
| `POST /me/keys`, `DELETE /me/keys/{id}` | add an SSH key (`{"key": "ssh-ed25519 ..."}`, from a login's session only); delete one | any |
| `GET /users`, `/users/{name}` | users | read |
| `GET /areas`, `/areas/{id}` | message areas, with your unread | read |
| `GET /areas/{id}/messages?after=&limit=&body=no` | messages after an id (200 at most) | read |
| `POST /areas/{id}/messages` | post: `{"subject", "body", "reply_to", "alias": true, "poll": ["answer", ...]}` | post |
| `PUT /areas/{id}/lastread` | mark read up to a message | read |
| `GET /messages/{id}` | a message | read |
| `GET /mail`, `GET`, `DELETE /mail/{id}` | your inbox; a mail (marked read); delete it | mail |
| `POST /mail` | send: to a user, or `Name@zone:net/node` as netmail | mail |
| `GET /qwk?mark=no` | your [QWK packet](Offline-Mail) (`X-QWK-Messages`: how many); its messages then count as read, unless `mark=no` | mail |
| `POST /qwk/rep` | a REP packet (the body): `{"posted", "mailed", "duplicates", "errors"}` | mail and post |
| `GET /fileareas`, `/fileareas/{id}/files`, `/files/{id}` | file areas and files | files |
| `POST /fileareas/{id}/files?name=&description=` | upload: the body is the file | files |
| `GET /files/{id}/content` | download (ranges work) | files |
| `GET /messages/{id}/thread` | the message's thread from its start, with each one's depth | read |
| `POST /messages/{id}/vote`, `POST /messages/{id}/answer` | vote (`{"value": 1}`, -1, 0); answer a poll (`{"answer": 0}`) | post |
| `GET /files?q=` or `?since=` | files by words in their names and descriptions, or added since a time (RFC 3339), newest first | files |
| `GET /admin/bans`, `DELETE /admin/bans/{ip}` | addresses banned for failed logins; lift a ban | sysop |
| `GET /admin/hacklog?lines=` | the hack log's last lines | sysop |
| `GET /admin/events`, `POST /admin/events/{name}/run` | the scheduled events; run one now | sysop |
| `PUT /admin/available` | `{"available": false}`: pages reach no sysop | sysop |
| `GET /files/{id}/listing` | what is in an archive (ZIP, tar, tar.gz); 422 for another file | files |
| `GET /chat/rooms` | chat rooms in use | chat |
| `POST /nodes/{n}/message`, `POST /page` | a line to a node; page the sysop (both need an open stream) | chat |
| `GET /events` | server-sent events | chat or read |
| `GET /ws`, `GET /ws/proto` | WebSockets | chat |
| `GET /admin/pages`, `POST /admin/pages/{node}/answer` | pages waiting; answer one (node 0: the longest waiting) | sysop |
| `POST /admin/users`, `PATCH /admin/users/{name}` | add a user; lock, unlock, make sysop, set password, `level`, `flags`, `qwk_node` | sysop |
| `POST /admin/areas`, `PATCH /admin/areas/{id}` | add an area; set its newsgroup, echo tag, `read_require`, `post_require` | sysop |
| `POST /admin/nodes/{n}/kick` | throw a caller off | sysop |
| `GET /admin/fido`, `POST /admin/fido/poll` | FidoNet links and outbound; call them now | sysop |
| `GET /admin/news`, `POST /admin/news/sync` | news peers; sync now | sysop |
| `GET /admin/qwknet`, `POST /admin/qwknet/call?net=` | [QWK networks](Offline-Mail#qwk-networks); call their hubs now | sysop |

```sh
TOKEN=$(curl -s https://bbs.example.org:8443/api/v1/login \
  -d '{"name":"alice","password":"..."}' | jq -r .token)
curl -s -H "Authorization: Bearer $TOKEN" https://bbs.example.org:8443/api/v1/areas
curl -s -H "Authorization: Bearer $TOKEN" https://bbs.example.org:8443/api/v1/areas/1/messages \
  -d '{"subject":"Hello","body":"From curl."}'
```

## Streams and nodes

The event stream and each WebSocket take a node, as a caller does
(`Via` is `web`): it counts against `max_nodes`, shows in *Who's online*,
and gets the node's events: lines said in its chat room, node messages,
pages (for sysops), new mail and notices.  The node goes when the
connection does.  `POST /nodes/{n}/message`, `/page` and answering a
page are done from the token's open stream (409 without one).

### Server-sent events

`GET /api/v1/events` (with the `Authorization` header; a browser does
this with `fetch`, as `EventSource` cannot send headers) starts with
`event: welcome` (its node and user), then sends each event as
`event: said` (`emoted`, `joined`, `left`, `private`, `paged`, `notice`,
`mail`) with the event as JSON in `data:`, and a comment every 25
seconds to keep it open.

## WebSockets

Two endpoints speak the same protocol, `bbs.v1`, defined in
[`proto/bbs.proto`](https://github.com/Neo-Desktop/bbs-sec/blob/HEAD/proto/bbs.proto):

| endpoint | subprotocol | frames |
|---|---|---|
| `/api/v1/ws` | `bbs.v1.json` | text: protobuf's JSON mapping (lowerCamelCase fields, enums by name, RFC 3339 times) |
| `/api/v1/ws/proto` | `bbs.v1.proto` | binary: protobuf |

The client sends `ClientMessage`s with an `id` of its choosing; each is
answered by `ServerMessage`s with that `id`: `ok` (with `room` when the
client's room changed), `error` (`code` and `message`), or what was asked
for.  Events and the welcome come with id 0.

A browser cannot set headers on a WebSocket, so unless the upgrade had an
`Authorization` header the first message must be `auth`, within 10
seconds.  The token needs the `chat` scope.

| request | answer |
|---|---|
| `auth {token}` | `welcome {bbs, user, node, scopes}` |
| `join {room}` | `lines` (the room's last lines), then `ok {room}` |
| `leave {}` | `ok` |
| `say {text, emote}` | `ok`; the room gets the line |
| `nodeMessage {node, text}` | `ok` |
| `page {reason}` | `ok {room}`: the private room to wait in (the client is put in it) |
| `answer {node}` | sysops: `lines`, `ok {room}` |
| `command {line}` | a line as typed in chat (`/who`, `/rooms`, `/msg`, `/join` ...): `lines`, `ok` |
| `who {}` | `nodes {nodes: [{node, user, via, since, doing}]}` |
| `rooms {}` | `rooms {rooms: [{name, users}]}` |
| `ping {}` | `pong` |

A session in JSON (`>` from the client):

```
> {"id":1,"auth":{"token":"bbs_..."}}
  {"welcome":{"bbs":"The BBS","user":"alice","node":3,"scopes":["read","post","mail","chat","files"]}}
> {"id":2,"join":{"room":"main"}}
  {"id":2,"lines":{"lines":["bob: hi all"]}}
  {"id":2,"ok":{"room":"main"}}
> {"id":3,"say":{"text":"hello from the web"}}
  {"id":3,"ok":{}}
  {"event":{"kind":"SAID","room":"main","from":"alice","node":3,"text":"hello from the web","time":"2026-10-11T12:00:00Z","line":"alice: hello from the web"}}
  {"event":{"kind":"SAID","room":"main","from":"bob","node":1,"text":"welcome!","time":"2026-10-11T12:00:04Z","line":"bob: welcome!"}}
```

Clients of the protobuf endpoint generate code from `proto/bbs.proto`
(`protoc --go_out=...`, `--python_out=...` and so on).  In JavaScript:

```js
const ws = new WebSocket("wss://bbs.example.org:8443/api/v1/ws", "bbs.v1.json");
ws.onopen = () => ws.send(JSON.stringify({id: 1, auth: {token}}));
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.welcome) ws.send(JSON.stringify({id: 2, join: {room: "main"}}));
  if (msg.event) console.log(msg.event.line);
};
```

The server pings every 30 seconds.  Frames are at most 16 KB; a frame
that does not read is answered by an error, and the same limits as on a
terminal apply (200 characters a line, 8 lines in 10 seconds).

## The web terminal

`/terminal` (and `/`, which goes there when the [web forum](Web-Forum) is off) is the BBS in a browser: a page
with [xterm.js](https://xtermjs.org/) (5.5, MIT licensed, built into the
`bbs` binary: nothing is loaded from elsewhere) whose terminal is joined
to the text BBS by a WebSocket, `/terminal/ws`.  The caller is a node like
a telnet caller (*Who's online* says `web terminal`) and logs in at the
BBS's own prompt, as on telnet: no token.  They get ANSI color and UTF-8
(ANSI art's CP437 shown as Unicode), menus, messages, mail, chat, paging
and doors; not file transfers (download files by the API or FTP).

| | |
|---|---|
| `GET /terminal` | the page |
| `GET /terminal/xterm.js`, `addon-fit.js`, `terminal.js`, `xterm.css`, `terminal.css`, `LICENSE.xterm` | its files |
| `GET /terminal/ws?cols=&rows=` | the WebSocket, subprotocol `bbs.terminal`: binary frames, the terminal's bytes both ways; the size the page fits its window to (40-255 columns, 10-200 rows), kept for the call |

The page allows scripts only from the BBS itself, no framing, and
WebSockets only from its own host (and the `cors` origin).  Connections
count against `max_per_ip` and `rate`, logins against the throttle as a
terminal's do; behind a proxy on 127.0.0.1, throttling is the proxy's to
do (the BBS sees every caller as the proxy).  `terminal = no` turns the
page off.

## Metrics

With `metrics = 127.0.0.1:9100`, `/metrics` there has, in Prometheus's
text format: `bbs_nodes`, `bbs_nodes_by_via{via}`, `bbs_max_nodes`,
`bbs_users`, `bbs_messages`, `bbs_logins_total`,
`bbs_login_failures_total`, `bbs_posts_total`, `bbs_mail_total`,
`bbs_pages_total`, `bbs_chat_lines_total`, `bbs_pages_waiting` and
`bbs_fido_outbound_files{link}`.

## Security

- TLS by default; tokens never go in URLs (only in the `Authorization`
  header or a WebSocket's first message).
- Tokens are 32 random bytes; only their SHA-256 is stored.  A wrong
  token waits as a wrong password does, and failed logins are logged and
  counted like a terminal's.
- Rate limits per token (per address without one), connections per
  address, JSON bodies at most 64 KB with unknown fields refused,
  uploads limited as on the BBS.
- No CORS unless one origin is named; WebSockets from origins other than
  it and the API's own host are refused.  Responses carry `nosniff` and `no-store`.
- What web clients say and post is cleaned as a terminal caller's is.

**See also:** [Chat and paging](Chat-and-Paging), [Security](Security), [Configuration](Configuration)
