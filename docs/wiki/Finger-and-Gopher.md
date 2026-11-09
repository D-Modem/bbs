Three small servers tell the world about the BBS. Finger shows who is on
and who a user is. Gopher serves the system's own gopherspace, with the
message and file areas the sysop opens to it. Gemini serves the same
capsule, and finger's users, over TLS. Each is off unless its section is
in the configuration.

Finger's and gopher's usual ports are below 1024, so either run the BBS with
`AmbientCapabilities=CAP_NET_BIND_SERVICE` (see
[bbs.service.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.service.sample)),
or use another port.

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

Finger never shows addresses, caller IDs or anything about locked
accounts, and it forwards nothing (`user@otherhost` is refused).
`online = no` and `users = no` turn off its two halves.

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
users and the message of the day), *Who's online*, and, if the sysop has
opened them, *Message areas* and *File areas*. An area lists its messages
50 at a time (oldest first, with *More...*). A message is served as text,
and a file as it is (type 9, or as an image for GIF, PNG and JPEG).

Gopher has no login, so everything it shows, it shows to anyone. That is
why it shows no message areas and no files by default, and `bbs check`
warns when `areas` or `files` is set. `areas` matches area names in any
case (`*` matches all of them), and an area that does not match cannot be
reached, whatever is asked. Selectors are not paths: they name records in
the BBS's database, and nothing on the server's disk can be reached.

```
$ curl gopher://bbs.example.org/1/areas
$ lynx gopher://bbs.example.org
```

## Gemini

```
[gemini]
address = :1965
hostname = bbs.example.org   ; the name a certificate made here is for
;cert_file = /etc/bbs/gemini-cert.pem   ; or neither, for one made here
;key_file = /etc/bbs/gemini-key.pem
areas = general,announcements ; as gopher's
files = no
users = yes                  ; /users: look up a user, as finger does
max_clients = 32
max_per_ip = 8
```

[Gemini](https://geminiprotocol.net/) is gopher's younger cousin: one
request a connection, always over TLS, pages in gemtext. The capsule is
gopher's: the home page links *About this system*, *Who's online*, *Look
up a user* (it asks for a name and shows what finger would) and, if the
sysop opened them, *Message areas* and *File areas*. `gemini://host/~alice`
goes straight to a user. A message is a gemtext page with its headers
first; its lines are shown as text, so a line in a message cannot become
a link or a heading. A file goes as it is, typed by its name
(`application/octet-stream` if the name says nothing).

Gemini clients trust a capsule's certificate the first time they see it
and warn if it ever changes, so self-signed certificates are the usual
thing. With no `cert_file` and `key_file`, the BBS makes one for
`hostname` at its first start, beside the database (`gemini-cert.pem`,
`gemini-key.pem`), and keeps it; `bbs check` shows its SHA-256
fingerprint. Keep those two files with your backups: a new certificate
makes every client that has been before warn. A certificate from a
certificate authority (Let's Encrypt's, say) works too.

What gopher shows anyone, Gemini shows anyone: no areas and no files by
default, `bbs check` warns when `areas` or `files` is set, and file areas
with a download requirement are never shown. `users = no` turns off the
user lookup. Port 1965 is above 1024 and needs no privilege.

```
$ amfora gemini://bbs.example.org
$ printf 'gemini://bbs.example.org/areas\r\n' | openssl s_client -quiet -connect bbs.example.org:1965
```

**See also:** [Configuration](Configuration), [Security](Security)
