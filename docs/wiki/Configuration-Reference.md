<!-- Made by go generate ./internal/confedit from bbs.conf.sample: edit the sample, not this page. -->

# Configuration reference

Every section and key of bbs.conf, from the comments in
[bbs.conf.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.conf.sample)
(the same help the [configuration screens](Configuration#editing-it-the-configuration-screens)
show).  **Live** keys take effect when the file is read again
(`systemctl reload bbs`, or saving in the configuration screens); the
rest when the BBS starts again.  [Configuration](Configuration) says how
the file is laid out.

| section | what it is |
|---|---|
| [`[general]`](#general) | The system: its name, database, log, logins, and what callers see. |
| [`[listen NAME]`](#listen-name) | A listener: a port (or unix socket) callers come in by, and how their terminals are treated. |
| [`[security]`](#security) | keeping callers out: a file of addresses and networks refused, one of names no new user may take (* wildcards), the hack log, and how long an address throttled for failed logins is banned outright. |
| [`[event NAME]`](#event-name) | scheduled events: a program (no shell), daily at a time or every N minutes; see the wiki's Sysop tools. |
| [`[backup]`](#backup) | Backups: this file, the database (a SQLite file, whatever the engine, taken whole at one moment while callers are on) and the files' directories (file areas, image boards, FidoNet, screens, scripts), one archive (bbs-YYYYMMDD-HHMMSS.tar.gz) a backup. |
| [`[modemmenu]`](#modemmenu) | modemmenu's auth socket ([authd] there): the accounts modemmenu logs in (no password here) log in by telnet, SSH, the web ... |
| [`[http]`](#http) | The HTTP API (REST, server-sent events, and WebSockets in JSON and protobuf): see docs/wiki/HTTP-API.md. |
| [`[chan]`](#chan) | How the image boards are run. |
| [`[level NAME]`](#level-name) | An access level: a user's level gets what the [level] at or below it says. |
| [`[ftp]`](#ftp) | The FTP server for the file areas: users log in with their BBS name and password; each file area is a directory. |
| [`[news]`](#news) | Newsgroups: a message area with a newsgroup name (bbs area newsgroup General bbs.general) is a newsgroup, served by NNTP to newsreaders and kept in step with peer news servers. |
| [`[nntp]`](#nntp) | The NNTP server. |
| [`[peer NAME]`](#peer-name) | A peer news server, one section each. |
| [`[mail]`](#mail) | E-mail. |
| [`[smarthost]`](#smarthost) | An outgoing mail server to send through, instead of straight to each domain's MX: your provider's, or a relay such as Postfix. |
| [`[pop3]`](#pop3) | The mail servers: each runs only with its section. |
| [`[imap]`](#imap) | IMAP: mail programs read and file mail in the folders. |
| [`[smtp]`](#smtp) | mail submission: users send mail, logged in (port 587 or 465) |
| [`[mx]`](#mx) | mail from the internet: on port 25, named by the domain's MX record. |
| [`[lmtp]`](#lmtp) | LMTP, for a mail server in front (Postfix's virtual_transport) to hand mail in: a unix socket's path or host:port. |
| [`[fido]`](#fido) | FidoNet (or another FTN network). |
| [`[binkp]`](#binkp) | binkp: answer the links' calls. |
| [`[link NAME]`](#link-name) | A system we exchange mail with, one section each. |
| [`[qwknet NAME]`](#qwknet-name) | QWK networks this BBS is a node of (DOVE-Net and the like), one section each. |
| [`[door NAME]`](#door-name) | A door: a program callers play, or another system's doors. |
| [`[irc]`](#irc) | IRC: the chat rooms as channels (#main ...), for IRC clients. |
| [`[oscar]`](#oscar) | AIM and ICQ: the chat for AIM and ICQ clients (OSCAR), beside IRC. |
| [`[msn]`](#msn) | MSN Messenger: the chat for MSN Messenger 1.0 to 4.7 and Windows Messenger 4.x (MSNP2 to 7), beside IRC and AIM. |
| [`[mrc]`](#mrc) | Multi Relay Chat: the BBSes' chat network. |
| [`[finger]`](#finger) | finger: who is on ("finger @host"), and who a user is ("finger name@host"). |
| [`[gopher]`](#gopher) | gopher: about the system, who is on, and what areas the sysop opens: no login, so anyone sees them. |
| [`[menu NAME]`](#menu-name) | A menu of the sysop's: [menu main] takes the place of the built-in main menu; others are reached from it (menu:NAME). |
| [`[strings]`](#strings) | The prompts and messages, with @-codes and tags. |

## `[general]`

The system: its name, database, log, logins, and what callers see.

| key | default | live | what it does |
|---|---|---|---|
| `name` |  | yes | the system's name, its sysop's, and where it is (system info, @-codes) Examples: `D-Modem BBS`. |
| `sysop` |  | yes | the system's name, its sysop's, and where it is (system info, @-codes) Examples: `Sysop`. |
| `location` |  | yes | the system's name, its sysop's, and where it is (system info, @-codes) Examples: `Somewhere, Earth`. |
| `postal_address` |  | yes | how to reach it, for the system info (and @POSTAL@, @PHONE@, @ADDRESSES@): its postal address, the numbers callers dial, and its internet addresses. Examples: `1 Main St, Springfield`. |
| `phone` |  | yes | how to reach it, for the system info (and @POSTAL@, @PHONE@, @ADDRESSES@): its postal address, the numbers callers dial, and its internet addresses. Examples: `+1 555 0100 (300-33600), +1 555 0101 (V.92)`. |
| `addresses` |  | yes | how to reach it, for the system info (and @POSTAL@, @PHONE@, @ADDRESSES@): its postal address, the numbers callers dial, and its internet addresses. Examples: `telnet bbs.example.org, ssh bbs.example.org:2222, https://bbs.example.org/`. |
| `user` |  |  | ; started as root, serve binds its ports (23, 79 ...) and sockets, then runs as this user (and its group, or group; with the user's other groups, for socket_group and docker): what it makes is that user's. Other bbs commands run as root become it too (but check, config, backup restore). Under systemd: no User= then. See docs/wiki/Running.md. Examples: `bbs`. |
| `group` |  |  | ; started as root, serve binds its ports (23, 79 ...) and sockets, then runs as this user (and its group, or group; with the user's other groups, for socket_group and docker): what it makes is that user's. Other bbs commands run as root become it too (but check, config, backup restore). Under systemd: no User= then. See docs/wiki/Running.md. Examples: `bbs`. |
| `database` |  |  | the database: SQLite's file, or a PostgreSQL, MySQL or MariaDB server (whose password then lives here: keep this file 0640). bbs db copy moves one into another. See docs/wiki/Databases.md. Examples: `/var/lib/bbs/bbs.db`, `postgres://bbs:PASSWORD@db.example.org/bbs`, `mysql://bbs:PASSWORD@db.example.org:3306/bbs`, `mariadb://bbs:PASSWORD@db.example.org:3306/bbs`. |
| `log` |  |  | syslog (daemon facility), stderr, none, or a file. Examples: `syslog`. |
| `banner` |  | yes | shown before the login (text, or a file) Examples: `Call us at 555-1000, 300 to 33600 bit/s.`. |
| `banner_file` |  | yes | shown before the login (text, or a file) Examples: `/etc/bbs/banner.txt`. |
| `motd` |  | yes | the message of the day, after the login: motd, then the file (read at each login) Examples: `The line is down for maintenance on Sunday.`. |
| `motd_file` |  | yes | the message of the day, after the login: motd, then the file (read at each login) Examples: `/etc/bbs/motd.txt`. |
| `screens_dir` |  |  | the sysop's screens (display files): logon, newuser, welcome, goodbye and those menus name; NAME.ans (ANSI, CP437), NAME.asc (text), NAME.3270. screens.sample has some to start from. Examples: `/etc/bbs/screens`. |
| `scripts_dir` |  |  | the sysop's JavaScript: NAME.js runs from a menu's script:NAME, logon.js after each login; scripts.sample has some to start from. Examples: `/etc/bbs/scripts`. |
| `page_command` |  | yes | a caller pages the sysop and no sysop is on: tell them this way ({user}, {node} and {reason} filled in; no shell). E.g. a push by ntfy.sh: Examples: `curl -s -d "{user} on node {node}: {reason}" https://ntfy.sh/my-bbs-pages`. |
| `goodbye` |  | yes | shown when a caller logs off. Examples: `Thanks for calling.`. |
| `qwk_id` |  | yes | QWK offline mail: the packets' name (BBSID.QWK), up to 8 letters and digits; by default made of the name. See docs/wiki/Offline-Mail.md. Examples: `DMODEM`. |
| `logo` |  |  | the logo: a PNG (at most 1000x400), shown as sixel graphics on telnet terminals that have them, and on 3270s (below). Without one, the system's name is drawn in a pixel font. Examples: `/etc/bbs/logo.png`. |
| `tn3270_symbols` |  |  | 3270 graphics. Box drawing (graphic escape) is used wherever the terminal has it. tn3270_symbols: the logo (the system's name, or tn3270_logo, a PNG, or logo: one color, dark or opaque pixels drawn) loaded as programmed symbols on terminals that take them (IBM PCOMM, HostExplorer; not the x3270 family). tn3270_vector: pictures (an image board's) drawn in the terminal's own colors, and the logo, with vector graphics on terminals that have them (PCOMM with host graphics on); without them, pictures are drawn in programmed symbols (tn3270_symbols), in blocks (graphic escape, x3270), or said in words. Examples: `yes`. |
| `tn3270_vector` |  |  | 3270 graphics. Box drawing (graphic escape) is used wherever the terminal has it. tn3270_symbols: the logo (the system's name, or tn3270_logo, a PNG, or logo: one color, dark or opaque pixels drawn) loaded as programmed symbols on terminals that take them (IBM PCOMM, HostExplorer; not the x3270 family). tn3270_vector: pictures (an image board's) drawn in the terminal's own colors, and the logo, with vector graphics on terminals that have them (PCOMM with host graphics on); without them, pictures are drawn in programmed symbols (tn3270_symbols), in blocks (graphic escape, x3270), or said in words. Examples: `yes`. |
| `tn3270_logo` |  |  | 3270 graphics. Box drawing (graphic escape) is used wherever the terminal has it. tn3270_symbols: the logo (the system's name, or tn3270_logo, a PNG, or logo: one color, dark or opaque pixels drawn) loaded as programmed symbols on terminals that take them (IBM PCOMM, HostExplorer; not the x3270 family). tn3270_vector: pictures (an image board's) drawn in the terminal's own colors, and the logo, with vector graphics on terminals that have them (PCOMM with host graphics on); without them, pictures are drawn in programmed symbols (tn3270_symbols), in blocks (graphic escape, x3270), or said in words. Examples: `/etc/bbs/logo.png`. |
| `files_dir` |  |  | file areas: where the files are kept (by number, never by their names), the largest file, and what a user may upload a day (sysops: no limit) Examples: `/var/lib/bbs/files`. |
| `max_file_size` |  |  | file areas: where the files are kept (by number, never by their names), the largest file, and what a user may upload a day (sysops: no limit) Examples: `64M`. |
| `upload_quota` |  |  | file areas: where the files are kept (by number, never by their names), the largest file, and what a user may upload a day (sysops: no limit) Examples: `256M`. |
| `new_users` |  | yes | callers may make their own accounts (NEW at the login, PF5 on a 3270) Examples: `yes`. |
| `max_nodes` |  | yes | callers at once, on all listeners together. Examples: `32`. |
| `attempts` |  | yes | logins before hanging up, seconds after a wrong one. Examples: `3`. |
| `fail_delay` |  | yes | logins before hanging up, seconds after a wrong one. Examples: `3`. |
| `login_timeout` |  | yes | seconds to log in, and for a key once logged in. Examples: `120`. |
| `idle_timeout` |  | yes | seconds to log in, and for a key once logged in. Examples: `600`. |
| `throttle_failures` |  | yes | this many failed logins (or new accounts) from one IP address or caller ID within throttle_time seconds turn it away until they age out (0: never). Callers with neither (a modem server's, no caller ID) are not throttled: one would lock out all. Examples: `10`. |
| `throttle_time` |  | yes | this many failed logins (or new accounts) from one IP address or caller ID within throttle_time seconds turn it away until they age out (0: never). Callers with neither (a modem server's, no caller ID) are not throttled: one would lock out all. Examples: `600`. |
| `geoip` |  | yes | where callers are. geoip: a country database, a MaxMind DB (.mmdb: GeoLite2 Country, DB-IP Lite...) or a CSV of ranges, read again when it changes; with it the log, who's online and the web forum's flags give callers' countries. A city database (GeoLite2 City, DB-IP City Lite, IP2Location DB3 and up) gives the log and the sysop their region and city too (some 120 MB of memory). geoip_credit: the credit its licence asks for, at the foot of the pages that show it. See docs/wiki/Modem-Callers.md. Examples: `/var/lib/bbs/dbip-country-lite.mmdb`, `/var/lib/bbs/dbip-city-lite.mmdb`. |
| `geoip_credit` |  | yes | where callers are. geoip: a country database, a MaxMind DB (.mmdb: GeoLite2 Country, DB-IP Lite...) or a CSV of ranges, read again when it changes; with it the log, who's online and the web forum's flags give callers' countries. A city database (GeoLite2 City, DB-IP City Lite, IP2Location DB3 and up) gives the log and the sysop their region and city too (some 120 MB of memory). geoip_credit: the credit its licence asks for, at the foot of the pages that show it. See docs/wiki/Modem-Callers.md. Examples: `IP geolocation by DB-IP`. |
| `geoip_credit_url` |  | yes | where callers are. geoip: a country database, a MaxMind DB (.mmdb: GeoLite2 Country, DB-IP Lite...) or a CSV of ranges, read again when it changes; with it the log, who's online and the web forum's flags give callers' countries. A city database (GeoLite2 City, DB-IP City Lite, IP2Location DB3 and up) gives the log and the sysop their region and city too (some 120 MB of memory). geoip_credit: the credit its licence asks for, at the foot of the pages that show it. See docs/wiki/Modem-Callers.md. Examples: `https://db-ip.com`. |
| `phone_country` |  | yes | phone_country: the country (ISO 3166: US, GB, DE) whose national numbers modem caller IDs are, as the exchange sends them ("4155550123", "02071234567"); international ones (+44..., 011 44...) are read anyway. A caller ID gives the country, the kind of number and, where the plan says, the place (by libphonenumber's tables, built in) Examples: `US`. |
| `new_user_level` | `20` | yes | Access levels: a new user's level (0-255). |
| `validated_level` | `20` | yes | Access levels: the level a sysop's validation gives a user. |


## `[listen NAME]`

A listener: a port (or unix socket) callers come in by, and how their terminals are treated.

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `type` | `telnet` |  | a telnet server; with tn3270 = yes (the default), a client that says it is a 3270 (IBM-3278-2-E, IBM-DYNAMIC) gets the 3270 screens raw the caller's bytes as they come, no protocol: app_dmodem's mode = tcp, modemmenu tcp/unix items rlogin RFC 1282, as modemmenu's rlogin items speak it ssh SSH: passwords or users' keys (host_keys, legacy) Examples: `telnet`, `raw`, `ssh`, `rlogin`. |
| `address` | `host:port` |  | or a unix socket's path (/run/bbs/bbs.sock) Examples: `:2300`, `:992`, `127.0.0.1:2323`, `:2324`, `:2222`, `127.0.0.1:2513`, `/run/bbs/menu.sock`, `/run/bbs/handoff.sock`. |
| `via` | `...` |  | how its callers came, in who's online (telnet, modem, rlogin by type) |
| `ansi` | `auto` |  | the terminal's ANSI color: auto asks the terminal (or believes its terminal type), yes, no. |
| `utf8` | `auto` |  | UTF-8 both ways: auto by the terminal type (xterm and friends yes; "ansi" BBS terminals no), yes, no. |
| `sixel` | `auto` |  | the logo as sixel graphics on terminals that say they have them (device attributes); no. auto for telnet, no for raw and rlogin (slow modem lines) |
| `petscii` | `no` |  | Commodore 64s and 128s: yes (every caller is one), auto (a caller that is not ANSI is watched while it logs in: a key only a C64 has, or DEL asked for after a name in capitals, makes it PETSCII), no. |
| `vt` | `auto` |  | ask terminals what they are (answerback, DA1, DA2, XTVERSION; ESC Z for a VT52): DEC line drawing for VTs without UTF-8, bold on monochrome VTs, a VT52's escapes; no: ask nothing. |
| `rip` | `auto` |  | RIPscrip menus and screens for terminals that answer ESC [ ! (BBS terminals; modern ones are not asked), yes, no. |
| `max_per_ip` | `4` |  | connections at once from one address (loopback and unix sockets: no limit, they are modem servers) |
| `header` | `no` |  | yes: a session header comes first, NAME=value lines and an empty line: app_dmodem's DMODEM_* or a modemmenu item's MODEM_* (rate, caller ID, number) Examples: `yes`. |
| `header_timeout` | `10` |  | seconds for the header. |
| `handoff` | `no` |  | type = raw on a unix socket, with header = yes: the caller's line itself may come with the header (SCM_RIGHTS, modemmenu's item with handoff = yes), the session then on that terminal, nothing copied; closing the connection gives the line back. Examples: `yes`. |
| `trust_user` | `no` |  | yes: the header's MODEM_USER (an rlogin's server user) is logged in without a password. Only where nothing but trusted menus can connect. Examples: `yes`. |
| `create_users` | `no` |  | yes, with trust_user: a user unknown here gets an account (with no password of its own) Examples: `yes`. |
| `socket_mode` | `0660` |  | a unix socket's permissions. |
| `socket_group` | `...` |  | and its group. Examples: `asterisk`. |
| `frontend` | `bbs` |  | what callers get: the BBS, or compuserve: the CompuServe Information Service (telnet and raw listeners; no 3270). See the wiki's CompuServe. Examples: `compuserve`. |
| `connect` | `9600` |  | compuserve: the CONNECT line before Host Name: (telnet: 9600; raw: none, the modem says its own) |
| `tn3270` |  |  | See the section above. Examples: `yes`. |
| `tls_cert` |  |  | See the section above. Examples: `/etc/bbs/cert.pem`. |
| `tls_key` |  |  | See the section above. Examples: `/etc/bbs/key.pem`. |
| `host_keys` |  |  | See the section above. Examples: `/etc/bbs/ssh_host_ed25519_key /etc/bbs/ssh_host_rsa_key`. |
| `legacy` |  |  | old clients only: CBC ciphers and 1024-bit DH too. Examples: `no`. |


## `[security]`

keeping callers out: a file of addresses and networks refused, one of names no new user may take (* wildcards), the hack log, and how long an address throttled for failed logins is banned outright.

| key | default | live | what it does |
|---|---|---|---|
| `deny_file` |  |  | See the section above. Examples: `/etc/bbs/deny.txt`. |
| `name_deny_file` |  |  | See the section above. Examples: `/etc/bbs/names.txt`. |
| `hack_log` |  |  | See the section above. Examples: `/var/log/bbs/hack.log`. |
| `ban_minutes` |  |  | See the section above. Examples: `60`. |


## `[event NAME]`

scheduled events: a program (no shell), daily at a time or every N minutes; see the wiki's Sysop tools.

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `command` |  |  | See the section above. Examples: `/usr/local/bin/poll-something`. |
| `every` |  |  | See the section above. Examples: `60`. |
| `timeout` |  |  | See the section above. Examples: `600`. |


## `[backup]`

Backups: this file, the database (a SQLite file, whatever the engine, taken whole at one moment while callers are on) and the files' directories (file areas, image boards, FidoNet, screens, scripts), one archive (bbs-YYYYMMDD-HHMMSS.tar.gz) a backup.  bbs backup makes one now; bbs backup restore ARCHIVE puts one back.  See docs/wiki/Backups.md.

| key | default | live | what it does |
|---|---|---|---|
| `dir` |  | yes | where the archives go (by default backups beside an SQLite database; a server's database needs one said). Keep it on another disk if you can. Examples: `/var/backups/bbs`. |
| `at` |  | yes | daily, at this time (none: only when asked) Examples: `03:30`. |
| `keep` |  | yes | archives kept, the oldest taken away (0: all) Examples: `7`. |
| `files` |  | yes | the files' directories too (no: the configuration, database and SSH host keys alone) Examples: `yes`. |


## `[modemmenu]`

modemmenu's auth socket ([authd] there): the accounts modemmenu logs in (no password here) log in by telnet, SSH, the web ... with their modemmenu password, asked of modemmenu's backends.  See docs/wiki/Modem-Callers.md.

| key | default | live | what it does |
|---|---|---|---|
| `auth_socket` |  |  | See the section above. Examples: `/run/modemmenu/auth.sock`. |
| `create_users` |  |  | a user modemmenu knows and the BBS does not gets an account at their first login, any way in. Examples: `yes`. |
| `timeout` |  |  | seconds for its answer. Examples: `15`. |


## `[http]`

The HTTP API (REST, server-sent events, and WebSockets in JSON and protobuf): see docs/wiki/HTTP-API.md.  Tokens: POST /api/v1/login, or bbs token add USER -scope read,post.  Without this section there is none.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:8443`. |
| `cert_file` |  |  | TLS is the default; tls = no only on 127.0.0.1 behind a proxy that has it. Examples: `/etc/bbs/tls/cert.pem`. |
| `key_file` |  |  | TLS is the default; tls = no only on 127.0.0.1 behind a proxy that has it. Examples: `/etc/bbs/tls/key.pem`. |
| `trusted_proxies` |  |  | behind a reverse proxy (nginx, Caddy): its addresses (or ranges), which are believed when they say whose a request is (X-Forwarded-For, Forwarded, X-Real-IP), for rate limits, logs, bans and the deny lists. secure_cookies: logins' cookies over HTTPS only (the default with tls = yes; say yes when the proxy has TLS) Examples: `127.0.0.1, ::1`. |
| `secure_cookies` |  |  | behind a reverse proxy (nginx, Caddy): its addresses (or ranges), which are believed when they say whose a request is (X-Forwarded-For, Forwarded, X-Real-IP), for rate limits, logs, bans and the deny lists. secure_cookies: logins' cookies over HTTPS only (the default with tls = yes; say yes when the proxy has TLS) Examples: `yes`. |
| `anonymous` |  |  | anonymous callers may see /system and /who. Examples: `yes`. |
| `cors` |  |  | one browser origin allowed to call it (no *) Examples: `https://www.example.org`. |
| `metrics` |  |  | Prometheus metrics, on an address of their own (keep it private) Examples: `127.0.0.1:9100`. |
| `max_per_ip` |  |  | connections at once from one address. Examples: `20`. |
| `rate` |  |  | requests a second per token (or per IP without one), and the burst. Examples: `10`. |
| `burst` |  |  | requests a second per token (or per IP without one), and the burst. Examples: `20`. |
| `session_hours` |  |  | how long a /login session lasts. Examples: `24`. |
| `terminal` |  |  | the web terminal at /terminal: the BBS in a browser (xterm.js, built in) Examples: `yes`. |
| `forum` |  |  | the web forum at /forum/ (and /): the message areas as a bulletin board; forum_guests: those not logged in read the areas anyone may read. Examples: `yes`. |
| `forum_guests` |  | yes | the web forum at /forum/ (and /): the message areas as a bulletin board; forum_guests: those not logged in read the areas anyone may read. Examples: `yes`. |
| `forum_flags` |  | yes | forum_flags: posters' countries' flags by their names, where [general] geoip or phone_country knew them when they posted. Examples: `yes`. |
| `forum_theme` |  | yes | the forum's theme until a caller chooses (at the foot of each page): ipb3 (in the manner of Invision Power Board 3), ipb5 (fresh, as Invision Community 5), ipb4 (modern, as Invision Community 4), ipb2 (classic, as IPB 2), ipb1 (retro, as IPB 1), ikon (grey, as Ikonboard 2), vb6 (clean, as vBulletin 6), vb5 (connect, as vBulletin 5), vb4 (steel, as vBulletin 4), vb3 (blue, as vBulletin 3), vb2 (purple, as vBulletin 2), vblite (lavender, as vBulletin Lite), xf23 (light, as XenForo 2.3), xf2 (xen, as XenForo 2), xf1 (classic, as XenForo 1), mybb18 (flat, as MyBB 1.8), mybb16 (classic, as MyBB 1.6), mybb12 (original, as MyBB 1.2), pun14 (oxygen 2, as PunBB 1.4), flux15 (air, as FluxBB 1.5), pun12 (oxygen, as PunBB 1.2), yabb25 (silver, as YaBB 2.5), yabb2 (default, as YaBB 2), yabb1 (gold, as YaBB 1 Gold), smf30 (natural, as Simple Machines Forum 3.0), smf21 (simple, as SMF 2.1), smf20 (curve, as SMF 2.0), smf1 (core, as SMF 1), phpbb33, phpbb32, phpbb31 and phpbb30 (prosilver, as phpBB 3.3, 3.2, 3.1 and 3.0), phpbb2 (silver, as phpBB 2) or phpbb1 (dark, as phpBB 1) Examples: `ipb3`. |
| `chan` |  |  | the image boards at /chan/ (bbs chan board add b Random); see docs/wiki/Image-Boards.md. Examples: `yes`. |


## `[chan]`

How the image boards are run.

| key | default | live | what it does |
|---|---|---|---|
| `dir` |  |  | images and thumbnails, a directory a board; and the boards' secret. Examples: `chan`. |
| `max_file` |  | yes | the largest image (a board can have its own: bbs chan board set) Examples: `4M`. |
| `thread_wait` |  | yes | flood control: seconds between an address's threads, and its posts. Examples: `60`. |
| `reply_wait` |  | yes | flood control: seconds between an address's threads, and its posts. Examples: `15`. |
| `max_comment` |  | yes | the longest comment, in characters. Examples: `2000`. |
| `strip_metadata` |  | yes | JPEGs' EXIF (where a phone took the photo) taken out. Examples: `yes`. |
| `captcha` |  | yes | those not signed in answer a captcha (made here) to post; the BBS's users signed in at /chan/login, and callers on the terminals, do not. Examples: `yes`. |
| `captcha_length` |  | yes | its letters (4 to 10), how hard to read (easy, normal, hard), and the seconds one lasts. Examples: `6`. |
| `captcha_level` |  | yes | its letters (4 to 10), how hard to read (easy, normal, hard), and the seconds one lasts. Examples: `normal`. |
| `captcha_lifetime` |  | yes | its letters (4 to 10), how hard to read (easy, normal, hard), and the seconds one lasts. Examples: `600`. |
| `options` |  | yes | the post form's Options line (sage: no bump; nonoko); a board may say otherwise: bbs chan board set DIR options yes\|no\|default. Examples: `yes`. |
| `ffmpeg` |  | yes | files besides images, on the boards that take them (bbs chan board set DIR files webm,mp4,pdf; sound yes): the programs that make their thumbnails (a video's first frame, a PDF's first page; without them a card saying what the file is), and the longest a video may run, in seconds (0: any) Examples: `ffmpeg`. |
| `pdftoppm` |  | yes | files besides images, on the boards that take them (bbs chan board set DIR files webm,mp4,pdf; sound yes): the programs that make their thumbnails (a video's first frame, a PDF's first page; without them a card saying what the file is), and the longest a video may run, in seconds (0: any) Examples: `pdftoppm`. |
| `max_video` |  | yes | files besides images, on the boards that take them (bbs chan board set DIR files webm,mp4,pdf; sound yes): the programs that make their thumbnails (a video's first frame, a PDF's first page; without them a card saying what the file is), and the longest a video may run, in seconds (0: any) Examples: `120`. |
| `ruffle` |  | yes | Flash (files swf,flv), played in the page by Ruffle: its self-hosted package's directory (make ruffle fetches it into ruffle/) Examples: `/opt/bbs/ruffle`. |
| `archive_days` |  | yes | days a thread pruned off a board's last page stays in its archive (/chan/b/archive: read-only, its files kept; 0: always). A board can keep none: bbs chan board set DIR archive no. Examples: `3`. |
| `geoip` |  | yes | a country database for the boards with flags (bbs chan board set DIR flags yes): a MaxMind DB (.mmdb: GeoLite2 Country, DB-IP Lite...) or a CSV of ranges; read again when it changes. By default [general]'s. Examples: `/var/lib/bbs/GeoLite2-Country.mmdb`. |
| `geoip_credit` |  | yes | the credit its licence asks for, at the foot of each page (by default [general]'s) Examples: `IP geolocation by DB-IP`. |
| `geoip_credit_url` |  | yes | the credit its licence asks for, at the foot of each page (by default [general]'s) Examples: `https://db-ip.com`. |
| `style` |  | yes | the style callers get until they choose: by the board (yotsuba-b if it is worksafe, else yotsuba), or yotsuba, yotsuba-b, futaba, burichan, photon, tomorrow. |


## `[level NAME]`

An access level: a user's level gets what the [level] at or below it says.

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `name` |  | yes | shown in the profile and to the API. Examples: `New user`, `Member`. |
| `time` |  | yes | minutes online a day, and files downloaded a day (0: no limit) Examples: `30`, `120`. |
| `downloads` |  | yes | minutes online a day, and files downloaded a day (0: no limit) Examples: `2`, `20`. |
| `ratio` |  | yes | files a user may download for each they upload (0: no ratio), and how many before the ratio counts. Examples: `3`. |
| `ratio_free` |  | yes | files a user may download for each they upload (0: no ratio), and how many before the ratio counts. Examples: `10`. |
| `post` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `no`. |
| `mail` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `no`. |
| `chat` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `no`. |
| `page` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `yes`. |
| `download` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `yes`. |
| `upload` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `no`. |
| `doors` |  | yes | what it may do (yes unless it says no): post, mail, chat, page the sysop, download, upload, doors. Examples: `yes`. |


## `[ftp]`

The FTP server for the file areas: users log in with their BBS name and password; each file area is a directory.  bbs filearea add NAME makes one.  Without this section there is no FTP server.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:2121`. |
| `passive_ports` |  |  | passive data connections: open these in the firewall too. Examples: `50000-50100`. |
| `public_host` |  |  | behind NAT: the public IP address passive replies give. Examples: `203.0.113.7`. |
| `tls` |  |  | required: explicit FTPS (AUTH TLS before USER); implicit: TLS from the first byte; optional or no: passwords may cross in the clear. Examples: `required`. |
| `cert_file` |  |  | required: explicit FTPS (AUTH TLS before USER); implicit: TLS from the first byte; optional or no: passwords may cross in the clear. Examples: `/etc/bbs/cert.pem`. |
| `key_file` |  |  | required: explicit FTPS (AUTH TLS before USER); implicit: TLS from the first byte; optional or no: passwords may cross in the clear. Examples: `/etc/bbs/key.pem`. |
| `anonymous` |  |  | anonymous and ftp may log in to download. Examples: `no`. |
| `max_clients` |  |  | connections at once, and from one address; seconds idle before one is closed. Examples: `32`. |
| `max_per_ip` |  |  | connections at once, and from one address; seconds idle before one is closed. Examples: `4`. |
| `idle_timeout` |  |  | connections at once, and from one address; seconds idle before one is closed. Examples: `300`. |
| `advertise` |  |  | the address users are told, in the file areas. Examples: `bbs.example.org:2121`. |


## `[news]`

Newsgroups: a message area with a newsgroup name (bbs area newsgroup General bbs.general) is a newsgroup, served by NNTP to newsreaders and kept in step with peer news servers.  [news] names this site in Path and Message-IDs; it defaults to the machine's name.

| key | default | live | what it does |
|---|---|---|---|
| `hostname` |  |  | See the section above. Examples: `news.example.org`. |
| `domain` |  |  | See the section above. Examples: `news.example.org`. |
| `organization` |  |  | See the section above. Examples: `D-Modem BBS`. |
| `max_article` |  |  | the largest article taken (from readers and peers) Examples: `64K`. |


## `[nntp]`

The NNTP server.  Without this section there is none (peers still sync)

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:1119`. |
| `tls` |  |  | no, optional (STARTTLS), required (no AUTHINFO before STARTTLS), implicit (TLS from the first byte, NNTPS: usually port 563) Examples: `required`. |
| `cert_file` |  |  | no, optional (STARTTLS), required (no AUTHINFO before STARTTLS), implicit (TLS from the first byte, NNTPS: usually port 563) Examples: `/etc/bbs/cert.pem`. |
| `key_file` |  |  | no, optional (STARTTLS), required (no AUTHINFO before STARTTLS), implicit (TLS from the first byte, NNTPS: usually port 563) Examples: `/etc/bbs/key.pem`. |
| `read` |  |  | read = anyone, or users: AUTHINFO first. post = yes: logged-in users post. Examples: `anyone`. |
| `post` |  |  | read = anyone, or users: AUTHINFO first. post = yes: logged-in users post. Examples: `yes`. |
| `max_clients` |  |  | connections at once, and from one address; seconds idle before one is closed. Examples: `32`. |
| `max_per_ip` |  |  | connections at once, and from one address; seconds idle before one is closed. Examples: `8`. |
| `idle_timeout` |  |  | connections at once, and from one address; seconds idle before one is closed. Examples: `600`. |


## `[peer NAME]`

A peer news server, one section each.  pull: its new articles in our newsgroups are fetched (GROUP, OVER, ARTICLE); push: our new articles are offered (IHAVE, or POST if it refuses IHAVE).  accept_from: the addresses it may send us articles from by IHAVE (nothing else may)

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `news.example.net:119`. |
| `tls` |  |  | See the section above. Examples: `starttls`. |
| `ca_file` |  |  | See the section above. Examples: `/etc/bbs/peer-ca.pem`. |
| `username` |  |  | See the section above. Examples: `bbs`. |
| `password` |  |  | See the section above. Examples: `secret`. |
| `groups` |  |  | See the section above. Examples: `bbs.*,!bbs.test`. |
| `pull` |  |  | See the section above. Examples: `yes`. |
| `push` |  |  | See the section above. Examples: `yes`. |
| `interval` |  |  | See the section above. Examples: `600`. |
| `max_per_run` |  |  | See the section above. Examples: `1000`. |
| `initial` |  |  | on first contact, how many of each group's latest articles to fetch. Examples: `100`. |
| `accept_from` |  |  | the addresses it may send us articles from (IHAVE) Examples: `news.example.net`. |


## `[mail]`

E-mail.  Each user is name@domain (bbs mail address NAME shows it), and their BBS mail is their e-mail: Inbox, Sent, Drafts, Starred, Unread, All Mail and Trash, in any mail program, over POP3 and IMAP.  Without this section the BBS has no e-mail.

| key | default | live | what it does |
|---|---|---|---|
| `domain` |  |  | See the section above. Examples: `example.org`. |
| `hostname` |  |  | this host's name, said in SMTP's greeting and Received lines. Examples: `mail.example.org`. |
| `outbound` |  |  | how mail for elsewhere goes: direct (to each domain's MX, port 25), smarthost (through [smarthost]) or none (only mail here) Examples: `direct`. |
| `max_message` |  |  | the largest message taken; recipients in one; days in the trash before mail is gone (0: until emptied); days the queue keeps trying. Examples: `10M`. |
| `max_recipients` |  |  | the largest message taken; recipients in one; days in the trash before mail is gone (0: until emptied); days the queue keeps trying. Examples: `50`. |
| `trash_days` |  |  | the largest message taken; recipients in one; days in the trash before mail is gone (0: until emptied); days the queue keeps trying. Examples: `30`. |
| `queue_days` |  |  | the largest message taken; recipients in one; days in the trash before mail is gone (0: until emptied); days the queue keeps trying. Examples: `5`. |
| `postmaster` |  |  | who gets postmaster@, abuse@ and bounces' notes (default: the sysop) Examples: `sysop`. |
| `dkim_key_file` |  |  | DKIM: bbs mail dkim /etc/bbs/dkim.pem makes the key and prints its DNS record (selector._domainkey.domain) Examples: `/etc/bbs/dkim.pem`. |
| `dkim_selector` |  |  | DKIM: bbs mail dkim /etc/bbs/dkim.pem makes the key and prints its DNS record (selector._domainkey.domain) Examples: `bbs`. |
| `cert_file` |  |  | the certificate for the mail servers (each can name its own) Examples: `/etc/bbs/cert.pem`. |
| `key_file` |  |  | the certificate for the mail servers (each can name its own) Examples: `/etc/bbs/key.pem`. |


## `[smarthost]`

An outgoing mail server to send through, instead of straight to each domain's MX: your provider's, or a relay such as Postfix.  tls: starttls, implicit (port 465) or no.  insecure = yes: its certificate unchecked.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `smtp.example.net:587`. |
| `tls` |  |  | See the section above. Examples: `starttls`. |
| `username` |  |  | See the section above. Examples: `bbs@example.org`. |
| `password` |  |  | See the section above. Examples: `secret`. |
| `insecure` |  |  | See the section above. Examples: `no`. |


## `[pop3]`

The mail servers: each runs only with its section.  tls: no, optional (STARTTLS offered), required (STARTTLS before a login) or implicit (TLS from the first byte: POP3S 995, IMAPS 993, SMTPS 465).  Each takes address, tls, cert_file, key_file, max_clients, max_per_ip and idle_timeout, as [nntp] does.  POP3: mail programs fetch the inbox.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:1110`. |
| `tls` |  |  | See the section above. Examples: `required`. |


## `[imap]`

IMAP: mail programs read and file mail in the folders.  Its IDLE wants a long idle_timeout.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:1143`. |
| `tls` |  |  | See the section above. Examples: `required`. |
| `idle_timeout` |  |  | See the section above. Examples: `1800`. |


## `[smtp]`

mail submission: users send mail, logged in (port 587 or 465)

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:1587`. |
| `tls` |  |  | See the section above. Examples: `required`. |


## `[mx]`

mail from the internet: on port 25, named by the domain's MX record. checks = yes: SPF, DKIM and DMARC checked; mail is refused that fails a DMARC reject policy, or (no DMARC) fails SPF and is not signed.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:25`. |
| `tls` |  |  | See the section above. Examples: `optional`. |
| `checks` |  |  | See the section above. Examples: `yes`. |
| `dnsbl` |  |  | DNS blocklists the sending address is looked up in (none by default). A listed sender is refused at MAIL FROM (554, with the list's reason); a list that does not answer refuses no one. Spamhaus refuses queries through public resolvers (8.8.8.8, 1.1.1.1) and wants a paid feed for heavy or commercial use: run a resolver of your own (unbound) for it. Examples: `zen.spamhaus.org, bl.spamcop.net`. |


## `[lmtp]`

LMTP, for a mail server in front (Postfix's virtual_transport) to hand mail in: a unix socket's path or host:port.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `/run/bbs/lmtp.sock`. |


## `[fido]`

FidoNet (or another FTN network).  bbs area echo General FSX_GEN makes an area an echo; mail to Name@zone:net/node is netmail.  Without this section the BBS is not on FidoNet.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | our address, then any AKAs. Examples: `21:1/999`. |
| `origin` |  |  | the origin line under echomail written here. Examples: `D-Modem BBS, by phone and telnet`. |
| `dir` |  |  | inbound, outbound and bad packets go under it. Examples: `/var/lib/bbs/fido`. |
| `charset` |  |  | what messages are written in: cp437 (what FidoNet reads) or utf-8. Examples: `cp437`. |
| `max_message` |  |  | the largest message taken. Examples: `64K`. |
| `nodelist` |  |  | nodelists, to check netmail and look systems up: paths or patterns (the newest file of each), or area:NAME, the newest file in a file area. Examples: `/var/lib/bbs/fido/nodelist/NODELIST.* area:Nodelist`. |


## `[binkp]`

binkp: answer the links' calls.  Without it, links are only called.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:24554`. |
| `max_clients` |  |  | See the section above. Examples: `16`. |
| `max_per_ip` |  |  | See the section above. Examples: `4`. |


## `[link NAME]`

A system we exchange mail with, one section each.

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `21:1/100`. |
| `host` |  |  | to call it (port 24554 if none); without a host it calls us; nodelist: where the nodelist says. Examples: `hub.example.net:24554`. |
| `password` |  |  | the session password (CRAM-MD5); and the packets', up to 8 characters. Examples: `secret`. |
| `packet_password` |  |  | the session password (CRAM-MD5); and the packets', up to 8 characters. |
| `plain_password` |  |  | it may be sent in the clear to a mailer without CRAM-MD5. Examples: `no`. |
| `echoes` |  |  | the echoes exchanged with it: a wildmat of tags. Examples: `FSX_*`. |
| `route` |  |  | netmail for systems that are not links goes by it (one link at most) Examples: `yes`. |
| `fileechoes` |  |  | the file echoes (TIC) exchanged with it: a wildmat (none if unset); and the TICs' password, both ways. Examples: `FSX_*`. |
| `tic_password` |  |  | the file echoes (TIC) exchanged with it: a wildmat (none if unset); and the TICs' password, both ways. |
| `areafix` |  |  | it asks for echoes and file echoes by AreaFix and FileFix (of those echoes and fileechoes allow) with this password; we ask its with it too. Examples: `no`. |
| `areafix_password` |  |  | it asks for echoes and file echoes by AreaFix and FileFix (of those echoes and fileechoes allow) with this password; we ask its with it too. |
| `create` |  |  | make the echoes and file echoes it sends that are not here. Examples: `no`. |
| `packer` |  |  | how mail to it is bundled: zip, arc, arj, lha; none: bare packets. Examples: `zip`. |
| `poll` |  |  | call it every this many seconds, mail or not (0: only with mail) Examples: `3600`. |
| `aka` |  |  | which of our addresses it knows us by (default: ours in its zone) Examples: `21:1/999`. |


## `[qwknet NAME]`

QWK networks this BBS is a node of (DOVE-Net and the like), one section each.  The BBS calls the hub by FTP, puts a REP of what was written here in the network's areas, and takes the hub's packet.  A hub run by this BBS gives each node an account: bbs user qwknode NAME. See docs/wiki/Offline-Mail.md.

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | the hub's FTP server. Examples: `hub.example.org:21`. |
| `tls` |  |  | FTPS: explicit (AUTH TLS), implicit, or no. Examples: `explicit`. |
| `user` |  |  | the account the hub gave this node. Examples: `MYBBS`. |
| `password` |  |  | the account the hub gave this node. Examples: `secret`. |
| `dir` |  |  | where the hub keeps its packet ("/" for one in the root) Examples: `/qwk`. |
| `hub_id` |  |  | the hub's BBSID (by default: the packet found there) Examples: `VERT`. |
| `every` |  |  | call it every this many minutes (0: only with bbs qwknet call) Examples: `60`. |

Any other key is a conference carried here: CONFERENCE = AREA (the hub's conference number, this BBS's area).


## `[door NAME]`

A door: a program callers play, or another system's doors.

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `description` |  |  | See the section above. Examples: `Legend of the Red Dragon`. |
| `command` |  |  | See the section above. Examples: `docker run --rm -i --network none --name bbs-door{node} --user {uid}:{gid} -v /srv/doors:/doors -v {dir}:/node bbs-dosemu2 'F:\LORD' 'START.BAT {node}'`, `/usr/games/adventure`. |
| `dir` |  |  | the node's directory ({node} its number): mounted as /node above. Examples: `/var/lib/bbs/doors/node{node}`. |
| `drop_files` |  |  | the node's directory ({node} its number): mounted as /node above. Examples: `door.sys dorinfo1.def door32.sys chain.txt`. |
| `charset` |  |  | cp437: shown as UTF-8 to UTF-8 terminals; utf-8; raw: as it comes. Examples: `cp437`, `utf-8`. |
| `time_limit` |  |  | minutes a visit. Examples: `60`. |
| `sysop_only` |  |  | yes: for sysops only. Examples: `no`. |
| `single` |  |  | yes: one caller at a time (a door that is not multi-node) Examples: `no`. |
| `cleanup` |  |  | after the door: make sure its container is gone. Examples: `docker rm -f bbs-door{node}`. |
| `pty` |  |  | See the section above. Examples: `yes`. |
| `door32_comm` |  |  | See the section above. Examples: `local`. |
| `type` |  |  | See the section above. Examples: `telnet`, `rlogin`, `bbslink`, `doorparty`. |
| `address` |  |  | See the section above. Examples: `doors.example.org:2323`, `doors.example.org:513`. |
| `client_user` |  |  | the names it is given: the caller's ({user}) Examples: `{user}`. |
| `server_user` |  |  | the names it is given: the caller's ({user}) Examples: `{user}`. |
| `syscode` |  |  | See the section above. Examples: `XXXXXX`. |
| `authcode` |  |  | See the section above. Examples: `XXXXXX`. |
| `schemecode` |  |  | See the section above. Examples: `XXXXXX`. |
| `door` |  |  | See the section above. Examples: `lord`. |
| `url` |  |  | its web address, for the codes (by default http://games.bbslink.net) Examples: `http://games.bbslink.net`. |
| `ssh_user` |  |  | See the section above. Examples: `myuser`. |
| `ssh_password` |  |  | See the section above. Examples: `secret`. |
| `tag` |  |  | See the section above. Examples: `XB`. |
| `host_key` |  |  | See the section above. Examples: `SHA256:...`. |
| `rlogin_address` |  |  | its RLogin server, where the SSH session goes on to. Examples: `dp.throwbackbbs.com:513`. |


## `[irc]`

IRC: the chat rooms as channels (#main ...), for IRC clients.  Users log in with their BBS names and passwords (SASL PLAIN, or the server password as name:password).  See docs/wiki/IRC.md.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:6667`. |
| `tls_address` |  |  | IRC over TLS (and address = none for TLS only) Examples: `:6697`. |
| `tls_cert` |  |  | IRC over TLS (and address = none for TLS only) Examples: `/etc/bbs/tls.crt`. |
| `tls_key` |  |  | IRC over TLS (and address = none for TLS only) Examples: `/etc/bbs/tls.key`. |
| `server_name` |  |  | IRC over TLS (and address = none for TLS only) Examples: `irc.example.org`. |
| `network` |  |  | IRC over TLS (and address = none for TLS only) Examples: `DModem`. |
| `max_clients` |  |  | IRC over TLS (and address = none for TLS only) Examples: `64`. |
| `max_per_ip` |  |  | IRC over TLS (and address = none for TLS only) Examples: `4`. |
| `service` |  |  | the services bot's nick: /msg BBSServ HELP (your account's settings) Examples: `BBSServ`. |


## `[oscar]`

AIM and ICQ: the chat for AIM and ICQ clients (OSCAR), beside IRC.  Users sign on with their BBS names (or ICQ numbers: uin_base plus the account's number) and passwords; buddies are the BBS's users, online by any route; an IM is a private message (a PRIVMSG on IRC), a chat room a room (an IRC channel).  The port is AOL's 5190, which AOL's own clients will share. See docs/wiki/AIM-and-ICQ.md.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:5190`. |
| `host` |  |  | where clients are sent to reconnect, host:port (default: the address each one came to; set it behind NAT) Examples: `aim.example.org:5190`. |
| `max_clients` |  |  | where clients are sent to reconnect, host:port (default: the address each one came to; set it behind NAT) Examples: `256`. |
| `max_per_ip` |  |  | each client opens a few connections: sign-on, the BOS, chat rooms. Examples: `16`. |
| `uin_base` |  |  | an account's ICQ number is this plus its number. Examples: `100000`. |
| `system` |  |  | the screen name the BBS's notices come from. Examples: `OnlineHost`. |


## `[msn]`

MSN Messenger: the chat for MSN Messenger 1.0 to 4.7 and Windows Messenger 4.x (MSNP2 to 7), beside IRC and AIM.  Users sign in as name@domain with their BBS passwords; contacts are their friends list; an IM is a private message (a PRIVMSG on IRC), kept for one away.  The clients must be pointed at this host (see the wiki page). See docs/wiki/MSN-Messenger.md.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:1863`. |
| `host` |  |  | where clients are sent for conversations, host:port (default: the address each one came to; set it behind NAT) Examples: `msn.example.org:1863`. |
| `domain` |  |  | handles are name@domain (default bbs.invalid); clients may sign in with any domain, as MSN 1.0's @hotmail.com. Examples: `msn.example.org`. |
| `max_clients` |  |  | handles are name@domain (default bbs.invalid); clients may sign in with any domain, as MSN 1.0's @hotmail.com. Examples: `256`. |
| `max_per_ip` |  |  | each conversation is a connection of its own. Examples: `16`. |
| `system` |  |  | the name the BBS's notices come from. Examples: `BBS`. |
| `site` |  |  | a web address the clients' links (inbox, profiles) open. Examples: `https://bbs.example.org/`. |


## `[mrc]`

Multi Relay Chat: the BBSes' chat network.  Its rooms are the chat rooms beginning mrc- here (mrc-lobby is its lobby), on terminals and IRC alike. See docs/wiki/MRC.md.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | or TLS, on 5001. Examples: `mrc.bottomlessabyss.net:5000`, `mrc.bottomlessabyss.net:5001`. |
| `tls` |  |  | or TLS, on 5001. Examples: `yes`. |
| `prefix` |  |  | or TLS, on 5001. Examples: `mrc-`. |
| `system_name` |  |  | or TLS, on 5001. Examples: `\|11My \|03BBS`. |
| `web` |  |  | or TLS, on 5001. Examples: `https://bbs.example.org/`. |
| `telnet` |  |  | or TLS, on 5001. Examples: `bbs.example.org:23`. |
| `ssh` |  |  | or TLS, on 5001. Examples: `bbs.example.org:22`. |
| `require` |  |  | or TLS, on 5001. Examples: `20`. |


## `[finger]`

finger: who is on ("finger @host"), and who a user is ("finger name@host"). Port 79 needs CAP_NET_BIND_SERVICE (bbs.service.sample)

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:79`. |
| `online` |  |  | See the section above. Examples: `yes`. |
| `users` |  |  | See the section above. Examples: `yes`. |


## `[gopher]`

gopher: about the system, who is on, and what areas the sysop opens: no login, so anyone sees them.  Port 70 needs CAP_NET_BIND_SERVICE.

| key | default | live | what it does |
|---|---|---|---|
| `address` |  |  | See the section above. Examples: `:70`. |
| `hostname` |  |  | the name and port menus send clients back to. Examples: `bbs.example.org`. |
| `port` |  |  | the name and port menus send clients back to. Examples: `70`. |
| `areas` |  |  | message areas shown (a wildmat of area names; "": none) Examples: `general,announcements`. |
| `files` |  |  | file areas listed and downloadable. Examples: `no`. |


## `[menu NAME]`

A menu of the sysop's: [menu main] takes the place of the built-in main menu; others are reached from it (menu:NAME).

There may be any number of these, each with its own NAME.

| key | default | live | what it does |
|---|---|---|---|
| `title` |  | yes | See the section above. Examples: `{cyan}{bright}@BBS@{reset}`, `{cyan}Extras{reset}`. |
| `screen` |  | yes | See the section above. Examples: `mainmenu`. |
| `prompt` |  | yes | See the section above. Examples: `{cyan}Extras{default}> `. |
| `require` |  | yes | The access a caller needs to be taken into the menu (a level and flags; see docs/wiki/Access-Levels.md). |

Any other key is a menu item: KEY = ACTION Label, KEY one character; ACTION areas, mail, who, users, profile, info, files, boards, doors, chat, page, callers, feedback, qwk, sysop, terminal, back, logoff, or door:NAME menu:NAME screen:NAME script:NAME area:NAME; \|REQUIRE after the label limits it.


## `[strings]`

The prompts and messages, with @-codes and tags.

| key | default | live | what it does |
|---|---|---|---|
| `name_prompt` |  | yes | See the section above. Examples: `{cyan}Name:{default} `. |
| `new_name_prompt` |  | yes | See the section above. Examples: `{cyan}Name{default} (or NEW): `. |
| `password_prompt` |  | yes | See the section above. Examples: `{cyan}Password:{default} `. |
| `command_prompt` |  | yes | See the section above. Examples: `{yellow}@USER@{default} @ {cyan}@BBS@{default} > `. |
| `pause` |  | yes | See the section above. Examples: `{blue}[Press a key]{reset}`. |
| `logoff_confirm` |  | yes | See the section above. Examples: `{red}Log off?{reset}`. |
| `welcome` |  | yes | See the section above. Examples: `Welcome, @USER@.`. |

**See also:** [Configuration](Configuration)
