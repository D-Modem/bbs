This page shows how to build bbs from source. You need Go 1.26 or later.

```
make            # or: go build ./cmd/bbs
make test
make install    # installs /usr/local/bin/bbs, and /etc/bbs/bbs.conf if there is none
```

bbs does not use cgo, so there is no C compiler to set up. Every database
driver is built in: SQLite is [modernc.org/sqlite](https://modernc.org/sqlite),
PostgreSQL is [pgx](https://github.com/jackc/pgx), and MySQL and MariaDB are
[go-sql-driver](https://github.com/go-sql-driver/mysql).

## go3270

bbs uses the D-Modem suite's fork of go3270,
[D-Modem/go3270](https://github.com/D-Modem/go3270) (Go module
`github.com/D-Modem/go3270`), which adds 3270 query replies and graphics.
While that repository is private, Go has to fetch it from GitHub with your
own credentials:

```
export GOPRIVATE=github.com/D-Modem/*     # the Makefile sets this for you
git config --global url."git@github.com:".insteadOf "https://github.com/"   # or use a token
```

**See also:** [Running](Running), [Development](Development)
