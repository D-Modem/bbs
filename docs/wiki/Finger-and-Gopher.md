Two small servers tell the world about the BBS. Finger shows who is on
and who a user is. Gopher serves the system's own gopherspace, with the
message and file areas the sysop opens to it. Both are off unless their
section is in the configuration.

Their usual ports are below 1024, so either run the BBS with
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

**See also:** [Configuration](Configuration), [Security](Security)
