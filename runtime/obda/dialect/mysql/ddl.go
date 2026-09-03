package mysql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
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
	var stmts []string
	modelNames := make([]string, 0, len(compiled.Models))
	for name := range compiled.Models {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)
	for _, name := range modelNames {
		m := compiled.Models[name]
		s, err := createTableStmt(m.Table, modelColumns(m), m.IdentityColumns, false)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
	}
	linkNames := make([]string, 0, len(compiled.Links))
	for name := range compiled.Links {
		linkNames = append(linkNames, name)
	}
	sort.Strings(linkNames)
	for _, name := range linkNames {
		l := compiled.Links[name]
		activeKey := needsActiveKey(l)
		s, err := createTableStmt(l.Table, linkColumns(l), l.IdentityColumns, activeKey)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
		idx, err := cardinalityIndexes(l)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, idx...)
	}
	return stmts, nil
}

type ddlColumn struct {
	name       string
	typ        string
	notNull    bool
	defaultSQL string
}

func modelColumns(m *obda.CompiledModel) []ddlColumn {
	seen := map[string]struct{}{}
	var cols []ddlColumn
	add := func(c ddlColumn) {
		if c.name == "" {
			return
		}
		if _, ok := seen[c.name]; ok {
			return
		}
		seen[c.name] = struct{}{}
		cols = append(cols, c)
	}
	for _, name := range m.IdentityColumns {
		add(ddlColumn{name: name, typ: "VARCHAR(255)", notNull: true})
	}
	if m.TenantColumn != "" {
		add(ddlColumn{name: m.TenantColumn, typ: "VARCHAR(255)", notNull: true})
	}
	for _, f := range m.Fields {
		add(ddlColumn{name: f.Column, typ: sqlType(m.PropertyTypes[f.Logical])})
	}
	addSystemColumns(&cols, seen, m.Omit)
	return cols
}

func linkColumns(l *obda.CompiledLink) []ddlColumn {
	seen := map[string]struct{}{}
	var cols []ddlColumn
	add := func(c ddlColumn) {
		if c.name == "" {
			return
		}
		if _, ok := seen[c.name]; ok {
			return
		}
		seen[c.name] = struct{}{}
		cols = append(cols, c)
	}
	for _, name := range l.IdentityColumns {
		add(ddlColumn{name: name, typ: "VARCHAR(255)", notNull: true})
	}
	if l.TenantColumn != "" {
		add(ddlColumn{name: l.TenantColumn, typ: "VARCHAR(255)", notNull: true})
	}
	for _, name := range l.FromColumns {
		add(ddlColumn{name: name, typ: "VARCHAR(255)", notNull: true})
	}
	for _, name := range l.ToColumns {
		add(ddlColumn{name: name, typ: "VARCHAR(255)", notNull: true})
	}
	for _, f := range l.Fields {
		add(ddlColumn{name: f.Column, typ: sqlType(l.PropertyTypes[f.Logical])})
	}
	addSystemColumns(&cols, seen, l.Omit)
	return cols
}

func addSystemColumns(cols *[]ddlColumn, seen map[string]struct{}, omit obda.OmitFlags) {
	add := func(c ddlColumn) {
		if _, ok := seen[c.name]; ok {
			return
		}
		seen[c.name] = struct{}{}
		*cols = append(*cols, c)
	}
	if !omit.Version {
		add(ddlColumn{name: "version", typ: "BIGINT", notNull: true, defaultSQL: "1"})
	}
	if !omit.CreatedAt {
		add(ddlColumn{name: "created_at", typ: "VARCHAR(64)", notNull: true})
	}
	if !omit.UpdatedAt {
		add(ddlColumn{name: "updated_at", typ: "VARCHAR(64)", notNull: true})
	}
	if !omit.DeletedAt {
		add(ddlColumn{name: "deleted_at", typ: "VARCHAR(64)"})
	}
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

// needsActiveKey reports whether the link's cardinality unique indexes must
// exclude soft-deleted rows, which MySQL expresses through of_active.
func needsActiveKey(l *obda.CompiledLink) bool {
	if l.Omit.DeletedAt {
		return false
	}
	switch l.Cardinality {
	case spi.CardinalityManyToOne, spi.CardinalityOneToMany, spi.CardinalityOneToOne:
		return true
	default:
		return false
	}
}

func cardinalityIndexes(l *obda.CompiledLink) ([]string, error) {
	activeKey := needsActiveKey(l)
	switch l.Cardinality {
	case spi.CardinalityManyToOne:
		s, err := uniqueIndex(l, l.Table+"_from_active", appendTenant(l.TenantColumn, l.FromColumns), activeKey)
		if err != nil {
			return nil, err
		}
		return []string{s}, nil
	case spi.CardinalityOneToMany:
		s, err := uniqueIndex(l, l.Table+"_to_active", appendTenant(l.TenantColumn, l.ToColumns), activeKey)
		if err != nil {
			return nil, err
		}
		return []string{s}, nil
	case spi.CardinalityOneToOne:
		from, err := uniqueIndex(l, l.Table+"_from_active", appendTenant(l.TenantColumn, l.FromColumns), activeKey)
		if err != nil {
			return nil, err
		}
		to, err := uniqueIndex(l, l.Table+"_to_active", appendTenant(l.TenantColumn, l.ToColumns), activeKey)
		if err != nil {
			return nil, err
		}
		return []string{from, to}, nil
	default:
		return nil, nil
	}
}

func appendTenant(tenant string, cols []string) []string {
	if tenant == "" {
		return append([]string(nil), cols...)
	}
	out := make([]string, 0, len(cols)+1)
	out = append(out, tenant)
	return append(out, cols...)
}

func uniqueIndex(l *obda.CompiledLink, name string, columns []string, activeKey bool) (string, error) {
	idx, err := quote(sqlast.Identifier{Name: name})
	if err != nil {
		return "", err
	}
	tbl, err := quote(sqlast.Identifier{Name: l.Table})
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
