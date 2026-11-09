The BBS can be a server on an IRC network: a hub that links to any
number of other servers, in and out, and routes between them. It speaks
TS6, the server protocol of Solanum (Libera.Chat), charybdis,
ircd-ratbox and ircd-hybrid (EFnet). It needs no ircd of its own.

- **Every BBS user is on the network.** Whoever is online, on a terminal,
  a 3270, the web, [IRC](IRC), [AIM or ICQ](AIM-and-ICQ), is a user there
  under their name (`Carol Ann` is `Carol_Ann`). Anyone on the network can
  `/msg` and `/whois` them; the message reaches them however they came.
- **The rooms beginning `irc-` are the network's channels.** `irc-retro`
  is `#retro`: whoever is in the room is in the channel, and talk in one is
  talk in the other. The network's users are in the room's member list.
- **A taken nick is never fought over.** When a BBS user comes on and
  their nick is in use on the network, or a link comes up with someone
  there by that nick, the BBS user is renamed `Nick|Tag`, Tag being the
  BBS's short tag (`alice|DM`). It is cut to fit the network's nick length,
  and numbered (`alice|DM2`) if that too is taken. The BBS user's name on
  the BBS does not change.

```ini
[irclink]
server_name = irc.bbs.example.org
sid = 1BB
tag = DM
address = :7000

[ircpeer hub.example.net]
address = hub.example.net:7000
send_password = ours-to-it
accept_password = its-to-us
```

| Setting | Default | What it sets |
|---|---|---|
| `server_name` | (required) | the BBS's server name on the network |
| `sid` | (required) | its TS6 server ID: a digit and two letters or digits, unique on the network |
| `description` | the BBS's name | its description, in `LINKS` |
| `network` | the BBS's name | the network's name, as `WHOIS` of its users shows it on the BBS |
| `tag` | the BBS name's first letters | `Nick\|Tag` for a BBS user whose nick is taken |
| `host` | `server_name` | the BBS users' host, as the network sees them (`alice!bbs@host`) |
| `prefix` | `irc-` | the rooms that are channels (`none`: every room but `sysop-` ones) |
| `nick_length` | `30` | the network's `NICKLEN` (Solanum's is 31 less one; ratbox and hybrid: 9) |
| `address` | `:7000` | where servers link in (`none`: nowhere) |
| `tls_address`, `tls_cert`, `tls_key` | (none) | servers linking in by TLS |
| `retry` | `30` | seconds between tries of a link out |

Each server the BBS links with directly has an `[ircpeer NAME]`, its name
being the server's:

| Setting | Default | What it sets |
|---|---|---|
| `address` | (none: it links in) | where to link out to; the BBS links again whenever the link drops |
| `tls` | `no` | link out by TLS |
| `tls_verify` | `yes` | check its certificate (`no` for a self-signed one) |
| `send_password` | (required) | the password the BBS sends (its `accept_password`) |
| `accept_password` | (required) | the password it must send (its `send_password`) |
| `tag` | its name's first letters | its users' tag, beside a BBS user of the same nick |
| `protocol` | `ts6` | `ts6` (Solanum, charybdis, ratbox) or `hybrid` (ircd-hybrid 8, whose TS6 differs); one linking in is told apart by its handshake |

The other side's `connect` block names the BBS's `server_name`, with the
passwords the other way round. For Solanum:

```
connect "irc.bbs.example.org" {
	host = "192.0.2.10";
	port = 7000;
	send_password = "its-to-us";
	accept_password = "ours-to-it";
	class = "server";
	flags = topicburst;
};
```

A server name, SID, password or address that is wrong, or a server already
on the network by another way (a loop), is refused with an `ERROR` that
says why.

## On the BBS

- In the chat, `/msg NICK TEXT` sends to someone on the network (`/msg 3
  TEXT` is still node 3).
- On the BBS's own [IRC server](IRC), the network's users are in the
  linked channels' `NAMES` and `WHO`, and `/msg`, `/whois` and `ISON` reach
  them as they reach BBS users.
- Someone on the network with the nick of a BBS user online is shown on
  the BBS as `Nick|Tag`, with their server's tag (`alice|hub`), so the two
  are never confused; `/msg alice|hub` reaches them.
- Topics set on the network are told to the room.

## As a hub

The BBS keeps the network's map: which servers are behind which link,
their users and channels. A server linking in is given everything not
behind it: the servers (`SID`), users (`EUID`, or `UID`), channels
(`SJOIN`) and topics (`TB`). What comes from one link goes on to the
others; a private message goes only toward the one it is for. When a link
drops, the servers behind it are gone from the network (a netsplit): the
rooms are told those users left, and the other links get the `SQUIT`.

The BBS sends its burst once the other side's has come (the answer to its
`PING`), so a BBS user whose nick the other side has is renamed `Nick|Tag`
before they are sent. When two users of other servers collide, the TS6
rules apply: the newer is killed, or both if they are as old. A BBS user
killed off the network (by an oper, or services) is back on it when they
next log in. Channel modes, bans and the like (`TMODE`, `BMASK`, `ENCAP`
...) are passed on as they come; the BBS sets none of its own.

The BBS answers `WHOIS`, `VERSION`, `MOTD`, `ADMIN`, `INFO`, `TIME` and
`LINKS` for itself, and passes those for other servers on.

## Testing against a real ircd

`internal/irclink`'s `TestLive` links to a real ircd when one is set up:
see the top of `live_test.go`. It has been run against Solanum (built from
its main branch) and ircd-hybrid 8.2.43.

**See also:** [IRC](IRC), [Chat and paging](Chat-and-Paging), [MRC](MRC)
