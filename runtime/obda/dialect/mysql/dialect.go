package mysql

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/obda/dialect"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

var _ dialect.Dialect = (*Dialect)(nil)

// Dialect is the MySQL 8.0 adapter. It assumes 8.0+ features: recursive
// CTE, window functions, JSON, FULLTEXT, generated columns, online index.
type Dialect struct{}

// New returns a MySQL dialect.
func New() *Dialect { return &Dialect{} }

func (d *Dialect) Name() string { return "mysql" }

func (d *Dialect) Capabilities() dialect.Capabilities {
	return dialect.Capabilities{
		Transactions:     true,
		Savepoints:       true,
		RecursiveCTE:     true,
		FullTextSearch:   true,
		GeneratedColumns: true,
		JSON:             true,
		Spatial:          true,
		OnlineIndex:      true,
	}
}

func (d *Dialect) QuoteIdentifier(id sqlast.Identifier) (string, error) {
	return quote(id)
}

func (d *Dialect) Placeholder(position int) string { return "?" }

func (d *Dialect) Render(stmt sqlast.Statement) (dialect.SQLStatement, error) {
	switch s := stmt.(type) {
	case *sqlast.Select:
		return d.renderSelect(s)
	case *sqlast.Insert:
		return d.renderInsert(s)
	case *sqlast.Update:
		return d.renderUpdate(s)
	case *sqlast.Delete:
		return d.renderDelete(s)
	case *sqlast.AggregateSelect:
		return d.renderAggregateSelect(s)
	default:
		return dialect.SQLStatement{}, fmt.Errorf("%w: render %T", spi.ErrUnsupportedCapability, stmt)
	}
}

func (d *Dialect) NormalizeValue(odlType string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch strings.ToLower(odlType) {
	case "boolean", "bool":
		switch t := v.(type) {
		case bool:
			return t, nil
		case int64:
			return t != 0, nil
		case int:
			return t != 0, nil
		case float64:
			return t != 0, nil
		case []byte:
			return len(t) != 0 && t[0] != '0', nil
		default:
			return nil, fmt.Errorf("mysql: boolean %T", v)
		}
	case "integer", "int":
		switch t := v.(type) {
		case int:
			return t, nil
		case int64:
			return int(t), nil
		case float64:
			return int(t), nil
		default:
			return nil, fmt.Errorf("mysql: int %T", v)
		}
	default:
		return v, nil
	}
}

func (d *Dialect) renderSelect(s *sqlast.Select) (dialect.SQLStatement, error) {
	from, err := quoteTable(s.From, s.As)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	parts := make([]string, 0, len(s.Columns)+1)
	if len(s.Columns) > 0 {
		for _, c := range s.Columns {
			id, ok := c.(sqlast.Identifier)
			if !ok {
				return dialect.SQLStatement{}, fmt.Errorf("mysql: select column %T", c)
			}
			q, err := quote(id)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			parts = append(parts, q)
		}
	}
	if s.Search != nil {
		match, err := renderMatchAgainst(s.Search)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		if len(parts) == 0 {
			parts = append(parts, "*")
		}
		parts = append(parts, match+" AS `of_score`")
	}
	cols := "*"
	if len(parts) > 0 {
		cols = strings.Join(parts, ", ")
	}
	sql := "SELECT " + cols + " FROM " + from
	for _, j := range s.Joins {
		kind := strings.ToUpper(j.Kind)
		if kind == "" {
			kind = "INNER"
		}
		tbl, err := quoteTable(j.Table, j.As)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " " + kind + " JOIN " + tbl
		if j.On != nil {
			w, err := d.renderPred(j.On)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			sql += " ON " + w
		}
	}
	if s.Where != nil {
		w, err := d.renderPred(s.Where)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " WHERE " + w
	}
	if s.Search != nil {
		match, err := renderMatchAgainst(s.Search)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		if s.Where != nil {
			sql += " AND " + match
		} else {
			sql += " WHERE " + match
		}
	}
	orders := s.Order
	if s.Search != nil {
		orders = append([]sqlast.Order{{Field: sqlast.Identifier{Name: "of_score"}, Desc: true}}, s.Order...)
	}
	if len(orders) > 0 {
		orderParts := make([]string, 0, len(orders))
		for _, o := range orders {
			q, err := quote(o.Field)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			dir := "ASC"
			if o.Desc {
				dir = "DESC"
			}
			orderParts = append(orderParts, q+" "+dir)
		}
		sql += " ORDER BY " + strings.Join(orderParts, ", ")
	}
	if s.Limit != nil {
		sql += " LIMIT ? OFFSET ?"
	}
	return dialect.SQLStatement{SQL: sql}, nil
}

// renderMatchAgainst emits MATCH (`c1`, `c2`) AGAINST (?). Columns must be
// non-empty; Source is ignored (sqlite still keys off Source).
func renderMatchAgainst(m *sqlast.FullTextMatch) (string, error) {
	if m == nil || len(m.Columns) == 0 {
		return "", fmt.Errorf("%w: MATCH without columns", spi.ErrUnsupportedCapability)
	}
	parts := make([]string, 0, len(m.Columns))
	for _, c := range m.Columns {
		q, err := quote(c)
		if err != nil {
			return "", err
		}
		parts = append(parts, q)
	}
	return "MATCH (" + strings.Join(parts, ", ") + ") AGAINST (?)", nil
}

