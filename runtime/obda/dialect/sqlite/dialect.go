package sqlite

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/obda/dialect"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

var _ dialect.Dialect = (*Dialect)(nil)

// Dialect is the SQLite v1 adapter.
type Dialect struct {
	fts5 bool
}

// New returns a SQLite dialect. Probe FTS5 separately via ProbeFTS5.
func New() *Dialect {
	return &Dialect{fts5: true}
}

func (d *Dialect) Name() string { return "sqlite" }

func (d *Dialect) Capabilities() dialect.Capabilities {
	return dialect.Capabilities{
		Transactions:     true,
		Savepoints:       true,
		RecursiveCTE:     true,
		FullTextSearch:   d.fts5,
		GeneratedColumns: true,
		JSON:             true,
	}
}

func (d *Dialect) SetFTS5(ok bool) { d.fts5 = ok }

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
		default:
			return nil, fmt.Errorf("sqlite: boolean %T", v)
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
			return nil, fmt.Errorf("sqlite: int %T", v)
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
	cols := "*"
	if len(s.Columns) > 0 {
		parts := make([]string, 0, len(s.Columns))
		for _, c := range s.Columns {
			id, ok := c.(sqlast.Identifier)
			if !ok {
				return dialect.SQLStatement{}, fmt.Errorf("sqlite: select column %T", c)
			}
			q, err := quote(id)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			parts = append(parts, q)
		}
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
	if s.Search != nil {
		src, err := quote(s.Search.Source)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " JOIN " + src + " ON 1=1 WHERE " + src + " MATCH ?"
		if s.Where != nil {
			w, err := d.renderPred(s.Where)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			sql += " AND " + w
		}
		return dialect.SQLStatement{SQL: sql}, nil
	}
	if s.Where != nil {
		w, err := d.renderPred(s.Where)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " WHERE " + w
	}
	if len(s.Order) > 0 {
		parts := make([]string, 0, len(s.Order))
		for _, o := range s.Order {
			q, err := quote(o.Field)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			dir := "ASC"
			if o.Desc {
				dir = "DESC"
			}
			parts = append(parts, q+" "+dir)
		}
		sql += " ORDER BY " + strings.Join(parts, ", ")
	}
	if s.Limit != nil {
		sql += " LIMIT ? OFFSET ?"
	}
	return dialect.SQLStatement{SQL: sql}, nil
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
	sql := "INSERT INTO " + tbl + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(ph, ", ") + ")"
	if len(s.Returning) > 0 {
		ret := make([]string, len(s.Returning))
		for i, c := range s.Returning {
			q, err := quote(c)
			if err != nil {
				return dialect.SQLStatement{}, err
			}
			ret[i] = q
		}
		sql += " RETURNING " + strings.Join(ret, ", ")
	}
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
			return "", fmt.Errorf("sqlite: eq without field")
		}
		q, err := quote(*p.Field)
		if err != nil {
			return "", err
		}
		return q + " = ?", nil
	case "col_eq":
		if p.Field == nil || p.Other == nil {
			return "", fmt.Errorf("sqlite: col_eq without fields")
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
			return "", fmt.Errorf("sqlite: is_null without field")
		}
		q, err := quote(*p.Field)
		if err != nil {
			return "", err
		}
		return q + " IS NULL", nil
	default:
		return "", fmt.Errorf("%w: predicate %s", spi.ErrUnsupportedCapability, p.Op)
	}
}
