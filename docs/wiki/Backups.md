The BBS backs itself up: this configuration file, the database and the
files, one archive a backup, while callers are on.

```
bbs -config /etc/bbs/bbs.conf backup                  # one now
bbs -config /etc/bbs/bbs.conf backup list             # those there are, the newest first
bbs -config /etc/bbs/bbs.conf backup restore ARCHIVE  # what it would put back, and where
bbs -config /etc/bbs/bbs.conf backup restore ARCHIVE -yes
```

The [configuration screens](Configuration#editing-it-the-configuration-screens)'
Maintenance menu has the same: Back up now, the backups made, and
`[backup]`'s settings.

## What a backup has

Each is `bbs-YYYYMMDD-HHMMSS.tar.gz` in the backup directory:

| in it | |
|---|---|
| `MANIFEST` | when it was made, the database's engine, where each thing was |
| `bbs.conf` | the configuration file the BBS runs by |
| `database.db` | the database as a SQLite file, whatever its engine, taken whole at one moment: SQLite's by `VACUUM INTO`, a PostgreSQL or MySQL server's copied in one read-only transaction that sees nothing written after it began.  Callers may go on as it is taken |
| `files/`, `chan/`, `fido/`, `screens/`, `scripts/` | the file areas', image boards', FidoNet's, display files' and scripts' directories (with `files = yes`) |

An archive is written beside its final name and renamed to it when it is
whole: one there is one made.  Past `keep`, the oldest are taken away.

## [backup]

| key | default | |
|---|---|---|
| `dir` | `backups` beside an SQLite database | where the archives go: better on another disk, or copied off the machine (a server's database has no default: say one) |
| `at` | none | daily, at this time (`03:30`) |
| `keep` | `7` | archives kept (0: all) |
| `files` | `yes` | the files' directories too; `no`: the configuration and the database alone |

They take effect at a reload (`systemctl reload bbs`) or when the
Configuration screen saves them.

## Putting one back

`bbs backup restore ARCHIVE` says what the archive has and where each
thing would go: where *this* system's configuration says (so a backup
moves to another machine), not where it was.  With the BBS stopped, `-yes`
puts it back:

- each file or directory there now is renamed aside first,
  `NAME.before-restore-YYYYMMDD-HHMMSS`, never written over (take them
  away when all is well);
- an SQLite database is put in the database's place;
- a PostgreSQL, MySQL or MariaDB database must be new and empty (made as
  for `bbs db copy`): the archive's is copied into it;
- what the archive does not have (the files, with `files = no`) is left
  as it is.

An archive whose names would climb out of where they go is refused before
anything is changed.

**See also:** [Databases](Databases), [Configuration](Configuration), [Sysop tools](Sysop-Tools)
