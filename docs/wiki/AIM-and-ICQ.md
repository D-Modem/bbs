The BBS has an AIM and ICQ server (OSCAR, the protocol AOL's AIM and ICQ
clients spoke), beside its [IRC](IRC) server and on the same chat. Someone
signed on in AIM 5, AIM 4, ICQ 2003, Pidgin, Miranda or any other OSCAR
client sees the BBS's users on their buddy list, IMs them and talks in its
[chat rooms](Chat-and-Paging), with callers on telnet, SSH, 3270s, IRC,
the web terminal and the API.

```ini
[oscar]
address = :5190
host = aim.example.org:5190
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:5190` | where to listen |
| `host` | the address each client came to | where clients are sent to reconnect (`host:port`); set it behind NAT |
| `max_clients` | `256` | connections at once |
| `max_per_ip` | `16` | from one address (each client opens a few) |
| `uin_base` | `100000` | an account's ICQ number is this plus its number |
| `system` | `OnlineHost` | the screen name the BBS's notices come from |

Point the client at the BBS: in AIM, the "Host" under Connection settings
(`login.oscar.aol.com` was AOL's, so a hosts file entry also works); in
Pidgin, the server and port of an AIM or ICQ account, with "Use clientLogin"
unticked and encryption off (clientLogin and SSL were AOL's later web
sign-on, which this server does not serve, nor AIM 6's). The deny list and
bans in `[security]` apply as they do everywhere else.

## Signing on

Every AIM user is a BBS user, signed on with their BBS name as the screen
name and their BBS password. A name with spaces may be typed with or
without them (`Carol Ann` or `carolann`). ICQ clients sign on by number:
`uin_base` plus the account's number (with the default, account 7 is
`100007`). Failed logins count towards the throttle and the hack log, like
any other login, and the user's access level must allow chat.

AIM clients since 3.5 send the password as an MD5 digest, which the BBS's
password hashes can't check. So the BBS keeps each account's AIM digests
too, made whenever the password is known: when the account is made, when
its password is set, and at any login by password. An account from before
`[oscar]` was set up has none yet: its user logs in once some other way
(telnet, SSH, IRC, the web) and can then sign on with any AIM client. The
log says so when that is why a sign-on failed. Old clients (AIM 1 to 3, ICQ,
Java AIM) send the password itself and sign on at once. The digests are
kept only while `[oscar]` is set up; anyone who read them from the database
could sign on by AIM as that user, as with any AIM server.

## Buddies

The buddy list is the user's [friends list](Chat-and-Paging#friends):
the same list the BBS's chat and IRC show, so a buddy is always a BBS
user (by name, or ICQ number). Adding anyone else is refused. A friend
added by IRC or the BBS comes to an AIM client that is on at once, in
their group (`Friends` if they have none); one added or removed on AIM
is on or off the list everywhere. Groups and aliases are kept with the
friends list; the list's other items (permit and deny entries,
preferences) as the client sent them. A client that keeps its own list
(AIM 3 and 4) has its buddies put on the friends list as it sends them.

A buddy is online while they are on the BBS by any route, and shown away
when they are away on AIM or IRC. Their profile is the one their AIM
client set, or for someone not on AIM their line about themselves, their
location and what they are doing on which node. Permit and deny lists,
"allow only buddies" and ICQ's invisible status hide the user and refuse
their IMs, from AIM users and others alike.

## IMs

An IM to a BBS user reaches every node they are on: an AIM or ICQ client
gets it as an IM (from AIM to AIM with its fonts and colours), an IRC client
as a `PRIVMSG`, a caller on a terminal as a message line. What they send
back comes as an IM from them. Typing notices, file transfers and direct
connections pass between AIM clients. IMs are [kept](Chat-and-Paging#private-messages).
An IM to a BBS user who isn't on is kept for them, and the sender told so
(once) by `OnlineHost`; they get it when they next sign on: on AIM as an
IM marked with when it was sent, on ICQ as an offline message, on IRC or
a terminal as a message line. An IM to a name that is no BBS user's is
refused ("not logged on"). The BBS's notices (new mail, a page for the sysop) come as IMs
from `OnlineHost`.

## Chat rooms

An AIM chat room is a BBS chat room, and so an IRC channel: "Join a Chat
Room" with `main` is in `#main` with IRC users and the terminals' Chat. A
name is made a room's: lower case, spaces as dashes, at most 20 letters
and digits (`Test Room` is `test-room`). Inviting a buddy who isn't on AIM
sends them a line naming the room.

## Port 5190 and AOL

5190 was AOL's own port before it was AIM's. A connection that begins as
an AOL client's (a P3 packet) is told apart by its first byte, so an AOL
front end can share the port; until there is one, such a connection is
logged and closed.
