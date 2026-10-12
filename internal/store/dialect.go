package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // "pgx"
	_ "modernc.org/sqlite"             // "sqlite"
)

// The engines the store runs on.
const (
	SQLite   = "sqlite"
	Postgres = "postgres"
	MySQL    = "mysql"
)

// dialect is what differs between the engines: the store's SQL is written
// once, with ? placeholders and SQLite's forms, and made the engine's here.
type dialect struct {
	name string
	// the schema's types
	types map[string]string
}

var dialects = map[string]*dialect{
	SQLite: {name: SQLite, types: map[string]string{
		"{id}": "INTEGER PRIMARY KEY", "{int}": "INTEGER", "{bool}": "INTEGER",
		"{name}": "TEXT COLLATE NOCASE", "{key}": "TEXT", "{str}": "TEXT", "{text}": "TEXT", "{blob}": "BLOB",
	}},
	// names: a case-insensitive ICU collation (made in Open)
	Postgres: {name: Postgres, types: map[string]string{
		"{id}": "BIGSERIAL PRIMARY KEY", "{int}": "BIGINT", "{bool}": "BOOLEAN",
		"{name}": "TEXT COLLATE nocase", "{key}": "TEXT", "{str}": "TEXT", "{text}": "TEXT", "{blob}": "BYTEA",
	}},
	// indexed strings are VARCHARs; names case-insensitive, not accent-
	// insensitive; keys (Message-IDs, hashes) exact
	MySQL: {name: MySQL, types: map[string]string{
		"{id}": "BIGINT AUTO_INCREMENT PRIMARY KEY", "{int}": "BIGINT", "{bool}": "BOOLEAN",
		"{name}": "VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_ci",
		"{key}":  "VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin",
		"{str}":  "VARCHAR(1024) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs",
		"{text}": "MEDIUMTEXT CHARACTER SET utf8mb4", "{blob}": "LONGBLOB",
	}},
}

