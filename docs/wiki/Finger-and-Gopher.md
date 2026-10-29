Two small servers tell the world about the BBS: finger, who is on and who
a user is; gopher, the system's own gopherspace, with the message and
file areas the sysop opens to it.  Both are off unless their section is
there.

Their usual ports are below 1024: run the BBS with
`AmbientCapabilities=CAP_NET_BIND_SERVICE` (see
[bbs.service.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.service.sample)),
or give another port.

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

It tells no addresses, no caller IDs and nothing of locked accounts, and
forwards nothing (`user@otherhost` is refused).  `online = no` and
`users = no` turn the two halves off.

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
users, the message of the day), *Who's online*, and, when they are
opened, *Message areas* and *File areas*.  An area lists its messages 50
at a time (oldest first, with *More...*); a message is text; a file is
sent as it is (type 9; GIF and PNG/JPEG as images).

Gopher has no login: what it shows, it shows to anyone.  So by default it
shows no message areas and no files, and `bbs check` warns when `areas`
or `files` is set.  `areas` matches area names without regard to case
(`*` is all of them); an area not matched is not there, whatever is
asked.  Selectors are not paths: they name records of the BBS's
database, and nothing of the server's disk is reachable.

```
$ curl gopher://bbs.example.org/1/areas
$ lynx gopher://bbs.example.org
```

**See also:** [Configuration](Configuration), [Security](Security)
