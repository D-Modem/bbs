Go 1.24 or later:

```
make            # or: go build ./cmd/bbs
make test
make install    # /usr/local/bin/bbs, and /etc/bbs/bbs.conf if there is none
```

There is no cgo: SQLite is [modernc.org/sqlite](https://modernc.org/sqlite); PostgreSQL is [pgx](https://github.com/jackc/pgx), MySQL [go-sql-driver](https://github.com/go-sql-driver/mysql), all built in.

go3270 is replaced (in `go.mod`) by the
[go3270-sec](https://github.com/Neo-Desktop/go3270-sec) fork, which adds
the query replies and graphics.  While that repository is private, Go
needs to fetch it from GitHub itself, with your credentials:

```
export GOPRIVATE=github.com/Neo-Desktop/*     # the Makefile sets this
git config --global url."git@github.com:".insteadOf "https://github.com/"   # or a token
```

**See also:** [Running](Running), [Development](Development)
