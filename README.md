# bbs: a bulletin board for telnet, TN3270 and modem callers

`bbs` is a bulletin board system in Go.  One telnet port serves both kinds
of terminal: a TN3270 client (x3270, c3270, wc3270, Vista, PCOMM) gets
full 3270 screens with PF keys, through
[go3270](https://github.com/racingmars/go3270); any other telnet client
gets a text BBS with ANSI color where the terminal has it.  Modem callers
come in through the D-Modem suite: [app_dmodem](https://github.com/Neo-Desktop/D-Modem-RE)
straight from Asterisk, or [modemmenu](https://github.com/Neo-Desktop/PBX-Modem)
after its login.

```
D-Modem BBS
Node 2, modem at 26400 bit/s

Name (or NEW): alice
Password: **********

Welcome, alice.

Main menu
  [M] Message areas         [E] E-mail (1 new)
  [W] Who's online          [U] User list
  [P] Your profile          [I] System info
  [G] Goodbye
```

The same caller on a 3270:

```
 D-Modem BBS                                    Main menu                 Node 1
   Welcome, alice.
     1  Message areas
     2  E-mail   (1 new)
     3  Who's online
     4  User list
     5  Your profile
     6  System info
     X  Log off
   Option ===>
 Enter=Select  PF3=Log off
```

What there is:

- message areas: read new, list, read by number, post, reply (quoting),
  reply by mail; areas only the sysop posts in; authors and the sysop
  delete messages
- private mail: inbox, read, reply, delete
- who's online (and how each caller came: telnet, tn3270, rlogin, modem
  with its rate), the user list, system info
- your profile: where you call from, your password, ANSI and UTF-8
- file areas: files listed with their size, uploader, downloads and
  SHA-256, moved by XMODEM, YMODEM, ZMODEM or Kermit on the text BBS, by
  IND$FILE on a 3270, or by FTP (FTPS); deleted by their uploader or the
  sysop
- newsgroups: message areas given a newsgroup name are served by NNTP to
  newsreaders and exchanged with other news servers (INN and the like)
- FidoNet (or any FTN network): message areas that are echoes, netmail
  as private mail, and a binkp mailer to exchange packets with links
- doors: DOS door games in dosemu2 (in Docker), with a FOSSIL on COM1
  and the usual drop files, or any Linux program, for text callers
- new users sign themselves up (or not: `new_users = no`)
- SQLite storage, one file

## Building

Go 1.24 or later:

```
make            # or: go build ./cmd/bbs
make test
make install    # /usr/local/bin/bbs, and /etc/bbs/bbs.conf if there is none
```

There is no cgo: SQLite is [modernc.org/sqlite](https://modernc.org/sqlite).

go3270 is replaced (in `go.mod`) by the
[go3270-sec](https://github.com/Neo-Desktop/go3270-sec) fork, which adds
the query replies and graphics.  While that repository is private, Go
needs to fetch it from GitHub itself, with your credentials:

```
export GOPRIVATE=github.com/Neo-Desktop/*     # the Makefile sets this
git config --global url."git@github.com:".insteadOf "https://github.com/"   # or a token
```

## Starting

```
bbs -config /etc/bbs/bbs.conf check     # the configuration, its warnings
bbs -config /etc/bbs/bbs.conf init      # the database, with two areas
bbs -config /etc/bbs/bbs.conf user add sysop -sysop
bbs -config /etc/bbs/bbs.conf serve
```

[bbs.service.sample](bbs.service.sample) runs it under systemd as its own
user, with the database in `/var/lib/bbs` and sockets in `/run/bbs`.

Without `-config` it runs with the defaults: telnet and TN3270 on port
2300, modem callers raw on 127.0.0.1:2323, the database `bbs.db` here.

### Looking after it

```
bbs user list
bbs user add NAME [-sysop]          # asks for the password (or reads it from a pipe)
bbs user passwd NAME
bbs user lock NAME                  # and unlock
bbs user sysop NAME                 # and unsysop
bbs user del NAME                   # their mail goes; their messages stay
bbs area list
bbs area add NAME "DESCRIPTION" [-sysop-only]
bbs area del NAME                   # and its messages
```

A sysop also adds areas from the BBS (`+` in the message areas) and
deletes any message.

## Configuration

[bbs.conf.sample](bbs.conf.sample) has every option.  `[general]` is the
system (name, database, log, banner, message of the day, timeouts, limits);
each `[listen NAME]` is a port or unix socket:

| `type` | what comes in |
|---|---|
| `telnet` | a telnet client; with `tn3270 = yes` (the default) 3270s get 3270 screens |
| `raw` | bytes as they come: app_dmodem's `mode = tcp`, a modemmenu `tcp` or `unix` item |
| `rlogin` | RFC 1282, as modemmenu's `rlogin` items speak it |

`address` is `host:port`, or a path for a unix socket (`socket_mode`,
`socket_group`).  Any listener may read a session header first (`header =
yes`), and with `trust_user = yes` take the caller as logged in already;
see below.

## Modem callers

### Straight from app_dmodem

app_dmodem's `[bbs]` profile hands each call to a TCP server, raw.  The
`[listen modem]` of the sample is that server:

```
; dmodem.conf
[bbs]
mode = tcp
host = 127.0.0.1
port = 2323

; bbs.conf
[listen modem]
type = raw
address = 127.0.0.1:2323
```

The caller logs in to the BBS (or signs up).  Over a raw line the BBS
asks the terminal for its cursor position: an ANSI terminal (SyncTERM,
Telix, Qmodem, HyperTerminal in ANSI mode) answers, and the answer gives
its size too; anything else gets plain text.  Text is 7-bit ASCII unless
the caller turns UTF-8 on.  An app_dmodem that sends a session header
(`header = yes`) gives the BBS the call's rate and caller ID: set `header
= yes` on the listener too.

### Behind modemmenu

modemmenu logs the caller in first and offers the BBS in its menu.  Three
ways to hand the caller over, from the simplest to the closest:

**rlogin**, modemmenu's default for a BBS: it sends the caller's password
as the rlogin client user and the name as the server user (Synchronet's
convention).  The BBS checks them against its own accounts; if they are
not right there (a user with no account here, or another password) the
caller is asked to log in, with the name typed in already.

```
; modemmenu.conf
[item bbs]
key = B
label = Bulletin board
mode = rlogin
host = 127.0.0.1
port = 2513
after = menu

; bbs.conf
[listen rlogin]
type = rlogin
address = 127.0.0.1:2513
```

**A unix socket with the session header**: modemmenu has checked the
caller, and the BBS believes it (`trust_user`).  Callers need no account
of their own here first: `create_users = yes` makes one for each new name
(it has no password; it logs in only this way).  The header's rate,
caller ID and number show in the log and who's online.

```
; modemmenu.conf
[item bbs]
key = B
label = Bulletin board
mode = unix
path = /run/bbs/menu.sock
header = yes
pass_password = no
after = menu

; bbs.conf
[listen menu]
type = raw
address = /run/bbs/menu.sock
header = yes
trust_user = yes
create_users = yes
socket_mode = 0660
socket_group = asterisk
```

`trust_user` believes whoever can connect: keep the socket to modemmenu's
user (Asterisk's group, mode 0660, or 0600 and the same user).  `bbs
check` warns about a trusting socket or port that others can reach.

**telnet** works too (`mode = telnet`, `port = 2300`): modemmenu's telnet
client says it is an `ANSI` terminal, and the caller logs in again.

## TN3270

A 3270 client connects to the telnet listener like any other.  The BBS
asks every client for its terminal type; one that says `IBM-3278-*`,
`IBM-3279-*`, `IBM-3277-*` or `IBM-DYNAMIC` is turned over to go3270,
which negotiates binary and end-of-record, finds the screen size (24x80
up to the model 5's 27x132, or what IBM-DYNAMIC says) and the code page,
and the caller gets the 3270 screens:

| key | |
|---|---|
| Enter | log on, select (type `S` by a line), save |
| PF3 | back, log off |
| PF5 | new user (at the logon); read new (in an area); send (in the editor) |
| PF6 | post (in an area), new mail (in the mail) |
| PF7 / PF8 | up / down a page |
| PF4 | reply; PF5 in a message: reply by mail |
| PF9 | delete |
| PF10 / PF11 | previous / next message |

```
c3270 localhost:2300
x3270 -model 3279-4 localhost:2300
```

`tn3270 = no` on a listener sends 3270s the text BBS (which they cannot
show).

## Files and FTP

File areas hold files; callers browse them from the BBS (*F* in the text
menu, *8* on a 3270) and move them with an FTP client, logging in with
their BBS name and password: each area is a directory.

```
; bbs.conf
[general]
files_dir = /var/lib/bbs/files
max_file_size = 64M
upload_quota = 256M        ; a user's uploads a day; sysops have none

[ftp]
address = :2121
passive_ports = 50000-50100
public_host = 203.0.113.7  ; behind NAT: the address passive replies give
tls = required             ; explicit FTPS: AUTH TLS before the login
cert_file = /etc/bbs/cert.pem
key_file = /etc/bbs/key.pem
advertise = bbs.example.org:2121
```

```
bbs filearea add Games "DOS games"
bbs filearea add Official "From the sysop" -sysop-upload
bbs file add Games ./doom.zip "Shareware Doom"
bbs file list Games
```

The FTP side is [ftpserverlib](https://github.com/fclairamb/ftpserverlib)
with a file system of the BBS's own: `/` lists the areas, an area its
files, and nothing of the server's disk is reachable.  Files are stored
under their record's number (`files_dir/1`, `files_dir/2`, ...), so no name
a user gives becomes a path.  Users may:

- download anything (resuming too);
- upload a new file into an area (not a sysop-only one), whose name is
  letters, digits and a few marks (no dots in front, no slashes, nothing a
  shell or Windows minds), up to `max_file_size` and what is left of
  `upload_quota`; an upload is written to a temporary file, hashed, and
  only then recorded and put in place;
- delete their own files (sysops any).

Everything else (overwriting, appending, resuming uploads, renaming,
directories, SITE, MFMT, CHMOD) is refused.  Active mode is off (FTP
bounce), data connections must come from the control connection's
address, and the login is the BBS's, with its failure delay and its
throttling by address.  `tls = required` (the default) refuses `USER`
before TLS, so no password crosses in the clear; `implicit` is TLS from
the first byte; `optional` and `no` are there for old clients, and `bbs
check` warns of them.  `anonymous = yes` lets `anonymous` and `ftp` in to
download only.

Open the FTP port and the passive range in the firewall.  An FTP session
is not a node: it does not show in who's online or count against
`max_nodes`; `[ftp] max_clients` and `max_per_ip` limit it.

## File transfers on the BBS

Besides FTP, files move over the caller's own line.

**Text callers** (telnet, modem, rlogin) pick a file's details and press
*D* to download, or *U* in an area to upload, then a protocol:

| key | protocol | |
|---|---|---|
| X | XMODEM | CRC, or checksum if the terminal program asks; 128-byte blocks |
| 1 | XMODEM-1K | 1K blocks |
| Y | YMODEM | batch: names and exact sizes |
| Z | ZMODEM | batch, CRC-32, error recovery; most terminal programs start it by themselves |
| K | Kermit | for 7-bit lines and anything else that fails |

XMODEM sends no name, so the BBS asks for one.  Names a terminal program
sends lose any path (`C:\DOS\GAME.ZIP` becomes `GAME.ZIP`) and characters
the file areas do not take; an upload goes through the same checks, size
limit and daily allowance as one by FTP, and is kept only if it all came.
X/Y/ZMODEM need an 8-bit clean line: a modem, rlogin, or telnet in binary
mode (the BBS asks for it; a client that refuses is warned).

**3270 callers** use their emulator's file transfer (IND$FILE): in a file
area (8 on the main menu, then S by an area), with the cursor on the
command line, start a transfer: *send* to upload into the area, *receive*
to download a file of it, the host file name being the file's name.  The
emulator types `IND$FILE PUT name` or `IND$FILE GET name` and the BBS plays
the host's part in DFT mode (structured fields), as TSO's IND$FILE does.
Binary mode keeps a file as it is; ASCII with CR/LF stores text with CR LF
line ends and gives it back as the PC wants it.  TSO or VM/CMS host type
both work.  The emulator must do DFT (x3270, c3270, wc3270 and PCOMM do;
*Your terminal* shows reply 95, Distributed Data Management).

### Sixel graphics

Telnet callers whose terminal has sixel graphics (xterm `-ti vt340`,
mlterm, foot, WezTerm, Windows Terminal, iTerm2, SyncTERM, ...) see the
logo as a picture above the system's name: `logo = /etc/bbs/logo.png`, or
without one the name drawn in a pixel font.  The BBS asks every ANSI
terminal for its device attributes (`CSI c`; sixel is attribute 4) and its
character cell in pixels (`CSI 16 t`, to fit the logo to the window), so
nothing changes for terminals without sixel.  A caller turns it off (or on,
if their terminal has it) in their profile.

`sixel = auto` is the default for telnet listeners and `no` for raw and
rlogin ones: a picture is slow over a modem.

### 3270 graphics

The BBS reads everything the terminal says of itself when it connects (its
query replies) and uses what it has:

| | terminals | used for |
|---|---|---|
| graphic escape (code page 310) | nearly all: x3270, c3270, wc3270, PCOMM, Vista | the box around the main menu, rules, bar charts in System info; ASCII otherwise |
| programmed symbols | IBM PCOMM, HostExplorer, Quick3270 | the logo on the logon screen: the system's name in a pixel font, or `tn3270_logo` (a PNG), cut into character cells and loaded as a symbol set |
| vector graphics | PCOMM (host graphics, native protocol), HostExplorer | the logo as a bitmap image instead, with `tn3270_vector = yes`; experimental |

Option 7 of the 3270 main menu, *Your terminal*, shows what the terminal
reported: code page, cell size, graphic escape, its symbol sets and Load PS
formats, vector graphics, and every query reply in hex.  The log has a line
of it for each 3270 that connects.  It is the place to start when an
emulator does not show the logo.

In PCOMM, programmed symbols and vector graphics need host graphics turned
on in the session's configuration, with the native protocol for vector
graphics (see PCOMM's *3270 Emulator User's Reference*, "Host Graphics").  The formats are from IBM's
[3270 Data Stream Programmer's Reference, GA23-0059-07](https://bitsavers.org/pdf/ibm/3174/GA23-0059-07_3270_Data_Stream_Programmers_Reference_199206.pdf)
and Greg Price's [3270 Graphics](https://www.prycroft6.com.au/misc/3270grfx.html).

## Doors

Text callers (telnet, modem, rlogin) open doors with *D* on the main menu.
A door is a program the BBS hands the caller's line to: it writes the drop
files doors read (DOOR.SYS, DORINFO1.DEF, DOOR32.SYS and CHAIN.TXT, in the
node's own directory), runs the door, and joins the two until the door
ends, the caller hangs up (the door is stopped), the door's time limit
runs out, or the caller types nothing for the idle time.  A door's output
in CP437 is shown as UTF-8 to UTF-8 terminals, and what they type goes
back as CP437; a CP437 terminal (SyncTERM, NetRunner, a modem caller) gets
the bytes as they are.  3270 callers do not see doors: doors are ANSI.

**DOS doors** run in dosemu2, in Docker.  `doors/dosemu2` builds the image:
dosemu2 with its fdpp kernel and comcom64 shell, built from source on
Ubuntu (no PPA needed):

```
make doors-image                 ; docker build -t bbs-dosemu2 doors/dosemu2
```

In the container the door's drive is F: (`/doors`, mount your doors there)
and the node's drop files are on G: (`/node`); the caller is on COM1 behind
dosemu2's own FOSSIL driver, which is what DOS doors talk to.  The image's
`rundoor DOSDIR COMMAND` loads the FOSSIL, goes to the door's directory and
runs it; what the door shows on its own screen (not COM1) and dosemu2's
messages go to `dosemu.out` in the node's directory, and the command's
own errors (Docker's) to `door.log` there.

```
[door lord]
description = Legend of the Red Dragon
command = docker run --rm -i --network none --name bbs-door{node} --user {uid}:{gid} -v /srv/doors:/doors -v {dir}:/node bbs-dosemu2 'F:\LORD' 'START.BAT {node}'
dir = /var/lib/bbs/doors/node{node}
time_limit = 60
cleanup = docker rm -f bbs-door{node}
```

`{node}`, `{dir}` (the node's directory), `{uid}` and `{gid}` (the BBS's
own: the door writes the node's files as the BBS), `{userid}`,
`{minutes}`, `{rows}`, `{cols}` and `{baud}` are filled in.  Most doors are
told where the drop file is on their command line or in their own
configuration: here that is `G:\DOOR.SYS` (or `G:\DORINFO1.DEF`, ...).
Each node has its own directory, so multi-node doors work; `single = yes`
lets one caller at a time into a door that is not.  dosemu2 runs the door
under its CPU emulator unless the host offers `/dev/kvm` to the container
(`--device /dev/kvm`, untried here); a door starts in about a second
under the emulator.

**Linux doors**: any program that talks to a terminal on its standard
input and output, with `pty = yes` for one that wants a terminal (most
do), and `charset = utf-8` for one that writes UTF-8.  DOOR32.SYS says
`door32_comm = local` to them.

The BBS runs `docker` itself, so its user must be allowed to: the
`docker` group (which is as good as root: think twice), or rootless Docker
or Podman (`command = podman run ...`).  The door's container has no
network (`--network none`), runs as the BBS's user, not root, and is
removed when the door ends or the caller hangs up.

It was tried with a FOSSIL door of its own (`doors/dosemu2/testdoor`,
which reads DOOR.SYS, writes CP437 and ANSI and echoes keys) through the
whole BBS: the caller types, the door answers, the caller hangs up in it
and the container goes.

## Newsgroups and NNTP

A message area becomes a newsgroup when it is given a newsgroup name.  Its
messages are then articles: newsreaders (Thunderbird, slrn, tin, Pan) read
and post them through the BBS's NNTP server, and peer news servers swap
them with it, so a message posted from a modem shows up on INN and a reply
from Usenet-style readers shows up on the 3270.

```
bbs area newsgroup General bbs.general
bbs area newsgroup General -          ; no longer a newsgroup
bbs news sync                         ; sync with every peer now
bbs news sync upstream                ; or one
```

```
; bbs.conf
[news]
hostname = news.example.org    ; this site in Path and Message-IDs
organization = D-Modem BBS
max_article = 64K

[nntp]
address = :563
tls = implicit                 ; or required: STARTTLS before AUTHINFO
cert_file = /etc/bbs/cert.pem
key_file = /etc/bbs/key.pem
read = anyone                  ; or users: log in first
post = yes

[peer upstream]
address = news.example.net:119
tls = starttls
username = bbs
password = secret
groups = bbs.*
accept_from = news.example.net ; may feed us by IHAVE
```

**The server** speaks RFC 3977 with the readers' commands (`GROUP`,
`LISTGROUP`, `ARTICLE`/`HEAD`/`BODY`/`STAT`, `NEXT`/`LAST`, `OVER`/`XOVER`,
`HDR`/`XHDR`, `LIST ACTIVE`/`NEWSGROUPS`/`OVERVIEW.FMT`, `NEWNEWS`, `POST`),
`IHAVE` for peers, `AUTHINFO USER/PASS` (RFC 4643) with the BBS's login,
failure delay and throttling, and `STARTTLS` (RFC 4642).  Every message has
a Message-ID; a message posted on the BBS is made into an article (From,
Date, Path, References from the thread it replies to, MIME UTF-8 text).
An article that came from elsewhere is kept exactly as it came and shown on
the BBS as text: its poster, subject and body decoded (quoted-printable,
base64, the charset, the text part of a multipart) and cleaned like
anything else a caller writes.

**Peers** are synced every `interval` seconds, and by `bbs news sync`:

- *pull*: for each of our newsgroups the peer carries (and `groups`
  matches: a wildmat, `bbs.*,!bbs.test`), the articles after the last one
  fetched, by `OVER` and `ARTICLE`.  The first time, only the latest
  `initial` of each group.
- *push*: our articles posted since the last push (on the BBS, by a reader,
  or from another peer, never back to the peer they came from), by `IHAVE`;
  a server that refuses `IHAVE` to us (a reader-only account) gets `POST`.
  The first time only records where to start: the history is not dumped
  on a new peer.
- the peer may *feed* us by `IHAVE` from the addresses in `accept_from`;
  nobody else may use `IHAVE`.

An article is taken once (a history of Message-IDs, also of those refused),
never if this site is in its Path already (so two peers that both carry a
group do not loop), and stored in the first of its newsgroups carried here;
this site's name goes at the head of its Path.  It was tried against INN
2.7 both ways: pushing to it, pulling from it, and innxmit feeding the BBS.

What is not done: control messages (cancel, newgroup, rmgroup) are refused,
not acted on, and deleting a message on the BBS does not cancel it
elsewhere; a crossposted article lives in one area here; there is no
`NEWGROUPS` (newsgroups are made by the sysop) and no streaming
(`CHECK`/`TAKETHIS`), which suits a BBS's volume.

## FidoNet

The BBS can be a node (or point) of FidoNet or any other FTN network
(fsxNet, AgoraNet, a network of your own): a message area becomes an
echo when it is given an echo tag, users write netmail by addressing mail
to `Name@zone:net/node`, and the BBS exchanges packets with its links by
binkp, calling them and answering them.  It is its own tosser, scanner and
mailer: no other FTN software is needed.

```
bbs area echo General FSX_GEN       ; General is the echo FSX_GEN
bbs area echo General -             ; no longer an echo
bbs fido poll                       ; call the links now (also: poll LINK)
bbs fido scan                       ; pack new mail into the outbound
bbs fido toss                       ; take in what has come
```

```
; bbs.conf
[fido]
address = 21:1/999              ; ours; more after it are AKAs
origin = D-Modem BBS, by phone and telnet
dir = /var/lib/bbs/fido          ; inbound, outbound, bad packets
charset = cp437                  ; or utf-8

[binkp]                          ; answer calls (port 24554)
address = :24554

[link hub]                       ; our hub: a link per system
address = 21:1/100
host = hub.example.net:24554       ; to call it ("": it calls us)
password = secret                ; the session password
echoes = FSX_*                   ; the echoes exchanged, a wildmat
route = yes                      ; netmail for anywhere goes by it
poll = 3600                      ; call every hour, mail or not
```

**Echomail**: what users post in an echo is packed for each link that
carries it (with MSGID, REPLY for threads, PID, TZUTC, CHRS, tear and
origin lines, SEEN-BY and PATH), and what links send is tossed into the
echo, decoded from its character set (CP437 and the other DOS code pages,
Latin-1, KOI8, Windows code pages, UTF-8, by its CHRS kludge) and cleaned
like anything else a caller writes.  With more than one link the BBS
passes echomail on between them, by SEEN-BY, as a hub does.  Messages are
taken once (by MSGID, or a digest of messages that lack one) and never if
this system is in their PATH already.

**Netmail**: on the text BBS and on a 3270, mail to `Name@1:2/3` (or
`Name@1:2/3.4`) is netmail, sent by the link with that address, the
link of that point's node, or else the link with `route = yes`.  Netmail
for this system goes to the user of that name, or the sysop, from
`Name@zone:net/node`, so *Reply* answers it as netmail (with a REPLY
kludge).  The BBS routes no one else's netmail: it is a leaf.

**binkp** (FTS-1026) with CRAM-MD5 passwords (FTS-1027): links are known by
their address and password, others are turned away; a password is never
sent in the clear unless `plain_password = yes`.  Packets are Type 2+,
sent as ZIP bundles (ARCmail, `packer = zip`, the default) or bare
(`none`); received ZIP bundles and packets are tossed, files that are not
mail (TIC file echoes, requests) are set aside in `dir/in/LINK/files`, and
broken packets in `dir/bad`.  New mail is packed within a minute
(netmail at once), and a link with a host is called when it has mail waiting (again every
five minutes while calls fail) and every `poll` seconds.  The first scan
for a new link only marks where to start: the history is not dumped on
it.

An area that is both an echo and a newsgroup is a gateway: echomail is
served to newsreaders as articles (From `Name <Name@f1.n2.z3.fidonet.org>`,
the Message-ID made of the MSGID as other gateways make it), and articles
from news peers go out as echomail.  Most FidoNet echoes do not want
gated traffic: ask first.

It was tried against binkd 1.1a and CrashMail II 1.7: echomail and netmail
both ways, calling binkd and called by it, with ZIP bundles and CRAM-MD5.

What is not done: ARC, ARJ and LHA bundles (ZIP only, which nearly every
hub sends), file echoes (TIC), file requests, AreaFix (the sysop sets
`echoes` by hand and asks the hub's AreaFix by netmail), nodelists, routing
others' netmail, binkp's NR mode, CRYPT and multiple batches (it speaks
binkp/1.0, which every mailer accepts), and EMSI for mailers calling by
modem.

## Security

- Passwords are bcrypt (cost 12); unknown users take as long to refuse as
  wrong passwords; failures wait `fail_delay`; `attempts` failures hang
  up; `throttle_failures` turns away an address or caller ID that keeps
  failing (or signing up).
- Everything users write is cleaned before it is stored and again before
  it is shown: no escape sequences, control characters, C1 controls,
  bidirectional overrides or invalid UTF-8 reach another caller's
  terminal.  On a 3270 these would become 3270 orders once in EBCDIC; go3270
  does not filter them itself.
- Limits everywhere: name, subject and line lengths, message size, telnet
  subnegotiations, session headers, rlogin fields, 64 KB for a 3270
  screen's answer; timeouts for every step (telnet and TN3270 negotiation,
  headers, the login, idle callers, writes to callers who stopped
  reading); `max_nodes` callers at once and `max_per_ip` from one address.
- The database is created 0600; unix sockets are made private and then
  opened to `socket_mode`, never wider in between.
- Private mail is read only by its recipient (every query asks for the
  user's own); a locked account is said to be locked only to someone who
  knows its password.
- Telnet and rlogin carry passwords in the clear, as they always have:
  keep rlogin to 127.0.0.1 or a socket, and put telnet behind a VPN or an
  SSH tunnel where that matters.  A modem line is as private as the phone
  network.
- News: a reader's article is posted as its user whatever its From says,
  with the BBS's own Message-ID, Date and Path; only the headers a reader
  may choose (Subject, References, the body) are kept.  `IHAVE` is taken
  only from a peer's `accept_from` addresses, and peers' articles are
  checked (the headers RFC 5536 requires, sizes of the article, its
  headers and lines) and cleaned before a caller sees them.  `tls =
  required` refuses `AUTHINFO` before `STARTTLS`; `no` and `optional` make
  `bbs check` warn.  Commands, lines and articles are limited, and so are
  clients at once and from one address.
- FidoNet: a link is known by its address and its password (CRAM-MD5;
  in the clear only if allowed); a wrong password waits and is refused,
  an address that is not a link is turned away, and a system calling with
  a link's address but not its password gets nothing.  Packets are tossed
  only from a link's own inbound, only if from its address to ours, with
  its packet password if it has one.  Received file names are reduced to a
  safe base name; files, sessions, packets, bundles and messages have size
  limits.  Netmail's sender is what the message says, as anywhere in
  FidoNet: it is not proof of who wrote it.
- Doors: a DOS door runs in a container with no network, as the BBS's
  user, seeing only the doors' directory and its node's; the caller's
  line is joined to it only through COM1.  Hanging up, the time limit and
  the idle time stop it.  The drop files carry no password.
- The log has logins, failures, posts and mail sent (never passwords or
  message text).

## Layout

| | |
|---|---|
| `cmd/bbs` | the command: serve, check, init, user and area administration |
| `internal/server` | listeners, session headers, rlogin, telling 3270s from the rest |
| `internal/telnet` | the server side of telnet: options, terminal type, window size, the handoff to go3270 |
| `internal/textui` | the text BBS: line editing, ANSI detection, paging, the menus |
| `internal/ui3270` | the 3270 BBS: screens and PF keys on go3270, and its graphics |
| `internal/sixel` | the sixel encoder |
| `internal/files` | file areas' storage: names, uploads, limits |
| `internal/ftp` | the FTP server: the file areas as a file system |
| `internal/news` | articles: parsing, checking, making, decoding; wildmats |
| `internal/nntp` | the NNTP server and the peers' client |
| `internal/ftn` | FidoNet's formats: addresses, Type 2+ packets, messages, kludges, character sets |
| `internal/fido` | the BBS on FidoNet: tosser, scanner, netmail, the binkp mailer |
| `internal/door` | doors: drop files, running a door on the caller's line |
| `doors/dosemu2` | the Docker image DOS doors run in: dosemu2, its FOSSIL on COM1 |
| `internal/xfer` | XMODEM, YMODEM, ZMODEM and Kermit, both ways |
| `internal/logo` | the logo: a PNG, or the name in a pixel font |
| `internal/bbs` | the BBS itself: nodes, logins, new users, posting, cleaning text |
| `internal/store` | the SQLite database |
| `internal/config` | the configuration file |

## Tests

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
it.
