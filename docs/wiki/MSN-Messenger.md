The BBS has an MSN Messenger server (MSNP, the protocol of Microsoft's
MSN Messenger), beside its [IRC](IRC) and [AIM](AIM-and-ICQ) servers and
on the same chat. Someone signed in with MSN Messenger 1.0 to 4.7 or
Windows Messenger 4.x sees their BBS friends as contacts and IMs anyone on
the BBS: other MSN users, IRC and AIM users, and callers on telnet, SSH,
3270s and the web terminal.

```ini
[msn]
address = :1863
host = msn.example.org:1863
domain = msn.example.org
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:1863` | where to listen |
| `host` | the address each client came to | where clients are sent for conversations (`host:port`); set it behind NAT |
| `domain` | `bbs.invalid` | users' handles are `name@domain` |
| `max_clients` | `256` | connections at once |
| `max_per_ip` | `16` | from one address (each conversation is a connection) |
| `system` | `BBS` | the name the BBS's notices come from |
| `site` | none | a web address the client's links (inbox, profiles) open |

One port serves the whole of it: sign-in, the contact list and presence
(the "notification server"), and the conversations ("switchboards").

## Versions served

MSNP2 to MSNP7: MSN Messenger 1.0 to 4.7, Windows Messenger 4.0 to 4.7,
and third-party clients that speak those versions. They sign in by MD5
(MSN 1.0 by the password itself) over plain TCP. MSN Messenger 5.0 and
later, and Windows Live Messenger, sign in by Passport over HTTPS, which
is not served yet: they are told the version is not supported, and the
log says so.

## Pointing a client at the BBS

The clients were made for Microsoft's servers, so each must be pointed
at the BBS:

- MSN Messenger 1.0 to 4.7: the `Server` value (a string) under
  `HKEY_CURRENT_USER\Software\Microsoft\MessengerService` in the
  registry, set to the BBS's host (with `:1863` if the port is another).
  A hosts file entry sending `messenger.hotmail.com` to the BBS may also
  work. Neither has been tried with the real clients yet.
- Third-party clients (aMSN, Miranda, Trillian and others) take a server
  and port in the account's settings.

The deny list and bans in `[security]` apply as they do everywhere else.

## Signing in

Every MSN user is a BBS user. They sign in with `name@domain` as the
e-mail address (a name with spaces may be typed with or without them,
`CarolAnn@msn.example.org`) and their BBS password. The domain part is not
checked, so a client that insists on `@hotmail.com` (MSN 1.0) works too.
Failed sign-ins count towards the throttle and the hack log, like any
other login, and the user's access level must allow chat. One account is
signed in on one MSN client at a time: signing in again signs the older
one out.

MSN clients send the password as an MD5 digest, which the BBS's password
hashes can't check. So the BBS keeps each account's MSN digest too, made
whenever the password is known: when the account is made, when its
password is set, and at any login by password. An account from before
`[msn]` was set up has none yet: its user logs in once some other way
(telnet, SSH, IRC, AIM, the web) and can then sign in with MSN. The log
says so when that is why a sign-in failed. The digest is kept only while
`[msn]` is set up. MSN's challenge for an account is the same each time,
as it was on Microsoft's servers, so anyone who saw a sign-in on the wire
(or read the digest from the database) could sign in by MSN as that
user. The connection is not encrypted.

## Contacts

The contact list is the user's [friends list](Chat-and-Paging#friends),
the same list the BBS's chat, IRC and AIM show, with its groups (MSN
Messenger 4.6 and later show them). A contact is always a BBS user;
adding anyone else is refused. A friend added elsewhere comes to a
client that is on at once, and one added or removed on MSN is on or off
the list everywhere. Someone who adds an MSN user is on their reverse
list, so the client asks "X has added you to their contact list".

The block list is the user's [blocked list](Chat-and-Paging#blocking),
shared with the chat's `/block`: a blocked user's messages are refused
however they come, and the user appears offline to them. "Only people on
my Allow List can see my status" shows the user offline to those not on
their friends list.

A contact is online while they are on the BBS by any route: with the
status their MSN client set (busy, away, appearing offline...), or plain
online on any other route.

## Conversations

An IM to an MSN user opens a conversation on their client as usual. An
IM to a BBS user on another route puts them in the conversation at once:
what is said reaches them as a private message (a `PRIVMSG` on IRC, an
IM on AIM, a message line on a terminal), and what they send back comes
into it. A private message from someone not on MSN opens a conversation
with them on the client. Conversations of several MSN users work as they
did on Microsoft's servers; typing notices and invitations pass between
MSN clients, not to other routes.

Every line is [kept](Chat-and-Paging#private-messages). One to a user who
isn't on is kept for them, and the sender told so (once); they get it
when they next log in. Lines kept for an MSN user come when they sign in,
in a conversation from each sender, marked with when they were sent. The
BBS's notices (new mail, a page for the sysop) come from the `system`
name. Calling someone who is offline is refused, as on Microsoft's
servers: to leave a message for them, use their conversation while it is
open, or another route.
