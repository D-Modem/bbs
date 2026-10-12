# IRC

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

| setting | default | |
|---|---|---|
| `address` | `:6667` | plain IRC (`none`: TLS only) |
| `tls_address` | (none) | IRC over TLS, with `tls_cert` and `tls_key` |
| `server_name` | the host's name | the server's name to clients |
| `network` | the BBS's name | the `NETWORK` clients show |
| `max_clients` | `64` | connections at once |
| `max_per_ip` | `4` | from one address |

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

| IRC | the BBS |
|---|---|
| `PRIVMSG #room :text` | a line in the room |
| `/me waves` (CTCP ACTION) | `* alice waves` |
| `PRIVMSG nick :text` | a message to that user's nodes |
| a caller's node message | a `PRIVMSG` to you |
| `NAMES`, `WHO`, `WHOIS` | who is in a room, on which node and how (`bob!telnet@node3`) |
| `LIST` | the rooms in use |
| a page, mail, a notice | a `NOTICE` from the server |

Sysops show as channel operators (`@`), but there's nothing to operate:
rooms have no topics, modes, kicks or bans. Colors and other formatting
are stripped from lines going into the rooms.

A sysop is told of a page by a `NOTICE`. Joining `#sysop-N` answers the
page from node N, just as `/answer` does in the BBS's chat.

The server offers the IRCv3 capabilities `sasl`, `server-time` (the time
of each line, the history replayed on join included), `echo-message`,
`multi-prefix` and `userhost-in-names`.

## Ergo

The IRC protocol's parts come from [ergo](https://github.com/ergochat/ergo):

- messages are read and written by ergo's `irc-go` library (`ircmsg`,
  `ircreader`, and `ircfmt` for stripping formatting);
- the numerics and the `ISUPPORT` list builder are taken from ergo's
  source, under its MIT license (`internal/ircd/LICENSE.ergo`).

Its tests also use irc-go's client (`ircevent`).
