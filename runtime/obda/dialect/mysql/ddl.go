package mysql

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/obda/sqlast"
)

// ActiveKeyColumn is a virtual generated column backing cardinality unique
// indexes: 1 while the row is live, NULL once soft-deleted. MySQL has no
// partial indexes, so the sqlite "UNIQUE ... WHERE deleted_at IS NULL"
// constraint becomes UNIQUE (..., of_active); NULL values never collide,
// so at most one live row per endpoint is enforced exactly like sqlite.
const ActiveKeyColumn = "of_active"

// MappedTableStatements generates CREATE TABLE / UNIQUE INDEX for compiled
// mapped relations. It never emits of_* sidecar objects; of_active is a
// column inside the mapped link table, not a separate relation. Unlike
// sqlite, MySQL has no "CREATE INDEX IF NOT EXISTS"; running it twice
// against one schema fails on the second duplicate index.
func MappedTableStatements(compiled *obda.Compiled) ([]string, error) {
	if compiled == nil {
		return nil, fmt.Errorf("mysql: nil compiled mapping")
	}
	expect := obda.PhysicalSchema(compiled)
	models := tablesByName(compiled.Models)
	links := linksByName(compiled.Links)
	fks := inlineFKColumns(compiled)
	var stmts []string
	for _, tbl := range expect.Tables {
		if m, ok := models[tbl.Name]; ok {
			activeKey := needsActiveKey(tbl)
			s, err := createTableStmt(tbl.Name, modelDDLColumns(m, tbl.Columns, fks), m.IdentityColumns, activeKey)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, s)
			for _, spec := range tbl.Uniques {
				name := uniqueIndexName(tbl.Name, spec, nil, nil)
				idx, err := uniqueIndex(tbl.Name, name, spec.Columns, spec.ExcludeSoftDeleted)
				if err != nil {
					return nil, err
				}
				stmts = append(stmts, idx)
			}
			continue
		}
		l, ok := links[tbl.Name]
		if !ok {
			return nil, fmt.Errorf("mysql: unexpected table %q", tbl.Name)
		}
		activeKey := needsActiveKey(tbl)
		s, err := createTableStmt(tbl.Name, linkDDLColumns(l, tbl.Columns), l.IdentityColumns, activeKey)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
		for _, spec := range tbl.Uniques {
			name := uniqueIndexName(tbl.Name, spec, l.FromColumns, l.ToColumns)
			idx, err := uniqueIndex(tbl.Name, name, spec.Columns, spec.ExcludeSoftDeleted)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, idx)
		}
	}
	return stmts, nil
}

// DropTableStatements emits DROP TABLE IF EXISTS for each mapped table,
// reverse of PhysicalSchema order so junction tables go before hosts.
func DropTableStatements(compiled *obda.Compiled) ([]string, error) {
	if compiled == nil {
		return nil, fmt.Errorf("mysql: nil compiled mapping")
	}
	tables := obda.PhysicalSchema(compiled).Tables
	stmts := make([]string, 0, len(tables))
	for i := len(tables) - 1; i >= 0; i-- {
		q, err := quote(sqlast.Identifier{Name: tables[i].Name})
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, "DROP TABLE IF EXISTS "+q)
	}
	return stmts, nil
}

func tablesByName(models map[string]*obda.CompiledModel) map[string]*obda.CompiledModel {
	out := make(map[string]*obda.CompiledModel, len(models))
	for _, m := range models {
		out[m.Table] = m
	}
	return out
}

func linksByName(links map[string]*obda.CompiledLink) map[string]*obda.CompiledLink {
	out := make(map[string]*obda.CompiledLink, len(links))
	for _, l := range links {
		if l.Inline {
			continue
		}
		out[l.Table] = l
	}
	return out
}

func inlineFKColumns(compiled *obda.Compiled) map[string]struct{} {
	out := map[string]struct{}{}
	for _, l := range compiled.Links {
		if l.Inline && l.FKColumn != "" {
			out[l.FKColumn] = struct{}{}
		}
	}
	return out
}

func needsActiveKey(tbl obda.PhysicalTable) bool {
	for _, spec := range tbl.Uniques {
		if spec.ExcludeSoftDeleted {
			return true
		}
	}
	return false
}

func uniqueIndexName(table string, spec obda.UniqueSpec, from, to []string) string {
	if uniqueMatches(spec.Columns, from) {
		return table + "_from_active"
	}
	if uniqueMatches(spec.Columns, to) {
		return table + "_to_active"
	}
	if n := len(spec.Columns); n > 0 {
		return table + "_" + spec.Columns[n-1] + "_active"
	}
	return table + "_from_active"
}

