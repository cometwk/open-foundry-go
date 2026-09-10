package mysqlobda_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestAggregateFnsAndGrouping(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Ming", "membershipLevel": "gold", "currentBorrowCount": 100})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Lao Wang", "membershipLevel": "gold", "currentBorrowCount": 200})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Li", "membershipLevel": "gold"})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Hong", "membershipLevel": "silver", "currentBorrowCount": 50})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Old Zhang", "membershipLevel": "silver", "currentBorrowCount": 150})
	other := spi.RequestContext{TenantID: "t2"}
	mustCreate(t, p, other, "Reader", map[string]any{"name": "Spy", "membershipLevel": "gold", "currentBorrowCount": 9999})

	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "*", Fn: "count", Alias: "cnt"},
			{Field: "currentBorrowCount", Fn: "sum", Alias: "sum_count"},
			{Field: "currentBorrowCount", Fn: "avg", Alias: "avg_count"},
			{Field: "currentBorrowCount", Fn: "min", Alias: "min_count"},
			{Field: "currentBorrowCount", Fn: "max", Alias: "max_count"},
		},
		GroupBy: []string{"membershipLevel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups=%d want 2", len(res.Groups))
	}
	if res.TotalGroups != 2 {
		t.Fatalf("TotalGroups=%d want 2", res.TotalGroups)
	}
	byLevel := map[string]spi.AggregateGroup{}
	for _, g := range res.Groups {
		byLevel[g.Keys["membershipLevel"].(string)] = g
	}
	gold := byLevel["gold"]
	wantNum(t, gold.Values["cnt"], 3)
	wantNum(t, gold.Values["sum_count"], 300)
	wantNum(t, gold.Values["avg_count"], 150)
	wantNum(t, gold.Values["min_count"], 100)
	wantNum(t, gold.Values["max_count"], 200)
	silver := byLevel["silver"]
	wantNum(t, silver.Values["cnt"], 2)
	wantNum(t, silver.Values["sum_count"], 200)
}

func TestAggregateNoGroupByZeroHits(t *testing.T) {
	p := activateAgg(t)
	res, err := p.AggregateObjects(spi.RequestContext{TenantID: "t1"}, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "*", Fn: "count", Alias: "cnt"},
			{Field: "currentBorrowCount", Fn: "sum", Alias: "sum_count"},
			{Field: "currentBorrowCount", Fn: "avg", Alias: "avg_count"},
			{Field: "currentBorrowCount", Fn: "min", Alias: "min_count"},
			{Field: "currentBorrowCount", Fn: "max", Alias: "max_count"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups=%d want 1 (AE4)", len(res.Groups))
	}
	if res.TotalGroups != 1 {
		t.Fatalf("TotalGroups=%d want 1", res.TotalGroups)
	}
	if len(res.Groups[0].Keys) != 0 {
		t.Fatalf("keys=%v want empty", res.Groups[0].Keys)
	}
	wantNum(t, res.Groups[0].Values["cnt"], 0)
	if res.Groups[0].Values["sum_count"] != nil {
		t.Fatalf("sum=%v want nil", res.Groups[0].Values["sum_count"])
	}
	if res.Groups[0].Values["avg_count"] != nil {
		t.Fatalf("avg=%v want nil", res.Groups[0].Values["avg_count"])
	}
	if res.Groups[0].Values["min_count"] != nil {
		t.Fatalf("min=%v want nil", res.Groups[0].Values["min_count"])
	}
	if res.Groups[0].Values["max_count"] != nil {
		t.Fatalf("max=%v want nil", res.Groups[0].Values["max_count"])
	}
}

func TestAggregateGroupByZeroHits(t *testing.T) {
	p := activateAgg(t)
	res, err := p.AggregateObjects(spi.RequestContext{TenantID: "t1"}, "Reader", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"membershipLevel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("groups=%d want 0 (AE5)", len(res.Groups))
	}
	if res.TotalGroups != 0 {
		t.Fatalf("TotalGroups=%d want 0", res.TotalGroups)
	}
}

func TestAggregateCountSkipsNullSumNil(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Ming", "membershipLevel": "gold", "currentBorrowCount": 10})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Lao Wang", "membershipLevel": "gold", "currentBorrowCount": 20})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Li", "membershipLevel": "gold"})
	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "currentBorrowCount", Fn: "count", Alias: "c"},
			{Field: "currentBorrowCount", Fn: "sum", Alias: "s"},
		},
		GroupBy: []string{"membershipLevel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups=%d", len(res.Groups))
	}
	wantNum(t, res.Groups[0].Values["c"], 2)
	wantNum(t, res.Groups[0].Values["s"], 30)

	ghost := spi.RequestContext{TenantID: "t1"}
	p2 := activateAgg(t)
	mustCreate(t, p2, ghost, "Reader", map[string]any{"name": "Ghost", "membershipLevel": "basic"})
	res, err = p2.AggregateObjects(ghost, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "currentBorrowCount", Fn: "sum", Alias: "s"},
			{Field: "*", Fn: "count", Alias: "c"},
		},
		GroupBy: []string{"membershipLevel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Groups[0].Values["s"] != nil {
		t.Fatalf("sum on no-numeric = %v want nil (AE6)", res.Groups[0].Values["s"])
	}
	wantNum(t, res.Groups[0].Values["c"], 1)
}

