The BBS has an MSN Messenger server (MSNP, the protocol of MSN Messenger
from Microsoft). This server is adjacent to the [IRC](IRC) and
[AIM](AIM-and-ICQ) servers of the BBS, and it uses the same chat. A user
who signs in with MSN Messenger 1.0 to 7.5 or Windows Messenger 4.x sees
BBS friends as contacts. The user can send IMs to all users on the BBS:
other MSN users, IRC and AIM users, and callers on telnet, SSH, 3270s and
the web terminal.

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
| `address` | `:1863` | the address on which to listen |
| `host` | the address to which each client came | the address to which the BBS sends clients for conversations (`host:port`). Set it behind NAT |
| `domain` | `bbs.invalid` | the handles of users are `name@domain` |
| `max_clients` | `256` | the maximum number of connections at one time |
| `max_per_ip` | `16` | the maximum from one address (each conversation is a connection) |
| `system` | `BBS` | the name from which the notices of the BBS come |
| `site` | none | a web address that the links of the client (inbox, profiles) open |
| `https_address` | none | the address on which the [Passport server](#msn-messenger-50-to-75-passport) listens. Without it, the BBS does not serve MSN 5.0 and later |
| `tls_cert`, `tls_key` | none | its certificate, for `nexus.passport.com`, `login.passport.com` and `login.live.com` |
| `passport_host` | the name at which the client asked the nexus | the address to which the BBS sends clients to log in (`host[:port]`) |
| `http_address` | none | the Passport server without TLS, behind a proxy that does the TLS |

One port serves the messages: sign-in, the contact list and presence
(the "notification server"), and the conversations ("switchboards"). MSN
Messenger 5.0 and later also log in at a Passport server over HTTPS. The
BBS serves this on `https_address`.

## Versions served

| Protocol | Clients | Sign-in |
|---|---|---|
| MSNP2 to MSNP7 | MSN Messenger 1.0 to 4.7, Windows Messenger 4.x | MD5 (MSN 1.0: the password itself) |
| MSNP8 to MSNP11 | MSN Messenger 5.0 to 7.0 | Passport 1.4 (`login2.srf`), over HTTPS |
| MSNP12 | MSN Messenger 7.5 | Passport SOAP (`RST.srf`), over HTTPS |

Third-party clients that use these versions also operate. The BBS does
not serve Windows Live Messenger (MSNP13 and later) yet: its address book
and sign-in are separate web services. If a client asks only for versions
that the BBS does not serve, the BBS tells the client, and the log gives
the reason (without `https_address`, the BBS does not serve MSNP8 and
later).

We did not try these with the real clients yet. The tests use scripted
clients.

## Pointing a client at the BBS

The clients were made for the servers of Microsoft. Thus, you must point
each client at the BBS:

- With the [DNS server](DNS) of the BBS (`[dns]`), the machines of
  callers that use it come here for all of these names, with no hosts
  file.
- All clients: the server is `messenger.hotmail.com`. Thus, an entry in
  the hosts file that sends this name to the BBS points all clients
  here.
- MSN Messenger 1.0 to 4.7: set the `Server` value (a string) under
  `HKEY_CURRENT_USER\Software\Microsoft\MessengerService` in the
  registry to the host of the BBS (with `:1863` if the port is
  different). An entry in the hosts file that sends
  `messenger.hotmail.com` to the BBS can also operate. We did not try
  these methods with the real clients yet.
- Third-party clients (aMSN, Miranda, Trillian and others) accept a
  server and port in the settings of the account.

The deny list and bans in `[security]` apply, as on all other routes.

## MSN Messenger 5.0 to 7.5: Passport

From MSN Messenger 5.0, the client does not give its password to the
messaging server. It gives it to Passport, over HTTPS. Then it gives the
messaging server the ticket from Passport. When `https_address` is set,
the BBS is also that Passport server. It checks the password as for all
logins:

- `nexus.passport.com/rdr/pprdr.asp` tells the client where to log in
  (`passport_host`, or the name at which the client asked).
- `login.passport.com/login2.srf` (Passport 1.4) logs in MSN 5.0 to 7.0.
- `login.live.com/RST.srf` (Passport SOAP) logs in MSN 7.5.

A ticket is valid one time, for ten minutes, for the account that got
it.

The machine that runs the client must send those three names to the BBS
(in its hosts file) and trust `tls_cert` for them. No public certificate
authority signs for the names of Microsoft. Thus, do these steps:

1. Make a small certificate authority of your own.
2. With it, sign a certificate for the three names.
3. Add the certificate of the authority (`ca.crt`) to the trusted roots
   of the machine.

```sh
openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout ca.key -out ca.crt -subj "/CN=BBS Passport CA"
openssl req -newkey rsa:2048 -nodes -keyout passport.key \
  -out passport.csr -subj "/CN=login.passport.com"
printf 'subjectAltName=DNS:nexus.passport.com,DNS:login.passport.com,DNS:login.live.com\n' > san.txt
openssl x509 -req -in passport.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -days 3650 -sha256 -extfile san.txt -out passport.crt
```

The entries in the hosts file, with the BBS at 192.0.2.1:

```
192.0.2.1 messenger.hotmail.com nexus.passport.com login.passport.com login.live.com
```

Windows XP uses only TLS 1.0 with old ciphers (3DES, RC4). The Passport
server accepts those, and also current ciphers. If a proxy does TLS in
front of the BBS, set `http_address` in place of `https_address`, and
send the requests of the proxy there.

## Signing in

Each MSN user is a BBS user. The user signs in with `name@domain` as the
e-mail address and with the BBS password. You can type a name with
spaces with or without the spaces (`CarolAnn@msn.example.org`). The BBS
does not check the domain part. Thus, a client that must have
`@hotmail.com` (MSN 1.0) also operates. Failed sign-ins count towards
the throttle and the hack log, as all other logins do. The access level
of the user must permit chat. One account can be signed in on one MSN
client at a time. A new sign-in signs out the older one.

MSN Messenger 5.0 and later sign in by
[Passport](#msn-messenger-50-to-75-passport), which checks the password
itself. MSN 1.0 to 4.7 send the password as an MD5 digest. The password
hashes of the BBS cannot check this digest. Thus, the BBS also keeps the
MSN digest of each account. It makes it each time that it knows the
password: when the account is made, when the password is set, and at all
logins by password. An account from before the configuration of `[msn]`
has no digest yet. Its user must log in one time by a different method
(telnet, SSH, IRC, AIM, the web). Then the user can sign in with MSN. If
this is the cause of a failed sign-in, the log tells you.

The BBS keeps the digest only while `[msn]` is configured. The MSN
challenge for an account is the same each time, as it was on the servers
of Microsoft. Thus, a person who saw a sign-in on the wire (or read the
digest from the database) can sign in by MSN as that user. The
connection is not encrypted.

## Contacts

The contact list is the [friends list](Chat-and-Paging#friends) of the
user. It is the same list that the chat of the BBS, IRC and AIM show,
with its groups (MSN Messenger 4.6 and later show them). A contact is
always a BBS user. The BBS refuses all other contacts. If you add a
friend on a different route, a client that is on gets it immediately. If
you add or remove a contact on MSN, the change applies on all routes. If
a user adds an MSN user, that user is on the reverse list of the MSN
user. Thus, the client asks "X has added you to their contact list". The
BBS keeps groups made on MSN with the friends in them. If a group is
empty, it is removed at the next sign-in.

The block list is the [blocked list](Chat-and-Paging#blocking) of the
user. The chat command `/block` uses the same list. The BBS refuses the
messages of a blocked user on all routes, and the user shows as offline
to the blocked user. "Only people on my Allow List can see my status"
shows the user as offline to the users who are not on the friends list.

A contact is online while the contact is on the BBS by any route. The
contact has the status that the MSN client of the contact set (busy,
away, appear offline...), or plain online on all other routes. Between
MSN clients 5.0 and later, display pictures and the personal message
(MSN 7.0 and later) go across as they did on the servers of Microsoft.

## Conversations

An IM to an MSN user opens a conversation on the client, as usual. An IM
to a BBS user on a different route puts that user in the conversation
immediately. The text goes to that user as a private message (a
`PRIVMSG` on IRC, an IM on AIM, a message line on a terminal). The
answers of that user come into the conversation. A private message from
a user who is not on MSN opens a conversation with that user on the
client. Conversations of more than one MSN user operate as they did on
the servers of Microsoft. Typing notices and invitations go between MSN
clients, not to other routes.

The BBS [keeps](Chat-and-Paging#private-messages) each line. If a user
is not on, the BBS keeps the line for the user, and tells the sender
(one time). The user gets it at the next login. Lines kept for an MSN
user come at the sign-in, in one conversation from each sender, with
the time that they were sent. The notices of the BBS (new mail, a page
for the sysop) come from the `system` name. The BBS refuses a call to a
user who is offline, as the servers of Microsoft did. To leave a message
for that user, use the conversation with the user while it is open, or
use a different route.
