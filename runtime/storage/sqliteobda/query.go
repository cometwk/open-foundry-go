package sqliteobda

import (
	"fmt"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

func (p *Provider) QueryObjects(ctx spi.RequestContext, typ string, filter spi.FilterExpression, options *spi.QueryOptions) (spi.ObjectPage, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return spi.ObjectPage{}, err
	}
	m, err := act.model(typ)
	if err != nil {
		return spi.ObjectPage{}, spi.ErrObjectNotFound
	}
	if options != nil && (options.AsOfTime != nil || options.AsOfVersion != nil) {
		return spi.ObjectPage{}, spi.ErrUnsupportedCapability
	}
	phys, err := translateFilter(m, filter)
	if err != nil {
		return spi.ObjectPage{}, err
	}
	b := m.Binding()
	sel, args, err := obda.PlanQuery(b, ctx.TenantID, phys)
	if err != nil {
		return spi.ObjectPage{}, err
	}
	if len(m.IdentityColumns) != 1 || m.TenantColumn == "" {
		return spi.ObjectPage{}, spi.ErrUnsupportedCapability
	}
	qualifySelect(sel, "b")
	sel.Joins = append(sel.Joins, sqlast.Join{
		Kind:  "INNER",
		Table: sqlast.Identifier{Name: "of_object_meta"},
		As:    "m",
		On: andPred(
			&sqlast.Predicate{
				Op:    "col_eq",
				Field: &sqlast.Identifier{Qualifier: "m", Name: "physical_key"},
				Other: &sqlast.Identifier{Qualifier: "b", Name: m.IdentityColumns[0]},
			},
			&sqlast.Predicate{
				Op:    "col_eq",
				Field: &sqlast.Identifier{Qualifier: "m", Name: "tenant_id"},
				Other: &sqlast.Identifier{Qualifier: "b", Name: m.TenantColumn},
			},
		),
	})
	args = append(args, m.Name)
	sel.Where = andPred(sel.Where, &sqlast.Predicate{
		Op:    "eq",
		Field: &sqlast.Identifier{Qualifier: "m", Name: "object_type"},
		Value: sqlast.Param{Position: len(args)},
	})
	includeDeleted := options != nil && options.IncludeDeleted
	if !includeDeleted {
		sel.Where = andPred(sel.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Qualifier: "m", Name: "deleted_at"},
		})
	}
	metaCols := []string{"engine_id", "version", "created_at", "updated_at", "deleted_at"}
	for _, c := range metaCols {
		sel.Columns = append(sel.Columns, sqlast.Identifier{Qualifier: "m", Name: c})
	}
	if options != nil {
		for _, o := range options.OrderBy {
			f, ok := m.FieldByLogical[o.Field]
			if !ok {
				return spi.ObjectPage{}, fmt.Errorf("%w: unknown order field %q", spi.ErrInvalidMapping, o.Field)
			}
			sel.Order = append(sel.Order, sqlast.Order{
				Field: sqlast.Identifier{Qualifier: "b", Name: f.Column},
				Desc:  o.Direction == "desc" || o.Direction == "DESC",
			})
		}
	}
	for _, col := range m.IdentityColumns {
		sel.Order = append(sel.Order, sqlast.Order{Field: sqlast.Identifier{Qualifier: "b", Name: col}})
	}
	countSel := *sel
	countSel.Limit = nil
	countStmt, err := p.dialect.Render(&countSel)
	if err != nil {
		return spi.ObjectPage{}, err
	}
	var total int
	if err := p.db.QueryRow("SELECT COUNT(*) FROM ("+countStmt.SQL+") AS q", args...).Scan(&total); err != nil {
		return spi.ObjectPage{}, sqlitedialect.Classify(err)
	}
	limit := 100
	offset := 0
	if options != nil {
		if options.Limit > 0 {
			limit = options.Limit
		}
		if limit > 1000 {
			limit = 1000
		}
		if options.Offset > 0 {
			offset = options.Offset
		}
	}
	sel.Limit = &sqlast.LimitOffset{Limit: sqlast.Param{}, Offset: sqlast.Param{}}
	pageArgs := append(append([]any{}, args...), limit+1, offset)
	stmt, err := p.dialect.Render(sel)
	if err != nil {
		return spi.ObjectPage{}, err
	}
	rows, err := p.db.Query(stmt.SQL, pageArgs...)
	if err != nil {
		return spi.ObjectPage{}, sqlitedialect.Classify(err)
	}
	defer rows.Close()
	bizCols := b.SelectColumns
	nScan := len(bizCols) + len(metaCols)
	var items []spi.OntologyObject
	for rows.Next() {
		dest := make([]any, nScan)
		ptrs := make([]any, nScan)
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return spi.ObjectPage{}, err
		}
		biz := map[string]any{}
		for i, col := range bizCols {
			biz[col] = unwrap(dest[i])
		}
		base := len(bizCols)
		meta := &metaRow{
			EngineID:  fmt.Sprint(unwrap(dest[base])),
			TenantID:  ctx.TenantID,
			Type:      m.Name,
			Version:   asInt(unwrap(dest[base+1])),
			CreatedAt: fmt.Sprint(unwrap(dest[base+2])),
			UpdatedAt: fmt.Sprint(unwrap(dest[base+3])),
		}
		if d := unwrap(dest[base+4]); d != nil && fmt.Sprint(d) != "" {
			meta.DeletedAt.Valid = true
			meta.DeletedAt.String = fmt.Sprint(d)
		}
		obj, err := p.assemble(m, meta, biz)
		if err != nil {
			return spi.ObjectPage{}, err
		}
		items = append(items, obj)
	}
	if err := rows.Err(); err != nil {
		return spi.ObjectPage{}, err
	}
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return spi.ObjectPage{Items: items, TotalCount: total, HasNextPage: hasNext}, nil
}

func translateFilter(m *obda.CompiledModel, f spi.FilterExpression) (spi.FilterExpression, error) {
	empty := f.Field == "" && f.Operator == "" && len(f.And) == 0 && len(f.Or) == 0 && f.Not == nil
	if empty {
		return f, nil
	}
	if f.Field != "" {
		cf, ok := m.FieldByLogical[f.Field]
		if !ok {
			return f, fmt.Errorf("%w: unknown filter field %q", spi.ErrInvalidMapping, f.Field)
		}
		f.Field = cf.Column
		return f, nil
	}
	return f, fmt.Errorf("%w: unsupported filter", spi.ErrInvalidMapping)
}

func qualifySelect(s *sqlast.Select, q string) {
	s.As = q
	for i, c := range s.Columns {
		if id, ok := c.(sqlast.Identifier); ok {
			s.Columns[i] = qualifyIdent(id, q)
		}
	}
	qualifyPred(s.Where, q)
	for i, o := range s.Order {
		s.Order[i].Field = qualifyIdent(o.Field, q)
	}
}

func qualifyIdent(id sqlast.Identifier, q string) sqlast.Identifier {
	if id.Qualifier == "" {
		id.Qualifier = q
	}
	return id
}

func qualifyPred(p *sqlast.Predicate, q string) {
	if p == nil {
		return
	}
	if p.Field != nil {
		f := qualifyIdent(*p.Field, q)
		p.Field = &f
	}
	for _, c := range p.Children {
		qualifyPred(c, q)
	}
}

func andPred(a, b *sqlast.Predicate) *sqlast.Predicate {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &sqlast.Predicate{Op: "and", Children: []*sqlast.Predicate{a, b}}
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}
