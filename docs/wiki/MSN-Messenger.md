The BBS has an MSN Messenger server (MSNP, the protocol of Microsoft's
MSN Messenger), beside its [IRC](IRC) and [AIM](AIM-and-ICQ) servers and
on the same chat. Someone signed in with MSN Messenger 1.0 to 7.5 or
Windows Messenger 4.x sees their BBS friends as contacts and IMs anyone on
the BBS: other MSN users, IRC and AIM users, and callers on telnet, SSH,
3270s and the web terminal.

```ini
[msn]
address = :1863
host = msn.example.org:1863
domain = msn.example.org
; for MSN Messenger 5.0 to 7.5
https_address = :443
tls_cert = /etc/bbs/passport.crt
tls_key = /etc/bbs/passport.key
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
| `https_address` | none | where the [Passport server](#msn-messenger-50-to-75-passport) listens; without it, MSN 5.0 and later are not served |
| `tls_cert`, `tls_key` | none | its certificate, for `nexus.passport.com`, `login.passport.com` and `login.live.com` |
| `passport_host` | the name the client asked the nexus at | where clients are sent to log in (`host[:port]`) |
| `http_address` | none | the Passport server without TLS, behind a proxy that does it |

One port serves the messaging: sign-in, the contact list and presence
(the "notification server"), and the conversations ("switchboards").
MSN Messenger 5.0 and later also log in at a Passport server over
HTTPS, which the BBS serves on `https_address`.

## Versions served

| Protocol | Clients | Sign-in |
|---|---|---|
| MSNP2 to MSNP7 | MSN Messenger 1.0 to 4.7, Windows Messenger 4.x | MD5 (MSN 1.0: the password itself) |
| MSNP8 to MSNP11 | MSN Messenger 5.0 to 7.0 | Passport 1.4 (`login2.srf`), over HTTPS |
| MSNP12 | MSN Messenger 7.5 | Passport SOAP (`RST.srf`), over HTTPS |

Third-party clients that speak these versions work too. Windows Live
Messenger (MSNP13 and later) is not served yet: its address book and
sign-in are web services of their own. A client asking only for versions
not served is told so, and the log says why (without `https_address`,
MSNP8 and later are not served).

None of these has been tried with the real clients yet; the tests use
scripted ones.

## Pointing a client at the BBS

The clients were made for Microsoft's servers, so each must be pointed
at the BBS:

- With the BBS's [DNS server](DNS) (`[dns]`), the callers' machines that
  use it are sent here for all of these names, with no hosts file.
- All of them: the server is `messenger.hotmail.com`, so a hosts file
  entry sending it to the BBS points any of them here.
- MSN Messenger 1.0 to 4.7: the `Server` value (a string) under
  `HKEY_CURRENT_USER\Software\Microsoft\MessengerService` in the
  registry, set to the BBS's host (with `:1863` if the port is another).
  A hosts file entry sending `messenger.hotmail.com` to the BBS may also
  work. Neither has been tried with the real clients yet.
- Third-party clients (aMSN, Miranda, Trillian and others) take a server
  and port in the account's settings.

The deny list and bans in `[security]` apply as they do everywhere else.

## MSN Messenger 5.0 to 7.5: Passport

From MSN Messenger 5.0 the client gives its password not to the
messaging server but to Passport, over HTTPS, and hands the messaging
server the ticket Passport gave it. The BBS is that Passport server too
when `https_address` is set, and checks the password as any login's:

- `nexus.passport.com/rdr/pprdr.asp` says where to log in
  (`passport_host`, or the name the client asked at).
- `login.passport.com/login2.srf` (Passport 1.4) logs in MSN 5.0 to 7.0.
- `login.live.com/RST.srf` (Passport SOAP) logs in MSN 7.5.

A ticket is good once, for ten minutes, for the account it was given to.

The machine running the client must send those three names to the BBS
(its hosts file) and trust `tls_cert` for them. No public certificate
authority signs for Microsoft's names, so make a small certificate
authority of your own, sign a certificate for the three names with it,
and add the authority's certificate (`ca.crt`) to the machine's trusted
roots:

```sh
openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout ca.key -out ca.crt -subj "/CN=BBS Passport CA"
openssl req -newkey rsa:2048 -nodes -keyout passport.key \
  -out passport.csr -subj "/CN=login.passport.com"
printf 'subjectAltName=DNS:nexus.passport.com,DNS:login.passport.com,DNS:login.live.com\n' > san.txt
openssl x509 -req -in passport.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -days 3650 -sha256 -extfile san.txt -out passport.crt
```

The hosts file entries, with the BBS at 192.0.2.1:

```
192.0.2.1 messenger.hotmail.com nexus.passport.com login.passport.com login.live.com
```

 Windows XP
speaks only TLS 1.0 with old ciphers (3DES, RC4); the Passport server
takes those as well as current ones. With a proxy doing TLS in front of
the BBS, set `http_address` instead and send the proxy's requests there.

## Signing in

Every MSN user is a BBS user. They sign in with `name@domain` as the
e-mail address (a name with spaces may be typed with or without them,
`CarolAnn@msn.example.org`) and their BBS password. The domain part is not
checked, so a client that insists on `@hotmail.com` (MSN 1.0) works too.
Failed sign-ins count towards the throttle and the hack log, like any
other login, and the user's access level must allow chat. One account is
signed in on one MSN client at a time: signing in again signs the older
one out.

MSN Messenger 5.0 and later sign in by [Passport](#msn-messenger-50-to-75-passport),
which checks the password itself. MSN 1.0 to 4.7 send the password as an
MD5 digest, which the BBS's password
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
Groups made on MSN are kept with the friends in them: a group left
empty is gone at the next sign-in.

The block list is the user's [blocked list](Chat-and-Paging#blocking),
shared with the chat's `/block`: a blocked user's messages are refused
however they come, and the user appears offline to them. "Only people on
my Allow List can see my status" shows the user offline to those not on
their friends list.

A contact is online while they are on the BBS by any route: with the
status their MSN client set (busy, away, appearing offline...), or plain
online on any other route. Between MSN clients 5.0 and later, display
pictures and the personal message (MSN 7.0 and later) pass as they did.

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
