package memory

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
)

// File covers U4: QueryObjects (10 operators + and/or/not + pagination +
// orderBy + AsOfTime best-effort), AggregateObjects (5 fns + groupBy),
// SearchObjects (token score + highlights + default fields + blank query).
// Mirrors the TS memory provider's intent (memory-storage-provider.ts:
// 70-119, 545-581, 583-707, 709-787), not its AST.

// testQueryObjects seeds 4 Supplier objects in tenant A and returns the
// created objects so individual operator tests can reference them.
func seedQueryObjects(t *testing.T, p *Provider) []spi.OntologyObject {
	t.Helper()
	a, _ := tenancyA()
	specs := []struct {
		name string
		tier string
		city string
	}{
		{"Acme", "Gold", "NYC"},
		{"Globex", "Silver", "LA"},
		{"Initech", "Gold", "SF"},
		{"Acme Corp", "Silver", "NYC"},
	}
	out := make([]spi.OntologyObject, 0, len(specs))
	for _, s := range specs {
		obj, err := p.CreateObject(a, "Supplier", map[string]any{
			"name": s.name,
			"tier": s.tier,
			"city": s.city,
		})
		if err != nil {
			t.Fatalf("seed CreateObject %s err: %v", s.name, err)
		}
		out = append(out, obj)
	}
	// Different tenant (B): must never appear in tenant-A queries.
	// tenancyA() returns (tenantA, tenantB) so use blank-assign to take B.
	_, b := tenancyA()
	if _, err := p.CreateObject(b, "Supplier", map[string]any{"name": "In Spy"}); err != nil {
		t.Fatalf("seed cross-tenant CreateObject err: %v", err)
	}
	return out
}

func eqFilter(field, op string, val any) spi.FilterExpression {
	return spi.FilterExpression{Field: field, Operator: op, Value: val}
}

func TestQueryObjects_FieldOperators(t *testing.T) {
	// gt/gte/lt/lte are numeric-only in TS (memory-storage-provider.ts:
	// 88-94 gate on typeof === 'number'); string comparisons short-circuit
	// to false. Mirror that semantics here: those rows use a numeric
	// field (score) for true-positive coverage.
	cases := []struct {
		name   string
		filter spi.FilterExpression
		want   int
	}{
		{"eq tier=Gold", eqFilter("tier", "eq", "Gold"), 2},
		{"neq tier!=Gold", eqFilter("tier", "neq", "Gold"), 2},
		{"gt tier (string op on non-number returns false)", eqFilter("tier", "gt", "Globex"), 0},
		{"gte tier (string op on non-number returns false)", eqFilter("tier", "gte", "Globex"), 0},
		{"lt tier (string op on non-number returns false)", eqFilter("tier", "lt", "Globex"), 0},
		{"lte tier (string op on non-number returns false)", eqFilter("tier", "lte", "Globex"), 0},
		{"in tier", eqFilter("tier", "in", []any{"Gold", "Silver"}), 4},
		{"contains name", eqFilter("name", "contains", "Acm"), 2}, // Acme, Acme Corp
		{"startsWith name", eqFilter("name", "startsWith", "Acme"), 2},
		{"exists name", eqFilter("name", "exists", true), 4},
		{"exists missingKey", eqFilter("nope", "exists", false), 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := New()
			seedQueryObjects(t, p)
			a, _ := tenancyA()
			page, err := p.QueryObjects(a, "Supplier", c.filter, &spi.QueryOptions{Limit: 100})
			if err != nil {
				t.Fatalf("QueryObjects err = %v, want nil (U4)", err)
			}
			if len(page.Items) != c.want {
				t.Errorf("QueryObjects(%s) returned %d items, want %d (U4)", c.name, len(page.Items), c.want)
			}
			if page.TotalCount != c.want {
				t.Errorf("QueryObjects(%s) totalCount = %d, want %d (U4)", c.name, page.TotalCount, c.want)
			}
		})
	}
}

