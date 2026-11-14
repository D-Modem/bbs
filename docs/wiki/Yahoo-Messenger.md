The BBS has a Yahoo! Messenger server (YMSG, the protocol of the pager
of Yahoo). This server is adjacent to the [IRC](IRC), [AIM](AIM-and-ICQ)
and [MSN](MSN-Messenger) servers of the BBS, and it uses the same chat.
A user who signs in with Yahoo! Messenger can do these actions:

- See BBS friends as buddies.
- Send IMs to all users on the BBS (other Yahoo users, IRC, AIM and MSN
  users, and callers on telnet, SSH, 3270s and the web terminal).
- Join the chat rooms of the BBS as Yahoo chat rooms.

```ini
[ymsg]
address = :5050
http_address = :80
```

| Setting | Default | What it sets |
|---|---|---|
| `address` | `:5050` | the address on which the pager listens |
| `http_address` | none | the web part (the capacity lookup, the room list and file transfer), plain HTTP. Port 80 for file transfer |
| `https_address` | none | the web part over HTTPS: the login of Y!M 9 and later |
| `tls_cert`, `tls_key` | none | the certificate for `https_address` (for `login.yahoo.com`) |
| `host` | the address on which each client asked | the address of the pager that the capacity lookup gives. Set it behind NAT |
| `max_clients` | `256` | the maximum number of connections at one time |
| `max_per_ip` | `4` | the maximum from one address |
| `system` | `bbs` | the Yahoo ID from which the notices of the BBS come |
| `max_file_mb` | `16` | the largest file that users can send to each other, in MB. `0` disables file transfer |
| `old_login` | `no` | `yes` gives Y!M 6 to 8 the Y!M 5 login in place of their own login |

## Versions served

The BBS serves YMSG 9 to 16, with three logins:

- **The Y!M 5 challenge login** ("auth type 0", YMSG 9): Yahoo!
  Messenger 5.x.
- **The Y!M 6 to 8 challenge login** ("auth type 1", YMSG 10 to 15):
  Yahoo! Messenger 6, 7 and 8, and the third-party clients of that time
  (Gaim, Kopete, the client of libyahoo2). These clients use the login
  that the server offers. The version of this login from Yahoo sent the
  challenge through a set of lookup tables. The BBS writes its challenges
  so that the client goes through the tables zero times. Thus, the BBS
  does not need a copy of the tables. If a client does not accept these
  challenges, `old_login = yes` offers it the Y!M 5 login.
- **The token login** (YMSG 16): Yahoo! Messenger 9 and later, and the
  later Pidgin 2.x releases. The client sends the password over HTTPS to
  `login.yahoo.com`, gets a token, and proves it on the pager. This login
  must have `https_address`.

## Pointing a client at the BBS

The clients were made for the servers of Yahoo. Thus, you must point
each client at the BBS. We did not try these procedures with the real
clients yet.

- With the [DNS server](DNS) of the BBS (`[dns]`), the machines of
  callers that use it come here for all of these names, with no hosts
  file.
- Yahoo! Messenger 5 to 8: under `HKEY_CURRENT_USER\Software\Yahoo\Pager`
  in the registry, set the string values `socket server` and
  `IPLookup` to the host of the BBS (community guides also set
  `Conn Server` and `Host Name`). Or, in the hosts file of the machine,
  send `scs.msg.yahoo.com` and `scsa.msg.yahoo.com` to the BBS.
- Yahoo! Messenger 9 and later: in the hosts file, send
  `vcs1.msg.yahoo.com`, `vcs2.msg.yahoo.com`, `scsa.msg.yahoo.com` and
  `login.yahoo.com` to the BBS. The machine must trust the certificate
  in `tls_cert` for `login.yahoo.com`. To do this, make a small
  certificate authority of your own, and add its certificate to the
  trusted roots of the machine. The HTTPS server accepts the TLS 1.0 and
  the old ciphers of Windows XP.
- If you send `insider.msg.yahoo.com` to the BBS, the chat room browser
  shows the rooms of the BBS.
- Third-party clients (Pidgin, Kopete, Miranda, Trillian) accept a pager
  server in the settings of the account.

The deny list and bans in `[security]` apply, as on all other routes.

## Signing in

