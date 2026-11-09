The BBS has a Yahoo! Messenger server (YMSG, the protocol of Yahoo's
pager), beside its [IRC](IRC), [AIM](AIM-and-ICQ) and
[MSN](MSN-Messenger) servers and on the same chat. Someone signed in with
Yahoo! Messenger sees their BBS friends as buddies, IMs anyone on the BBS
(other Yahoo users, IRC, AIM and MSN users, and callers on telnet, SSH,
3270s and the web terminal), and joins the BBS's chat rooms as Yahoo chat
rooms.

```ini
[ymsg]
address = :5050
http_address = :80
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:5050` | where the pager listens |
| `http_address` | none | the web half (the capacity lookup and the room list), plain HTTP |
| `https_address` | none | the web half over HTTPS: the login of Y!M 9 and later |
| `tls_cert`, `tls_key` | none | the certificate for `https_address` (for `login.yahoo.com`) |
| `host` | the address each client asked on | the pager's address the capacity lookup gives; set it behind NAT |
| `max_clients` | `256` | connections at once |
| `max_per_ip` | `4` | from one address |
| `system` | `bbs` | the Yahoo ID the BBS's notices come from |

## Versions served

YMSG 9 to 16. Two logins are served:

- **The challenge login** (YMSG 9 to 15): Yahoo! Messenger 5.x, and
  third-party clients of that time (Gaim, Kopete, libyahoo2's), which
  take the login type the server offers. Yahoo! Messenger 6 to 8 used a
  later challenge ("auth type 1") that is not served; whether they take
  the older one when it is offered has not been tried yet.
- **The token login** (YMSG 16): Yahoo! Messenger 9 and later, and
  the later Pidgin 2.x releases. The client sends the password over HTTPS to
  `login.yahoo.com`, gets a token, and proves it on the pager. This
  needs `https_address`.

## Pointing a client at the BBS

The clients were made for Yahoo's servers, so each must be pointed at
the BBS. None of this has been tried with the real clients yet.

- Yahoo! Messenger 5 to 8: under `HKEY_CURRENT_USER\Software\Yahoo\Pager`
  in the registry, set the string values `socket server` and
  `IPLookup` to the BBS's host (community guides also set `Conn Server`
  and `Host Name`). Or send `scs.msg.yahoo.com` and
  `scsa.msg.yahoo.com` to the BBS in the machine's hosts file.
- Yahoo! Messenger 9 and later: the hosts file sends
  `vcs1.msg.yahoo.com`, `vcs2.msg.yahoo.com`, `scsa.msg.yahoo.com` and
  `login.yahoo.com` to the BBS; the machine must trust the certificate
  given in `tls_cert` for `login.yahoo.com` (make a small certificate
  authority of your own and add its certificate to the machine's trusted
  roots). The HTTPS server takes the TLS 1.0 and old ciphers Windows XP
  has.
- `insider.msg.yahoo.com` sent to the BBS gives the chat room browser
  the BBS's rooms.
- Third-party clients (Pidgin, Kopete, Miranda, Trillian) take a pager
  server in the account's settings.

The deny list and bans in `[security]` apply as they do everywhere else.

## Signing in

Every Yahoo user is a BBS user. Their Yahoo ID is their BBS name in lower
case without its spaces (`carolann` is Carol Ann), and the password is
their BBS password. Failed sign-ins count towards the throttle and the
hack log, like any other login, and the user's access level must allow
chat. One account is signed in on one Yahoo client at a time: signing in
again signs the older one out.

The challenge login proves the password by two digests of it, which the
BBS's password hashes can't check. So the BBS keeps each account's Yahoo
digests too, made whenever the password is known: when the account is
made, when its password is set, and at any login by password. An account
from before `[ymsg]` was set up has none yet: its user logs in once some
other way (telnet, SSH, IRC, AIM, the web) and can then sign in with
Yahoo. The log says so when that is why a sign-in failed. The digests are
kept only while `[ymsg]` is set up; anyone who read them from the
database could sign in by Yahoo as that user. The token login checks the
password itself, as any login does. The pager's connection is not
encrypted.

## Buddies

The buddy list is the user's [friends list](Chat-and-Paging#friends),
the same list the BBS's chat, IRC, AIM and MSN show, with its groups
(`Friends` for friends in none). A buddy is always a BBS user: adding
anyone else is refused, and the BBS says so. Adding a buddy asks no one's
permission. A friend added or moved elsewhere comes to a client that is
on at once, and one added, moved or removed on Yahoo is changed
everywhere.

The ignore list is the user's [blocked list](Chat-and-Paging#blocking):
a blocked user's messages are refused however they come, and the user
appears offline to them.

A buddy is online while they are on the BBS by any route: with the
status their Yahoo client set (busy, be right back, a custom message,
invisible), or, on another route, with "on the BBS by telnet" (or IRC,
AIM, MSN...) as their status message.

## IMs

An IM to a BBS user is a [private
message](Chat-and-Paging#private-messages): a `PRIVMSG` on IRC, an IM on
AIM or MSN, a message line on a terminal. Yahoo's colours and fonts are
taken off. Typing notices pass between Yahoo clients only.

Every line is kept. One to a user who isn't on is kept for them; they
get it when they next log in. Lines kept for a Yahoo user come when they
sign in, marked with when they were sent. The BBS's notices (new mail, a
page for the sysop, errors) come from the `system` ID.

Sending files is not served: the client is told to use the BBS's file
areas.

## Chat rooms and conferences

A Yahoo chat room is a BBS [chat room](Chat-and-Paging): "Linux, FreeBSD,
Solaris:1" is the room `linux-freebsd-solari` (lower case, letters and
digits, the lobby number dropped, at most 20 characters), the same room
IRC calls `#linux-freebsd-solari`. The room browser lists the rooms in
use and the permanent ones.

A conference (Yahoo's invited group IM) is a BBS room of its own, `yc-`
and the conference's name. Yahoo users invited are asked by their
clients; anyone else invited gets a private message naming the room,
which they join from wherever they are. Anyone told the room's name can
join it.
