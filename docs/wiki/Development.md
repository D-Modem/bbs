## Layout

| | |
|---|---|
| `cmd/bbs` | the command: serve, check, init, user and area administration |
| `internal/server` | listeners, session headers, rlogin, SSH, telling 3270s from the rest |
| `internal/telnet` | the server side of telnet: options, terminal type, window size, the handoff to go3270 |
| `internal/view` | what the screens show, described once: menus, tables, messages, forms, panels, drafts |
| `internal/script` | the sysop's JavaScript (goja): screens of internal/view, the BBS as the caller, data kept, the time a script may compute |
| `internal/guard` | deny lists, bans, the name filter, the hack log |
| `internal/events` | the sysop's scheduled events |
| `internal/app` | the screens after the login, for every front end: menus, areas, mail, files, who's online, the profile, validation, paging the sysop |
| `internal/textui` | the text front end: the login, line editing, ANSI detection, paging, RIP menus; the screens as prompts and keys; chat, doors, transfers |
| `internal/ui3270` | the 3270 front end: the logon, the screens as fields and PF keys on go3270; chat, IND$FILE, its graphics |
| `internal/sixel` | the sixel encoder |
| `internal/petscii` | the Commodore's character set: translating ANSI and UTF-8 out, keys in |
| `internal/rip` | RIPscrip: MegaNums, commands, menu scenes |
| `internal/vt` | terminal identification (DA1, DA2, XTVERSION, CTerm, answerback, VT52); DEC graphics and VT52 output |
| `internal/files` | file areas' storage: names, uploads, limits |
| `internal/ftp` | the FTP server: the file areas as a file system |
| `internal/news` | articles: parsing, checking, making, decoding; wildmats |
| `internal/nntp` | the NNTP server and the peers' client |
| `internal/qwk` | QWK packets and REPs: MESSAGES.DAT's blocks, CONTROL.DAT, indexes (MBF floats), QWKE kludges, CP437 |
| `internal/ftn` | FidoNet's formats: addresses, Type 2+ packets, messages, kludges, character sets |
| `internal/fido` | the BBS on FidoNet: tosser, scanner, netmail, the binkp mailer |
| `internal/door` | doors: drop files, running a door on the caller's line |
| `doors/dosemu2` | the Docker image DOS doors run in: dosemu2, its FOSSIL on COM1 |
| `internal/finger` | the finger server |
| `internal/gopher` | the gopher server |
| `internal/api` | the HTTP API: REST, server-sent events, WebSockets, metrics, OpenAPI; the web terminal page (`web/`: xterm.js, the page) |
| `proto` | the WebSocket protocol, `bbs.v1` (`make proto` regenerates `internal/api/pb`) |
| `internal/tcpserv` | what finger and gopher share: limits, timeouts |
| `internal/screens` | the sysop's display files: finding them, @-codes, 3270 color tags |
| `internal/xfer` | XMODEM, YMODEM, ZMODEM and Kermit, both ways |
| `internal/logo` | the logo: a PNG, or the name in a pixel font |
| `internal/bbs` | the BBS itself: nodes, logins, new users, posting, cleaning text |
| `internal/store` | the database: SQLite, PostgreSQL, MySQL or MariaDB (`dialect.go` what differs, `schema.go` the tables), copying between them |
| `internal/config` | the configuration file |

## Screens

The screens after the login are written once, in `internal/app`, and
drawn by each front end.  A screen is a description in
`internal/view`'s terms, handed to the front end, which shows it, waits
for the caller and says what they chose:

| view | text terminal | 3270 |
|---|---|---|
| `Menu` | `[K] Label` in two columns (one under 60 columns), RIP buttons, the command prompt | a box of keys, `Option ===>` |
| `Table` | numbered rows a page at a time; `Area number, [N]ew scan, [Q]uit: ` | `S` by a row; PF keys; PF7/PF8 |
| `Message` | headers, the body paged; `[N]ext, [R]eply, ...` | headers, the body scrolled; PF10/PF11, PF4 ... |
| `Form` | a question a field | one screen of fields |
| `Panel` | lines, then `[L]ocation, [P]assword, ...` | lines, PF keys |
| `Draft` | `To`, `Subject`, the full-screen editor (ANSI) or the line editor | the editor screen |
| `Confirm`, `Say`, `Show` | `[Y/n]`, a line, lines and a pause | a Y/N field, the message line, a page |

