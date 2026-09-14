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

// PlanSearch builds a FullTextMatch against compiled searchable columns.
// Guard is len(SearchableFields)==0 (SearchIndex is no longer filled by compile).
//
// Args follow MySQL `?` appearance order: SELECT MATCH, tenant, filter extras,
// then WHERE MATCH. The query value is therefore bound twice, with any filter
// values spliced between the two MATCH placeholders.
func PlanSearch(b ObjectBinding, tenant, query string, filter spi.FilterExpression) (*sqlast.Select, []any, error) {
	if len(b.SearchableFields) == 0 {
		return nil, nil, spi.ErrUnsupportedCapability
	}
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	searchCols := make([]sqlast.Identifier, len(b.SearchableFields))
	for i, c := range b.SearchableFields {
		searchCols[i] = ident(c)
	}
	where := eq(ident(b.TenantColumn), 1)
	pred, extra, err := compileFilter(filter, knownColumns(b), 2)
	if err != nil {
		return nil, nil, err
	}
	if pred != nil {
		where = and(where, pred)
	}
	sel := &sqlast.Select{
		From:    ident(b.Table),
		Columns: cols(b.SelectColumns),
		Where:   where,
		Search: &sqlast.FullTextMatch{
			Source:  ident(b.SearchIndex),
			Columns: searchCols,
			Query:   sqlast.Param{Position: 2},
		},
	}
	args := append([]any{query, tenant}, extra...)
	args = append(args, query)
	return sel, args, nil
}

// PlanAggregate builds a GROUP BY plan. Tenant is arg position 1; filter args follow.
// Order and Limit are left unset for the provider to fill (tiebreak, pagination).
func PlanAggregate(b ObjectBinding, tenant string, groupBy []string, aggs []sqlast.Aggregate, filter spi.FilterExpression) (*sqlast.AggregateSelect, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	known := knownColumns(b)
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
	gb := make([]sqlast.Identifier, len(groupBy))
	for i, c := range groupBy {
		gb[i] = ident(c)
	}
	return &sqlast.AggregateSelect{
		From:    ident(b.Table),
		GroupBy: gb,
		Aggs:    append([]sqlast.Aggregate(nil), aggs...),
		Where:   where,
	}, args, nil
}

// PlanQuery selects with tenant and a compiled filter. Unknown fields fail before SQL.
func PlanQuery(b ObjectBinding, tenant string, filter spi.FilterExpression) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	known := knownColumns(b)
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
	Inline          bool
	FKColumn        string
	HostPKCol       string
}

