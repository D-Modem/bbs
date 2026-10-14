```
bbs -config /etc/bbs/bbs.conf config    # the configuration screens (begun from the sample)
bbs -config /etc/bbs/bbs.conf check     # the configuration, its warnings
bbs -config /etc/bbs/bbs.conf init      # the database, with two areas
bbs -config /etc/bbs/bbs.conf user add sysop -sysop
bbs -config /etc/bbs/bbs.conf serve
```

[bbs.service.sample](https://github.com/Neo-Desktop/bbs-sec/blob/HEAD/bbs.service.sample) runs it under systemd as its own
user, with the database in `/var/lib/bbs` and sockets in `/run/bbs`;
`systemctl reload bbs` puts what may change while callers are on in force
([Configuration](Configuration#changing-it-while-callers-are-on)).

## Running as a user of its own

Ports under 1024 (telnet's 23, finger's 79, gopher's 70) need root, or
`CAP_NET_BIND_SERVICE`.  Either systemd starts the BBS as its user with
that capability (bbs.service.sample's `User=bbs` and
`AmbientCapabilities=`), or the BBS is started as root and gives root up
itself:

```
[general]
user = bbs
; group = bbs     (the user's own by default)
```

Started as root, `bbs serve` reads the configuration, binds every port and
unix socket it listens on (the listeners, the HTTP API and metrics, FTP,
NNTP, IRC, finger, gopher, binkp), gives up root (its supplementary groups
become the user's: `socket_group`'s and docker's, say) and runs itself
again as the user, the same process (its PID, for systemd and `systemctl
reload`), the sockets handed down.  Nothing is written as root: the
database, logs, host keys and backups are the user's.  A unix socket
without `socket_group` is the user's too.

Other commands started as root (`bbs user add`, `bbs init`, `bbs backup`)
give root up first as well, so that the database stays the user's; `bbs
check` and `bbs config` stay root (the configuration file is root's to
write), and `bbs backup restore` puts things back as root and gives them
(but the configuration) to the user.

The user must be able to read the configuration (`chown root:bbs
/etc/bbs/bbs.conf; chmod 0640`), write where the database and files are,
and run the program.  Under systemd, leave `User=` out (bbs.service.sample
says how).

Without `-config` it runs with the defaults: telnet and TN3270 on port
2300, modem callers raw on 127.0.0.1:2323, the database `bbs.db` here.  The database can as well
be PostgreSQL, MySQL or MariaDB: see [Databases](Databases).

Day to day: [Administration](Administration).

**See also:** [Configuration](Configuration), [Administration](Administration)
