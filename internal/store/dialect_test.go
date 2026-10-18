package store

import (
	"strings"
	"testing"
)

// TestQuery: statements made each engine's: PostgreSQL's numbered marks (not
// in quotes), INSERT OR IGNORE and the upsert each engine's way, and MySQL's
// table definitions.
func TestQuery(t *testing.T) {
	pg, my, lite := dialects[Postgres], dialects[MySQL], dialects[SQLite]
	q := `SELECT a FROM t WHERE x = ? AND y = '?' AND "z?" = ? LIMIT ?`
	if got := pg.query(q); got != `SELECT a FROM t WHERE x = $1 AND y = '?' AND "z?" = $2 LIMIT $3` {
		t.Errorf("rebind: %s", got)
	}
	if lite.query(q) != q || my.query(q) != q {
		t.Error("? changed for SQLite or MySQL")
	}
	ins := `INSERT OR IGNORE INTO history (message_id, seen) VALUES (?, ?)`
	if got := pg.query(ins); got != `INSERT INTO history (message_id, seen) VALUES ($1, $2) ON CONFLICT DO NOTHING` {
		t.Errorf("pg ignore: %s", got)
	}
	if got := my.query(ins); got != `INSERT IGNORE INTO history (message_id, seen) VALUES (?, ?)` {
		t.Errorf("mysql ignore: %s", got)
	}
	up := `INSERT INTO lastread (user_id, area_id, message_id) VALUES (?, ?, ?)
		ON CONFLICT (user_id, area_id) DO UPDATE SET message_id = {greatest}(lastread.message_id, excluded.message_id)`
	if got := my.query(up); !strings.Contains(got, "ON DUPLICATE KEY UPDATE message_id = GREATEST(lastread.message_id, VALUES(message_id))") {
		t.Errorf("mysql upsert: %s", got)
	}
	if got := lite.query(up); !strings.Contains(got, "MAX(lastread.message_id, excluded.message_id)") {
		t.Errorf("sqlite upsert: %s", got)
	}
	if got := pg.query(up); !strings.Contains(got, "GREATEST(lastread.message_id, excluded.message_id)") || !strings.Contains(got, "$3") {
		t.Errorf("pg upsert: %s", got)
	}
	if got := my.ddl("id {id}, n {name} NOT NULL UNIQUE, b {bool}"); !strings.Contains(got, "AUTO_INCREMENT") || !strings.Contains(got, "utf8mb4_0900_as_ci") {
		t.Errorf("mysql ddl: %s", got)
	}
}

// TestAddresses: Describe hides an address's password; MySQL and MariaDB
// addresses become the driver's settings, a database name wanted; IsFile
// tells a SQLite file from a server.
func TestAddresses(t *testing.T) {
	for addr, want := range map[string]string{
		"postgres://bbs:secret@db/bbs":          "postgres://bbs:xxxxx@db/bbs",
		"mysql://bbs:secret@db:3306/bbs":        "mysql://bbs:xxxxx@db:3306/bbs",
		"mysql:bbs:secret@unix(/run/my.sock)/b": "mysql:bbs:xxxxx@unix(/run/my.sock)/b",
		"mariadb://bbs:secret@db/bbs":           "mariadb://bbs:xxxxx@db/bbs",
		"mariadb:bbs:secret@tcp(db:3306)/bbs":   "mariadb:bbs:xxxxx@tcp(db:3306)/bbs",
		"/var/lib/bbs/bbs.db":                   "/var/lib/bbs/bbs.db",
	} {
		if got := Describe(addr); !strings.HasPrefix(got, want) || strings.Contains(got, "secret") {
			t.Errorf("%s: %s", addr, got)
		}
	}
	cfg, err := mysqlConfig("mysql://bbs:pw@db.example/bbs?tls=true")
	if err != nil || cfg.Addr != "db.example:3306" || cfg.User != "bbs" || cfg.Passwd != "pw" || cfg.DBName != "bbs" ||
		!strings.Contains(cfg.Params["sql_mode"], "ANSI_QUOTES") || !cfg.ClientFoundRows {
		t.Errorf("mysql config: %+v %v", cfg, err)
	}
	if _, err := mysqlConfig("mysql://bbs:pw@db/"); err == nil {
		t.Error("no database name taken")
	}
	if !IsFile("bbs.db") || !IsFile("sqlite:x.db") || IsFile("postgresql://x/y") || IsFile("mysql:u@/d") || IsFile("mariadb://u@h/d") {
		t.Error("IsFile")
	}
}
