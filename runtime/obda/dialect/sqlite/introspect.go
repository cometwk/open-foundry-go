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
	return Snapshot{Table: table.Name, Columns: cols, Hash: hex.EncodeToString(sum[:])}, nil
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