func TestAggregateExcludesSoftDeletedAndOtherTenant(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	obj, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming", "membershipLevel": "gold", "currentBorrowCount": 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Reader", obj[spi.FieldID].(string), "soft"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, p, spi.RequestContext{TenantID: "t2"}, "Reader", map[string]any{"name": "Xiao Hong", "membershipLevel": "gold", "currentBorrowCount": 99})
	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantNum(t, res.Groups[0].Values["cnt"], 0)
}

func TestAggregateOrderByAndPagination(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "r1", "membershipLevel": "gold", "currentBorrowCount": 1})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "r2", "membershipLevel": "basic", "currentBorrowCount": 2})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "r3", "membershipLevel": "silver", "currentBorrowCount": 3})

	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"membershipLevel"},
		OrderBy: []spi.OrderBy{{Field: "membershipLevel", Direction: "asc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Groups[0].Keys["membershipLevel"]; got != "basic" {
		t.Fatalf("first level=%v want basic", got)
	}

	res, err = p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"membershipLevel"},
		OrderBy: []spi.OrderBy{{Field: "membershipLevel", Direction: "desc"}},
		Limit:   1,
		Offset:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalGroups != 3 {
		t.Fatalf("TotalGroups=%d want 3", res.TotalGroups)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups=%d want 1", len(res.Groups))
	}
	if got := res.Groups[0].Keys["membershipLevel"]; got != "gold" {
		t.Fatalf("offset level=%v want gold", got)
	}

	_, err = p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"membershipLevel"},
		OrderBy: []spi.OrderBy{{Field: "nope"}},
	})
	if err == nil {
		t.Fatal("unknown order field should error")
	}
}

// TestAggregateOrderByAlias uses counts that differ per group so ordering by
// the aggregate alias (not just the group key) is actually observable.
func TestAggregateOrderByAlias(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "r1", "membershipLevel": "gold"})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "r2", "membershipLevel": "gold"})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "r3", "membershipLevel": "silver"})

	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"membershipLevel"},
		OrderBy: []spi.OrderBy{{Field: "cnt", Direction: "desc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups=%d want 2", len(res.Groups))
	}
	if got := res.Groups[0].Keys["membershipLevel"]; got != "gold" {
		t.Fatalf("first level (order by alias desc)=%v want gold (cnt=2)", got)
	}
	wantNum(t, res.Groups[0].Values["cnt"], 2)
	if got := res.Groups[1].Keys["membershipLevel"]; got != "silver" {
		t.Fatalf("second level=%v want silver (cnt=1)", got)
	}
}

// TestAggregateDefaultAlias covers R3: an omitted Alias defaults to fn_field.
func TestAggregateDefaultAlias(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Ming", "currentBorrowCount": 10})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Lao Wang", "currentBorrowCount": 20})

	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "currentBorrowCount", Fn: "sum"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantNum(t, res.Groups[0].Values["sum_currentBorrowCount"], 30)
}

func TestAggregateValidationAndFilter(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Ming", "membershipLevel": "gold", "currentBorrowCount": 10})
	mustCreate(t, p, ctx, "Reader", map[string]any{"name": "Xiao Hong", "membershipLevel": "silver", "currentBorrowCount": 20})

	if _, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{}); err == nil {
		t.Fatal("empty Fields should error")
	}
	if _, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "currentBorrowCount", Fn: "median"}},
	}); err == nil {
		t.Fatal("invalid fn should error")
	}
	if _, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "sum"}},
	}); err == nil {
		t.Fatal("sum(*) should error")
	}
	_, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "nope", Fn: "count"}},
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("unknown field err=%v want ErrInvalidMapping", err)
	}
	_, err = p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"nope"},
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("unknown groupBy err=%v want ErrInvalidMapping", err)
	}

	eq := spi.FilterExpression{Field: "membershipLevel", Operator: "eq", Value: "gold"}
	res, err := p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		Filter: &eq,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantNum(t, res.Groups[0].Values["cnt"], 1)

	// AE7/R13: non-eq operators and compound filters must be rejected, not
	// silently ignored, matching Query/Search's filter-unification behavior.
	ne := spi.FilterExpression{Field: "membershipLevel", Operator: "ne", Value: "gold"}
	_, err = p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		Filter: &ne,
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("ne filter err=%v want ErrInvalidMapping (AE7)", err)
	}
	and := spi.FilterExpression{And: []spi.FilterExpression{eq}}
	_, err = p.AggregateObjects(ctx, "Reader", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		Filter: &and,
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("And filter err=%v want ErrInvalidMapping (AE7)", err)
	}
}

func activateAgg(t *testing.T) *mysqlobda.Provider {
	t.Helper()
	raw := testdata(t, "library.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, readerSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema()); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustCreate(t *testing.T, p *mysqlobda.Provider, ctx spi.RequestContext, typ string, props map[string]any) {
	t.Helper()
	if _, err := p.CreateObject(ctx, typ, props); err != nil {
		t.Fatal(err)
	}
}

func wantNum(t *testing.T, got any, want float64) {
	t.Helper()
	f, ok := toFloat(got)
	if !ok {
		t.Fatalf("got %v (%T), want %v", got, got, want)
	}
	if f != want {
		t.Fatalf("got %v want %v", f, want)
	}
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	default:
		return 0, false
	}
}