// PlanGetLinksJoin selects link rows INNER JOINed to the live peer object.
func PlanGetLinksJoin(b LinkJoinBinding, tenant, objectID string) (*sqlast.Select, []any, error) {
	if tenant == "" {
		return nil, nil, spi.ErrTenantRequired
	}
	if b.Inline {
		return planGetLinksJoinInline(b, tenant, objectID)
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

func planGetLinksJoinInline(b LinkJoinBinding, tenant, objectID string) (*sqlast.Select, []any, error) {
	if b.LinkTable == "" || b.PeerTable == "" || b.EndpointCol == "" || b.FKColumn == "" {
		return nil, nil, spi.ErrInvalidMapping
	}
	lTenant := sqlast.Identifier{Qualifier: "l", Name: b.LinkTenant}
	on := and(
		&sqlast.Predicate{
			Op:    "col_eq",
			Field: &sqlast.Identifier{Qualifier: "p", Name: b.PeerIDCol},
			Other: &sqlast.Identifier{Qualifier: "l", Name: b.FKColumn},
		},
		&sqlast.Predicate{
			Op:    "col_eq",
			Field: &lTenant,
			Other: &sqlast.Identifier{Qualifier: "p", Name: b.PeerTenantCol},
		},
	)
	where := and(eq(lTenant, 1), eq(sqlast.Identifier{Qualifier: "l", Name: b.EndpointCol}, 2))
	if b.HostPKCol != "" && b.EndpointCol == b.HostPKCol {
		where = and(where, &sqlast.Predicate{Op: "is_not_null", Field: &sqlast.Identifier{Qualifier: "l", Name: b.FKColumn}})
	}
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
	Inline            bool
	FKColumn          string
	FKOnPrev          bool
	// LinkSelect projects the junction link row's columns for this hop
	// (junction hops only) so the scanner can assemble real Edges.
	LinkSelect []string
	// HostSelect projects the host binding's columns for inline hops; the
	// host alias is the previous hop alias when FKOnPrev, else this hop's
	// target alias.
	HostSelect []string
	// MidSelect projects this hop's target object columns when the hop is
	// not terminal and a projection hint asked for business fields.
	// Empty means no extra bucket (callers synthesize id-only skeletons).
	MidSelect []string
}

// TraverseBucket describes one hop's projected columns inside a Traverse row.
type TraverseBucket struct {
	Inline bool
	Alias  string // link alias (junction) or host alias (inline)
	Offset int    // column index of the bucket's first column
	Cols   []string
}

// TraverseLayout maps the flat Traverse projection into buckets: terminal
// object columns first, then one bucket per hop in path order, then one
// optional mid-object bucket per non-terminal hop. Scanning is positional;
// duplicate column names across buckets are expected and safe.
type TraverseLayout struct {
	NodeAlias string
	NodeCols  []string
	Hops      []TraverseBucket
	Mids      []TraverseBucket // index-aligned with non-terminal hops
}

// PlanTraverse selects terminal object columns via a chained INNER JOIN.
// FROM is the start object table; each hop adds the link table then the target object table.
// The projection carries the terminal bucket first, then one bucket per hop
// (junction link columns, or host columns for inline hops), then one mid
// object bucket per non-terminal hop when MidSelect is set; TraverseLayout
// maps the column offsets. WHERE, ORDER, and args are unaffected by the
// projection — args stay [tenant, startID].
func PlanTraverse(start ObjectBinding, hops []TraverseHop, tenant, startID string) (*sqlast.Select, *TraverseLayout, []any, error) {
	if tenant == "" {
		return nil, nil, nil, spi.ErrTenantRequired
	}
	if start.Table == "" || len(start.IdentityColumns) == 0 || len(hops) == 0 {
		return nil, nil, nil, spi.ErrInvalidMapping
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
		prevAlias := fmt.Sprintf("s%d", i)
		nextAlias := fmt.Sprintf("s%d", i+1)
		if h.Inline {
			if h.FKColumn == "" || h.TargetTable == "" {
				return nil, nil, nil, spi.ErrInvalidMapping
			}
			sel.Joins = append(sel.Joins, sqlast.Join{
				Kind:  "INNER",
				Table: ident(h.TargetTable),
				As:    nextAlias,
				On:    inlineHopOn(h, prevAlias, nextAlias),
			})
			hostAlias := prevAlias
			if !h.FKOnPrev {
				hostAlias = nextAlias
			}
			if !h.OmitLinkDeleted {
				where = and(where, &sqlast.Predicate{
					Op:    "is_null",
					Field: &sqlast.Identifier{Qualifier: hostAlias, Name: "deleted_at"},
				})
			}
			if !h.OmitTargetDeleted {
				where = and(where, &sqlast.Predicate{
					Op:    "is_null",
					Field: &sqlast.Identifier{Qualifier: nextAlias, Name: "deleted_at"},
				})
			}
			continue
		}
		if h.LinkTable == "" || h.TargetTable == "" || h.FromCol == "" || h.ToCol == "" {
			return nil, nil, nil, spi.ErrInvalidMapping
		}
		linkAlias := fmt.Sprintf("l%d", i)
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
	cols := make([]sqlast.Expr, 0, len(last.TargetSelect))
	for _, c := range last.TargetSelect {
		cols = append(cols, sqlast.Identifier{Qualifier: termAlias, Name: c})
	}
	layout := &TraverseLayout{
		NodeAlias: termAlias,
		NodeCols:  append([]string(nil), last.TargetSelect...),
		Hops:      make([]TraverseBucket, 0, len(hops)),
	}
	offset := len(last.TargetSelect)
	for i, h := range hops {
		var bucketCols []string
		alias := fmt.Sprintf("l%d", i)
		if h.Inline {
			bucketCols = h.HostSelect
			alias = fmt.Sprintf("s%d", i)
			if !h.FKOnPrev {
				alias = fmt.Sprintf("s%d", i+1)
			}
		} else {
			bucketCols = h.LinkSelect
		}
		for _, c := range bucketCols {
			cols = append(cols, sqlast.Identifier{Qualifier: alias, Name: c})
		}
		layout.Hops = append(layout.Hops, TraverseBucket{
			Inline: h.Inline,
			Alias:  alias,
			Offset: offset,
			Cols:   append([]string(nil), bucketCols...),
		})
		offset += len(bucketCols)
	}
	for i, h := range hops {
		if i == len(hops)-1 {
			break
		}
		midAlias := fmt.Sprintf("s%d", i+1)
		layout.Mids = append(layout.Mids, TraverseBucket{
			Alias:  midAlias,
			Offset: offset,
			Cols:   append([]string(nil), h.MidSelect...),
		})
		for _, c := range h.MidSelect {
			cols = append(cols, sqlast.Identifier{Qualifier: midAlias, Name: c})
		}
		offset += len(h.MidSelect)
	}
	sel.Columns = cols
	sel.Order = append(sel.Order, sqlast.Order{
		Field: sqlast.Identifier{Qualifier: termAlias, Name: last.TargetIDCol},
	})
	if last.Inline {
		hostAlias := fmt.Sprintf("s%d", len(hops)-1)
		if !last.FKOnPrev {
			hostAlias = termAlias
		}
		if last.LinkIdentityCol != "" {
			sel.Order = append(sel.Order, sqlast.Order{
				Field: sqlast.Identifier{Qualifier: hostAlias, Name: last.LinkIdentityCol},
			})
		}
	} else if last.LinkIdentityCol != "" {
		sel.Order = append(sel.Order, sqlast.Order{
			Field: sqlast.Identifier{Qualifier: linkAlias, Name: last.LinkIdentityCol},
		})
	}
	return sel, layout, []any{tenant, startID}, nil
}

func inlineHopOn(h TraverseHop, prevAlias, nextAlias string) *sqlast.Predicate {
	if h.FKOnPrev {
		return and(
			colEq(nextAlias, h.TargetIDCol, prevAlias, h.FKColumn),
			colEq(nextAlias, h.TargetTenantCol, prevAlias, h.PrevTenantCol),
		)
	}
	return and(
		colEq(nextAlias, h.FKColumn, prevAlias, h.PrevIDCol),
		colEq(nextAlias, h.TargetTenantCol, prevAlias, h.PrevTenantCol),
	)
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
		if f.Operator != "" && f.Operator != "eq" {
			return nil, nil, fmt.Errorf("%w: unsupported filter operator %q", spi.ErrInvalidMapping, f.Operator)
		}
		return eq(ident(f.Field), next), []any{f.Value}, nil
	}
	if len(f.Or) > 0 {
		// Or is accepted only over eq leaves — the batch-by-ids shape. Children
		// carrying compound expressions or other operators stay unsupported so
		// args order (textual ? appearance) stays trivially correct.
		children := make([]*sqlast.Predicate, 0, len(f.Or))
		args := make([]any, 0, len(f.Or))
		for _, c := range f.Or {
			if c.Field == "" || (c.Operator != "" && c.Operator != "eq") {
				return nil, nil, fmt.Errorf("%w: or children must be eq leaves", spi.ErrInvalidMapping)
			}
			if _, ok := known[c.Field]; !ok {
				return nil, nil, fmt.Errorf("%w: unknown filter field %q", spi.ErrInvalidMapping, c.Field)
			}
			children = append(children, eq(ident(c.Field), next+len(args)))
			args = append(args, c.Value)
		}
		return &sqlast.Predicate{Op: "or", Children: children}, args, nil
	}
	return nil, nil, fmt.Errorf("%w: unsupported filter", spi.ErrInvalidMapping)
}

func ident(name string) sqlast.Identifier { return sqlast.Identifier{Name: name} }

func knownColumns(b ObjectBinding) map[string]struct{} {
	known := map[string]struct{}{}
	if b.TenantColumn != "" {
		known[b.TenantColumn] = struct{}{}
	}
	for _, c := range b.IdentityColumns {
		known[c] = struct{}{}
	}
	for _, c := range b.SelectColumns {
		known[c] = struct{}{}
	}
	return known
}

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
