The BBS can make backups while callers are on. Each backup is one
archive. The archive contains the configuration file, the database and
the files. This page tells how to make backups, what they contain, and
how to restore a backup.

```
bbs -config /etc/bbs/bbs.conf backup                  # make one now
bbs -config /etc/bbs/bbs.conf backup list             # list the backups, newest first
bbs -config /etc/bbs/bbs.conf backup restore ARCHIVE  # show what it would restore, and where
bbs -config /etc/bbs/bbs.conf backup restore ARCHIVE -yes
```

The Maintenance menu of the [configuration screens](Configuration#editing-it-the-configuration-screens)
does the same tasks: Back up now, the list of backups, and the
`[backup]` settings.

## What a backup holds

Each backup is `bbs-YYYYMMDD-HHMMSS.tar.gz` in the backup directory:

| File | What it is |
|---|---|
| `MANIFEST` | the time of the backup, the engine of the database, and the initial location of each item |
| `bbs.conf` | the configuration file that the BBS uses |
| `database.db` | the database as a SQLite file, for all engines. The BBS copies the full database at one time. For SQLite, it uses `VACUUM INTO`. For a PostgreSQL or MySQL server, it copies the database in one read-only transaction. This transaction does not see data that the server writes after the transaction starts. Callers can continue while the BBS makes the copy |
| `key.ssh_host_ed25519_key` ... | the host keys of the SSH listeners (always, for all values of `files`). Thus, a BBS that you restore on a different machine answers with the keys that its callers know |
| `files/`, `chan/`, `fido/`, `screens/`, `scripts/` | the directories of the file areas, image boards, FidoNet, display files and scripts (with `files = yes`) |

The BBS writes an archive with a temporary name. It changes the name when
the archive is complete. Thus, each archive in the directory is complete.
If there are more than `keep` archives, the BBS deletes the oldest.

## [backup]

| Key | Default | What it sets |
|---|---|---|
| `dir` | `backups` adjacent to an SQLite database | the location of the archives. Another disk is better, or copy them to a different machine. (A database on a server has no default: set one) |
| `at` | none | daily, at this time (`03:30`) |
| `keep` | `7` | the number of archives to keep (0: all) |
| `files` | `yes` | also the directories of the files. `no`: only the configuration, the database and the host keys |

Changes start at a reload (`systemctl reload bbs`) or when the
Configuration screen saves them.

## Restoring a backup

`bbs backup restore ARCHIVE` lists the contents of the archive and the
destination of each part. It uses the configuration of *this* system,
not the locations at the time of the backup. Thus, you can move a backup
to a different machine.

To restore the backup, stop the BBS and add `-yes`. The restore does
these steps:

- It first gives each file or directory that is already there the new
  name `NAME.before-restore-YYYYMMDD-HHMMSS`. It never overwrites them.
  Delete these items when you know that the restore is correct.
- It puts an SQLite database in the location of the database.
- A PostgreSQL, MySQL or MariaDB database must be new and empty
  (configure it as for `bbs db copy`). The restore copies the database of
  the archive into it. If the database already has users, the restore
  stops before it unpacks or changes anything. In that case, create a new
  database, point `database` at it, and do the restore again.
- It does not change items that are not in the archive (for example, the
  files, with `files = no`).

If an archive has paths that go outside their destination, the restore
stops before it changes anything.

**See also:** [Databases](Databases), [Configuration](Configuration), [Sysop tools](Sysop-Tools)
