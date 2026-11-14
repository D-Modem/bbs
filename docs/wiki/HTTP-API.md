The BBS has an HTTP API for programs and web front ends. It has REST for
accounts, message areas, mail and files, server-sent events, and
WebSockets for chat, in JSON or protobuf. A web client is a node, as all
other clients are. It shows in *Who's online*, chats with terminal
callers, gets node messages and can page the sysop.

The API is enabled when `bbs.conf` has an `[http]` section:

```ini
[http]
address = :8443
cert_file = /etc/bbs/tls/cert.pem
key_file = /etc/bbs/tls/key.pem
;cors = https://www.example.org
;metrics = 127.0.0.1:9100
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:8443` | the address on which it listens |
| `tls` | `yes` | TLS must have `cert_file` and `key_file`. `tls = no` is for `127.0.0.1` behind a proxy that has TLS. `bbs check` gives a warning about it on all other addresses |
| `trusted_proxies` | (none) | reverse proxies in front (addresses or CIDR ranges, with spaces or commas between them). The server believes them when they tell the source of a request: [below](#behind-a-proxy) |
| `secure_cookies` | as `tls` | the login cookies of the forum and image boards go over HTTPS only. Set `yes` when a proxy in front has the TLS |
| `anonymous` | `yes` | `/system` and `/who` without a token |
| `cors` | (none) | one browser origin that can call the API (no `*`). The API accepts WebSockets only from this origin and from its own host |
| `metrics` | (none) | an address for Prometheus metrics, `/metrics` (keep it private) |
| `max_per_ip` | `20` | the maximum number of connections at one time from one address |
| `rate`, `burst` | `10`, `20` | requests each second for each token (for each address without a token), and the number that can come at one time |
| `session_hours` | `24` | the validity of the token of a login |
| `terminal` | `yes` | the web terminal page (below) |
| `forum`, `forum_guests`, `forum_theme`, `forum_flags` | `yes`, `yes`, `ipb3`, `yes` | the [web forum](Web-Forum), guest access to it, and its theme (`ipb3`, `ipb5`, `ipb4`, `ipb2`, `ipb1`, `ikon`, `vb6`, `vb5`, `vb4`, `vb3`, `vb2`, `vblite`, `xf23`, `xf2`, `xf1`, `mybb18`, `mybb16`, `mybb12`, `pun14`, `flux15`, `pun12`, `yabb25`, `yabb2`, `yabb1`, `smf30`, `smf21`, `smf20`, `smf1`, `phpbb33`, `phpbb32`, `phpbb31`, `phpbb30`, `phpbb2` or `phpbb1`) |
| `chan` | `yes` | the [image boards](Image-Boards) at `/chan/` (configured in `[chan]`) |

The OpenAPI 3.1 description is at `/api/v1/openapi.json`.


## Behind a proxy

The server can be behind a reverse proxy (nginx, Caddy, HAProxy, a load
balancer) that does the TLS. Then all requests come from the address of
the proxy. Thus, put the proxy in `trusted_proxies`. Then the server
trusts the proxy to tell the caller of each request:

```ini
[http]
address = 127.0.0.1:8080
tls = no
trusted_proxies = 127.0.0.1, ::1
secure_cookies = yes
```

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_http_version 1.1;                  # the WebSockets: streams
    proxy_set_header Upgrade $http_upgrade;  # and the web terminal
    proxy_set_header Connection "upgrade";
    client_max_body_size 8m;                 # the image boards' images
}
```

The address of the caller is the last address in `X-Forwarded-For` (or
the `for=` of `Forwarded`, or `X-Real-IP`) that is not a trusted proxy.
The caller sent the addresses before it, and the server does not trust
them. The server then uses that address for the request in all
functions: rate limits, logins and the logs, the forum, the throttling
of the web terminal, the flood control and bans of the
[image boards](Image-Boards), and the `[security]` deny lists and bans
(a refused caller gets 403). Connections from a trusted proxy do not
count against `max_per_ip`, because they carry many callers. The server
accepts requests from all other sources as they come, and ignores their
forwarding headers.

## Tokens and scopes

All requests except `/login`, `/openapi.json` and (when
`anonymous = yes`) `/system` and `/who` must have a token:
`Authorization: Bearer bbs_...`.

- A **session**: `POST /api/v1/login` with `{"name": ..., "password": ...}`
  (and `"scopes"`, for fewer scopes) gives `{"token": ..., "expires": ...}`.
  `POST /api/v1/logout` ends it.
- An **API key**, for a program: `bbs token add alice -scope read,post
  -expires 90 -name "my bot"` prints it one time. `bbs token list [USER]`
  and `bbs token revoke ID` manage them. Users see and revoke their own
  keys at `/api/v1/me/tokens`.

The BBS keeps only a hash of a token. The tokens of a locked user stop.

| Scope | Allows |
|---|---|
| `read` | the account, users, message areas and messages, last read |
| `post` | posts |
| `mail` | read, send and delete mail |
| `chat` | the event stream, WebSockets, node messages, pages |
| `files` | file areas, uploads and downloads |
| `sysop` | `/admin/...` (only for the tokens of a sysop) |

## REST

All endpoints are under `/api/v1`. They accept and give JSON (except
uploads and downloads).
Errors are `{"error": "...", "code": "..."}`.

| Request | What it does | Scope |
|---|---|---|
| `POST /login`, `POST /logout` | gives a session token. Ends it | (none), any |
| `GET /system` | the name, sysop, counts, features | anonymous |
| `GET /who` | the users who are online (with each `terminal`, as it identified itself, and `country`, ISO 3166, if known) | anonymous |
| `GET /callers?limit=` | the last callers, newest first | read |
| `GET`, `PATCH /me` | your account, access and settings. Change your location, a line about yourself (`about`), password or settings (`{"prefs": {"ansi": "off"}}`. `""` removes one) | read |
| `GET /me/tokens`, `DELETE /me/tokens/{id}` | your tokens. Revokes one | any |
| `GET /me/keys` | your SSH keys | read |
| `POST /me/keys`, `DELETE /me/keys/{id}` | adds an SSH key (`{"key": "ssh-ed25519 ..."}`, from the session of a login only). Deletes one | any |
| `GET /users`, `/users/{name}` | users | read |
| `GET /areas`, `/areas/{id}` | message areas, with your unread messages | read |
| `GET /areas/{id}/messages?after=&limit=&body=no` | messages after an id (a maximum of 200) | read |
| `POST /areas/{id}/messages` | posts: `{"subject", "body", "reply_to", "alias": true, "poll": ["answer", ...]}` | post |
| `PUT /areas/{id}/lastread` | marks the area as read up to a message | read |
| `GET /messages/{id}` | a message | read |
| `GET /mail`, `GET`, `DELETE /mail/{id}` | your inbox. A mail (marked as read). Deletes it | mail |
| `POST /mail` | sends: to a user, or `Name@zone:net/node` as netmail | mail |
| `GET /qwk?mark=no` | your [QWK packet](Offline-Mail) (`X-QWK-Messages`: the number of messages). Its messages then count as read, unless `mark=no` | mail |
| `POST /qwk/rep` | a REP packet (the body): `{"posted", "mailed", "duplicates", "errors"}` | mail and post |
| `GET /fileareas`, `/fileareas/{id}/files`, `/files/{id}` | file areas and files | files |
| `POST /fileareas/{id}/files?name=&description=` | uploads: the body is the file | files |
| `GET /files/{id}/content` | downloads (ranges operate) | files |
| `GET /messages/{id}/thread` | the thread of the message from its start, with the depth of each message | read |
| `POST /messages/{id}/vote`, `POST /messages/{id}/answer` | votes (`{"value": 1}`, -1, 0). Answers a poll (`{"answer": 0}`) | post |
| `GET /files?q=` or `?since=` | files by words in their names and descriptions, or added since a time (RFC 3339), newest first | files |
| `GET /admin/bans`, `DELETE /admin/bans/{ip}` | addresses banned for failed logins. Removes a ban | sysop |
| `GET /admin/hacklog?lines=` | the last lines of the hack log | sysop |
| `GET /admin/events`, `POST /admin/events/{name}/run` | the scheduled events. Runs one now | sysop |
| `PUT /admin/available` | `{"available": false}`: pages go to no sysop | sysop |
| `GET /files/{id}/listing` | the contents of an archive (ZIP, tar, tar.gz). 422 for other files | files |
| `GET /chat/rooms` | chat rooms in use | chat |
| `POST /nodes/{n}/message`, `POST /page` | a line to a node. Pages the sysop (the two must have an open stream) | chat |
| `GET /events` | server-sent events | chat or read |
| `GET /ws`, `GET /ws/proto` | WebSockets | chat |
| `GET /admin/pages`, `POST /admin/pages/{node}/answer` | the pages that wait. Answers one (node 0: the page that waited longest) | sysop |
| `POST /admin/users`, `PATCH /admin/users/{name}` | adds a user. Lock, unlock, make sysop, set password, `level`, `flags`, `qwk_node` | sysop |
| `POST /admin/areas`, `PATCH /admin/areas/{id}` | adds an area. Sets its newsgroup, echo tag, `read_require`, `post_require` | sysop |
| `POST /admin/nodes/{n}/kick` | disconnects a caller | sysop |
| `GET /admin/fido`, `POST /admin/fido/poll` | FidoNet links and outbound. Calls them now | sysop |
| `GET /admin/news`, `POST /admin/news/sync` | news peers. Syncs now | sysop |
| `GET /admin/qwknet`, `POST /admin/qwknet/call?net=` | [QWK networks](Offline-Mail#qwk-networks). Calls their hubs now | sysop |

```sh
TOKEN=$(curl -s https://bbs.example.org:8443/api/v1/login \
  -d '{"name":"alice","password":"..."}' | jq -r .token)
