package mysqlobda

import (
	"fmt"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
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
	includeDeleted := options != nil && options.IncludeDeleted
	if !includeDeleted && !m.Omit.DeletedAt {
		sel.Where = andPred(sel.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Name: "deleted_at"},
		})
	}
	if options != nil {
		for _, o := range options.OrderBy {
			f, ok := m.FieldByLogical[o.Field]
			if !ok {
				return spi.ObjectPage{}, fmt.Errorf("%w: unknown order field %q", spi.ErrInvalidMapping, o.Field)
			}
			sel.Order = append(sel.Order, sqlast.Order{
				Field: sqlast.Identifier{Name: f.Column},
				Desc:  o.Direction == "desc" || o.Direction == "DESC",
			})
		}
	}
	for _, col := range m.IdentityColumns {
		sel.Order = append(sel.Order, sqlast.Order{Field: sqlast.Identifier{Name: col}})
	}
	countSel := *sel
	countSel.Limit = nil
	countStmt, err := p.dialect.Render(&countSel)
	if err != nil {
		return spi.ObjectPage{}, err
	}
	var total int
	if err := p.db.QueryRow("SELECT COUNT(*) FROM ("+countStmt.SQL+") AS q", args...).Scan(&total); err != nil {
		return spi.ObjectPage{}, mysqldialect.Classify(err)
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
		return spi.ObjectPage{}, mysqldialect.Classify(err)
	}
	defer rows.Close()
	bizCols := b.SelectColumns
	var items []spi.OntologyObject
	for rows.Next() {
		dest := make([]any, len(bizCols))
		ptrs := make([]any, len(dest))
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
		obj, err := p.assemble(m, ctx.TenantID, biz)
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
