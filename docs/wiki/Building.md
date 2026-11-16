This page shows how to build bbs from source. You must have Go 1.26 or later.

```
make            # or: go build ./cmd/bbs
make test
make install    # installs /usr/local/bin/bbs, and /etc/bbs/bbs.conf if there is none
```

bbs does not use cgo. Thus, you do not have to configure a C compiler. All
database drivers are in the binary. SQLite is [modernc.org/sqlite](https://modernc.org/sqlite),
PostgreSQL is [pgx](https://github.com/jackc/pgx), and MySQL and MariaDB are
[go-sql-driver](https://github.com/go-sql-driver/mysql).

## go3270

bbs uses the fork of go3270 from the D-Modem suite,
[D-Modem/go3270](https://github.com/D-Modem/go3270) (Go module
`github.com/D-Modem/go3270`). This fork adds 3270 query replies and graphics.
While that repository is private, Go must get it from GitHub with your
own credentials:

```
export GOPRIVATE=github.com/D-Modem/*     # the Makefile sets this for you
git config --global url."git@github.com:".insteadOf "https://github.com/"   # or use a token
```

**See also:** [Running](Running), [Development](Development)
