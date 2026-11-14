The BBS can be a server on an IRC network: a hub that links to many
other servers, in and out, and routes between them. It uses TS6, the
server protocol of Solanum (Libera.Chat), charybdis, ircd-ratbox and
ircd-hybrid (EFnet). It also uses P10, the server protocol of ircu
(Undernet). It does not need its own ircd.

- **All BBS users are on the network.** All online users, on a terminal,
  a 3270, the web, [IRC](IRC), [AIM or ICQ](AIM-and-ICQ), are users
  there with their names (`Carol Ann` is `Carol_Ann`). All users on the
  network can `/msg` and `/whois` them. The message goes to them on the
  route that they use.
- **The rooms that start with `irc-` are the channels of the network.**
  `irc-retro` is `#retro`. The users in the room are in the channel, and
  text in one goes to the other. The users of the network are in the
  member list of the room.
- **The BBS never fights for a nick that is in use.** Sometimes a BBS
  user comes on and the nick is in use on the network. Or a link starts,
  and a user there has that nick. In these cases, the BBS gives the BBS
  user the name `Nick|Tag`, where Tag is the short tag of the BBS
  (`alice|DM`). The BBS cuts it to the nick length of the network. If
  that nick is also in use, the BBS adds a number (`alice|DM2`). The
  name of the BBS user on the BBS does not change.

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
| `server_name` | (required) | the server name of the BBS on the network |
| `sid` | (required) | its TS6 server ID: a digit and two letters or digits, unique on the network |
| `numeric` | (none) | its P10 server numeric, 0 to 4095, unique on the network (necessary for a P10 link) |
| `description` | the name of the BBS | its description, in `LINKS` |
| `network` | the name of the BBS | the name of the network, as the `WHOIS` of its users shows it on the BBS |
| `tag` | the first letters of the BBS name | `Nick\|Tag` for a BBS user whose nick is in use |
| `host` | `server_name` | the host of the BBS users, as the network sees them (`alice!bbs@host`) |
| `prefix` | `irc-` | the rooms that are channels (`none`: all rooms except `sysop-` rooms) |
| `nick_length` | `30` (`12` with a P10 peer) | the `NICKLEN` of the network (the value of Solanum is 31 minus one. ratbox and hybrid: 9. ircu: 12) |
| `address` | `:7000` | the address where servers link in (`none`: no address) |
| `tls_address`, `tls_cert`, `tls_key` | (none) | servers that link in by TLS |
| `retry` | `30` | seconds between tries of a link out |

Each server with a direct link to the BBS has an `[ircpeer NAME]`. Its
name is the name of the server:

| Setting | Default | What it sets |
|---|---|---|
| `address` | (none: it links in) | the address to link out to. The BBS links again each time that the link stops |
| `tls` | `no` | link out by TLS |
| `tls_verify` | `yes` | check its certificate (`no` for a self-signed certificate) |
| `send_password` | (required) | the password that the BBS sends (its `accept_password`) |
| `accept_password` | (required) | the password that it must send (its `send_password`) |
| `tag` | the first letters of its name | the tag of its users, adjacent to a BBS user with the same nick |
| `protocol` | `ts6` | `ts6` (Solanum, charybdis, ratbox), `hybrid` (ircd-hybrid 8, which has a different TS6) or `p10` (ircu). For a server that links in, the BBS finds the protocol from its handshake |

The `connect` block of the other side names the `server_name` of the
BBS, with the passwords in the opposite direction. For Solanum:

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

The BBS refuses an incorrect server name, SID, password or address. It
also refuses a server that is already on the network through a different
route (a loop). It sends an `ERROR` that gives the reason.

## With ircu (P10)

You link an ircu network in the same way. Set `numeric` in `[irclink]`
and `protocol = p10` on the peer. ircu has one password for a link, in
the two directions. Thus, `send_password` and `accept_password` are the
same. Its `Connect` block names the BBS. It must have `hub;` if the BBS
will bring other servers behind it:

```ini
[irclink]
server_name = irc.bbs.example.org
sid = 1BB
numeric = 42

[ircpeer hub.undernet.example]
address = hub.undernet.example:4400
protocol = p10
send_password = linkpass
accept_password = linkpass
```

```
Connect {
 name = "irc.bbs.example.org";
 host = "192.0.2.10";
 password = "linkpass";
 port = 7000;
 class = "Server";
 hub;
};
```

The users of ircu come with their accounts (`+r`), as `WHOIS` on the BBS
shows them. The users of the BBS go to ircu with their accounts. The
nick length of ircu is 12, unless its `NICKLEN` feature gives a
different value. Set `nick_length` to the same value. If not, ircu
refuses a longer nick.

The BBS keeps the network by the TS6 names. Thus, it can link TS6 and
P10 servers at the same time. Each server gets the servers, users and
channels of the other in its own protocol. Between two P10 servers, each
line goes as it came. Between P10 and TS6, only the items that both
protocols have go across: users, nicks, channels and their members and
ops, topics, talk, private messages, `AWAY`, `KILL`, `WHOIS` and the
other queries. Channel modes go across as `TMODE` and `MODE`, and bans
from TS6. G-lines, jupes and the `CLEARMODE`, `OPMODE` and `ACCOUNT` of
ircu stay on the P10 side. The `ENCAP` of TS6 stays on its side.

## On the BBS

- In the chat, `/msg NICK TEXT` sends to a user on the network (`/msg 3
  TEXT` is still node 3).
- On the [IRC server](IRC) of the BBS, the users of the network are in
  the `NAMES` and `WHO` of the linked channels. `/msg`, `/whois` and
  `ISON` go to them as they go to BBS users.
- If a user on the network has the nick of an online BBS user, the BBS
  shows that user as `Nick|Tag`, with the tag of their server
  (`alice|hub`). Thus, the two users are never confused. `/msg alice|hub`
  goes to them.
- The BBS tells the room the topics that users set on the network.

## As a hub

The BBS keeps the map of the network: the servers behind each link, and
their users and channels. A server that links in gets all items that
are not behind it: the servers (`SID`), users (`EUID`, or `UID`),
channels (`SJOIN`) and topics (`TB`). Data from one link goes on to the
other links. A private message goes only in the direction of its
recipient. When a link stops, the servers behind it are no longer on the
network (a netsplit). The BBS tells the rooms that those users left, and
the other links get the `SQUIT`.

The BBS sends its burst after the burst of the other side arrives (the
answer to its `PING`. For P10, `END_OF_BURST`). Thus, if the other side
has the nick of a BBS user, the BBS gives that user the name `Nick|Tag`
before it sends the user. When two users of other servers collide, the
TS6 rules apply: the server kills the newer user, or both users if they
have the same age. If an oper or services kill a BBS user from the
network, the user comes back on the network at the next login. The BBS
passes on channel modes, bans and similar items (`TMODE`, `BMASK`,
`ENCAP` ...) as they come. The BBS sets no modes of its own.

The BBS answers `WHOIS`, `VERSION`, `MOTD`, `ADMIN`, `INFO`, `TIME` and
`LINKS` for itself. It passes on these requests for other servers.

## Testing against a real ircd

The `TestLive` of `internal/irclink` links to a real ircd if you
configure one. Refer to the top of `live_test.go`. We ran it against
Solanum (built from its main branch), ircd-hybrid 8.2.43 and ircu
2.10.12.19 (built from the `ircu2` of UndernetIRC, with links in and
out, with `IRCLINK_LIVE_PROTOCOL=p10`).

**See also:** [IRC](IRC), [Chat and paging](Chat-and-Paging), [MRC](MRC)
