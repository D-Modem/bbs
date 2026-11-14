The BBS has an AIM and ICQ server (OSCAR, the protocol of the AIM and
ICQ clients of AOL). This server is adjacent to the [IRC](IRC) server of
the BBS, and it uses the same chat. A user can sign on in AIM 7, AIM 5,
AIM 4, ICQ 2003, Pidgin, Miranda or a different OSCAR client. That user
sees the users of the BBS on the buddy list, sends IMs to them, and
talks in the [chat rooms](Chat-and-Paging) of the BBS. The other users
in these rooms can be on telnet, SSH, 3270s, IRC, the web terminal and
the API.

```ini
[oscar]
address = :5190
host = aim.example.org:5190
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:5190` | the address on which to listen |
| `host` | the address to which each client came | the address to which the BBS sends clients to connect again (`host:port`). Set it behind NAT |
| `max_clients` | `256` | the maximum number of connections at one time |
| `max_per_ip` | `16` | the maximum from one address (each client opens some connections) |
| `uin_base` | `100000` | the ICQ number of an account is this value plus the account number |
| `system` | `OnlineHost` | the screen name from which the notices of the BBS come |
| `tls_address` | none | a TLS port for the [later logins](#later-aim-versions), for example `:443` |
| `tls_cert`, `tls_key` | none | its certificate and key (`tls_cert = acme` for the certificate of `[acme]`) |
| `tls_host` | the name of `host` at the port of `tls_address` | the address to which the BBS sends clients that ask for TLS (`host:port`). The address of a TLS proxy, when there is one in front |

Point the client at the BBS:

- In AIM up to 6.1, set the "Host" in the Connection settings.
  `login.oscar.aol.com` was the host of AOL. Thus, an entry in the hosts
  file, or the [DNS server](DNS) of the BBS, also operates.
- In Pidgin, set the server and port of an AIM or ICQ account. Clear
  "Use clientLogin" and disable encryption. Or, to use clientLogin,
  refer to [later AIM versions](#later-aim-versions).

The deny list and bans in `[security]` apply, as on all other routes.

## Signing on

Each AIM user is a BBS user. The user signs on with the BBS name as the
screen name, and with the BBS password. You can type a name with spaces
with or without the spaces (`Carol Ann` or `carolann`). ICQ clients sign
on by number: `uin_base` plus the account number (with the default,
account 7 is `100007`). Failed logins count towards the throttle and the
hack log, as all other logins do. The access level of the user must
permit chat.

AIM clients since 3.5 send the password as an MD5 digest. The password
hashes of the BBS cannot check this digest. Thus, the BBS also keeps the
AIM digests of each account. It makes them each time that it knows the
password: when the account is made, when the password is set, and at all
logins by password. An account from before the configuration of
`[oscar]` has no digests yet. Its user must log in one time by a
different method (telnet, SSH, IRC, the web). Then the user can sign on
with all AIM clients. If this is the cause of a failed sign-on, the log
tells you. Old clients (AIM 1 to 3, ICQ, Java AIM) send the password
itself, and sign on immediately.

The BBS keeps the digests only while `[oscar]` is configured. A person
who reads them from the database can sign on by AIM as that user, as
with all AIM servers.

## Buddies

The buddy list is the [friends list](Chat-and-Paging#friends) of the
user. It is the same list that the chat of the BBS and IRC show. Thus, a
buddy is always a BBS user (by name, or ICQ number). The BBS refuses all
other buddies. If you add a friend by IRC or the BBS, an AIM client that
is on gets it immediately, in its group (`Friends` if it has no group).
If you add or remove a buddy on AIM, the change applies on all routes.
The BBS keeps groups and aliases with the friends list. It keeps the
other items of the list (permit and deny entries, preferences) as the
client sent them. If a client keeps its own list (AIM 3 and 4), the BBS
puts its buddies on the friends list when the client sends them.

A buddy is online while the buddy is on the BBS by any route. The buddy
shows as away when the buddy is away on AIM or IRC. The profile of the
buddy is the profile that the AIM client of the buddy set. For a user
who is not on AIM, the profile is the line about the user, the location,
and the current activity on the node. Permit and deny lists, "allow only
buddies" and the invisible status of ICQ hide the user. They also
refuse IMs to the user, from AIM users and from other users.

## IMs

An IM to a BBS user goes to all nodes where the user is on:

- An AIM or ICQ client gets it as an IM (from AIM to AIM with its fonts
  and colours).
- An IRC client gets it as a `PRIVMSG`.
- A caller on a terminal gets it as a message line.

The answers come as an IM from that user. Typing notices, file transfers
and direct connections go between AIM clients. The BBS
[keeps](Chat-and-Paging#private-messages) IMs. If a BBS user is not on,
the BBS keeps an IM for the user, and `OnlineHost` tells the sender
(one time). The user gets the IM at the next sign-on: on AIM as an IM
with the time that it was sent, on ICQ as an offline message, on IRC or
a terminal as a message line. The BBS refuses an IM to a name that is
not a BBS user ("not logged on"). The notices of the BBS (new mail, a
page for the sysop) come as IMs from `OnlineHost`.

## Chat rooms

An AIM chat room is a BBS chat room, and thus an IRC channel. "Join a
Chat Room" with `main` puts the user in `#main` with IRC users and the
Chat of the terminals. The BBS changes a name into a room name: lower
case, dashes in place of spaces, a maximum of 20 letters and digits
(`Test Room` is `test-room`). If you invite a buddy who is not on AIM,
the BBS sends the buddy a line with the name of the room.

## Later AIM versions

The later clients of AOL sign on by HTTP before they connect to the
server. The BBS serves the two methods that they use. It serves them on
port 5190 (the BBS knows a connection that starts as an HTTP request by
its first byte) and on `tls_address`:

- **AIM 6.2 to 7.5** post a Kerberos-style login to `/`. In Settings,
  Connection, set the host and port of the client to the TLS address of
  the BBS. These clients ask for HTTPS and then a TLS connection. Thus,
  `tls_address` is necessary. The client checks the certificate against
  its own store (an NSS database in its folder). Thus, you must have a
  certificate from a public CA, or add the certificate of the BBS to
  that store.
- **AIM 6.0 and 6.1** cannot use a different server for this login. Set
  their `serviceManifest.xml` to the old (BUCP) login. Then they sign on
  as AIM 5 does.
- **Pidgin with "Use clientLogin"**, AIM Express and the AIM of the
  phones ask `api.screenname.aol.com` (`/auth/clientLogin`) and then
  `api.oscar.aol.com` (`/aim/startOSCARSession`). Point those names at
  the BBS in the hosts file. With HTTPS, they must have a certificate
  for those names that the client trusts. In all cases, the client then
  signs on to the host that the BBS names. It uses TLS if it asked for
  it ("Use SSL") and if there is a TLS host.

These logins send the password itself (inside TLS). Thus, they operate
for all accounts, without the AIM digests above.

Incorrect passwords count against the throttle and the hack log, as all
other logins do. The BBS does not use the session secret from
clientLogin to check the requests that follow. The token is a one-time
sign-on cookie, valid for five minutes.

### AIM 6.2 and later, and TLS

AIM 6.2 to 7.5 open TLS with a hello in SSL 2.0 format. They use only
TLS 1.0 with RSA key exchange. The TLS of Go cannot read that hello.
Thus, the BBS answers those clients with
[retrossl](https://github.com/D-Modem/retrossl), the SSL 3.0 and TLS 1.0
server of D-Modem (AES, 3DES, RC4 or DES, by RSA). It does the same for
clients that offer only SSL 3.0. All other clients get the TLS of Go.
Thus, `tls_cert` must be
an RSA certificate (with `[acme]`, `key_type = rsa2048`). If it is not,
the log tells you at the start. The BBS also sends a chat room that the
client asks for by TLS to the TLS host.

A TLS proxy in front (stunnel, nginx) also operates. Configure it to
pass the plain connection to port 5190, and set `tls_host` to its
address. Behind the proxy, all clients come from the address of the
proxy. Thus, `max_per_ip`, the login throttle and bans see only that
address.

## Port 5190 and AOL

5190 was the port of AOL before it was the port of AIM. The BBS knows a
connection that starts as an AOL client connection (a P3 packet) by its
first byte. Thus, an AOL front end can share the port. If there is no
AOL front end, the BBS logs such a connection and closes it.
