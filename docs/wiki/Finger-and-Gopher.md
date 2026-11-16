Three small servers give the world data about the BBS:

- Finger shows the users who are on, and data about a user.
- Gopher serves the gopherspace of the system, with the message and file
  areas that the sysop opens to it.
- Gemini serves the same capsule, and the users of finger, over TLS.

Each server is disabled until you put its section in the configuration.

The usual ports of finger and gopher are below 1024. Thus, run the BBS
with `AmbientCapabilities=CAP_NET_BIND_SERVICE` (refer to
[bbs.service.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.service.sample)),
or use a different port.

## Finger

```
[finger]
address = :79
online = yes        ; "finger @bbs.example.org": who is on
users = yes         ; "finger alice@bbs.example.org": the user
max_clients = 16
max_per_ip = 4
```

```
$ finger @bbs.example.org
D-Modem BBS: 2 callers on

Node  User                     Via            On  Doing
   1  sysop                    telnet        12m  Chatting in main
   2  alice                    modem 26400    3m  Reading mail

$ finger alice@bbs.example.org
User:         alice
From:         Zürich
Member since: 2026-10-01
Last call:    on now, node 2: Reading mail
Calls:        14
Posts:        5
```

Finger never shows addresses, caller IDs or data about locked accounts.
It forwards nothing (it refuses `user@otherhost`). `online = no` and
`users = no` disable its two parts.

## Gopher

```
[gopher]
address = :70
hostname = bbs.example.org   ; what the menus say to come back to
port = 70                    ; and on which port (behind NAT, a forward)
areas = general,announcements ; message areas anyone may read (a wildmat)
files = no                   ; yes: file areas listed and downloadable
max_clients = 32
max_per_ip = 8
```

The root menu has *About this system* (name, location, sysop, number of
users and the message of the day) and *Who's online*. If the sysop
opened them, it also has *Message areas* and *File areas*. An area lists
its messages 50 at a time (oldest first, with *More...*). The server
sends a message as text. It sends a file as it is (type 9, or as an
image for GIF, PNG and JPEG).

Gopher has no login. Thus, it shows all its data to all users. For this
reason, it shows no message areas and no files by default, and `bbs
check` gives a warning when you set `areas` or `files`. `areas` matches
area names with upper or lower case (`*` matches all of them). Users
cannot get to an area that does not match, for all requests. Selectors
are not paths. They name records in the database of the BBS. Users
cannot get to items on the disk of the server.

```
$ curl gopher://bbs.example.org/1/areas
$ lynx gopher://bbs.example.org
```

## Gemini

```
[gemini]
address = :1965
hostname = bbs.example.org   ; offered by bbs gemini cert
;cert_file = /etc/bbs/gemini-cert.pem   ; or neither, for bbs gemini cert's
;key_file = /etc/bbs/gemini-key.pem
areas = general,announcements ; as gopher's
files = no
users = yes                  ; /users: look up a user, as finger does
max_clients = 32
max_per_ip = 8
```

[Gemini](https://geminiprotocol.net/) is a newer protocol that is
similar to gopher. It uses one request for each connection, always over
TLS, with pages in gemtext. The capsule is the same as the capsule of
gopher. The home page has links to *About this system*, *Who's online*
and *Look up a user* (it asks for a name and shows the same data as
finger). If the sysop opened them, it also has links to *Message areas*
and *File areas*. `gemini://host/~alice` goes directly to a user.

A message is a gemtext page with its headers first. The server shows its
lines as text. Thus, a line in a message cannot become a link or a
heading. The server sends a file as it is, with a type from its name
(`application/octet-stream` if the name does not give a type).

Gemini clients trust the certificate of a capsule the first time that
they see it. They give a warning if it changes. Thus, self-signed
certificates are usual. The BBS never makes a certificate automatically.
If there is no `cert_file` and `key_file`, make one with
`bbs gemini cert` before you start the BBS. The command asks for the
contents of the certificate and shows them. It makes the certificate
only when you say yes:

```
$ bbs -config /etc/bbs/bbs.conf gemini cert
A certificate for the Gemini server, made here and signed by itself.
Clients trust it the first time they connect and warn if it ever changes:
make it once, and keep it (and its key) with your backups.
Host names callers use, by commas [bbs.example.org]: bbs.example.org, gemini.example.org
Organization (- for none) [D-Modem BBS]:
Years it is good for [20]:

For:      bbs.example.org, gemini.example.org
Org:      D-Modem BBS
Until:    2046-11-09
Files:    /var/lib/bbs/gemini-cert.pem, /var/lib/bbs/gemini-key.pem
Make it? (yes/no) [no]: yes
Made: bbs.example.org, gemini.example.org until 2046-11-09, SHA-256 3F2A...
```

The first host name is the common name of the certificate. You can also
list addresses (IPv4 or IPv6). The files go adjacent to the database
(`gemini-cert.pem`, `gemini-key.pem`). Keep them with your backups. A
new certificate causes a warning on all clients that connected before.
For this reason, `bbs gemini cert` does not replace a certificate
without `-force` (and with `-force`, it keeps the old certificate as
`.old`).

`bbs check` shows the names, the expiry date and the SHA-256 fingerprint
of the certificate, or it tells you that there is no certificate yet. If
there is no certificate, `bbs
serve` stops and tells you to make one. A certificate from a certificate
authority (for example, Let's Encrypt) also operates: set `cert_file`
and `key_file`.

Gemini shows all users the same data that gopher shows all users. By
default, it shows no areas and no files. `bbs check` gives a warning
when you set `areas` or `files`. Gemini never shows file areas that have
a download requirement. `users = no` disables the user lookup. Port 1965
is above 1024 and does not need a privilege.

```
$ amfora gemini://bbs.example.org
$ printf 'gemini://bbs.example.org/areas\r\n' | openssl s_client -quiet -connect bbs.example.org:1965
```

**See also:** [Configuration](Configuration), [Security](Security)
