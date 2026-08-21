package obda

import (
	"fmt"

	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

// ObjectBinding is the compiled physical shape planner needs for one model.
type ObjectBinding struct {
	Table            string
	TenantColumn     string
	IdentityColumns  []string
	SelectColumns    []string
	Writable         bool
	SearchIndex      string
	SearchableFields []string
}

// PlanGetObject selects by tenant and identity. Soft-deleted rows are included.
func PlanGetObject(b ObjectBinding, tenant string, key []any) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	if len(key) != len(b.IdentityColumns) {
		return nil, nil, fmt.Errorf("%w: identity arity", spi.ErrInvalidMapping)
	}
	args := []any{tenant}
	args = append(args, key...)
	where := eq(ident(b.TenantColumn), 1)
	for i, col := range b.IdentityColumns {
		where = and(where, eq(ident(col), i+2))
	}
	return &sqlast.Select{
		From:    ident(b.Table),
		Columns: cols(b.SelectColumns),
		Where:   where,
	}, args, nil
}

// PlanCreateObject inserts tenant plus mapped columns.
func PlanCreateObject(b ObjectBinding, columns []string, values []any) (*sqlast.Insert, []any, error) {
	if !b.Writable {
		return nil, nil, spi.ErrReadOnlyMapping
	}
	if len(columns) != len(values) {
		return nil, nil, fmt.Errorf("%w: insert arity", spi.ErrInvalidMapping)
	}
	params := make([]sqlast.Expr, len(values))
	for i := range values {
		params[i] = sqlast.Param{Position: i + 1}
	}
	ids := make([]sqlast.Identifier, len(columns))
	for i, c := range columns {
		ids[i] = ident(c)
	}
	return &sqlast.Insert{Table: ident(b.Table), Columns: ids, Values: params}, values, nil
}

// PlanUpdateObject assigns columns under tenant + identity (+ optional version).
func PlanUpdateObject(b ObjectBinding, tenant string, key []any, columns []string, values []any) (*sqlast.Update, []any, error) {
	if !b.Writable {
		return nil, nil, spi.ErrReadOnlyMapping
	}
	sets := make([]sqlast.Assignment, len(columns))
	args := make([]any, 0, len(values)+1+len(key))
	for i, c := range columns {
		sets[i] = sqlast.Assignment{Column: ident(c), Value: sqlast.Param{Position: i + 1}}
		args = append(args, values[i])
	}
	pos := len(columns) + 1
	args = append(args, tenant)
	where := eq(ident(b.TenantColumn), pos)
	pos++
	for _, col := range b.IdentityColumns {
		where = and(where, eq(ident(col), pos))
		pos++
	}
	args = append(args, key...)
	return &sqlast.Update{Table: ident(b.Table), Set: sets, Where: where}, args, nil
}

// PlanDeleteObject deletes by tenant and identity.
func PlanDeleteObject(b ObjectBinding, tenant string, key []any) (*sqlast.Delete, []any, error) {
	if !b.Writable {
		return nil, nil, spi.ErrReadOnlyMapping
	}
	args := []any{tenant}
	args = append(args, key...)
	where := eq(ident(b.TenantColumn), 1)
	for i, col := range b.IdentityColumns {
		where = and(where, eq(ident(col), i+2))
	}
	return &sqlast.Delete{Table: ident(b.Table), Where: where}, args, nil
}

// PlanSearch builds a FullTextMatch against a logical search source.
func PlanSearch(b ObjectBinding, tenant, query string) (*sqlast.Select, []any, error) {
	if b.SearchIndex == "" {
		return nil, nil, spi.ErrUnsupportedCapability
	}
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	sel := &sqlast.Select{
		From:    ident(b.Table),
		Columns: cols(b.SelectColumns),
		Where:   eq(ident(b.TenantColumn), 1),
		Search: &sqlast.FullTextMatch{
			Source: ident(b.SearchIndex),
			Query:  sqlast.Param{Position: 2},
		},
	}
	return sel, []any{tenant, query}, nil
}

// PlanQuery selects with tenant and a compiled filter. Unknown fields fail before SQL.
func PlanQuery(b ObjectBinding, tenant string, filter spi.FilterExpression) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	known := map[string]struct{}{b.TenantColumn: {}}
	for _, c := range b.IdentityColumns {
		known[c] = struct{}{}
	}
	for _, c := range b.SelectColumns {
		known[c] = struct{}{}
	}
	args := []any{tenant}
	where := eq(ident(b.TenantColumn), 1)
	pred, extra, err := compileFilter(filter, known, 2)
	if err != nil {
		return nil, nil, err
	}
	args = append(args, extra...)
	if pred != nil {
		where = and(where, pred)
	}
	return &sqlast.Select{
		From:    ident(b.Table),
		Columns: cols(b.SelectColumns),
		Where:   where,
	}, args, nil
}

// PlanGetLinks selects links for one endpoint under tenant.
func PlanGetLinks(table, tenantCol, endpointCol, tenant, objectID string) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	if table == "" || tenantCol == "" || endpointCol == "" {
		return nil, nil, spi.ErrInvalidMapping
	}
	return &sqlast.Select{
		From: ident(table),
		Where: and(
			eq(ident(tenantCol), 1),
			eq(ident(endpointCol), 2),
		),
	}, []any{tenant, objectID}, nil
}

func compileFilter(f spi.FilterExpression, known map[string]struct{}, next int) (*sqlast.Predicate, []any, error) {
	empty := f.Field == "" && f.Operator == "" && len(f.And) == 0 && len(f.Or) == 0 && f.Not == nil
	if empty {
		return nil, nil, nil
	}
	if f.Field != "" {
		if _, ok := known[f.Field]; !ok {
			return nil, nil, fmt.Errorf("%w: unknown filter field %q", spi.ErrInvalidMapping, f.Field)
		}
		return eq(ident(f.Field), next), []any{f.Value}, nil
	}
	return nil, nil, fmt.Errorf("%w: unsupported filter", spi.ErrInvalidMapping)
}

func ident(name string) sqlast.Identifier { return sqlast.Identifier{Name: name} }

func cols(names []string) []sqlast.Expr {
	out := make([]sqlast.Expr, len(names))
	for i, n := range names {
		out[i] = ident(n)
	}
	return out
}

func eq(field sqlast.Identifier, pos int) *sqlast.Predicate {
	return &sqlast.Predicate{Op: "eq", Field: &field, Value: sqlast.Param{Position: pos}}
}

func and(a, b *sqlast.Predicate) *sqlast.Predicate {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &sqlast.Predicate{Op: "and", Children: []*sqlast.Predicate{a, b}}
}
