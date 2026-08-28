package mysql

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

// Column is one information_schema.COLUMNS row.
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

// InspectTable reads information_schema.COLUMNS for a compiled identifier.
// The DSN must select a database: tables are resolved within DATABASE().
func InspectTable(ctx context.Context, db *sql.DB, table sqlast.Identifier) (Snapshot, error) {
	if _, err := quote(table); err != nil {
		return Snapshot{}, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_KEY
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table.Name)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	var cols []Column
	for rows.Next() {
		var name, ctype, key string
		if err := rows.Scan(&name, &ctype, &key); err != nil {
			return Snapshot{}, err
		}
		cols = append(cols, Column{Name: name, Type: ctype, PK: key == "PRI"})
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	if len(cols) == 0 {
		return Snapshot{}, fmt.Errorf("mysql: table %q not found", table.Name)
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
	var b strings.Builder
	for _, c := range cols {
		fmt.Fprintf(&b, "%s:%s:%t;", c.Name, c.Type, c.PK)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return Snapshot{Table: table.Name, Columns: cols, Hash: hex.EncodeToString(sum[:])}, nil
}

// TableExists reports whether a table or view is present in the current database.
func TableExists(ctx context.Context, db *sql.DB, table sqlast.Identifier) (bool, error) {
	if _, err := quote(table); err != nil {
		return false, err
	}
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		  AND TABLE_TYPE IN ('BASE TABLE', 'VIEW')`, table.Name).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Index is one index on a table. Partial is always false on MySQL; CreateSQL
// is not surfaced by information_schema and stays empty.
type Index struct {
	Name      string
	Unique    bool
	Partial   bool
	Columns   []string
	CreateSQL string
}

// InspectIndexes reads information_schema.STATISTICS.
func InspectIndexes(ctx context.Context, db *sql.DB, table sqlast.Identifier) ([]Index, error) {
	if _, err := quote(table); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`, table.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byName := map[string]*Index{}
	var order []string
	for rows.Next() {
		var name string
		var nonUnique int
		var seq int
		var col string
		if err := rows.Scan(&name, &nonUnique, &seq, &col); err != nil {
			return nil, err
		}
		idx, ok := byName[name]
		if !ok {
			idx = &Index{Name: name, Unique: nonUnique == 0}
			byName[name] = idx
			order = append(order, name)
		}
		idx.Columns = append(idx.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Index, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

// HasUniqueIndex reports whether a unique index covers exactly columns.
// When requireActiveKey is true the index must end with of_active, the
// MySQL stand-in for sqlite's partial "WHERE deleted_at IS NULL" filter.
func HasUniqueIndex(indexes []Index, columns []string, requireActiveKey bool) bool {
	want := append([]string(nil), columns...)
	if requireActiveKey {
		want = append(want, ActiveKeyColumn)
	}
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
		if ok {
			return true
		}
	}
	return false
}
