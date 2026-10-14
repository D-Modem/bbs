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

Without `-config` it runs with the defaults: telnet and TN3270 on port
2300, modem callers raw on 127.0.0.1:2323, the database `bbs.db` here.  The database can as well
be PostgreSQL, MySQL or MariaDB: see [Databases](Databases).

Day to day: [Administration](Administration).

**See also:** [Configuration](Configuration), [Administration](Administration)