// TestQueryObjects_NumericComparators uses a numeric field to verify
// gt/gte/lt/lte return true matches on numbers, not just on no-match
// strings. Mirrors TS typeof === 'number' semantics.
func TestQueryObjects_NumericComparators(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "S1", "score": float64(10)})
	p.CreateObject(a, "Supplier", map[string]any{"name": "S2", "score": float64(20)})
	p.CreateObject(a, "Supplier", map[string]any{"name": "S3", "score": float64(30)})
	cases := []struct {
		name   string
		filter spi.FilterExpression
		want   int
	}{
		{"gt 10", eqFilter("score", "gt", float64(10)), 2},
		{"gte 20", eqFilter("score", "gte", float64(20)), 2},
		{"lt 30", eqFilter("score", "lt", float64(30)), 2},
		{"lte 20", eqFilter("score", "lte", float64(20)), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := p.QueryObjects(a, "Supplier", c.filter, nil)
			if err != nil {
				t.Fatalf("QueryObjects err = %v (U4)", err)
			}
			if len(page.Items) != c.want {
				t.Errorf("%s returned %d items, want %d (U4)", c.name, len(page.Items), c.want)
			}
		})
	}
}

func TestQueryObjects_LogicalCombinators(t *testing.T) {
	p := New()
	seedQueryObjects(t, p)
	a, _ := tenancyA()
	cases := []struct {
		name   string
		filter spi.FilterExpression
		want   int
	}{
		{
			"and tier=Gold AND city=NYC",
			spi.FilterExpression{And: []spi.FilterExpression{
				eqFilter("tier", "eq", "Gold"),
				eqFilter("city", "eq", "NYC"),
			}},
			1, // Acme only (Initech is Gold but SF)
		},
		{
			"or tier=Gold OR tier=Silver",
			spi.FilterExpression{Or: []spi.FilterExpression{
				eqFilter("tier", "eq", "Gold"),
				eqFilter("tier", "eq", "Silver"),
			}},
			4,
		},
		{
			"not tier=Gold",
			spi.FilterExpression{Not: &spi.FilterExpression{Field: "tier", Operator: "eq", Value: "Gold"}},
			2, // Globex + Acme Corp
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := p.QueryObjects(a, "Supplier", c.filter, nil)
			if err != nil {
				t.Fatalf("QueryObjects err = %v (U4)", err)
			}
			if len(page.Items) != c.want {
				t.Errorf("QueryObjects(%s) returned %d items, want %d (U4)", c.name, len(page.Items), c.want)
			}
		})
	}
}

func TestQueryObjects_EmptyFilter_ReturnsAll(t *testing.T) {
	p := New()
	seedQueryObjects(t, p)
	a, _ := tenancyA()
	page, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatalf("QueryObjects empty filter err = %v (U4)", err)
	}
	if len(page.Items) != 4 {
		t.Errorf("empty filter returned %d, want 4 (U4)", len(page.Items))
	}
}

func TestQueryObjects_Pagination(t *testing.T) {
	p := New()
	seedQueryObjects(t, p)
	a, _ := tenancyA()
	page, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("QueryObjects err = %v (U4)", err)
	}
	if len(page.Items) != 2 {
		t.Errorf("limit 2 offset 1 returned %d items, want 2 (U4)", len(page.Items))
	}
	if page.TotalCount != 4 {
		t.Errorf("totalCount = %d, want 4 (U4)", page.TotalCount)
	}
	if !page.HasNextPage {
		t.Errorf("HasNextPage = false for offset 1 limit 2 over 4 items, want true (U4)")
	}
	// Past end: empty items, no hasNextPage.
	pageEnd, _ := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{Limit: 2, Offset: 4})
	if len(pageEnd.Items) != 0 {
		t.Errorf("offset=4 returned %d items, want 0 (U4)", len(pageEnd.Items))
	}
	if pageEnd.HasNextPage {
		t.Errorf("HasNextPage = true at end, want false (U4)")
	}
}

func TestQueryObjects_OrderByMultiKey(t *testing.T) {
	p := New()
	seedQueryObjects(t, p)
	a, _ := tenancyA()
	page, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{
		OrderBy: []spi.OrderBy{
			{Field: "tier", Direction: "asc"},
			{Field: "name", Direction: "desc"},
		},
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("QueryObjects err = %v (U4)", err)
	}
	// tiers sorted asc => Gold first. Within Gold, names desc => Initech, Acme.
	// Within Silver, names desc => Globex, Acme Corp.
	want := []string{"Initech", "Acme", "Globex", "Acme Corp"}
	for i, o := range page.Items {
		if o["name"] != want[i] {
			t.Errorf("order[%d] name = %v, want %v (U4 orderBy multi)", i, o["name"], want[i])
		}
	}
}