Each Yahoo user is a BBS user. The Yahoo ID is the BBS name in lower
case without its spaces (`carolann` is Carol Ann). The password is the
BBS password. Failed sign-ins count towards the throttle and the hack
log, as all other logins do. The access level of the user must permit
chat. One account can be signed in on one Yahoo client at a time. A new
sign-in signs out the older one.

The challenge login proves the password with two digests of it. The
password hashes of the BBS cannot check these digests. Thus, the BBS
also keeps the Yahoo digests of each account. It makes them each time
that it knows the password: when the account is made, when the password
is set, and at all logins by password. An account from before the
configuration of `[ymsg]` has no digests yet. Its user must log in one
time by a different method (telnet, SSH, IRC, AIM, the web). Then the
user can sign in with Yahoo. If this is the cause of a failed sign-in,
the log tells you.

The BBS keeps the digests only while `[ymsg]` is configured. A person who
reads them from the database can sign in by Yahoo as that user. The
token login checks the password itself, as all logins do. The connection
of the pager is not encrypted.

## Buddies

The buddy list is the [friends list](Chat-and-Paging#friends) of the
user. It is the same list that the chat of the BBS, IRC, AIM and MSN
show, with its groups (`Friends` for friends that are not in a group). A
buddy is always a BBS user. The BBS refuses all other buddies, and it
tells you. To add a buddy, no permission is necessary. If you add or
move a friend on a different route, a client that is on gets the change
immediately. If you add, move or remove a buddy on Yahoo, the change
applies on all routes.

The ignore list is the [blocked list](Chat-and-Paging#blocking) of the
user. The BBS refuses the messages of a blocked user on all routes. The
user shows as offline to the blocked user.

A buddy is online while the buddy is on the BBS by any route. The buddy
has the status that the Yahoo client of the buddy set (busy, be right
back, a custom message, invisible). On a different route, the status
message is "on the BBS by telnet" (or IRC, AIM, MSN...).

## IMs

An IM to a BBS user is a [private
message](Chat-and-Paging#private-messages): a `PRIVMSG` on IRC, an IM on
AIM or MSN, a message line on a terminal. The BBS removes the colours and
fonts of Yahoo. Typing notices go only between Yahoo clients.

The BBS keeps each line. If a user is not on, the BBS keeps the line for
the user. The user gets it at the next login. Lines kept for a Yahoo
user come at the sign-in, with the time that they were sent. The notices
of the BBS (new mail, a page for the sysop, errors) come from the
`system` ID.

## Sending files

Files go between Yahoo! Messenger users, through the BBS. The BBS holds
them in a temporary file only until the receiver gets them (a maximum of
one hour). It never puts them in the file areas. The BBS declines a
file to a user who is not on Yahoo! Messenger at that time. It tells the
sender to use the [file areas](Files-and-FTP) of the BBS. It does the
same for a file larger than `max_file_mb`, or for a file to a user who
blocks the sender.

This function must have `http_address` on port 80, with these names sent
to the BBS (as for the other names above):

- `relay.msg.yahoo.com` (and `relay.yahoo.com`): Yahoo! Messenger 7 and
  later. The sender offers the file, the receiver accepts, and the two
  clients connect to the relay. The sender uploads, and the receiver
  downloads. If the BBS is behind NAT, set `host`, because the receiver
  gets that address for the relay.
- `filetransfer.msg.yahoo.com`: Yahoo! Messenger 5 and 6. The sender
  uploads the file, and the receiver gets a link from which to download
  it.

We did not try these methods with the real clients yet. We tried them
only with scripted clients that do the same as libyahoo2. The BBS does
not pass on direct connections between two clients. Thus, clients that
try a direct connection first then use the relay or the upload.

## Chat rooms and conferences

A Yahoo chat room is a BBS [chat room](Chat-and-Paging). "Linux,
FreeBSD, Solaris:1" is the room `linux-freebsd-solari` (lower case,
letters and digits, without the lobby number, a maximum of 20
characters). IRC uses the name `#linux-freebsd-solari` for the same
room. The room browser lists the rooms in use and the permanent rooms.

A conference (the invited group IM of Yahoo) is a BBS room of its own,
`yc-` and the name of the conference. The clients of invited Yahoo users
ask them. Other invited users get a private message with the name of the
room. They can join it from their route. All users who know the name of
the room can join it.
