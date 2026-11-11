The BBS has an IRC server, and its channels are the [chat rooms](Chat-and-Paging).
Someone in HexChat, irssi, WeeChat or any other IRC client talks with
callers on telnet, SSH, 3270s, the web terminal and the API. IRC is
just one more way into the same chat.

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
| `server_name` | the host's name | the server's name to clients |
| `network` | the BBS's name | the `NETWORK` clients show |
| `max_clients` | `64` | connections at once |
| `max_per_ip` | `4` | from one address |
| `service` | `BBSServ` | the [services bot](#bbsserv)'s nick |
| `chanserv` | `ChanServ` | the [rooms' bot](#chanserv)'s nick |
| `chanserv_expire` | `90` | days a registered room may go unused before it is dropped (`0`: never) |

`bbs check` warns about a plain `address` without a `tls_address`,
because passwords then travel in the clear. The deny list and bans in
`[security]` apply as they do everywhere else.

## Logging in

Every IRC user is a BBS user, logged in with their BBS name and password
in one of these ways:

- **SASL PLAIN**, which most clients offer as "SASL" or "account login";
- **the server password** (`PASS`), set to `name:password`, or to the
  password alone with your nick as your name.

Failed logins count towards the throttle and the hack log, like any
other login. The user's access level must allow chat.

Your nick is your BBS name. Spaces and dots become underscores, so
`Carol Ann` is `Carol_Ann`. A client that asked for another nick is
told of the change, and `NICK` can't change it later.

Each connection is a node, shown in *Who's online* as `irc`. Others can
send it node messages, and the sysop can throw it off like any caller.

## Channels

`#ROOM` is the room ROOM: `#main` is the main room, and any other name
(letters, digits and `-`, up to 20) makes a room. One client can be in
up to 20 at once. Joining shows the room's last 20 lines.

| On IRC | On the BBS |
|---|---|
| `PRIVMSG #room :text` | a line in the room |
| `/me waves` (CTCP ACTION) | `* alice waves` |
| `PRIVMSG nick :text` | a message to that user's nodes; to one not on, [kept](Chat-and-Paging#private-messages) for them (`301` says so) |
| a caller's node message | a `PRIVMSG` to you |
| `NAMES`, `WHO`, `WHOIS` | who is in a room, on which node and how (`bob!telnet@node3`) |
| `LIST` | the rooms in use |
| a page, mail, a notice | a `NOTICE` from the server |

In a room no one has registered, sysops show as channel operators (`@`)
and no one else does. A user can [register a room](#chanserv) with
ChanServ: then its access list says who is opped or voiced on coming in,
and its ops can `MODE +o`/`-o`/`+v`/`-v`, `KICK` and set the topic.
[Permanent](#permanent-rooms) and registered rooms have topics; others
don't. Colours and other formatting are stripped from lines going into
the rooms. A registered room's ops set its [channel modes](#channel-modes).

## Channel modes

A registered room has Solanum's channel modes, set by its ops with `MODE`
(`/mode` in the BBS's chat). A room nobody has registered is `+Ccnt`, and
its modes can't change. Like ops and bans, the modes are the room's, so a
caller on the text BBS, AOL or Yahoo is kept out or kept quiet just as an
IRC user is, and they are kept across restarts.

| Mode | What it does |
|---|---|
| `o`, `v` | an operator; a voice |
| `n` | no lines from outside the room (on when registered) |
| `t` | only ops set the topic (on when registered) |
| `s` | secret: not in `LIST` or the BBS's room lists, its members not shown, to those outside |
| `p` | private: no `KNOCK` |
| `m` | moderated: only ops and voices talk |
| `i` | invite only: in by `INVITE`, an `I` mask, or the `i` flag |
| `k KEY` | a key to come in: `JOIN #room KEY`, or `/join room KEY` |
| `l N` | at most N users (someone on two nodes counts once) |
| `z` | lines that `m`, `b` or `q` stop go to the ops instead |
| `g` | anyone in the room may `INVITE` |
| `T` | no `NOTICE`s to the room |
| `b MASK` | banned: kept out, and quiet if in |
| `q MASK` | quieted: may not talk |
| `e MASK` | an exception to `b` and `q` |
| `I MASK` | an exception to `i` |
| `c`, `C` | colours stripped, CTCPs dropped: always on |

A mask is `nick!user@host`, where a BBS user is `Nick!how@nodeN` (how they
came: `irc`, `telnet`, `ssh`...): `+b alice` bans `alice!*@*`, which, nicks
being BBS names, is alice's account, and `+q *!telnet@*` quiets everyone
on telnet. Extbans: `$a` matches every account (so `$~a` no one of ours),
`$a:MASK` an account by name, `$o` sysops. Each list holds 100.

An invite lasts an hour, gets past `i`, `k` and `l`, and is used up by
coming in. `KNOCK #room [why]` asks the ops of a room that is invite only,
keyed or full to let you in, once a minute. Sysops and a room's founder
come in always, and sysops, ops and voices always talk; someone with the
`e` flag isn't kept out by bans, and one with `i` gets past `i`, `k` and
`l`. Modes beside these (`f`, `j`, `S`, `P`, `L`...) are refused.

A sysop is told of a page by a `NOTICE`. Joining `#sysop-N` answers the
page from node N, just as `/answer` does in the BBS's chat.

Messages kept for you while you were away come as `PRIVMSG`s once you
log in, each with its time: as `server-time` if your client takes it,
otherwise at the start of the line.

The server offers the IRCv3 capabilities `sasl`, `server-time` (the time
of each line, the history replayed on join included), `echo-message`,
`multi-prefix` and `userhost-in-names`.

## Permanent rooms

A room closes when its last caller leaves, unless the sysop makes it
permanent. BBSServ holds a permanent room open, empty or not and across
restarts. It sits in the channel as an operator (`@BBSServ` in `NAMES`
and `WHO`), and `LIST` and BBSServ's `ROOMS` show the room with its
topic even when no one is in it. Permanent rooms are the BBS's own: on
the text BBS and 3270s too, `/rooms` lists them with their topics, and
coming in shows the topic.

| Command | What it does |
|---|---|
| `/msg BBSServ ROOMS` | the permanent rooms (anyone) |
| `/msg BBSServ ROOM ADD #room [topic]` | make a room permanent (the sysop) |
| `/msg BBSServ ROOM TOPIC #room [topic]` | set its topic, or clear it |
| `/msg BBSServ ROOM DEL #room` | permanent no more: it goes with its last caller |
| `TOPIC #room :text` | the sysop sets the topic from the channel |
| `/topic [text]` | in the BBS's chat: the room's topic; the sysop sets it |
| `bbs room list`, `add`, `topic`, `del` | the same from the shell |

When a topic is set, everyone in the room is told: on IRC as a `TOPIC` from
BBSServ, on the BBS as a line ("sysop set the topic: ..."). Changes made
with the `bbs` command reach a running BBS at its next start; those made
through BBSServ apply at once.

## ChanServ

ChanServ looks after registered rooms: who founded each, and who has which
flags there. A room is registered by someone in it, who becomes its
founder and is opped. Like everything about rooms, it holds for every way
in: a caller on the text BBS, AOL, Yahoo or AIM is opped, voiced, kept out
or put out just as an IRC user is. Talk to it with `/msg ChanServ COMMAND`
or `/CS COMMAND`; on the text BBS, `/cs COMMAND` in chat.

| Command | Needs | What it does |
|---|---|---|
| `REGISTER #room` | to be in it | makes the room yours |
| `DROP #room` | the founder | unregisters it |
| `INFO #room` | | its founder, when registered and last used, settings, topic |
| `FLAGS #room` | `A` | the access list |
| `FLAGS #room nick +flags-flags` | `f` | changes an entry; a template (`VOP`, `AOP`, `SOP`, `FOUNDER`) sets it, `-*` takes it off |
| `OP`, `DEOP #room [nick ...]` | `o` | ops you, or them |
| `VOICE`, `DEVOICE #room [nick ...]` | `v` | voices you, or them |
| `TOPIC #room [text]` | `t` | sets the topic |
| `INVITE #room` | `i` | invites you in (past `+i`, `+k` and `+l`, for the hour) |
| `GETKEY #room` | `i` | tells you the key |
| `KICK #room nick [reason]` | `r` | puts one out |
| `BAN`, `UNBAN #room nick` | `r` | keeps one out (`b` on their entry), or lets them in again |
| `CLEAR #room USERS [reason]` | `R` | puts everyone out but you |
| `SET #room FOUNDER nick` | the founder | gives the room away |
| `SET #room GUARD`, `TOPICLOCK`, `PRIVATE ON`\|`OFF` | `s` | ChanServ holds the room open; only `t` sets the topic; `INFO` shows little to those without `A` |
| `SET #room DESCRIPTION`, `URL`, `ENTRYMSG [text]` | `s` | the room's texts; the entry message is told to everyone coming in |
| `LIST` | | the rooms you have flags in (a sysop's `LIST ALL`: every room) |
| `WHY #room [nick]` | | what gives you (or them) their flags |

The flags are Atheme's:

| Flag | | Flag | |
|---|---|---|---|
| `v` | voice through ChanServ | `V` | voiced on coming in |
| `o` | ops through ChanServ | `O` | opped on coming in |
| `t` | sets the topic | `i` | `INVITE`, `GETKEY`; gets past `+i`, `+k`, `+l` |
| `r` | `KICK`, `BAN`, `UNBAN` | `R` | `CLEAR` |
| `f` | changes the access list | `s` | `SET` |
| `A` | sees the access list | `e` | not kept out by a ban |
| `b` | banned: kept out | `F` | founder: everything |

Templates: `VOP` is `+AV`, `AOP` `+AOiortv`, `SOP` `+AORefiorstv`, and
`FOUNDER` `+AFORefiorstv`. Someone with `f` gives or takes only flags they
have; `F` is a founder's to give. A sysop acts as a founder in every room,
but is opped in a registered one only as anyone is. Only a sysop puts a
sysop out, and no one but a sysop puts out the founder.

Page rooms (`sysop-N`) and rooms a [linked network](IRC-Linking) or MRC
carries can't be registered: the network's own services look after
those. A permanent room is registered by the sysop alone, who can then
give it away with `SET FOUNDER`. A room no one on its access list has
come into for `chanserv_expire` days is dropped. When a founder's account
is deleted, the room goes to the oldest entry with `F`, else with `f`,
else the oldest entry; with no one left, it is dropped.

## BBSServ

BBSServ is a services bot, like an IRC network's NickServ, for your BBS
account. Everyone on the server is logged in, so it always acts on the
account of whoever asks. Talk to it with `/msg BBSServ COMMAND`, or
with its own command, `/BS COMMAND` (`/BBSSERV` also works). Its
answers come as notices.

| Command | What it does |
|---|---|
| `HELP [command]` | the commands, or how to use one |
| `INFO` | your account: level, where you call from, alias, signature, settings, unread mail, keys, tokens, new scan |
| `SET LOCATION`, `ABOUT`, `ALIAS` *value* | your profile's fields (no value, or `-`, clears one) |
| `SET SIGNATURE` *line* `\|` *line* | up to three lines under what you write |
| `SET ANSI`, `UTF8`, `SIXEL`, `RIP`, `WIDTH`, `EDITOR`, `EXPERT` *value* | your terminal and menu settings; cleared, a terminal setting goes back to what your terminal says |
| `PASSWD` *now new* | your password |
| `SCAN`, `SCAN ON`\|`OFF` *area*\|`ALL` | the areas of your new scan (and of your [QWK](Offline-Mail) packets) |
| `KEYS`, `KEYS ADD` *password key*, `KEYS DEL` *n* | your [SSH keys](SSH). Adding one asks for your password, since a key opens your account |
| `TOKENS`, `TOKENS REVOKE` *id* | your [HTTP API](HTTP-API) tokens |
| `ROOMS`; `ROOM ADD`\|`DEL`\|`TOPIC` *#room* | the [permanent rooms](#permanent-rooms); making them (the sysop) |
| `FRIENDS`, `FRIENDS ADD`\|`DEL` *nick* | your [friends list](Chat-and-Paging#friends), and who on it is on |
| `HISTORY`, `HISTORY` *nick* [*lines*] | those you have talked with; your last lines with one (50, up to 500) |

The same checks apply as on the BBS: an alias that is someone's name is
refused, and so is a setting's wrong value. A wrong password (`KEYS
ADD`, `PASSWD`) counts as a failed login. CTCP `VERSION`, `PING` and
`TIME` are answered, and notices sent to the bot are ignored. A user
whose BBS name is the bot's nick can't be reached by nick over IRC.

`service = Account` in `[irc]` gives the bot another nick, and its own
command becomes `/ACCOUNT`.

## Ergo

The IRC protocol's parts come from [ergo](https://github.com/ergochat/ergo):

- messages are read and written by ergo's `irc-go` library (`ircmsg`,
  `ircreader`, and `ircfmt` for stripping formatting);
- the numerics and the `ISUPPORT` list builder are taken from ergo's
  source, under its MIT license (`internal/ircd/LICENSE.ergo`);
- BBSServ's framework (its command table, parameters, `HELP`, CTCP) is
  adapted from ergo's services (`irc/services.go`), on which its
  NickServ and ChanServ run.

Its tests also use irc-go's client (`ircevent`).

**See also:** [Chat and paging](Chat-and-Paging), [IRC server linking](IRC-Linking), [MRC](MRC), [Security](Security)
