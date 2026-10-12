package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// copyOrder is the tables in an order their references allow.
var copyOrder = []string{"users", "areas", "messages", "lastread", "scan_off", "mail", "file_areas", "files",
	"history", "peer_groups", "peer_push", "netmail_out", "api_tokens", "ssh_keys", "script_data", "calls"}

var columnPat = regexp.MustCompile(`(?m)^\s*("?\w+"?)\s+(\{\w+\})`)

// columns are a table's columns and their schema types ({int}, {bool}...),
// as tables has them.
func columns(table string) ([]string, map[string]string) {
	for _, t := range tables {
		if !strings.Contains(t, "EXISTS "+table+" (") {
			continue
		}
		var names []string
		kinds := map[string]string{}
		for _, m := range columnPat.FindAllStringSubmatch(t, -1) {
			names = append(names, m[1])
			kinds[m[1]] = m[2]
		}
		return names, kinds
	}
	return nil, nil
}

// Copy copies every table of src into dst, which must be empty (a new
// database: Open makes its tables).  progress is told each table's rows.
func Copy(src, dst *Store, progress func(table string, rows int)) error {
	var n int
	if err := dst.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("the database to copy into has users already: it must be new")
	}
	t, err := dst.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	for _, table := range copyOrder {
		cols, kinds := columns(table)
		rows, err := src.db.Query(`SELECT ` + strings.Join(cols, ", ") + ` FROM ` + table)
		if err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		ins := `INSERT INTO ` + table + ` (` + strings.Join(cols, ", ") + `) VALUES (?` +
			strings.Repeat(", ?", len(cols)-1) + `)`
		count := 0
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return fmt.Errorf("%s: %w", table, err)
			}
			for i, c := range cols {
				vals[i] = convert(vals[i], kinds[c])
			}
			if _, err := t.Exec(ins, vals...); err != nil {
				rows.Close()
				return fmt.Errorf("%s: %w", table, err)
			}
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		if progress != nil {
			progress(table, count)
		}
	}
	if err := t.Commit(); err != nil {
		return err
	}
	// ids go on from the copied ones
	if dst.db.d.name == Postgres {
		for _, table := range copyOrder {
			if cols, _ := columns(table); len(cols) > 0 && cols[0] == "id" {
				if _, err := dst.db.Exec(`SELECT setval(pg_get_serial_sequence('` + table + `', 'id'),
					COALESCE((SELECT MAX(id) FROM ` + table + `), 0) + 1, false)`); err != nil {
					return fmt.Errorf("%s: %w", table, err)
				}
			}
		}
	}
	return nil
}

// convert makes a value read from one engine one the other takes for its
// column: booleans from 0 and 1, strings from bytes (but bytes' columns).
func convert(v any, kind string) any {
	switch kind {
	case "{bool}":
		switch b := v.(type) {
		case int64:
			return b != 0
		case []byte:
			return string(b) == "1" || strings.EqualFold(string(b), "true")
		}
	case "{blob}":
		if s, ok := v.(string); ok {
			return []byte(s)
		}
	default:
		if b, ok := v.([]byte); ok {
			return string(b)
		}
	}
	return v
}
