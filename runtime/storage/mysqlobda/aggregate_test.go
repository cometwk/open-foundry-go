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
	mustCreate(t, p, ctx, map[string]any{"name": "S1", "city": "Gold", "score": 100})
	mustCreate(t, p, ctx, map[string]any{"name": "S2", "city": "Gold", "score": 200})
	mustCreate(t, p, ctx, map[string]any{"name": "S3", "city": "Gold"})
	mustCreate(t, p, ctx, map[string]any{"name": "S4", "city": "Silver", "score": 50})
	mustCreate(t, p, ctx, map[string]any{"name": "S5", "city": "Silver", "score": 150})
	other := spi.RequestContext{TenantID: "t2"}
	mustCreate(t, p, other, map[string]any{"name": "Spy", "city": "Gold", "score": 9999})

	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "*", Fn: "count", Alias: "cnt"},
			{Field: "score", Fn: "sum", Alias: "sum_score"},
			{Field: "score", Fn: "avg", Alias: "avg_score"},
			{Field: "score", Fn: "min", Alias: "min_score"},
			{Field: "score", Fn: "max", Alias: "max_score"},
		},
		GroupBy: []string{"city"},
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
	byCity := map[string]spi.AggregateGroup{}
	for _, g := range res.Groups {
		byCity[g.Keys["city"].(string)] = g
	}
	gold := byCity["Gold"]
	wantNum(t, gold.Values["cnt"], 3)
	wantNum(t, gold.Values["sum_score"], 300)
	wantNum(t, gold.Values["avg_score"], 150)
	wantNum(t, gold.Values["min_score"], 100)
	wantNum(t, gold.Values["max_score"], 200)
	silver := byCity["Silver"]
	wantNum(t, silver.Values["cnt"], 2)
	wantNum(t, silver.Values["sum_score"], 200)
}

func TestAggregateNoGroupByZeroHits(t *testing.T) {
	p := activateAgg(t)
	res, err := p.AggregateObjects(spi.RequestContext{TenantID: "t1"}, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "*", Fn: "count", Alias: "cnt"},
			{Field: "score", Fn: "sum", Alias: "sum_score"},
			{Field: "score", Fn: "avg", Alias: "avg_score"},
			{Field: "score", Fn: "min", Alias: "min_score"},
			{Field: "score", Fn: "max", Alias: "max_score"},
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
	if res.Groups[0].Values["sum_score"] != nil {
		t.Fatalf("sum=%v want nil", res.Groups[0].Values["sum_score"])
	}
	if res.Groups[0].Values["avg_score"] != nil {
		t.Fatalf("avg=%v want nil", res.Groups[0].Values["avg_score"])
	}
	if res.Groups[0].Values["min_score"] != nil {
		t.Fatalf("min=%v want nil", res.Groups[0].Values["min_score"])
	}
	if res.Groups[0].Values["max_score"] != nil {
		t.Fatalf("max=%v want nil", res.Groups[0].Values["max_score"])
	}
}

