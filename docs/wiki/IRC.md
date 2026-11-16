The BBS has an IRC server. Its channels are the
[chat rooms](Chat-and-Paging). A user in HexChat, irssi, WeeChat or a
different IRC client talks with callers on telnet, SSH, 3270s, the web
terminal and the API. IRC is only one more route into the same chat.

```ini
[irc]
address = :6667
tls_address = :6697
tls_cert = /etc/bbs/tls.crt
tls_key = /etc/bbs/tls.key
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:6667` | plain IRC (`none`: TLS only) |
| `tls_address` | (none) | IRC over TLS, with `tls_cert` and `tls_key` |
| `server_name` | the name of the host | the name of the server that clients see |
| `network` | the name of the BBS | the `NETWORK` that clients show |
| `max_clients` | `64` | the maximum number of connections at one time |
| `max_per_ip` | `4` | the maximum from one address |
| `service` | `BBSServ` | the nick of the [services bot](#bbsserv) |
| `chanserv` | `ChanServ` | the nick of the [bot of the rooms](#chanserv) |
| `chanserv_expire` | `90` | the number of days that a registered room can be unused before the BBS drops it (`0`: never) |

`bbs check` gives a warning about a plain `address` without a
`tls_address`, because then passwords go in clear text. The deny list
and bans in `[security]` apply, as on all other routes.

## Logging in

Each IRC user is a BBS user. The user logs in with the BBS name and
password in one of these ways:

- **SASL PLAIN**, which most clients offer as "SASL" or "account login".
- **The server password** (`PASS`), set to `name:password`, or to the
  password only, with your nick as your name.

Failed logins count towards the throttle and the hack log, as all other
logins do. The access level of the user must permit chat.

Your nick is your BBS name. Spaces and dots become underscores. Thus,
`Carol Ann` is `Carol_Ann`. If a client asked for a different nick, the
server tells the client about the change. `NICK` cannot change it later.

Each connection is a node, which *Who's online* shows as `irc`. Other
users can send node messages to it. The sysop can disconnect it, as for
all callers.

## Channels

`#ROOM` is the room ROOM. `#main` is the main room. All other names
(letters, digits and `-`, up to 20) make a room. One client can be in up
to 20 rooms at one time. When a client joins, it sees the last 20 lines
of the room.

| On IRC | On the BBS |
|---|---|
| `PRIVMSG #room :text` | a line in the room |
| `/me waves` (CTCP ACTION) | `* alice waves` |
| `PRIVMSG nick :text` | a message to the nodes of that user. For a user who is not on, the BBS [keeps](Chat-and-Paging#private-messages) it for the user (`301` tells this) |
| the node message of a caller | a `PRIVMSG` to you |
| `NAMES`, `WHO`, `WHOIS` | the users in a room, their nodes and their routes (`bob!telnet@node3`) |
| `LIST` | the rooms in use |
| a page, mail, a notice | a `NOTICE` from the server |

In a room that no one registered, sysops show as channel operators
(`@`), and no other users do. A user can
[register a room](#chanserv) with ChanServ. Then its access list sets
the users that get op or voice when they come in. Its ops can
`MODE +o`/`-o`/`+v`/`-v`, `KICK` and set the topic.
[Permanent](#permanent-rooms) and registered rooms have topics. Other
rooms do not. The BBS removes colours and other formats from lines that
go into the rooms. The ops of a registered room set its
[channel modes](#channel-modes).

## Channel modes

A registered room has the channel modes of Solanum. Its ops set them with
`MODE` (`/mode` in the chat of the BBS). A room that nobody registered is
`+Ccnt`, and its modes cannot change. As ops and bans, the modes belong
to the room. Thus, they keep out or silence a caller on the text BBS, AOL
or Yahoo in the same way as an IRC user. The BBS keeps them across
restarts.

| Mode | What it does |
|---|---|
| `o`, `v` | an operator. A voice |
| `n` | no lines from outside the room (on when registered) |
| `t` | only ops set the topic (on when registered) |
| `s` | secret: not in `LIST` or the room lists of the BBS. Its members are not shown to users outside |
| `p` | private: no `KNOCK` |
| `m` | moderated: only ops and voices can talk |
| `i` | invite only: users come in by `INVITE`, an `I` mask, or the `i` flag |
| `k KEY` | a key to come in: `JOIN #room KEY`, or `/join room KEY` |
| `l N` | a maximum of N users (a user on two nodes counts one time) |
| `z` | lines that `m`, `b` or `q` stop go to the ops |
| `g` | all users in the room can `INVITE` |
| `T` | no `NOTICE`s to the room |
| `b MASK` | banned: kept out, and silent if in the room |
| `q MASK` | quieted: cannot talk |
| `e MASK` | an exception to `b` and `q` |
| `I MASK` | an exception to `i` |
| `c`, `C` | colours removed, CTCPs dropped: always on |

A mask is `nick!user@host`. A BBS user is `Nick!how@nodeN`, where how is
the route (`irc`, `telnet`, `ssh`...). Nicks are BBS names. Thus,
`+b alice` bans `alice!*@*`, which is the account of alice.
`+q *!telnet@*` silences all users on telnet. Extbans: `$a` matches all
accounts (thus `$~a` matches none of our users), `$a:MASK` matches an
account by name, and `$o` matches sysops. Each list holds 100 entries.

An invite is valid for one hour. It gets past `i`, `k` and `l`, and
the user uses it when the user comes in. `KNOCK #room [why]` asks the ops
of a room that is invite only, keyed or full to let you in, one time
each minute. Sysops and the founder of a room can always come in.
Sysops, ops and voices can always talk. Bans do not keep out a user with
the `e` flag. A user with `i` gets past `i`, `k` and `l`. The BBS
refuses other modes (`f`, `j`, `S`, `P`, `L`...).

A `NOTICE` tells a sysop about a page. If the sysop joins `#sysop-N`, the
sysop answers the page from node N, as `/answer` does in the chat of the
BBS.

Messages that the BBS kept for you while you were away come as
`PRIVMSG`s after you log in, each with its time. The time comes as
`server-time` if your client accepts it. If not, it is at the start of
the line.

The server offers these IRCv3 capabilities: `sasl`, `server-time` (the
time of each line, also the history that the server sends again on
join), `echo-message`, `multi-prefix` and `userhost-in-names`.

## Permanent rooms

A room closes when its last caller leaves, unless the sysop makes it
permanent. BBSServ keeps a permanent room open, empty or not, and across
restarts. It is in the channel as an operator (`@BBSServ` in `NAMES` and
`WHO`). `LIST` and the `ROOMS` of BBSServ show the room with its topic,
also when no one is in it. Permanent rooms belong to the BBS. Thus, on
the text BBS and 3270s, `/rooms` also lists them with their topics, and
the BBS shows the topic when a user comes in.

| Command | What it does |
|---|---|
| `/msg BBSServ ROOMS` | the permanent rooms (all users) |
| `/msg BBSServ ROOM ADD #room [topic]` | makes a room permanent (the sysop) |
| `/msg BBSServ ROOM TOPIC #room [topic]` | sets its topic, or clears it |
| `/msg BBSServ ROOM DEL #room` | the room is no longer permanent: it closes with its last caller |
| `TOPIC #room :text` | the sysop sets the topic from the channel |
| `/topic [text]` | in the chat of the BBS: the topic of the room. The sysop sets it |
| `bbs room list`, `add`, `topic`, `del` | the same from the shell |

When a user sets a topic, the BBS tells all users in the room: on IRC as
a `TOPIC` from BBSServ, and on the BBS as a line ("sysop set the topic:
..."). Changes made with the `bbs` command get to a BBS that runs at its
next start. Changes made through BBSServ apply immediately.

## ChanServ

ChanServ manages registered rooms: the founder of each room, and the
flags of each user there. A user in a room registers it. That user
becomes its founder and gets op. As for all room data, this applies on
all routes: ChanServ gives op or voice to a caller on the text BBS, AOL,
Yahoo or AIM, or keeps or puts that caller out, in the same way as an
IRC user. Talk to it with `/msg ChanServ COMMAND` or `/CS COMMAND`. On
the text BBS, use `/cs COMMAND` in chat.

| Command | Needs | What it does |
|---|---|---|
| `REGISTER #room` | to be in it | makes the room yours |
| `DROP #room` | the founder | unregisters it |
| `INFO #room` | | its founder, when it was registered and last used, settings, topic |
| `FLAGS #room` | `A` | the access list |
| `FLAGS #room nick +flags-flags` | `f` | changes an entry. A template (`VOP`, `AOP`, `SOP`, `FOUNDER`) sets it. `-*` removes it |
| `OP`, `DEOP #room [nick ...]` | `o` | gives or removes op for you, or for them |
| `VOICE`, `DEVOICE #room [nick ...]` | `v` | gives or removes voice for you, or for them |
| `TOPIC #room [text]` | `t` | sets the topic |
| `INVITE #room` | `i` | invites you in (past `+i`, `+k` and `+l`, for one hour) |
| `GETKEY #room` | `i` | tells you the key |
| `KICK #room nick [reason]` | `r` | puts one user out |
| `BAN`, `UNBAN #room nick` | `r` | keeps one user out (`b` on their entry), or lets them in again |
| `CLEAR #room USERS [reason]` | `R` | puts all users out except you |
| `SET #room FOUNDER nick` | the founder | gives the room to a different user |
| `SET #room GUARD`, `TOPICLOCK`, `PRIVATE ON`\|`OFF` | `s` | ChanServ keeps the room open. Only `t` sets the topic. `INFO` shows little to users without `A` |
| `SET #room DESCRIPTION`, `URL`, `ENTRYMSG [text]` | `s` | the texts of the room. The BBS shows the entry message to all users who come in |
| `LIST` | | the rooms in which you have flags (`LIST ALL` of a sysop: all rooms) |
| `WHY #room [nick]` | | the source of your flags (or their flags) |

The flags are those of Atheme:

| Flag | | Flag | |
|---|---|---|---|
| `v` | voice through ChanServ | `V` | voice when the user comes in |
| `o` | op through ChanServ | `O` | op when the user comes in |
| `t` | sets the topic | `i` | `INVITE`, `GETKEY`. Gets past `+i`, `+k`, `+l` |
| `r` | `KICK`, `BAN`, `UNBAN` | `R` | `CLEAR` |
| `f` | changes the access list | `s` | `SET` |
| `A` | sees the access list | `e` | a ban does not keep the user out |
| `b` | banned: kept out | `F` | founder: all functions |

Templates: `VOP` is `+AV`, `AOP` is `+AOiortv`, `SOP` is `+AORefiorstv`,
and `FOUNDER` is `+AFORefiorstv`. A user with `f` can give or remove
only the flags that the user has. Only a founder can give `F`. A sysop
operates as a founder in all rooms. But in a registered room, a sysop
gets op only in the same way as all users. Only a sysop can put a sysop
out. Only a sysop can put out the founder.

Users cannot register page rooms (`sysop-N`) and rooms that a
[linked network](IRC-Linking) or MRC carries: the services of the
network manage those. Only the sysop can register a permanent room. The
sysop can then give it to a different user with `SET FOUNDER`. If no
user on its access list came into a room for `chanserv_expire` days,
the BBS drops the room. When the BBS deletes the account of a founder,
the room goes to the oldest entry with `F`. If there is none, it goes to
the oldest entry with `f`, or else to the oldest entry. If no entry
remains, the BBS drops the room.

## BBSServ

BBSServ is a services bot for your BBS account, similar to the NickServ
of an IRC network. All users on the server are logged in. Thus, it always
operates on the account of the user who asks. Talk to it with
`/msg BBSServ COMMAND`, or with its own command, `/BS COMMAND`
(`/BBSSERV` also operates). Its answers come as notices.

| Command | What it does |
|---|---|
| `HELP [command]` | the commands, or how to use one |
| `INFO` | your account: level, location, alias, signature, settings, unread mail, keys, tokens, new scan |
| `SET LOCATION`, `ABOUT`, `ALIAS` *value* | the fields of your profile (no value, or `-`, clears one) |
| `SET SIGNATURE` *line* `\|` *line* | up to three lines below the text that you write |
| `SET ANSI`, `UTF8`, `SIXEL`, `RIP`, `WIDTH`, `EDITOR`, `EXPERT` *value* | your terminal and menu settings. If you clear a terminal setting, it goes back to the value that your terminal gives |
| `PASSWD` *now new* | your password |
| `SCAN`, `SCAN ON`\|`OFF` *area*\|`ALL` | the areas of your new scan (and of your [QWK](Offline-Mail) packets) |
| `KEYS`, `KEYS ADD` *password key*, `KEYS DEL` *n* | your [SSH keys](SSH). To add one, you must give your password, because a key opens your account |
| `TOKENS`, `TOKENS REVOKE` *id* | your [HTTP API](HTTP-API) tokens |
| `ROOMS`; `ROOM ADD`\|`DEL`\|`TOPIC` *#room* | the [permanent rooms](#permanent-rooms). Changes to them (the sysop) |
| `FRIENDS`, `FRIENDS ADD`\|`DEL` *nick* | your [friends list](Chat-and-Paging#friends), and which friends are on |
| `HISTORY`, `HISTORY` *nick* [*lines*] | the users that you talked with. Your last lines with one user (50, up to 500) |

The same checks apply as on the BBS. The bot refuses an alias that is
the name of a different user, and an incorrect value for a setting. An
incorrect password (`KEYS
ADD`, `PASSWD`) counts as a failed login. The bot answers CTCP
`VERSION`, `PING` and `TIME`, and ignores notices sent to it. If the BBS
name of a user is the nick of the bot, IRC users cannot send to that
user by nick.

`service = Account` in `[irc]` gives the bot a different nick. Then its
own command becomes `/ACCOUNT`.

## Ergo

The parts of the IRC protocol come from
[ergo](https://github.com/ergochat/ergo):

- The `irc-go` library of ergo reads and writes messages (`ircmsg`,
  `ircreader`, and `ircfmt` to remove formats).
- The numerics and the `ISUPPORT` list builder come from the source of
  ergo, under its MIT license (`internal/ircd/LICENSE.ergo`).
- The framework of BBSServ (its command table, parameters, `HELP`, CTCP)
  is adapted from the services of ergo (`irc/services.go`), on which its
  NickServ and ChanServ run.

Its tests also use the client of irc-go (`ircevent`).

**See also:** [Chat and paging](Chat-and-Paging), [IRC server linking](IRC-Linking), [MRC](MRC), [Security](Security)