func uniqueMatches(spec, endpoint []string) bool {
	if eqStrings(spec, endpoint) {
		return true
	}
	return len(spec) == len(endpoint)+1 && eqStrings(spec[1:], endpoint)
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type ddlColumn struct {
	name       string
	typ        string
	notNull    bool
	defaultSQL string
}

func modelDDLColumns(m *obda.CompiledModel, cols []obda.PhysicalColumn, fks map[string]struct{}) []ddlColumn {
	out := make([]ddlColumn, 0, len(cols))
	for _, col := range cols {
		if _, ok := fks[col.Name]; ok {
			out = append(out, ddlColumn{name: col.Name, typ: "VARCHAR(255)", notNull: !col.Nullable})
			continue
		}
		out = append(out, typedColumn(col.Name, m.IdentityColumns, m.TenantColumn, nil, nil, m.Fields, m.PropertyTypes))
	}
	return out
}

func linkDDLColumns(l *obda.CompiledLink, cols []obda.PhysicalColumn) []ddlColumn {
	out := make([]ddlColumn, 0, len(cols))
	for _, col := range cols {
		out = append(out, typedColumn(col.Name, l.IdentityColumns, l.TenantColumn, l.FromColumns, l.ToColumns, l.Fields, l.PropertyTypes))
	}
	return out
}

func typedColumn(name string, identity []string, tenant string, from, to []string, fields []obda.CompiledField, types map[string]string) ddlColumn {
	if in(identity, name) || name == tenant || in(from, name) || in(to, name) {
		return ddlColumn{name: name, typ: "VARCHAR(255)", notNull: true}
	}
	for _, f := range fields {
		if f.Column == name {
			return ddlColumn{name: name, typ: sqlType(types[f.Logical])}
		}
	}
	switch name {
	case "version":
		return ddlColumn{name: name, typ: "BIGINT", notNull: true, defaultSQL: "1"}
	case "created_at", "updated_at":
		return ddlColumn{name: name, typ: "VARCHAR(64)", notNull: true}
	case "deleted_at":
		return ddlColumn{name: name, typ: "VARCHAR(64)"}
	default:
		return ddlColumn{name: name, typ: "TEXT"}
	}
}

func in(xs []string, name string) bool {
	for _, x := range xs {
		if x == name {
			return true
		}
	}
	return false
}

func activeKeyDef() (string, error) {
	del, err := quote(sqlast.Identifier{Name: "deleted_at"})
	if err != nil {
		return "", err
	}
	ak, err := quote(sqlast.Identifier{Name: ActiveKeyColumn})
	if err != nil {
		return "", err
	}
	return ak + " TINYINT GENERATED ALWAYS AS (IF(" + del + " IS NULL, 1, NULL)) VIRTUAL", nil
}

func createTableStmt(table string, cols []ddlColumn, identity []string, activeKey bool) (string, error) {
	tbl, err := quote(sqlast.Identifier{Name: table})
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(cols)+2)
	for _, c := range cols {
		q, err := quote(sqlast.Identifier{Name: c.name})
		if err != nil {
			return "", err
		}
		def := q + " " + c.typ
		if len(identity) == 1 && identity[0] == c.name {
			def += " PRIMARY KEY"
		} else if c.notNull {
			def += " NOT NULL"
		}
		if c.defaultSQL != "" {
			def += " DEFAULT " + c.defaultSQL
		}
		parts = append(parts, def)
	}
	if len(identity) > 1 {
		quoted := make([]string, len(identity))
		for i, name := range identity {
			q, err := quote(sqlast.Identifier{Name: name})
			if err != nil {
				return "", err
			}
			quoted[i] = q
		}
		parts = append(parts, "PRIMARY KEY ("+strings.Join(quoted, ", ")+")")
	}
	if activeKey {
		def, err := activeKeyDef()
		if err != nil {
			return "", err
		}
		parts = append(parts, def)
	}
	return "CREATE TABLE IF NOT EXISTS " + tbl + " (\n  " + strings.Join(parts, ",\n  ") + "\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4", nil
}

func uniqueIndex(table, name string, columns []string, activeKey bool) (string, error) {
	idx, err := quote(sqlast.Identifier{Name: name})
	if err != nil {
		return "", err
	}
	tbl, err := quote(sqlast.Identifier{Name: table})
	if err != nil {
		return "", err
	}
	quoted := make([]string, len(columns))
	for i, c := range columns {
		q, err := quote(sqlast.Identifier{Name: c})
		if err != nil {
			return "", err
		}
		quoted[i] = q
	}
	if activeKey {
		q, err := quote(sqlast.Identifier{Name: ActiveKeyColumn})
		if err != nil {
			return "", err
		}
		quoted = append(quoted, q)
	}
	return "CREATE UNIQUE INDEX " + idx + "\n  ON " + tbl + " (" + strings.Join(quoted, ", ") + ")", nil
}

func sqlType(odl string) string {
	switch strings.ToLower(odl) {
	case "integer", "int", "long":
		return "BIGINT"
	case "boolean", "bool":
		return "TINYINT"
	case "double", "float", "decimal":
		return "DOUBLE"
	default:
		return "TEXT"
	}
}
