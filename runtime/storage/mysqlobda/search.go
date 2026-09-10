package mysqlobda

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

func (p *Provider) SearchObjects(ctx spi.RequestContext, typ string, query spi.SearchQuery) (spi.SearchResult, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return spi.SearchResult{}, err
	}
	m, err := act.model(typ)
	if err != nil {
		return spi.SearchResult{}, spi.ErrObjectNotFound
	}
	// Blank query is empty regardless of mapping (R9). Non-blank search on a
	// model without search.fields is ErrUnsupportedCapability, not empty hits.
	// InnoDB FULLTEXT uses innodb_ft_min_token_size (default 3); shorter
	// tokens do not match — MySQL behavior, not a provider defect.
	if strings.TrimSpace(query.Query) == "" {
		return spi.SearchResult{Hits: []spi.SearchHit{}, TotalCount: 0}, nil
	}
	if len(m.SearchableFields) == 0 {
		return spi.SearchResult{}, spi.ErrUnsupportedCapability
	}
	declared := searchableLogical(m)
	if len(query.Fields) > 0 && !sameStringSet(query.Fields, declared) {
		return spi.SearchResult{}, spi.ErrUnsupportedCapability
	}

	var phys spi.FilterExpression
	if query.Filter != nil {
		phys, err = translateFilter(m, *query.Filter)
		if err != nil {
			return spi.SearchResult{}, err
		}
	}
	sel, args, err := obda.PlanSearch(m.Binding(), ctx.TenantID, query.Query)
	if err != nil {
		return spi.SearchResult{}, err
	}
	if phys.Field != "" {
		sel.Where = andPred(sel.Where, &sqlast.Predicate{
			Op:    "eq",
			Field: &sqlast.Identifier{Name: phys.Field},
			Value: sqlast.Param{},
		})
		args = append(args, phys.Value)
	}
	if !m.Omit.DeletedAt {
		sel.Where = andPred(sel.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Name: "deleted_at"},
		})
	}
	for _, col := range m.IdentityColumns {
		sel.Order = append(sel.Order, sqlast.Order{Field: sqlast.Identifier{Name: col}})
	}

	countSel := *sel
	countSel.Limit = nil
	countSel.Order = nil
	countStmt, err := p.dialect.Render(&countSel)
	if err != nil {
		return spi.SearchResult{}, err
	}
	var total int
	if err := p.db.QueryRow("SELECT COUNT(*) FROM ("+countStmt.SQL+") AS q", args...).Scan(&total); err != nil {
		return spi.SearchResult{}, mysqldialect.Classify(err)
	}

	limit, offset := pageLimitOffset(query.Limit, query.Offset)
	sel.Limit = &sqlast.LimitOffset{Limit: sqlast.Param{}, Offset: sqlast.Param{}}
	pageArgs := append(append([]any{}, args...), limit+1, offset)
	stmt, err := p.dialect.Render(sel)
	if err != nil {
		return spi.SearchResult{}, err
	}
	rows, err := p.db.Query(stmt.SQL, pageArgs...)
	if err != nil {
		return spi.SearchResult{}, mysqldialect.Classify(err)
	}
	defer rows.Close()

	bizCols := m.Binding().SelectColumns
	var hits []spi.SearchHit
	for rows.Next() {
		dest := make([]any, len(bizCols)+1)
		ptrs := make([]any, len(dest))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return spi.SearchResult{}, err
		}
		biz := map[string]any{}
		for i, col := range bizCols {
			biz[col] = unwrap(dest[i])
		}
		obj, err := p.assemble(m, ctx.TenantID, biz)
		if err != nil {
			return spi.SearchResult{}, err
		}
		hits = append(hits, spi.SearchHit{
			Object:     obj,
			Score:      asFloat64(dest[len(bizCols)]),
			Highlights: searchableHighlights(m, obj),
		})
	}
	if err := rows.Err(); err != nil {
		return spi.SearchResult{}, err
	}
	hasNext := len(hits) > limit
	if hasNext {
		hits = hits[:limit]
	}
	return spi.SearchResult{Hits: hits, TotalCount: total, HasNextPage: hasNext}, nil
}

func searchableLogical(m *obda.CompiledModel) []string {
	out := make([]string, 0, len(m.SearchableFields))
	for _, col := range m.SearchableFields {
		if f, ok := m.FieldByColumn[col]; ok {
			out = append(out, f.Logical)
		}
	}
	return out
}

func searchableHighlights(m *obda.CompiledModel, obj spi.OntologyObject) map[string][]string {
	hl := map[string][]string{}
	for _, logical := range searchableLogical(m) {
		v, ok := obj[logical]
		if !ok || v == nil {
			continue
		}
		hl[logical] = []string{fmt.Sprint(v)}
	}
	return hl
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func asFloat64(v any) float64 {
	switch t := unwrap(v).(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}