func TestQueryObjects_DefaultLimit100(t *testing.T) {
	// RS: MAX_QUERY_LIMIT=1000, default limit=100, hasNextPage = offset+limit < totalCount.
	// Test defaults behavior with nil options and seed 4 items.
	p := New()
	seedQueryObjects(t, p)
	a, _ := tenancyA()
	page, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatalf("QueryObjects err = %v (U4)", err)
	}
	if len(page.Items) != 4 {
		t.Errorf("nil options returned %d items, want 4 (U4 default limit only caps)", len(page.Items))
	}
	if page.HasNextPage {
		t.Errorf("4 items under default-100 limit HasNextPage = true, want false (U4)")
	}
}

func TestQueryObjects_IncludeDeleted(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "tier": "Gold"})
	id := obj["_id"].(string)
	if err := p.DeleteObject(a, "Supplier", id, "soft"); err != nil {
		t.Fatalf("soft delete err: %v", err)
	}
	// Default exclude
	page, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatalf("QueryObjects err = %v (U4)", err)
	}
	for _, it := range page.Items {
		if it["_deletedAt"] != nil {
			t.Errorf("default exclude returned soft-deleted item (U4)")
		}
	}
	// Include
	pageInc, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("QueryObjects include err = %v (U4)", err)
	}
	found := false
	for _, it := range pageInc.Items {
		if it["_id"] == id {
			found = true
		}
	}
	if !found {
		t.Errorf("includeDeleted did not return the soft-deleted item (U4)")
	}
}

func TestQueryObjects_CrossTenantIsolation(t *testing.T) {
	p := New()
	seedQueryObjects(t, p) // 4 tenant A, 1 tenant B (via _, b :=)
	// Use the tenant-A context seeded into p; query tenant B (the second
	// return value of tenancyA). B sees exactly its 1 item, not A's 4.
	_, b := tenancyA()
	page, err := p.QueryObjects(b, "Supplier", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatalf("QueryObjects err = %v (U4)", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("tenant B sees %d items, want 1 (U4 isolation)", len(page.Items))
	}
}

func TestAggregateObjects_FnsAndGrouping(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	// 3 Gold-suppliers, 2 Silver-suppliers, with tnr scores for sum/avg/min/max.
	p.CreateObject(a, "Supplier", map[string]any{"name": "S1", "tier": "Gold", "tnr": float64(100)})
	p.CreateObject(a, "Supplier", map[string]any{"name": "S2", "tier": "Gold", "tnr": float64(200)})
	p.CreateObject(a, "Supplier", map[string]any{"name": "S3", "tier": "Gold" /* nullable tnr */})
	p.CreateObject(a, "Supplier", map[string]any{"name": "S4", "tier": "Silver", "tnr": float64(50)})
	p.CreateObject(a, "Supplier", map[string]any{"name": "S5", "tier": "Silver", "tnr": float64(150)})
	// Different tenant (B) must never appear in the aggregate (isolation).
	_, b := tenancyA()
	p.CreateObject(b, "Supplier", map[string]any{"name": "Spy", "tier": "Gold", "tnr": float64(9999)})

	res, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "*", Fn: "count", Alias: "cnt"},
			{Field: "tnr", Fn: "sum", Alias: "sum_tnr"},
			{Field: "tnr", Fn: "avg", Alias: "avg_tnr"},
			{Field: "tnr", Fn: "min", Alias: "min_tnr"},
			{Field: "tnr", Fn: "max", Alias: "max_tnr"},
		},
		GroupBy: []string{"tier"},
	})
	if err != nil {
		t.Fatalf("AggregateObjects err = %v (U4)", err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("AggregateObjects groups = %d, want 2 (Gold/Silver) (U4)", len(res.Groups))
	}
	if res.TotalGroups != 2 {
		t.Errorf("TotalGroups = %d, want 2 (U4)", res.TotalGroups)
	}
	byTier := map[string]spi.AggregateGroup{}
	for _, g := range res.Groups {
		byTier[g.Keys["tier"].(string)] = g
	}
	gold := byTier["Gold"]
	if gold.Values["cnt"] != 3 {
		t.Errorf("Gold cnt = %v, want 3 (U4 count*)", gold.Values["cnt"])
	}
	if gold.Values["sum_tnr"] != float64(300) { // 100+200, nil excluded
		t.Errorf("Gold sum_tnr = %v, want 300 (U4)", gold.Values["sum_tnr"])
	}
	if gold.Values["avg_tnr"].(float64) != 150 { // (100+200)/2
		t.Errorf("Gold avg_tnr = %v, want 150 (U4)", gold.Values["avg_tnr"])
	}
	if gold.Values["min_tnr"] != float64(100) {
		t.Errorf("Gold min_tnr = %v, want 100 (U4)", gold.Values["min_tnr"])
	}
	if gold.Values["max_tnr"] != float64(200) {
		t.Errorf("Gold max_tnr = %v, want 200 (U4)", gold.Values["max_tnr"])
	}
	// Silver: count=2, tnr sum=200 (50+150)
	silver := byTier["Silver"]
	if silver.Values["cnt"] != 2 {
		t.Errorf("Silver cnt = %v, want 2 (U4)", silver.Values["cnt"])
	}
	if silver.Values["sum_tnr"] != float64(200) {
		t.Errorf("Silver sum_tnr = %v, want 200 (U4)", silver.Values["sum_tnr"])
	}
}