func (d *Dialect) renderInsert(s *sqlast.Insert) (dialect.SQLStatement, error) {
	tbl, err := quote(s.Table)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	cols := make([]string, len(s.Columns))
	ph := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		q, err := quote(c)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		cols[i] = q
		ph[i] = "?"
	}
	// MySQL has no RETURNING; the provider inserts then re-reads the row.
	if len(s.Returning) > 0 {
		return dialect.SQLStatement{}, fmt.Errorf("%w: mysql: INSERT ... RETURNING", spi.ErrUnsupportedCapability)
	}
	sql := "INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(ph, ", ") + ")"
	return dialect.SQLStatement{SQL: sql}, nil
}

func (d *Dialect) renderUpdate(s *sqlast.Update) (dialect.SQLStatement, error) {
	tbl, err := quote(s.Table)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	sets := make([]string, len(s.Set))
	for i, a := range s.Set {
		q, err := quote(a.Column)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sets[i] = q + " = ?"
	}
	sql := "UPDATE " + tbl + " SET " + strings.Join(sets, ", ")
	if s.Where != nil {
		w, err := d.renderPred(s.Where)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " WHERE " + w
	}
	return dialect.SQLStatement{SQL: sql}, nil
}

func (d *Dialect) renderDelete(s *sqlast.Delete) (dialect.SQLStatement, error) {
	tbl, err := quote(s.Table)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	sql := "DELETE FROM " + tbl
	if s.Where != nil {
		w, err := d.renderPred(s.Where)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " WHERE " + w
	}
	return dialect.SQLStatement{SQL: sql}, nil
}

func (d *Dialect) renderAggregateSelect(s *sqlast.AggregateSelect) (dialect.SQLStatement, error) {
	from, err := quote(s.From)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	parts := make([]string, 0, len(s.GroupBy)+len(s.Aggs))
	for _, g := range s.GroupBy {
		q, err := quote(g)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		parts = append(parts, q)
	}
	for _, a := range s.Aggs {
		expr, err := renderAggregateExpr(a)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		alias, err := quote(a.Alias)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		parts = append(parts, expr+" AS "+alias)
	}
	sql := "SELECT " + strings.Join(parts, ", ") + " FROM " + from
	if s.Where != nil {
		w, err := d.renderPred(s.Where)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " WHERE " + w
	}
	if len(s.GroupBy) > 0 {
		grpParts := make([]string, 0, len(s.GroupBy))
		for _, g := range s.GroupBy {
			q, err := quote(g)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			grpParts = append(grpParts, q)
		}
		sql += " GROUP BY " + strings.Join(grpParts, ", ")
	}
	if len(s.Order) > 0 {
		orderParts := make([]string, 0, len(s.Order))
		for _, o := range s.Order {
			q, err := quote(o.Field)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			dir := "ASC"
			if o.Desc {
				dir = "DESC"
			}
			orderParts = append(orderParts, q+" "+dir)
		}
		sql += " ORDER BY " + strings.Join(orderParts, ", ")
	}
	if s.Limit != nil {
		sql += " LIMIT ? OFFSET ?"
	}
	return dialect.SQLStatement{SQL: sql}, nil
}

// renderAggregateExpr renders one aggregate function call. Field.Name "*"
// means COUNT(*) (only valid for count); non-count with "*" is rejected.
func renderAggregateExpr(a sqlast.Aggregate) (string, error) {
	fn := strings.ToLower(a.Fn)
	switch fn {
	case "count", "sum", "avg", "min", "max":
	default:
		return "", fmt.Errorf("%w: aggregate function %q", spi.ErrUnsupportedCapability, a.Fn)
	}
	if a.Field == nil {
		return "", fmt.Errorf("mysql: aggregate %q without field", a.Fn)
	}
	if a.Field.Name == "*" {
		if fn != "count" {
			return "", fmt.Errorf("%w: %s(*) only valid for count", spi.ErrUnsupportedCapability, fn)
		}
		return "COUNT(*)", nil
	}
	q, err := quote(*a.Field)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(fn) + "(" + q + ")", nil
}

func (d *Dialect) renderPred(p *sqlast.Predicate) (string, error) {
	if p == nil {
		return "", nil
	}
	switch p.Op {
	case "and":
		parts := make([]string, 0, len(p.Children))
		for _, c := range p.Children {
			s, err := d.renderPred(c)
			if err != nil {
				return "", err
			}
			if s != "" {
				parts = append(parts, "("+s+")")
			}
		}
		return strings.Join(parts, " AND "), nil
	case "eq":
		if p.Field == nil {
			return "", fmt.Errorf("mysql: eq without field")
		}
		q, err := quote(*p.Field)
		if err != nil {
			return "", err
		}
		return q + " = ?", nil
	case "col_eq":
		if p.Field == nil || p.Other == nil {
			return "", fmt.Errorf("mysql: col_eq without fields")
		}
		a, err := quote(*p.Field)
		if err != nil {
			return "", err
		}
		b, err := quote(*p.Other)
		if err != nil {
			return "", err
		}
		return a + " = " + b, nil
	case "is_null":
		if p.Field == nil {
			return "", fmt.Errorf("mysql: is_null without field")
		}
		q, err := quote(*p.Field)
		if err != nil {
			return "", err
		}
		return q + " IS NULL", nil
	case "is_not_null":
		if p.Field == nil {
			return "", fmt.Errorf("mysql: is_not_null without field")
		}
		q, err := quote(*p.Field)
		if err != nil {
			return "", err
		}
		return q + " IS NOT NULL", nil
	default:
		return "", fmt.Errorf("%w: predicate %s", spi.ErrUnsupportedCapability, p.Op)
	}
}