curl -s -H "Authorization: Bearer $TOKEN" https://bbs.example.org:8443/api/v1/areas
curl -s -H "Authorization: Bearer $TOKEN" https://bbs.example.org:8443/api/v1/areas/1/messages \
  -d '{"subject":"Hello","body":"From curl."}'
```

## Streams and nodes

The event stream and each WebSocket take a node, as a caller does (`Via`
is `web`). The node counts against `max_nodes`, shows in *Who's online*,
and gets the events of the node: lines in its chat room, node messages,
pages (for sysops), new mail and notices. The node ends when the
connection ends. You must send `POST /nodes/{n}/message`, `/page` and
the answer to a page from the open stream of the token (409 without
one). A page that gets to no sysop gives 503 (`unavailable`), as the
Page request of the WebSocket answers it. The page does not wait.

### Server-sent events

`GET /api/v1/events` (with the `Authorization` header. A browser does
this with `fetch`, because `EventSource` cannot send headers) starts
with `event: welcome` (its node and user). Then it sends each event as
`event: said` (`emoted`, `joined`, `left`, `private`, `paged`, `notice`,
`mail`), with the event as JSON in `data:`. It sends a comment every 25
seconds to keep the connection open.

## WebSockets

Two endpoints use the same protocol, `bbs.v1`, defined in
[`proto/bbs.proto`](https://github.com/D-Modem/bbs/blob/HEAD/proto/bbs.proto):

| Endpoint | Subprotocol | Frames |
|---|---|---|
| `/api/v1/ws` | `bbs.v1.json` | text: the JSON mapping of protobuf (lowerCamelCase fields, enums by name, RFC 3339 times) |
| `/api/v1/ws/proto` | `bbs.v1.proto` | binary: protobuf |

The client sends `ClientMessage`s with an `id` that it selects. The
server answers each with `ServerMessage`s with that `id`: `ok` (with
`room` when the room of the client changed), `error` (`code` and
`message`), or the requested data. Events and the welcome come with id
0.

A browser cannot set headers on a WebSocket. Thus, unless the upgrade
had an `Authorization` header, the first message must be `auth`, within
10 seconds. The token must have the `chat` scope.

| Request | Answer |
|---|---|
| `auth {token}` | `welcome {bbs, user, node, scopes}` |
| `join {room}` | `lines` (the last lines of the room), then `ok {room}` |
| `leave {}` | `ok` |
| `say {text, emote}` | `ok`. The room gets the line |
| `nodeMessage {node, text}` | `ok` |
| `page {reason}` | `ok {room}`: the private room in which to wait (the server puts the client in it) |
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
(`protoc --go_out=...`, `--python_out=...` and other options). In
JavaScript:

```js
const ws = new WebSocket("wss://bbs.example.org:8443/api/v1/ws", "bbs.v1.json");
ws.onopen = () => ws.send(JSON.stringify({id: 1, auth: {token}}));
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.welcome) ws.send(JSON.stringify({id: 2, join: {room: "main"}}));
  if (msg.event) console.log(msg.event.line);
};
```

The server pings every 30 seconds. Frames have a maximum of 16 KB. The
server answers a frame that it cannot read with an error. The same limits
as on a terminal apply (200 characters for each line, 8 lines in 10
seconds).

## The web terminal

`/terminal` is the BBS in a browser. (`/` goes there when the
[web forum](Web-Forum) is disabled.) It is a page with
[xterm.js](https://xtermjs.org/) (5.5, MIT licensed, part of the `bbs`
binary: the page loads nothing from other locations). A WebSocket,
`/terminal/ws`, connects its terminal to the text BBS. The caller is a
node, as a telnet caller is (*Who's online* says `web terminal`). The
caller logs in at the prompt of the BBS, as on telnet: no token is
necessary.

The caller gets ANSI colour and UTF-8 (the CP437 of ANSI art shows as
Unicode), menus, messages, mail, chat, paging and doors. The caller does
not get file transfers (download files by the API or FTP). Its xterm.js
has the image addon. Thus, the pictures (sixel) of the
[image boards](Image-Boards) show in it.

| Path | What it serves |
|---|---|
| `GET /terminal` | the page |
| `GET /terminal/xterm.js`, `addon-fit.js`, `addon-image.js`, `terminal.js`, `xterm.css`, `terminal.css`, `LICENSE.xterm` | its files |
| `GET /terminal/ws?cols=&rows=` | the WebSocket, subprotocol `bbs.terminal`: binary frames, the bytes of the terminal in the two directions. The size to which the page fits its window (40-255 columns, 10-200 rows), kept for the call |

The page permits scripts only from the BBS, cannot be in a frame, and
accepts WebSockets only from its own host (and the `cors` origin).
Connections count against `max_per_ip` and `rate`. Logins count against
the throttle, as the logins of a terminal do. Behind a proxy on
127.0.0.1 without `trusted_proxies`, the proxy must do the throttling,
because the BBS sees all callers as the proxy. `terminal = no` disables
the page.

## Metrics

With `metrics = 127.0.0.1:9100`, `/metrics` at that address has these
items, in the text format of Prometheus: `bbs_nodes`,
`bbs_nodes_by_via{via}`, `bbs_max_nodes`, `bbs_users`, `bbs_messages`,
`bbs_logins_total`, `bbs_login_failures_total`, `bbs_posts_total`,
`bbs_mail_total`, `bbs_pages_total`, `bbs_chat_lines_total`,
`bbs_pages_waiting` and `bbs_fido_outbound_files{link}`.

## Security

- TLS by default. Tokens never go in URLs (only in the `Authorization`
  header or the first message of a WebSocket).
- Tokens are 32 random bytes. The BBS stores only their SHA-256. After
  an incorrect token, the server waits as it does after an incorrect
  password. The BBS logs and counts failed logins as for a terminal.
- Rate limits for each token (for each address without a token),
  connections for each address, JSON bodies of a maximum of 64 KB with
  unknown fields refused, and uploads with the same limits as on the
  BBS.
- No CORS unless you name one origin. The server refuses WebSockets from
  origins other than that origin and its own host. Responses have
  `nosniff` and `no-store`.
- The BBS cleans the text that web clients send and post, as it cleans
  the text of a terminal caller.

**See also:** [Chat and paging](Chat-and-Paging), [Security](Security), [Configuration](Configuration)
