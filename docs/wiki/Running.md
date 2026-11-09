This page covers starting the BBS for the first time, running it under
systemd, and running it as a user of its own. For day-to-day work once it
is running, see [Administration](Administration).

## First start

```
bbs -config /etc/bbs/bbs.conf config    # edit the configuration in its screens (starts from the sample)
bbs -config /etc/bbs/bbs.conf check     # read the configuration and show its warnings
bbs -config /etc/bbs/bbs.conf init      # make the database, with two message areas
bbs -config /etc/bbs/bbs.conf user add sysop -sysop
bbs -config /etc/bbs/bbs.conf serve
```

Without `-config`, bbs runs with its defaults: telnet and TN3270 on port
2300, modem callers (raw) on 127.0.0.1:2323, and the database in `bbs.db`
in the current directory. The database can also be PostgreSQL, MySQL or
MariaDB: see [Databases](Databases).

## Under systemd

[bbs.service.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.service.sample)
runs the BBS under systemd as its own user, with the database in
`/var/lib/bbs` and its sockets in `/run/bbs`. `systemctl reload bbs` applies
the settings that can change while callers are on (see
[Configuration](Configuration#changing-it-while-callers-are-on)).

## Running as a user of its own

Ports below 1024 (telnet's 23, finger's 79, gopher's 70) need root or the
`CAP_NET_BIND_SERVICE` capability. There are two ways to give the BBS
those ports without running it as root:

- systemd starts the BBS as its own user with that capability
  (bbs.service.sample's `User=bbs` and `AmbientCapabilities=`), or
- the BBS is started as root and gives up root itself:

```
[general]
user = bbs
; group = bbs     (the user's own group by default)
```

When it is started as root, `bbs serve` reads the configuration and binds
every port and unix socket it listens on (the listeners, the HTTP API and
metrics, FTP, NNTP, IRC, finger, gopher, Gemini and binkp). Then it gives up root,
takes the user's supplementary groups (such as `socket_group`'s and
docker's), and runs itself again as the user. It stays the same process,
with the same PID for systemd and `systemctl reload`, and keeps the
sockets. Nothing is written as root: the database, logs, host keys and
backups all belong to the user. A unix socket without `socket_group`
belongs to the user too.

Other commands started as root (`bbs user add`, `bbs init`, `bbs backup`)
also give up root first, so that the database stays the user's. `bbs
check` and `bbs config` stay root, because the configuration file is
root's to write. `bbs backup restore` restores files as root and then
hands them to the user, except the configuration.

The user must be able to read the configuration (`chown root:bbs
/etc/bbs/bbs.conf; chmod 0640`), write where the database and files are
kept, and run the program. Under systemd, leave `User=` out when you use
this method; bbs.service.sample explains how.

**See also:** [Configuration](Configuration), [Administration](Administration)
