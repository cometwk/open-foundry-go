package mysqlobda

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

func (p *Provider) AggregateObjects(ctx spi.RequestContext, typ string, query spi.AggregateQuery) (spi.AggregateResult, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return spi.AggregateResult{}, err
	}
	m, err := act.model(typ)
	if err != nil {
		return spi.AggregateResult{}, spi.ErrObjectNotFound
	}
	if err := validateAggregateQuery(query); err != nil {
		return spi.AggregateResult{}, err
	}

	groupCols := make([]string, 0, len(query.GroupBy))
	groupLogical := make([]string, 0, len(query.GroupBy))
	groupByCol := map[string]string{}
	for _, logical := range query.GroupBy {
		f, ok := m.FieldByLogical[logical]
		if !ok {
			return spi.AggregateResult{}, fmt.Errorf("%w: unknown groupBy field %q", spi.ErrInvalidMapping, logical)
		}
		groupCols = append(groupCols, f.Column)
		groupLogical = append(groupLogical, logical)
		groupByCol[logical] = f.Column
	}

	aggs := make([]sqlast.Aggregate, 0, len(query.Fields)+1)
	aliases := make([]string, 0, len(query.Fields))
	aliasSet := map[string]struct{}{}
	for _, f := range query.Fields {
		alias := aggregateAlias(f)
		aliasSet[alias] = struct{}{}
		aliases = append(aliases, alias)
		field, err := aggregateFieldIdent(m, f)
		if err != nil {
			return spi.AggregateResult{}, err
		}
		aggs = append(aggs, sqlast.Aggregate{
			Fn:    strings.ToLower(f.Fn),
			Field: field,
			Alias: sqlast.Identifier{Name: alias},
		})
	}

	var phys spi.FilterExpression
	if query.Filter != nil {
		phys, err = translateFilter(m, *query.Filter)
		if err != nil {
			return spi.AggregateResult{}, err
		}
	}
	stmt, args, err := obda.PlanAggregate(m.Binding(), ctx.TenantID, groupCols, aggs, phys)
	if err != nil {
		return spi.AggregateResult{}, err
	}
	if !m.Omit.DeletedAt {
		stmt.Where = andPred(stmt.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Name: "deleted_at"},
		})
	}

	// Hidden MIN(identity) tiebreak satisfies ONLY_FULL_GROUP_BY (KTD2).
	if len(m.IdentityColumns) == 0 {
		return spi.AggregateResult{}, fmt.Errorf("%w: identity columns", spi.ErrInvalidMapping)
	}
	tieCol := m.IdentityColumns[0]
	stmt.Aggs = append(stmt.Aggs, sqlast.Aggregate{
		Fn:    "min",
		Field: &sqlast.Identifier{Name: tieCol},
		Alias: sqlast.Identifier{Name: "of_tiebreak"},
	})
	for _, o := range query.OrderBy {
		ord, err := resolveAggregateOrder(o, groupByCol, aliasSet)
		if err != nil {
			return spi.AggregateResult{}, err
		}
		stmt.Order = append(stmt.Order, ord)
	}
	stmt.Order = append(stmt.Order, sqlast.Order{Field: sqlast.Identifier{Name: "of_tiebreak"}})

	countSel := *stmt
	countSel.Limit = nil
	countSel.Order = nil
	countStmt, err := p.dialect.Render(&countSel)
	if err != nil {
		return spi.AggregateResult{}, err
	}
	var total int
	if err := p.db.QueryRow("SELECT COUNT(*) FROM ("+countStmt.SQL+") AS q", args...).Scan(&total); err != nil {
		return spi.AggregateResult{}, mysqldialect.Classify(err)
	}

	limit, offset := pageLimitOffset(query.Limit, query.Offset)
	stmt.Limit = &sqlast.LimitOffset{Limit: sqlast.Param{}, Offset: sqlast.Param{}}
	pageArgs := append(append([]any{}, args...), limit, offset)
	rendered, err := p.dialect.Render(stmt)
	if err != nil {
		return spi.AggregateResult{}, err
	}
	rows, err := p.db.Query(rendered.SQL, pageArgs...)
	if err != nil {
		return spi.AggregateResult{}, mysqldialect.Classify(err)
	}
	defer rows.Close()

	nScan := len(groupLogical) + len(aliases) + 1 // + tiebreak
	var groups []spi.AggregateGroup
	for rows.Next() {
		dest := make([]any, nScan)
		ptrs := make([]any, nScan)
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return spi.AggregateResult{}, err
		}
		keys := map[string]any{}
		for i, logical := range groupLogical {
			keys[logical] = unwrap(dest[i])
		}
		values := map[string]any{}
		base := len(groupLogical)
		for i, alias := range aliases {
			values[alias] = coerceAgg(dest[base+i])
		}
		groups = append(groups, spi.AggregateGroup{Keys: keys, Values: values})
	}
	if err := rows.Err(); err != nil {
		return spi.AggregateResult{}, err
	}
	return spi.AggregateResult{Groups: groups, TotalGroups: total}, nil
}

func validateAggregateQuery(query spi.AggregateQuery) error {
	if len(query.Fields) == 0 {
		return fmt.Errorf("aggregate query must specify at least one field")
	}
	allowed := map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}
	for _, f := range query.Fields {
		fn := strings.ToLower(f.Fn)
		if !allowed[fn] {
			return fmt.Errorf("invalid aggregate function: %s", f.Fn)
		}
		if f.Field == "*" && fn != "count" {
			return fmt.Errorf("%s(*) only valid for count", fn)
		}
	}
	return nil
}

func aggregateAlias(f spi.AggregateField) string {
	if f.Alias != "" {
		return f.Alias
	}
	if f.Field == "*" {
		return strings.ToLower(f.Fn)
	}
	return strings.ToLower(f.Fn) + "_" + f.Field
}

func aggregateFieldIdent(m *obda.CompiledModel, f spi.AggregateField) (*sqlast.Identifier, error) {
	if f.Field == "*" {
		return &sqlast.Identifier{Name: "*"}, nil
	}
	cf, ok := m.FieldByLogical[f.Field]
	if !ok {
		return nil, fmt.Errorf("%w: unknown aggregate field %q", spi.ErrInvalidMapping, f.Field)
	}
	return &sqlast.Identifier{Name: cf.Column}, nil
}

func resolveAggregateOrder(o spi.OrderBy, groupByCol map[string]string, aliases map[string]struct{}) (sqlast.Order, error) {
	desc := o.Direction == "desc" || o.Direction == "DESC"
	if col, ok := groupByCol[o.Field]; ok {
		return sqlast.Order{Field: sqlast.Identifier{Name: col}, Desc: desc}, nil
	}
	if _, ok := aliases[o.Field]; ok {
		return sqlast.Order{Field: sqlast.Identifier{Name: o.Field}, Desc: desc}, nil
	}
	return sqlast.Order{}, fmt.Errorf("unknown order field %q", o.Field)
}

func coerceAgg(v any) any {
	v = unwrap(v)
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return t
	case string:
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return int(i)
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
		return t
	default:
		return v
	}
}