func TestAggregateGroupByZeroHits(t *testing.T) {
	p := activateAgg(t)
	res, err := p.AggregateObjects(spi.RequestContext{TenantID: "t1"}, "Patient", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"city"},
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
	mustCreate(t, p, ctx, map[string]any{"name": "A", "city": "X", "score": 10})
	mustCreate(t, p, ctx, map[string]any{"name": "B", "city": "X", "score": 20})
	mustCreate(t, p, ctx, map[string]any{"name": "C", "city": "X"})
	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "score", Fn: "count", Alias: "c"},
			{Field: "score", Fn: "sum", Alias: "s"},
		},
		GroupBy: []string{"city"},
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
	mustCreate(t, p2, ghost, map[string]any{"name": "G", "city": "Ghost"})
	res, err = p2.AggregateObjects(ghost, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{
			{Field: "score", Fn: "sum", Alias: "s"},
			{Field: "*", Fn: "count", Alias: "c"},
		},
		GroupBy: []string{"city"},
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
	obj, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada", "city": "X", "score": 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Patient", obj[spi.FieldID].(string), "soft"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, p, spi.RequestContext{TenantID: "t2"}, map[string]any{"name": "Bob", "city": "X", "score": 99})
	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
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
	mustCreate(t, p, ctx, map[string]any{"name": "a", "city": "B", "score": 1})
	mustCreate(t, p, ctx, map[string]any{"name": "b", "city": "A", "score": 2})
	mustCreate(t, p, ctx, map[string]any{"name": "c", "city": "C", "score": 3})

	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"city"},
		OrderBy: []spi.OrderBy{{Field: "city", Direction: "asc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Groups[0].Keys["city"]; got != "A" {
		t.Fatalf("first city=%v want A", got)
	}

	res, err = p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"city"},
		OrderBy: []spi.OrderBy{{Field: "city", Direction: "desc"}},
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
	if got := res.Groups[0].Keys["city"]; got != "B" {
		t.Fatalf("offset city=%v want B", got)
	}

	_, err = p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"city"},
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
	mustCreate(t, p, ctx, map[string]any{"name": "a1", "city": "A"})
	mustCreate(t, p, ctx, map[string]any{"name": "a2", "city": "A"})
	mustCreate(t, p, ctx, map[string]any{"name": "b1", "city": "B"})

	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"city"},
		OrderBy: []spi.OrderBy{{Field: "cnt", Direction: "desc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("groups=%d want 2", len(res.Groups))
	}
	if got := res.Groups[0].Keys["city"]; got != "A" {
		t.Fatalf("first city (order by alias desc)=%v want A (cnt=2)", got)
	}
	wantNum(t, res.Groups[0].Values["cnt"], 2)
	if got := res.Groups[1].Keys["city"]; got != "B" {
		t.Fatalf("second city=%v want B (cnt=1)", got)
	}
}

// TestAggregateDefaultAlias covers R3: an omitted Alias defaults to fn_field.
func TestAggregateDefaultAlias(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, map[string]any{"name": "A", "score": 10})
	mustCreate(t, p, ctx, map[string]any{"name": "B", "score": 20})

	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "score", Fn: "sum"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantNum(t, res.Groups[0].Values["sum_score"], 30)
}

func TestAggregateValidationAndFilter(t *testing.T) {
	p := activateAgg(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, map[string]any{"name": "Ada", "city": "X", "score": 10})
	mustCreate(t, p, ctx, map[string]any{"name": "Bob", "city": "Y", "score": 20})

	if _, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{}); err == nil {
		t.Fatal("empty Fields should error")
	}
	if _, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "score", Fn: "median"}},
	}); err == nil {
		t.Fatal("invalid fn should error")
	}
	if _, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "sum"}},
	}); err == nil {
		t.Fatal("sum(*) should error")
	}
	_, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "nope", Fn: "count"}},
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("unknown field err=%v want ErrInvalidMapping", err)
	}
	_, err = p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields:  []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		GroupBy: []string{"nope"},
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("unknown groupBy err=%v want ErrInvalidMapping", err)
	}

	eq := spi.FilterExpression{Field: "city", Operator: "eq", Value: "X"}
	res, err := p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		Filter: &eq,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantNum(t, res.Groups[0].Values["cnt"], 1)

	// AE7/R13: non-eq operators and compound filters must be rejected, not
	// silently ignored, matching Query/Search's filter-unification behavior.
	ne := spi.FilterExpression{Field: "city", Operator: "ne", Value: "X"}
	_, err = p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		Filter: &ne,
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("ne filter err=%v want ErrInvalidMapping (AE7)", err)
	}
	and := spi.FilterExpression{And: []spi.FilterExpression{eq}}
	_, err = p.AggregateObjects(ctx, "Patient", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "cnt"}},
		Filter: &and,
	})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("And filter err=%v want ErrInvalidMapping (AE7)", err)
	}
}

func activateAgg(t *testing.T) *mysqlobda.Provider {
	t.Helper()
	raw := testdata(t, "patient_agg.obda.yaml")
	p, db := openProvider(t, raw)
	schema := spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Patient", Properties: []spi.PropertyDefinition{
				{Name: "name", Type: "String"},
				{Name: "city", Type: "String"},
				{Name: "score", Type: "Integer"},
			}},
		},
	}
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustCreate(t *testing.T, p *mysqlobda.Provider, ctx spi.RequestContext, props map[string]any) {
	t.Helper()
	if _, err := p.CreateObject(ctx, "Patient", props); err != nil {
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
