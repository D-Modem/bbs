package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// testDatabase is, under go test with BBS_TEST_DATABASE set to a
// PostgreSQL or MySQL server's address, a database of its own there for
// each store the tests open (in place of their SQLite files): so that the
// whole suite runs on that engine.  It returns the address, and what drops
// the database after.
func testDatabase() (string, func(), error) {
	base := os.Getenv("BBS_TEST_DATABASE")
	if base == "" || !testing.Testing() {
		return "", nil, nil
	}
	var b [6]byte
	rand.Read(b[:])
	name := "bbs_test_" + hex.EncodeToString(b[:])
	switch {
	case strings.HasPrefix(base, "postgres"):
		admin, err := sql.Open("pgx", base)
		if err != nil {
			return "", nil, err
		}
		defer admin.Close()
		if _, err := admin.Exec("CREATE SCHEMA " + name); err != nil {
			return "", nil, err
		}
		u, err := url.Parse(base)
		if err != nil {
			return "", nil, err
		}
		q := u.Query()
		q.Set("search_path", name)
		u.RawQuery = q.Encode()
		return u.String(), func() { dropTest("pgx", base, "DROP SCHEMA "+name+" CASCADE") }, nil
	case strings.HasPrefix(base, "mysql"), strings.HasPrefix(base, "mariadb"):
		cfg, err := mysqlConfig(base)
		if err != nil {
			return "", nil, err
		}
		admin, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return "", nil, err
		}
		defer admin.Close()
		if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
			return "", nil, err
		}
		cfg.DBName = name
		return "mysql:" + cfg.FormatDSN(), func() {
			c := *cfg
			c.DBName = ""
			dropTest("mysql", c.FormatDSN(), "DROP DATABASE "+name)
		}, nil
	}
	return "", nil, fmt.Errorf("BBS_TEST_DATABASE: %q is not postgres://, mysql:// or mariadb://", base)
}

// dropTest runs stmt on the server, as best it can: what drops a test's
// database after.
func dropTest(driver, dsn, stmt string) {
	if d, err := sql.Open(driver, dsn); err == nil {
		d.Exec(stmt)
		d.Close()
	}
}