func TestAggregateObjects_AllSoftDeletedExcluded(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "tnr": float64(10)})
	if err := p.DeleteObject(a, "Supplier", obj["_id"].(string), "soft"); err != nil {
		t.Fatalf("soft delete err: %v", err)
	}
	res, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
	})
	if err != nil {
		t.Fatalf("AggregateObjects err = %v (U4)", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("AggregateObjects groups = %d, want 1 (single no-groupBy group) (U4)", len(res.Groups))
	}
	if res.Groups[0].Values["cnt"] != 0 {
		t.Errorf("AggregateObjects after soft delete cnt = %v, want 0 (aggregate always excludes _deletedAt) (U4)", res.Groups[0].Values["cnt"])
	}
}

func TestAggregateObjects_InvalidFnThrows(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "S", "tnr": float64(1)})
	_, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "tnr", Fn: "median"}},
	})
	if err == nil {
		t.Fatalf("AggregateObjects invalid fn median returned nil err, want non-nil (U4 early validate)")
	}
}

func TestAggregateObjects_EmptyFieldsThrows(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	if _, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{}); err == nil {
		t.Fatalf("AggregateObjects with no fields returned nil err, want non-nil (U4)")
	}
}

func TestAggregateObjects_NumericNullGroup(t *testing.T) {
	// When group has no numeric values, sum/avg/min/max must be nil, per TS.
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "S1", "tier": "Ghost" /* no tnr */})
	res, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "tnr", Fn: "sum", Alias: "sum"}, {Field: "*", Fn: "count", Alias: "c"}},
		GroupBy: []string{"tier"},
	})
	if err != nil {
		t.Fatalf("AggregateObjects err = %v (U4)", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups = %d, want 1 (U4)", len(res.Groups))
	}
	if res.Groups[0].Values["sum"] != nil {
		t.Errorf("sum on no-numeric group = %v, want nil (U4)", res.Groups[0].Values["sum"])
	}
	if res.Groups[0].Values["c"] != 1 {
		t.Errorf("count = %v, want 1 (U4)", res.Groups[0].Values["c"])
	}
}

func TestSearchObjects_TokenScoreAndHighlights(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "city": "New York"})
	p.CreateObject(a, "Supplier", map[string]any{"name": "Globex", "city": "Newark"})
	p.CreateObject(a, "Supplier", map[string]any{"name": "Initech", "city": "Sunnyvale"}) // no "new" anywhere
	res, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{Query: "new"})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (U4)", err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("SearchObjects(new) hits = %d, want 2 (New York + Newark) (U4)", len(res.Hits))
	}
	// Score: "newark" contains "new" once; "New York" lowercase "new york" contains "new" once.
	// Equal scores — verify both have score >= 1.
	for _, h := range res.Hits {
		if h.Score < 1 {
			t.Errorf("hit score = %v, want >= 1 (U4 scoring)", h.Score)
		}
		if len(h.Highlights) == 0 {
			t.Errorf("hit missing highlights (U4 highlights push entire field value)")
		}
	}
}

