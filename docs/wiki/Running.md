This page tells you how to start the BBS for the first time, how to run
it under systemd, and how to run it as its own user. For daily work after
the BBS starts, refer to [Administration](Administration).

## First start

```
bbs -config /etc/bbs/bbs.conf config    # edit the configuration in its screens (starts from the sample)
bbs -config /etc/bbs/bbs.conf check     # read the configuration and show its warnings
bbs -config /etc/bbs/bbs.conf init      # make the database, with two message areas
bbs -config /etc/bbs/bbs.conf user add sysop -sysop
bbs -config /etc/bbs/bbs.conf serve
```

Without `-config`, bbs uses its defaults:

- Telnet and TN3270 on port 2300.
- Modem callers (raw) on 127.0.0.1:2323.
- The database in `bbs.db` in the current directory.

The database can also be PostgreSQL, MySQL or MariaDB. Refer to
[Databases](Databases).

## Under systemd

[bbs.service.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.service.sample)
runs the BBS under systemd as its own user. The database is in
`/var/lib/bbs` and the sockets are in `/run/bbs`. `systemctl reload bbs`
applies the settings that you can change while callers are on (refer to
[Configuration](Configuration#changing-it-while-callers-are-on)).

## Running as a user of its own

Ports below 1024 (23 for telnet, 79 for finger, 70 for gopher) must have
root or the `CAP_NET_BIND_SERVICE` capability. There are two methods to
give these ports to the BBS without root:

- systemd starts the BBS as its own user with that capability
  (`User=bbs` and `AmbientCapabilities=` in bbs.service.sample).
- You start the BBS as root, and the BBS then stops its root access:

```
[general]
user = bbs
; group = bbs     (the user's own group by default)
```

If you start `bbs serve` as root, it reads the configuration. It then
binds all the ports and unix sockets on which it listens (the listeners,
the HTTP API and metrics, FTP, NNTP, IRC, finger, gopher, Gemini and
binkp). Then it stops its root access and takes the supplementary groups
of the user (for example, the group of `socket_group` and the docker
group). Then it runs itself again as the user. It stays the same process,
with the same PID for systemd and `systemctl reload`, and it keeps the
sockets.

The BBS writes nothing as root. The database, logs, host keys and backups
all belong to the user. A unix socket without `socket_group` also belongs
to the user.

If you start other commands as root (`bbs user add`, `bbs init`, `bbs
backup`), they also stop their root access first. Thus, the database
stays with the user. `bbs check` and `bbs config` keep root access,
because only root can write the configuration file. `bbs backup restore`
restores files as root. Then it gives them to the user, but not the
configuration.

The user must have these permissions:

- Read the configuration (`chown root:bbs
  /etc/bbs/bbs.conf; chmod 0640`).
- Write in the locations of the database and the files.
- Run the program.

Under systemd, do not use `User=` with this method. bbs.service.sample
tells you how.

**See also:** [Configuration](Configuration), [Administration](Administration)
