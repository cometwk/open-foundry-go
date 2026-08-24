package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/openfoundry/runtime/obda/sqlast"
)

// Column is one PRAGMA table_info row.
type Column struct {
	Name string
	Type string
	PK   bool
}

// Snapshot is a source fingerprint for one table.
type Snapshot struct {
	Table   string
	Columns []Column
	Hash    string
}

// InspectTable reads PRAGMA table_info for a compiled identifier.
func InspectTable(ctx context.Context, db *sql.DB, table sqlast.Identifier) (Snapshot, error) {
	q, err := quote(table)
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+q+")")
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	var cols []Column
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return Snapshot{}, err
		}
		cols = append(cols, Column{Name: name, Type: ctype, PK: pk > 0})
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
	var b strings.Builder
	for _, c := range cols {
		fmt.Fprintf(&b, "%s:%s:%t;", c.Name, c.Type, c.PK)
	}
	sum := sha256.Sum256([]byte(b.String()))
	if len(cols) == 0 {
		exists, err := TableExists(ctx, db, table)
		if err != nil {
			return Snapshot{}, err
		}
		if !exists {
			return Snapshot{}, fmt.Errorf("sqlite: table %q not found", table.Name)
		}
	}
	return Snapshot{Table: table.Name, Columns: cols, Hash: hex.EncodeToString(sum[:])}, nil
}

// TableExists reports whether a table or view is present in sqlite_master.
func TableExists(ctx context.Context, db *sql.DB, table sqlast.Identifier) (bool, error) {
	if _, err := quote(table); err != nil {
		return false, err
	}
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'view') AND name = ?`, table.Name).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Index is one user or unique index on a table.
type Index struct {
	Name      string
	Unique    bool
	Partial   bool
	Columns   []string
	CreateSQL string
}

// InspectIndexes reads PRAGMA index_list / index_info and sqlite_master SQL.
func InspectIndexes(ctx context.Context, db *sql.DB, table sqlast.Identifier) ([]Index, error) {
	q, err := quote(table)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA index_list("+q+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Index
	for rows.Next() {
		var seq int
		var name string
		var unique, partial int
		var origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		idx := Index{Name: name, Unique: unique != 0, Partial: partial != 0}
		info, err := indexColumns(ctx, db, name)
		if err != nil {
			return nil, err
		}
		idx.Columns = info
		var create sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&create); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		idx.CreateSQL = create.String
		out = append(out, idx)
	}
	return out, rows.Err()
}

func indexColumns(ctx context.Context, db *sql.DB, name string) ([]string, error) {
	q, err := quote(sqlast.Identifier{Name: name})
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA index_info("+q+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type col struct {
		seq  int
		name string
	}
	var cols []col
	for rows.Next() {
		var seq, cid int
		var cname string
		if err := rows.Scan(&seq, &cid, &cname); err != nil {
			return nil, err
		}
		cols = append(cols, col{seq: seq, name: cname})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].seq < cols[j].seq })
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.name
	}
	return names, nil
}

// HasUniqueIndex reports whether a unique index covers exactly columns.
// When requireDeletedNull is true, CreateSQL must contain deleted_at IS NULL.
func HasUniqueIndex(indexes []Index, columns []string, requireDeletedNull bool) bool {
	want := append([]string(nil), columns...)
	sort.Strings(want)
	for _, idx := range indexes {
		if !idx.Unique {
			continue
		}
		got := append([]string(nil), idx.Columns...)
		sort.Strings(got)
		if len(got) != len(want) {
			continue
		}
		ok := true
		for i := range want {
			if got[i] != want[i] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if requireDeletedNull && !deletedAtIsNull(idx.CreateSQL) {
			continue
		}
		return true
	}
	return false
}

func deletedAtIsNull(createSQL string) bool {
	s := strings.ToLower(strings.ReplaceAll(createSQL, `"`, ""))
	return strings.Contains(s, "deleted_at is null")
}

// ProbeFTS5 reports whether this SQLite build compiled ENABLE_FTS5.
func ProbeFTS5(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA compile_options")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var opt string
		if err := rows.Scan(&opt); err != nil {
			return false, err
		}
		if strings.Contains(strings.ToUpper(opt), "ENABLE_FTS5") {
			return true, nil
		}
	}
	return false, rows.Err()
}
