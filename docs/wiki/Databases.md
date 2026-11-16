The BBS keeps these items in one database: its users, messages, mail,
file records, the state of FidoNet and news, tokens and keys. The
database can be:

- **SQLite**, the default: one file, with no other program to run.
- **PostgreSQL** 12 or later.
- **MySQL** 8.0 or later.
- **MariaDB** 10.11 or later (10.6 also operates, with one difference.
  Refer to the text below).

The BBS always keeps the files of the file areas on disk (in
`files_dir`), not in the database. We test the BBS with PostgreSQL 16,
MySQL 8.4, and MariaDB 11.4, 10.11 and 10.6.

```ini
[general]
database = /var/lib/bbs/bbs.db                                ; SQLite
;database = postgres://bbs:PASSWORD@db.example.org/bbs         ; PostgreSQL
;database = mysql://bbs:PASSWORD@db.example.org:3306/bbs       ; MySQL
;database = mariadb://bbs:PASSWORD@db.example.org:3306/bbs     ; MariaDB
```

| `database` | Engine |
|---|---|
| a path (or `sqlite:PATH`) | SQLite. If the file is not there, the BBS makes it (0600) |
| `postgres://` or `postgresql://` | PostgreSQL, in the form of pgx. Put `?sslmode=require`, `?search_path=bbs` and other options after it |
| `mysql://user:password@host:port/db` | MySQL. Put `?tls=true` and the other options of go-sql-driver after it |
| `mariadb://...`, `mariadb:DSN` | MariaDB, in the same way (the BBS also finds MariaDB behind a `mysql://`) |
| `mysql:DSN` | MySQL in the form of go-sql-driver, for a unix socket: `mysql:bbs:PASSWORD@unix(/run/mysqld/mysqld.sock)/bbs` |

The BBS creates its tables when it starts, or with `bbs init`. It adds
the columns of later versions. On PostgreSQL, MySQL and MariaDB, the
database must already exist. The user of the BBS must own it or have
`CREATE` in it. On PostgreSQL, the BBS also creates a collation,
`nocase`. Thus, names match with upper or lower case.

Logs and `bbs check` show the address, with `xxxxx` in place of the
password. The configuration file contains the password in clear text.
Thus, keep the file at mode 0640, with the group of the BBS as the owner.

## What differs

The differences are very small, by design. The BBS has one version of
its SQL. Each engine gets its own form of the parts that are different
(ids, placeholders, upserts, booleans, and the rules for duplicates).

- **Names** (users, areas, files) match with upper or lower case on all
  three engines. Letters with accents are not the same as their plain
  letters (René is not Rene). SQLite folds the case of ASCII letters
  only: `RENÉ` finds René on PostgreSQL and MySQL, but not on SQLite.
- **MySQL** keeps names, Message-IDs and other indexed text in
  `VARCHAR(255)`, descriptions and similar text in `VARCHAR(1024)`, and
  messages in `MEDIUMTEXT` (16 MB). The connection of the BBS sets the
  necessary SQL mode (`ANSI_QUOTES`, strict). It does not use the default
  of the server.
- **MariaDB** is the same as MySQL for the BBS, but its collations are
  different. The BBS selects the collation when it connects: the
  collations of UCA 14.0 (`utf8mb4_uca1400_as_ci`, 10.10 and later).
  These match names as MySQL does. MariaDB 10.6 (out of support since
  July 2026) has no collation that ignores case but not accents. On 10.6,
  names ignore both (René *is* Rene, and only one of them can be a
  user). `bbs check` and the log tell you which collation the BBS uses.
- **SQLite** uses one connection (it accepts one writer at a time). The
  servers use up to ten.

## Moving to another engine

```
bbs db copy postgres://bbs:PASSWORD@db.example.org/bbs
```

This command copies all tables of the configured database into a new,
empty database (prepared as `bbs init` does). It keeps the ids. When the
copy is complete, set `database =` to the new database. Stop the BBS
while it copies. The copy operates in all directions: SQLite to
PostgreSQL, MySQL to SQLite, PostgreSQL to MySQL, and other directions.

## Backups

`bbs backup` (or a daily backup that you set with `at` in `[backup]`)
copies the full database while callers are on. It also copies the
configuration and the files. Refer to [Backups](Backups). The tools of a
database server also operate, for example `pg_dump` or
`mysqldump --single-transaction`.

**See also:** [Configuration](Configuration), [Running](Running), [Development](Development)
