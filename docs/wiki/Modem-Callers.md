## Straight from app_dmodem

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

## Behind modemmenu

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

**See also:** [Configuration](Configuration), [File transfers](File-Transfers)
