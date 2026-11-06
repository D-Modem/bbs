The BBS can back itself up while callers are on. Each backup is one
archive holding the configuration file, the database and the files. This
page covers making backups, what is in them, and restoring one.

```
bbs -config /etc/bbs/bbs.conf backup                  # make one now
bbs -config /etc/bbs/bbs.conf backup list             # list the backups, newest first
bbs -config /etc/bbs/bbs.conf backup restore ARCHIVE  # show what it would restore, and where
bbs -config /etc/bbs/bbs.conf backup restore ARCHIVE -yes
```

The [configuration screens](Configuration#editing-it-the-configuration-screens)'
Maintenance menu does the same: Back up now, the list of backups, and the
`[backup]` settings.

## What a backup holds

Each backup is `bbs-YYYYMMDD-HHMMSS.tar.gz` in the backup directory:

| File | What it is |
|---|---|
| `MANIFEST` | when it was made, the database's engine, where each thing was |
| `bbs.conf` | the configuration file the BBS runs by |
| `database.db` | the database as a SQLite file, whatever its engine, taken whole at one moment: SQLite's by `VACUUM INTO`, a PostgreSQL or MySQL server's copied in one read-only transaction that sees nothing written after it began. Callers may go on as it is taken |
| `key.ssh_host_ed25519_key` ... | the SSH listeners' host keys (always, whatever `files` says): a BBS put back on another machine answers with the keys its callers know |
| `files/`, `chan/`, `fido/`, `screens/`, `scripts/` | the file areas', image boards', FidoNet's, display files' and scripts' directories (with `files = yes`) |

An archive is written under a temporary name and renamed when it is
complete, so any archive in the directory is a whole one. Beyond `keep`
archives, the oldest are deleted.

## [backup]

| Key | Default | What it sets |
|---|---|---|
| `dir` | `backups` beside an SQLite database | where the archives go: better on another disk, or copied off the machine (a server's database has no default: say one) |
| `at` | none | daily, at this time (`03:30`) |
| `keep` | `7` | archives kept (0: all) |
| `files` | `yes` | the files' directories too; `no`: the configuration, the database and the host keys alone |

Changes take effect at a reload (`systemctl reload bbs`) or when the
Configuration screen saves them.

## Restoring a backup

`bbs backup restore ARCHIVE` lists what the archive holds and where each
part would go. It follows *this* system's configuration rather than where
things were when the backup was made, so a backup can move to another
machine. With the BBS stopped, add `-yes` to restore it:

- each file or directory already there is first renamed to
  `NAME.before-restore-YYYYMMDD-HHMMSS`, never overwritten (delete these
  once all is well);
- an SQLite database is put in the database's place;
- a PostgreSQL, MySQL or MariaDB database must be new and empty (set up
  as for `bbs db copy`), and the archive's database is copied into it. If
  the database already has users, the restore is refused before anything
  is unpacked or changed: create a new database, point `database` at it,
  and restore again;
- anything the archive does not hold (such as the files, with
  `files = no`) is left as it is.

An archive with paths that would escape their destination is refused
before anything is changed.

**See also:** [Databases](Databases), [Configuration](Configuration), [Sysop tools](Sysop-Tools)
