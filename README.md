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
- new users sign themselves up (or not: `new_users = no`)
- SQLite storage, one file

## Building

Go 1.22 or later:

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
and TN3270 clients that report programmed symbols (the logo is loaded and
shown), vector graphics (the logo is drawn) or neither (nothing is sent).
