The BBS keeps its users, messages, mail, file records, FidoNet and news
state, tokens and keys in one database. It can be:

- **SQLite**, the default: one file, with nothing else to run
- **PostgreSQL** 12 or later
- **MySQL** 8.0 or later
- **MariaDB** 10.11 or later (10.6 works with one difference, described
  below)

The files in the file areas are always kept on disk (in `files_dir`), not
in the database. The BBS is tested with PostgreSQL 16, MySQL 8.4, and
MariaDB 11.4, 10.11 and 10.6.

```ini
[general]
database = /var/lib/bbs/bbs.db                                ; SQLite
;database = postgres://bbs:PASSWORD@db.example.org/bbs         ; PostgreSQL
;database = mysql://bbs:PASSWORD@db.example.org:3306/bbs       ; MySQL
;database = mariadb://bbs:PASSWORD@db.example.org:3306/bbs     ; MariaDB
```

| `database` | Engine |
|---|---|
| a path (or `sqlite:PATH`) | SQLite: the file is made (0600) if it is not there |
| `postgres://` or `postgresql://` | PostgreSQL, in pgx's form: `?sslmode=require`, `?search_path=bbs` and the like after it |
| `mysql://user:password@host:port/db` | MySQL; `?tls=true` and go-sql-driver's other options after it |
| `mariadb://...`, `mariadb:DSN` | MariaDB, the same way (the BBS finds MariaDB behind a `mysql://` too) |
| `mysql:DSN` | MySQL in go-sql-driver's own form, for a unix socket: `mysql:bbs:PASSWORD@unix(/run/mysqld/mysqld.sock)/bbs` |

The BBS creates its tables when it starts, or with `bbs init`, and adds
any columns that later versions bring. On PostgreSQL, MySQL and MariaDB the
database itself must already exist, and the BBS's user must own it or have
`CREATE` in it. On PostgreSQL the BBS also creates a collation, `nocase`,
so that names match in any case.

Logs and `bbs check` show the address with its password replaced by
`xxxxx`. The configuration file holds the password in the clear, so keep
the file at mode 0640, owned by the BBS's group.

## What differs

Very little, by design. The BBS writes its SQL once, and each engine gets
its own form of the parts that differ (ids, placeholders, upserts,
booleans, and what counts as a duplicate).

- **Names** (users, areas, files) match in any case on all three; letters
  with accents are not their plain letters (René is not Rene). SQLite
  folds the case of ASCII letters only: `RENÉ` finds René on PostgreSQL
  and MySQL, not on SQLite.
- **MySQL** keeps names, Message-IDs and other indexed text in
  `VARCHAR(255)`, descriptions and the like in `VARCHAR(1024)`, messages
  in `MEDIUMTEXT` (16 MB). The BBS's connection sets the SQL mode it
  needs (`ANSI_QUOTES`, strict), whatever the server's default.
- **MariaDB** is a MySQL to the BBS, but for its collations, which the
  BBS chooses when it connects: UCA 14.0's (`utf8mb4_uca1400_as_ci`,
  10.10 and later), which match names as MySQL's do. MariaDB 10.6 (out of
  support since July 2026) has no collation that ignores case but not
  accents: there names ignore both (René *is* Rene, and only one of them
  can be a user). `bbs check` and the log say which.
- **SQLite** uses one connection (it takes one writer at a time); the
  servers up to ten.

## Moving to another engine

```
bbs db copy postgres://bbs:PASSWORD@db.example.org/bbs
```

This copies every table of the configured database into a new, empty one
(set up as `bbs init` would), keeping the ids. When it is done, set
`database =` to the new one. Stop the BBS while it copies. It works in any
direction: SQLite to PostgreSQL, MySQL to SQLite, PostgreSQL to MySQL, and
so on.

## Backups

`bbs backup` (or a daily backup set with `[backup]`'s `at`) copies the
whole database while callers are on, along with the configuration and the
files: see [Backups](Backups). A database server's own tools work as well,
such as `pg_dump` or `mysqldump --single-transaction`.

**See also:** [Configuration](Configuration), [Running](Running), [Development](Development)