// ddl is a schema statement in the engine's types.
func (d *dialect) ddl(s string) string {
	for k, v := range d.types {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

var (
	conflictPat = regexp.MustCompile(`(?s)ON CONFLICT \([^)]*\) DO UPDATE SET`)
	excludedPat = regexp.MustCompile(`excluded\.(\w+)`)
)

// query is a statement in the engine's SQL: its placeholders ($1 ... for
// PostgreSQL), INSERT OR IGNORE, upserts, {greatest}.
func (d *dialect) query(q string) string {
	switch d.name {
	case SQLite:
		return strings.ReplaceAll(q, "{greatest}", "MAX")
	case MySQL:
		q = strings.ReplaceAll(q, "{greatest}", "GREATEST")
		q = strings.Replace(q, "INSERT OR IGNORE INTO", "INSERT IGNORE INTO", 1)
		if conflictPat.MatchString(q) {
			q = conflictPat.ReplaceAllString(q, "ON DUPLICATE KEY UPDATE")
			q = excludedPat.ReplaceAllString(q, "VALUES($1)")
		}
		return q
	}
	q = strings.ReplaceAll(q, "{greatest}", "GREATEST")
	if strings.HasPrefix(q, "INSERT OR IGNORE INTO") {
		q = "INSERT INTO" + strings.TrimPrefix(q, "INSERT OR IGNORE INTO") + " ON CONFLICT DO NOTHING"
	}
	return rebind(q)
}

// rebind numbers the ? placeholders, outside quotes.
func rebind(q string) string {
	var b strings.Builder
	n := 0
	quote := byte(0)
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '?':
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// isUnique reports a unique constraint's violation, in any engine's words.
func isUnique(err error) bool {
	if err == nil {
		return false
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE") || strings.Contains(s, "SQLSTATE 23505") ||
		strings.Contains(s, "duplicate key value")
}

// db is the database, its statements made the engine's.
type db struct {
	*sql.DB
	d *dialect
}

func (x *db) Exec(q string, args ...any) (sql.Result, error) {
	return x.DB.Exec(x.d.query(q), args...)
}

func (x *db) Query(q string, args ...any) (*sql.Rows, error) {
	return x.DB.Query(x.d.query(q), args...)
}

func (x *db) QueryRow(q string, args ...any) *sql.Row {
	return x.DB.QueryRow(x.d.query(q), args...)
}

func (x *db) Begin() (*tx, error) {
	t, err := x.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &tx{Tx: t, d: x.d}, nil
}

// insertID runs an INSERT and returns the new row's id.
func (x *db) insertID(q string, args ...any) (int64, error) {
	return insertID(x.d, x.DB.Exec, x.DB.QueryRow, q, args)
}

// tx is a transaction, its statements made the engine's.
type tx struct {
	*sql.Tx
	d *dialect
}

func (t *tx) Exec(q string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(t.d.query(q), args...)
}

func (t *tx) insertID(q string, args ...any) (int64, error) {
	return insertID(t.d, t.Tx.Exec, t.Tx.QueryRow, q, args)
}

func insertID(d *dialect, exec func(string, ...any) (sql.Result, error),
	row func(string, ...any) *sql.Row, q string, args []any) (int64, error) {
	if d.name == MySQL {
		res, err := exec(d.query(q), args...)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	// SQLite (3.35 and later) and PostgreSQL say it themselves
	var id int64
	err := row(d.query(q+" RETURNING id"), args...).Scan(&id)
	return id, err
}

// hasColumn reports whether a table has a column (for migrations).
func (x *db) hasColumn(table, column string) (bool, error) {
	var q string
	switch x.d.name {
	case SQLite:
		q = `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`
	case Postgres:
		q = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`
	case MySQL:
		q = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`
	}
	var n int
	err := x.QueryRow(q, table, column).Scan(&n)
	return n > 0, err
}

// index makes an index if there is none of that name.
func (x *db) index(name, table, cols string, unique bool) error {
	kind := "INDEX"
	if unique {
		kind = "UNIQUE INDEX"
	}
	if x.d.name == MySQL {
		var n int
		if err := x.QueryRow(`SELECT COUNT(*) FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, table, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		_, err := x.Exec(fmt.Sprintf("CREATE %s %s ON %s (%s)", kind, name, table, cols))
		return err
	}
	_, err := x.Exec(fmt.Sprintf("CREATE %s IF NOT EXISTS %s ON %s (%s)", kind, name, table, cols))
	return err
}

// openDB opens a database by its address: a path (SQLite), postgres://
// or postgresql:// (PostgreSQL, as pgx takes it), mysql://user:pass@host:port/db
// or mysql:DSN (MySQL, go-sql-driver's form).
func openDB(addr string) (*db, error) {
	switch {
	case strings.HasPrefix(addr, "postgres://"), strings.HasPrefix(addr, "postgresql://"):
		sdb, err := sql.Open("pgx", addr)
		if err != nil {
			return nil, err
		}
		sdb.SetMaxOpenConns(10)
		return &db{DB: sdb, d: dialects[Postgres]}, nil
	case strings.HasPrefix(addr, "mysql:"):
		cfg, err := mysqlConfig(addr)
		if err != nil {
			return nil, err
		}
		sdb, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return nil, err
		}
		sdb.SetMaxOpenConns(10)
		return &db{DB: sdb, d: dialects[MySQL]}, nil
	}
	return nil, nil // SQLite: Open does it
}

// mysqlConfig reads mysql://user:password@host:port/database?params, or
// mysql:DSN; with the SQL mode the store's SQL wants (ANSI quotes, strict).
func mysqlConfig(addr string) (*mysql.Config, error) {
	var cfg *mysql.Config
	if strings.HasPrefix(addr, "mysql://") {
		u, err := url.Parse(addr)
		if err != nil {
			return nil, fmt.Errorf("database: %w", err)
		}
		cfg = mysql.NewConfig()
		cfg.User = u.User.Username()
		cfg.Passwd, _ = u.User.Password()
		cfg.Net, cfg.Addr = "tcp", u.Host
		if !strings.Contains(u.Host, ":") {
			cfg.Addr += ":3306"
		}
		cfg.DBName = strings.TrimPrefix(u.Path, "/")
		for k, v := range u.Query() {
			if cfg.Params == nil {
				cfg.Params = map[string]string{}
			}
			cfg.Params[k] = v[0]
		}
	} else {
		var err error
		if cfg, err = mysql.ParseDSN(strings.TrimPrefix(addr, "mysql:")); err != nil {
			return nil, fmt.Errorf("database: %w", err)
		}
	}
	if cfg.DBName == "" {
		return nil, errors.New("database: a MySQL database's name: mysql://user:password@host/bbs")
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["sql_mode"] = "'ANSI_QUOTES,STRICT_TRANS_TABLES,ONLY_FULL_GROUP_BY,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'"
	cfg.Params["charset"] = "utf8mb4"
	// rows matched, not changed, as the others count them (an UPDATE to the
	// same value is not "not found")
	cfg.ClientFoundRows = true
	return cfg, nil
}

// IsFile reports whether a database address is a file's (SQLite), not a
// server's.
func IsFile(addr string) bool {
	return !strings.HasPrefix(addr, "postgres://") && !strings.HasPrefix(addr, "postgresql://") &&
		!strings.HasPrefix(addr, "mysql:")
}

// Engine is the store's: SQLite, Postgres or MySQL.
func (s *Store) Engine() string { return s.db.d.name }

// Describe is the database's address without its password, for logs.
func Describe(addr string) string {
	if u, err := url.Parse(addr); err == nil && u.User != nil {
		if _, set := u.User.Password(); set {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
			return u.String()
		}
	}
	if strings.HasPrefix(addr, "mysql:") && !strings.HasPrefix(addr, "mysql://") {
		if cfg, err := mysql.ParseDSN(strings.TrimPrefix(addr, "mysql:")); err == nil && cfg.Passwd != "" {
			cfg.Passwd = "xxxxx"
			return "mysql:" + cfg.FormatDSN()
		}
	}
	return addr
}