An action has a key for text terminals and a PF key for 3270s (and a
short label for the 3270's key line).  What a front end alone can do is
its `app.Frontend`: the sysop's display files, chat, doors (text), file
transfers (protocols on text, IND$FILE on a 3270), the terminal's line
on the profile and what may be switched there, the goodbye.  The logins
stay with the front ends, since they decide what the terminal is.

A new screen is a function in `internal/app` building views; a new
front end implements `app.Frontend`.  `internal/app`'s tests drive the
screens with a scripted front end, no terminal at all.

## Tests

With `BBS_TEST_DATABASE` set to a PostgreSQL, MySQL or MariaDB server, every test's
store is a database of its own there (a schema on PostgreSQL, a database
on MySQL, dropped after), so the whole suite runs on that engine:

```
BBS_TEST_DATABASE='postgres://bbs:PASSWORD@127.0.0.1/bbs?sslmode=disable' go test ./...
BBS_TEST_DATABASE='mysql://bbs:PASSWORD@127.0.0.1:3306/bbs' go test ./...
BBS_TEST_DATABASE='mariadb://bbs:PASSWORD@127.0.0.1:3306/bbs' go test ./...
```

(the user may `CREATE SCHEMA`, or `CREATE DATABASE`).  The store's own
tests check what the engines do differently: names in any case, ids,
upserts, booleans, bytes, long text, cascades, copying from SQLite.

`make test` runs the unit tests (text cleaning, names and passwords, the
configuration, telnet negotiation and the 3270 handoff) and whole sessions
against a running server on unix sockets: a modem caller with a trusted
modemmenu header, one whose header is not trusted, a login and a post, a
new user, rlogin with a right and a wrong password and trusted, throttling
by caller ID, all lines busy, a telnet ANSI client, a TN3270 client
taken through go3270's negotiation to the logon screen and out with PF3,
the transfer protocols against lrzsz (`sx`/`rx`, `sb`/`rb`, `sz`/`rz`) and
C-Kermit both ways, and ZMODEM over a line that damages bytes; uploads and
downloads through the BBS by ZMODEM, Kermit, YMODEM and XMODEM-1K;
IND$FILE with a real s3270 (upload, download, a file that is not there);
the file store (names, limits, the daily allowance, sysop-only areas),
the FTP server with a real FTP client (uploads, downloads and their
count, every refused operation, path traversal, wrong logins, anonymous
access, TLS required and explicit TLS, the size limit, the per-address
limit), telnet terminals with and without sixel, the sixel encoder checked by
decoding its output with ImageMagick (when installed), and TN3270
clients that report programmed symbols (the logo is loaded and
shown), vector graphics (the logo is drawn) or neither (nothing is sent),
articles (parsing, limits, decoding, wildmats), the NNTP server with
Python's nntplib as a newsreader (login, reading, overview, posting), its
refusals (posting without a login, AUTHINFO before STARTTLS, forged
headers, control messages, oversized articles, IHAVE from a stranger), two
BBSes peered both ways, and IHAVE.  With `BBS_TEST_INN=host:port` it also
syncs with a running INN.  FidoNet: packets and messages, a hub with two
nodes exchanging echomail by binkp (SEEN-BY, PATH, threads, forwarding),
netmail and replies, wrong passwords, unknown addresses and impostors,
dupes, loops, bad and foreign packets, the echo-newsgroup gateway, and,
when binkd and CrashMail are installed, a hub run by them: echomail and
netmail both ways, calling it and called by it.  Doors: the drop files,
the runner (CP437 and UTF-8, time limit, hanging up, idle, single-caller
doors, a pseudo-terminal), a caller opening a door from the menu, and,
when the bbs-dosemu2 image is built, a real DOS door talking by FOSSIL in
it.  Chat: rooms, history, private rooms, flooding, node messages, paging
and answering, the page command (and that it runs no shell); two callers
chatting and paging on the text BBS; a 3270 (s3270) chatting with a text
caller and getting a node message on its message line.  Finger and
gopher: their answers and refusals, with the finger program and curl as
clients.  Customization: finding display files by terminal, SAUCE,
@-codes and widths, color tags; menus and strings in the configuration
and what it refuses; a text caller through an ANSI logon (CP437 as it
is), the strings, a welcome screen, the sysop's menus and submenus, a
screen with a pause and a goodbye screen; and a 3270 (s3270) through a
colored logon screen, welcome, the sysop's main menu and a screen.
The shared screens: `internal/app` driven by a scripted front end
(posting, reading on and back, a new scan and reply, a number not on the
page, mail to no one and to the sysop, a node message, deleting, the
profile, a password that differs, validating with a bad level first);
the column layout; the text prompts' keys; and a 3270 (s3270) through
the areas, a post from the editor, reading it, the profile, with a text
caller reading the 3270's post.
The full-screen editor: its text (wrapping as typed and as given, a word
too long for a line, joining lines by Backspace and Delete, moving over
line ends, quoting, the line limit), and an SSH caller writing a post
with the cursor keys, replying with lines from the quote window and
`/S`, then choosing the line editor; the message base (threads from any
message, votes, polls and their answers, aliases (taken names refused),
signatures, groups, keeping by count and by age; through internal/app and
the API); the sysop's tools (a sysop watching
a caller's screen as it is sent, calling them into chat at their next
menu, being away when they page; the user editor, statistics, bans, the
hack log and events through internal/app; events run, failing, timed
out and not run twice; their configuration and the API); the last callers, feedback to the
sysop, a user's details (more of them to a sysop) and mail from there, a
line about oneself, expert menus and `?` on a text terminal; keeping callers out (the deny file
read again as it changes, networks and IPv6, bans after the throttle and
their end, name patterns, the hack log's lines; telnet over TLS with a
login, a plain client on the TLS port getting nothing, a denied address
closed at once, the API's bans and log); scripts (screens, data kept for all
and for a user, posting with refusals caught, a runaway loop stopped and a
slow caller not, failures told to the sysop alone, a hang-up the script
cannot catch, exit, the sample poll, the configuration's checks, and the
sample one-liners on a text terminal and on a 3270 by s3270); the file base (FILE_ID.DIZ in CP437
with colors, a ZIP's and a tar.gz's listings, new files, finding by the
DIZ's words, tagging by numbers, a batch of two by YMODEM, ratios and the day's limit together, the API's
search and listings); QWK offline mail (packets and REPs read back
as a reader would, long subjects as QWKE kludges, CP437, another BBS's REP
refused, the indexes' MBF floats; a packet's messages read only once it
has gone, the REP's replies posted and mailed as the caller, a REP twice
posting once; by ZMODEM on a text line, by IND$FILE from s3270, by FTP
and the API); the new scan's areas, messages to
you and finding (the scan kept, LIKE's wildcards taken as text, areas
not read left out), through internal/app and in the store on every
engine; settings kept from call to call
(UTF-8 off over SSH, a Commodore's width) and through the API.
The HTTP API: logins, tokens and scopes, every REST endpoint and its
refusals, rate limits, the OpenAPI document against the routes, server-sent
events and node messages, both WebSockets (auth by header and by first
message, chat between them, paging and answering, mail events, bad
frames, refused origins and tokens) and the metrics.  And a terminal caller and
a web caller (the JSON WebSocket) together: chatting in one room both
ways, `/who`, node messages each way, and the web caller paging the
sysop, who answers at the terminal.  SSH: logging in by password,
keyboard-interactive and key (and not by a wrong one, someone else's, or
into a locked account), `new` to the BBS's prompt, the pty's terminal
type and size, refused commands, SFTP and forwarding, a user adding and
deleting a key in their profile, host keys made and kept (ed25519, RSA,
ECDSA) and refused when readable, the configuration, legacy ciphers only
when asked for, and the OpenSSH client logging in by key with a pty.
PETSCII: the translation (letters, colors, reverse, CR and LF, cursor
movement, box drawing, accents, runes split across writes, wrapping) and
the keys back; a Commodore logging in with DEL editing, its one column
menus, a .seq screen as it is, the profile's width, no escape ever sent;
a Commodore found while logging in: by DEL typed in the name, by a shifted letter (and not by UTF-8), by a name in capitals and the DEL question (or not, with a backspace), and no question for a name in lower case.  RIP: MegaNums, the
commands and their escaping, the menu scene; a RIP terminal found by its
answer, given a .rip screen, menus as buttons, the scene cleared on a
choice and a pause before the next; an ANSI terminal without RIP given
none of it.  Access levels: requirements read and written back, met and
not (flags, !flags, sysop, locked users), flag edits; levels found for a
user; areas hidden and posting refused by level and by flags, mail only to
the sysop, chat and paging refused, the day's downloads counted and
limited, doors, validation; the day's time running out (warned, hung up,
counted); a new user waiting and validated by the sysop on the BBS; the
API (no user raises their own level, levels and flags seen only by
sysops and the user, area requirements); the configuration.  Terminal identification: the answers of DEC's terminals and
of emulators (VT100 to VT525, xterm old and new, VTE, tmux, SyncTERM,
PuTTY, the Linux console, a VT52) read into names, levels, sixel and
monochrome; DEC Special Graphics and Latin-1 output, escapes passing
through it; VT52 output; a VT220 given DEC graphics, Latin-1 and bold,
shown as a VT220; a VT52 given its escapes and its cursor keys ignored;
a terminal answering nothing left plain.  The web terminal: the page, its
headers and files (and no others), WebSockets refused from another
origin or without the subprotocol, and a caller logging in through it,
its size taken, shown as a web terminal, logging off.  Updating xterm.js:
`npm pack @xterm/xterm @xterm/addon-fit`, and copy `lib/xterm.js`,
`css/xterm.css`, `lib/addon-fit.js` and the license into
`internal/api/web` (without their source map lines).

**See also:** [Building](Building)
