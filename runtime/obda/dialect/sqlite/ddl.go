package sqlite

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/obda/sqlast"
)

// MappedTableStatements generates CREATE TABLE / UNIQUE INDEX for compiled
// mapped relations. It never emits of_* sidecar objects.
func MappedTableStatements(compiled *obda.Compiled) ([]string, error) {
	if compiled == nil {
		return nil, fmt.Errorf("sqlite: nil compiled mapping")
	}
	expect := obda.PhysicalSchema(compiled)
	models := tablesByName(compiled.Models)
	links := linksByName(compiled.Links)
	var stmts []string
	for _, tbl := range expect.Tables {
		if m, ok := models[tbl.Name]; ok {
			s, err := createTableStmt(tbl.Name, modelDDLColumns(m, tbl.Columns), m.IdentityColumns)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, s)
			continue
		}
		l, ok := links[tbl.Name]
		if !ok {
			return nil, fmt.Errorf("sqlite: unexpected table %q", tbl.Name)
		}
		s, err := createTableStmt(tbl.Name, linkDDLColumns(l, tbl.Columns), l.IdentityColumns)
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
		out[l.Table] = l
	}
	return out
}

func uniqueIndexName(table string, spec obda.UniqueSpec, from, to []string) string {
	if uniqueMatches(spec.Columns, from) {
		return table + "_from_active"
	}
	if uniqueMatches(spec.Columns, to) {
		return table + "_to_active"
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

func modelDDLColumns(m *obda.CompiledModel, names []string) []ddlColumn {
	out := make([]ddlColumn, 0, len(names))
	for _, name := range names {
		out = append(out, typedColumn(name, m.IdentityColumns, m.TenantColumn, nil, nil, m.Fields, m.PropertyTypes))
	}
	return out
}

func linkDDLColumns(l *obda.CompiledLink, names []string) []ddlColumn {
	out := make([]ddlColumn, 0, len(names))
	for _, name := range names {
		out = append(out, typedColumn(name, l.IdentityColumns, l.TenantColumn, l.FromColumns, l.ToColumns, l.Fields, l.PropertyTypes))
	}
	return out
}

func typedColumn(name string, identity []string, tenant string, from, to []string, fields []obda.CompiledField, types map[string]string) ddlColumn {
	if in(identity, name) || name == tenant || in(from, name) || in(to, name) {
		return ddlColumn{name: name, typ: "TEXT", notNull: true}
	}
	for _, f := range fields {
		if f.Column == name {
			return ddlColumn{name: name, typ: sqlType(types[f.Logical])}
		}
	}
	switch name {
	case "version":
		return ddlColumn{name: name, typ: "INTEGER", notNull: true, defaultSQL: "1"}
	case "created_at", "updated_at":
		return ddlColumn{name: name, typ: "TEXT", notNull: true}
	case "deleted_at":
		return ddlColumn{name: name, typ: "TEXT"}
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

func createTableStmt(table string, cols []ddlColumn, identity []string) (string, error) {
	tbl, err := quote(sqlast.Identifier{Name: table})
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(cols)+1)
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
	return "CREATE TABLE IF NOT EXISTS " + tbl + " (\n  " + strings.Join(parts, ",\n  ") + "\n)", nil
}

func uniqueIndex(table, name string, columns []string, omitDeleted bool) (string, error) {
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
	sql := "CREATE UNIQUE INDEX IF NOT EXISTS " + idx + "\n  ON " + tbl + " (" + strings.Join(quoted, ", ") + ")"
	if omitDeleted {
		del, err := quote(sqlast.Identifier{Name: "deleted_at"})
		if err != nil {
			return "", err
		}
		sql += " WHERE " + del + " IS NULL"
	}
	return sql, nil
}

func sqlType(odl string) string {
	switch strings.ToLower(odl) {
	case "integer", "int", "long":
		return "INTEGER"
	case "boolean", "bool":
		return "INTEGER"
	case "double", "float", "decimal":
		return "REAL"
	default:
		return "TEXT"
	}
}