func TestSearchObjects_BlankQueryReturnsEmpty(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	res, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (U4)", err)
	}
	if len(res.Hits) != 0 || res.TotalCount != 0 {
		t.Errorf("blank query returned hits=%d total=%d, want 0/0 (U4)", len(res.Hits), res.TotalCount)
	}
	res2, _ := p.SearchObjects(a, "Supplier", spi.SearchQuery{Query: "   "})
	if len(res2.Hits) != 0 {
		t.Errorf("whitespace-only query returned hits = %d, want 0 (U4)", len(res2.Hits))
	}
}

func TestSearchObjects_DefaultFieldsAreNonSystemStrings(t *testing.T) {
	// Default search fields = object keys that do not start with _ and have string values.
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "Findable", "tier": "Gold"})
	p.CreateObject(a, "Supplier", map[string]any{"name": "HiddenField", "tier": "SecretNoMatch", "city": "Findable"})
	res, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{Query: "findable"})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (U4)", err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("expected 2 hits on default fields, got %d (U4)", len(res.Hits))
	}
}

func TestSearchObjects_SoftDeletedExcludedAlways(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "tier": "Findable"})
	if err := p.DeleteObject(a, "Supplier", obj["_id"].(string), "soft"); err != nil {
		t.Fatalf("soft delete err: %v", err)
	}
	res, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{Query: "findable"})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (U4)", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("SearchObjects included soft-deleted item: %d hits, want 0 (U4 always excludes _deletedAt)", len(res.Hits))
	}
}

func TestSearchObjects_CrossTenantIsolation(t *testing.T) {
	p := New()
	a, b := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "TenantA"})
	p.CreateObject(b, "Supplier", map[string]any{"name": "TenantA_B"})
	res, err := p.SearchObjects(b, "Supplier", spi.SearchQuery{Query: "tenanta"})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (U4)", err)
	}
	// "tenanta" matches both "TenantA" and "TenantA_B" (substring), but
	// tenant B must only see its own.
	if len(res.Hits) != 1 {
		t.Errorf("tenant B search saw %d hits, want 1 (U4 isolation)", len(res.Hits))
	}
	if res.Hits[0].Object["name"] != "TenantA_B" {
		t.Errorf("tenant B hit name = %v, want TenantA_B (U4)", res.Hits[0].Object["name"])
	}
}

func TestSearchObjects_FilterPrunesHits(t *testing.T) {
	// Search + filter: filter applies AFTER scoring (TS res身上 matches
	// query.filter). Use to narrow the candidate set.
	p := New()
	a, _ := tenancyA()
	p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "tier": "Gold"})
	p.CreateObject(a, "Supplier", map[string]any{"name": "Acme Corp", "tier": "Silver"})
	res, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{
		Query:  "acme",
		Filter: &spi.FilterExpression{Field: "tier", Operator: "eq", Value: "Gold"},
	})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (U4)", err)
	}
	if len(res.Hits) != 1 {
		t.Errorf("filter narrows acme search to %d hits, want 1 (U4 gold-only) (U4)", len(res.Hits))
	}
	if res.Hits[0].Object["tier"] != "Gold" {
		t.Errorf("filtered hit tier = %v, want Gold (U4)", res.Hits[0].Object["tier"])
	}
}

// Verify floor methods no longer accept ErrUnimplemented as a code path.
// (U4 commits QueryObjects/AggregateObjects/SearchObjects real impls.)
func TestQueryAggregateSearch_NotErrUnimplemented(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	seedQueryObjects(t, p)
	// All three methods must execute (and return a populated result or
	// typed err), never ErrUnimplemented. Sample any filter to validate.
	if _, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("QueryObjects returned ErrUnimplemented (U4 should have implemented it)")
	}
	if _, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{Fields: []spi.AggregateField{{Field: "*", Fn: "count"}}}); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("AggregateObjects returned ErrUnimplemented (U4 should have implemented it)")
	}
	if _, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{Query: "acme"}); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("SearchObjects returned ErrUnimplemented (U4 should have implemented it)")
	}
}