The BBS keeps its users, messages, mail, file records, FidoNet and news
state, tokens and keys in one database: **SQLite** (the default, one file,
nothing to run), **PostgreSQL** (12 or later), **MySQL** (8.0 or later) or
**MariaDB** (10.11 or later; 10.6 with a difference, below).
File areas' files are kept on disk in every case (`files_dir`).  Tested with
PostgreSQL 16, MySQL 8.4, and MariaDB 11.4, 10.11 and 10.6.

```ini
[general]
database = /var/lib/bbs/bbs.db                                ; SQLite
;database = postgres://bbs:PASSWORD@db.example.org/bbs         ; PostgreSQL
;database = mysql://bbs:PASSWORD@db.example.org:3306/bbs       ; MySQL
;database = mariadb://bbs:PASSWORD@db.example.org:3306/bbs     ; MariaDB
```

| `database` | |
|---|---|
| a path (or `sqlite:PATH`) | SQLite: the file is made (0600) if it is not there |
| `postgres://` or `postgresql://` | PostgreSQL, in pgx's form: `?sslmode=require`, `?search_path=bbs` and the like after it |
| `mysql://user:password@host:port/db` | MySQL; `?tls=true` and go-sql-driver's other options after it |
| `mariadb://...`, `mariadb:DSN` | MariaDB, the same way (the BBS finds MariaDB behind a `mysql://` too) |
| `mysql:DSN` | MySQL in go-sql-driver's own form, for a unix socket: `mysql:bbs:PASSWORD@unix(/run/mysqld/mysqld.sock)/bbs` |

The BBS makes its tables (and adds columns later versions bring) when it
starts, or with `bbs init`.  The database (PostgreSQL's, or MySQL's) must
be there, and the user own it or have `CREATE` in it; on PostgreSQL the
BBS also makes a collation, `nocase`, for names that match in any case.

Logs and `bbs check` show the address with its password as `xxxxx`.  The
configuration file has it in the clear: keep it 0640, the BBS's group's.

## What differs

Little, by design: the BBS writes its SQL once, and each engine is given
its own forms of what differs (ids, placeholders, upserts, booleans, what
counts as a duplicate).

- **Names** (users, areas, files) match in any case on all three; letters
  with accents are not their plain letters (René is not Rene).  SQLite
  folds the case of ASCII letters only: `RENÉ` finds René on PostgreSQL
  and MySQL, not on SQLite.
- **MySQL** keeps names, Message-IDs and other indexed text in
  `VARCHAR(255)`, descriptions and the like in `VARCHAR(1024)`, messages
  in `MEDIUMTEXT` (16 MB).  The BBS's connection sets the SQL mode it
  needs (`ANSI_QUOTES`, strict), whatever the server's default.
- **MariaDB** is a MySQL to the BBS, but for its collations, which the
  BBS chooses when it connects: UCA 14.0's (`utf8mb4_uca1400_as_ci`,
  10.10 and later), which match names as MySQL's do.  MariaDB 10.6 (out of
  support since July 2026) has no collation that ignores case but not
  accents: there names ignore both (René *is* Rene, and only one of them
  can be a user).  `bbs check` and the log say which.
- **SQLite** uses one connection (it takes one writer at a time); the
  servers up to ten.

## Moving to another engine

```
bbs db copy postgres://bbs:PASSWORD@db.example.org/bbs
```

copies the configured database, every table, into a new, empty one (made
as `bbs init` would), keeping ids, then says so; then set `database =` to
the new one.  The BBS should be stopped while it copies.  It goes any way:
SQLite to PostgreSQL, MySQL to SQLite, PostgreSQL to MySQL.

## Backups

`bbs backup` (and `[backup]`'s `at`, daily) takes the database whole while
callers are on, with the configuration and the files: see
[Backups](Backups).  A server's own tools do as well: `pg_dump`,
`mysqldump --single-transaction`.

**See also:** [Configuration](Configuration), [Running](Running), [Development](Development)
