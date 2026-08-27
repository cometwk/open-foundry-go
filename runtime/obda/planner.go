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

// LinkJoinBinding is the physical shape of GetLinks INNER JOIN peer object.
type LinkJoinBinding struct {
	LinkTable       string
	LinkTenant      string
	EndpointCol     string
	PeerFKCol       string
	PeerTable       string
	PeerIDCol       string
	PeerTenantCol   string
	SelectColumns   []string
	OmitLinkDeleted bool
	OmitPeerDeleted bool
}

// PlanGetLinksJoin selects link rows INNER JOINed to the live peer object.
func PlanGetLinksJoin(b LinkJoinBinding, tenant, objectID string) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	if b.LinkTable == "" || b.PeerTable == "" || b.EndpointCol == "" || b.PeerFKCol == "" {
		return nil, nil, spi.ErrInvalidMapping
	}
	lTenant := sqlast.Identifier{Qualifier: "l", Name: b.LinkTenant}
	on := and(
		&sqlast.Predicate{
			Op:    "col_eq",
			Field: &sqlast.Identifier{Qualifier: "l", Name: b.PeerFKCol},
			Other: &sqlast.Identifier{Qualifier: "p", Name: b.PeerIDCol},
		},
		&sqlast.Predicate{
			Op:    "col_eq",
			Field: &lTenant,
			Other: &sqlast.Identifier{Qualifier: "p", Name: b.PeerTenantCol},
		},
	)
	where := and(eq(lTenant, 1), eq(sqlast.Identifier{Qualifier: "l", Name: b.EndpointCol}, 2))
	if !b.OmitLinkDeleted {
		where = and(where, &sqlast.Predicate{Op: "is_null", Field: &sqlast.Identifier{Qualifier: "l", Name: "deleted_at"}})
	}
	if !b.OmitPeerDeleted {
		where = and(where, &sqlast.Predicate{Op: "is_null", Field: &sqlast.Identifier{Qualifier: "p", Name: "deleted_at"}})
	}
	cols := make([]sqlast.Expr, len(b.SelectColumns))
	for i, c := range b.SelectColumns {
		cols[i] = sqlast.Identifier{Qualifier: "l", Name: c}
	}
	return &sqlast.Select{
		From:    ident(b.LinkTable),
		As:      "l",
		Columns: cols,
		Joins: []sqlast.Join{{
			Kind:  "INNER",
			Table: ident(b.PeerTable),
			As:    "p",
			On:    on,
		}},
		Where: where,
	}, []any{tenant, objectID}, nil
}

// TraverseHop is one typed hop in a chained Traverse JOIN.
type TraverseHop struct {
	Direction         string
	LinkTable         string
	LinkTenant        string
	LinkIdentityCol   string
	FromCol           string
	ToCol             string
	PrevIDCol         string
	PrevTenantCol     string
	TargetTable       string
	TargetIDCol       string
	TargetTenantCol   string
	TargetSelect      []string
	OmitLinkDeleted   bool
	OmitTargetDeleted bool
}

// PlanTraverse selects terminal object columns via a chained INNER JOIN.
// FROM is the start object table; each hop adds the link table then the target object table.
func PlanTraverse(start ObjectBinding, hops []TraverseHop, tenant, startID string) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	if start.Table == "" || len(start.IdentityColumns) == 0 || len(hops) == 0 {
		return nil, nil, spi.ErrInvalidMapping
	}
	startIDCol := start.IdentityColumns[0]
	startAlias := "s0"
	sel := &sqlast.Select{
		From: ident(start.Table),
		As:   startAlias,
		Where: and(
			eq(sqlast.Identifier{Qualifier: startAlias, Name: start.TenantColumn}, 1),
			eq(sqlast.Identifier{Qualifier: startAlias, Name: startIDCol}, 2),
		),
	}
	var where *sqlast.Predicate
	for i, h := range hops {
		if h.LinkTable == "" || h.TargetTable == "" || h.FromCol == "" || h.ToCol == "" {
			return nil, nil, spi.ErrInvalidMapping
		}
		prevAlias := fmt.Sprintf("s%d", i)
		linkAlias := fmt.Sprintf("l%d", i)
		nextAlias := fmt.Sprintf("s%d", i+1)
		endCol, peerFK := h.FromCol, h.ToCol
		if h.Direction == "inbound" {
			endCol, peerFK = h.ToCol, h.FromCol
		}
		sel.Joins = append(sel.Joins, sqlast.Join{
			Kind:  "INNER",
			Table: ident(h.LinkTable),
			As:    linkAlias,
			On: and(
				colEq(linkAlias, endCol, prevAlias, h.PrevIDCol),
				colEq(linkAlias, h.LinkTenant, prevAlias, h.PrevTenantCol),
			),
		})
		sel.Joins = append(sel.Joins, sqlast.Join{
			Kind:  "INNER",
			Table: ident(h.TargetTable),
			As:    nextAlias,
			On: and(
				colEq(nextAlias, h.TargetIDCol, linkAlias, peerFK),
				colEq(nextAlias, h.TargetTenantCol, linkAlias, h.LinkTenant),
			),
		})
		if !h.OmitLinkDeleted {
			where = and(where, &sqlast.Predicate{
				Op:    "is_null",
				Field: &sqlast.Identifier{Qualifier: linkAlias, Name: "deleted_at"},
			})
		}
		if !h.OmitTargetDeleted {
			where = and(where, &sqlast.Predicate{
				Op:    "is_null",
				Field: &sqlast.Identifier{Qualifier: nextAlias, Name: "deleted_at"},
			})
		}
	}
	sel.Where = and(sel.Where, where)
	last := hops[len(hops)-1]
	termAlias := fmt.Sprintf("s%d", len(hops))
	linkAlias := fmt.Sprintf("l%d", len(hops)-1)
	cols := make([]sqlast.Expr, len(last.TargetSelect))
	for i, c := range last.TargetSelect {
		cols[i] = sqlast.Identifier{Qualifier: termAlias, Name: c}
	}
	sel.Columns = cols
	sel.Order = append(sel.Order, sqlast.Order{
		Field: sqlast.Identifier{Qualifier: termAlias, Name: last.TargetIDCol},
	})
	if last.LinkIdentityCol != "" {
		sel.Order = append(sel.Order, sqlast.Order{
			Field: sqlast.Identifier{Qualifier: linkAlias, Name: last.LinkIdentityCol},
		})
	}
	return sel, []any{tenant, startID}, nil
}

func colEq(aq, an, bq, bn string) *sqlast.Predicate {
	return &sqlast.Predicate{
		Op:    "col_eq",
		Field: &sqlast.Identifier{Qualifier: aq, Name: an},
		Other: &sqlast.Identifier{Qualifier: bq, Name: bn},
	}
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
