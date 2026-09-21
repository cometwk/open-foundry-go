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
	idBatch := isIDEqBatch(filter)
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
			f, ok := m.LookupLogical(o.Field)
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
	var total int
	if options == nil || !options.SkipTotalCount {
		countSel := *sel
		countSel.Limit = nil
		countStmt, err := p.dialect.Render(&countSel)
		if err != nil {
			return spi.ObjectPage{}, err
		}
		if err := p.db.QueryRow("SELECT COUNT(*) FROM ("+countStmt.SQL+") AS q", args...).Scan(&total); err != nil {
			return spi.ObjectPage{}, mysqldialect.Classify(err)
		}
	}
	limit, offset := 0, 0
	if options != nil {
		limit, offset = options.Limit, options.Offset
	}
	if idBatch && limit > 0 {
		if offset < 0 {
			offset = 0
		}
	} else {
		limit, offset = pageLimitOffset(limit, offset)
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
		dest, err := scan(rows, len(bizCols))
		if err != nil {
			return spi.ObjectPage{}, err
		}
		biz := bizMap(dest, bizCols)
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

const (
	// DefaultPageLimit is used when Limit <= 0 (or options is nil).
	DefaultPageLimit = 100
	// MaxPageLimit is the hard cap; larger Limit values are truncated to this.
	MaxPageLimit = 1000
)

// pageLimitOffset applies the shared pagination policy used by
// QueryObjects/AggregateObjects/SearchObjects/GetLinks/Traverse:
// limit<=0 defaults to DefaultPageLimit, hard cap MaxPageLimit, offset<0 clamps to 0.
func pageLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// filterColumn resolves a logical filter field to its physical column. The
// identity alias "_id" maps to the first identity column; business fields map
// through the compiled model.
// isIDEqBatch is the hydrate-by-ids channel: Or of eq leaves on _id.
// Those calls pass Limit=len(ids); clamping them to MaxPageLimit would
// silently drop rows.
func isIDEqBatch(f spi.FilterExpression) bool {
	if f.Field != "" || f.Not != nil || len(f.And) > 0 || len(f.Or) == 0 {
		return false
	}
	for _, c := range f.Or {
		if c.Field != spi.FieldID {
			return false
		}
		if c.Operator != "" && c.Operator != "eq" {
			return false
		}
		if len(c.Or) > 0 || len(c.And) > 0 || c.Not != nil {
			return false
		}
	}
	return true
}

// midColumns maps a projection hint's logical fields onto physical columns.
// Identity columns are always included so assemble can mint _id. A hint that
// only names id/_id (or unknown fields) returns nil — expand synthesizes
// skeletons from edge endpoints instead of widening the SELECT.
func midColumns(m *obda.CompiledModel, fields []string) []string {
	if m == nil || len(fields) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	cols := append([]string(nil), m.IdentityColumns...)
	for _, c := range cols {
		seen[c] = struct{}{}
	}
	nIdent := len(cols)
	for _, f := range fields {
		if f == "" || f == "id" || f == spi.FieldID {
			continue
		}
		cf, ok := m.FieldByLogical[f]
		if !ok || cf.Column == "" {
			continue
		}
		if _, dup := seen[cf.Column]; dup {
			continue
		}
		seen[cf.Column] = struct{}{}
		cols = append(cols, cf.Column)
	}
	if len(cols) == nIdent {
		return nil
	}
	return cols
}

func filterColumn(m *obda.CompiledModel, logical string) (string, bool) {
	if logical == spi.FieldID && len(m.IdentityColumns) > 0 {
		return m.IdentityColumns[0], true
	}
	cf, ok := m.LookupLogical(logical)
	if !ok {
		return "", false
	}
	return cf.Column, true
}

func translateFilter(m *obda.CompiledModel, f spi.FilterExpression) (spi.FilterExpression, error) {
	empty := f.Field == "" && f.Operator == "" && len(f.And) == 0 && len(f.Or) == 0 && f.Not == nil
	if empty {
		return f, nil
	}
	if f.Field != "" {
		col, ok := filterColumn(m, f.Field)
		if !ok {
			return f, fmt.Errorf("%w: unknown filter field %q", spi.ErrInvalidMapping, f.Field)
		}
		if f.Operator != "" && f.Operator != "eq" {
			return f, fmt.Errorf("%w: unsupported filter operator %q", spi.ErrInvalidMapping, f.Operator)
		}
		f.Field = col
		return f, nil
	}
	if len(f.Or) > 0 {
		// Or is accepted only over eq leaves — the batch-by-ids shape.
		for i := range f.Or {
			c := &f.Or[i]
			if c.Field == "" || (c.Operator != "" && c.Operator != "eq") {
				return f, fmt.Errorf("%w: or children must be eq leaves", spi.ErrInvalidMapping)
			}
			col, ok := filterColumn(m, c.Field)
			if !ok {
				return f, fmt.Errorf("%w: unknown filter field %q", spi.ErrInvalidMapping, c.Field)
			}
			c.Field = col
		}
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
