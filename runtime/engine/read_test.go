package engine

import (
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestEngine_QueryObjects_SameTenantHits_CrossTenantMiss(t *testing.T) {
	e := newEngine(t)
	a := tenantCtx("a")
	b := tenantCtx("b")
	if _, err := e.CreateObject(a, "Supplier", map[string]any{"name": "Acme"}); err != nil {
		t.Fatalf("CreateObject tenant a err = %v", err)
	}
	if _, err := e.CreateObject(b, "Supplier", map[string]any{"name": "Beta"}); err != nil {
		t.Fatalf("CreateObject tenant b err = %v", err)
	}

	page, err := e.QueryObjects(a, "Supplier", spi.FilterExpression{
		Field: "name", Operator: "eq", Value: "Acme",
	}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryObjects tenant a err = %v", err)
	}
	if page.TotalCount != 1 {
		t.Fatalf("QueryObjects tenant a TotalCount = %d, want 1", page.TotalCount)
	}

	miss, err := e.QueryObjects(a, "Supplier", spi.FilterExpression{
		Field: "name", Operator: "eq", Value: "Beta",
	}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryObjects cross-tenant filter err = %v", err)
	}
	if miss.TotalCount != 0 {
		t.Fatalf("QueryObjects saw other tenant TotalCount = %d, want 0", miss.TotalCount)
	}
}

func TestEngine_AggregateObjects_CountExcludesSoftDeleted(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	first, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "A"})
	if err != nil {
		t.Fatalf("CreateObject A err = %v", err)
	}
	if _, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "B"}); err != nil {
		t.Fatalf("CreateObject B err = %v", err)
	}
	if err := e.DeleteObject(ctx, "Supplier", first[spi.FieldID].(string), "soft"); err != nil {
		t.Fatalf("soft delete err = %v", err)
	}

	got, err := e.AggregateObjects(ctx, "Supplier", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "n"}},
	})
	if err != nil {
		t.Fatalf("AggregateObjects err = %v", err)
	}
	if len(got.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(got.Groups))
	}
	n, _ := toInt(got.Groups[0].Values["n"])
	if n != 1 {
		t.Fatalf("count = %v, want 1 (soft-deleted excluded)", got.Groups[0].Values["n"])
	}
}

func TestEngine_SearchObjects_TokenHitAndBlankQuery(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	if _, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme Widgets"}); err != nil {
		t.Fatalf("CreateObject err = %v", err)
	}

	hit, err := e.SearchObjects(ctx, "Supplier", spi.SearchQuery{Query: "Acme", Limit: 10})
	if err != nil {
		t.Fatalf("SearchObjects err = %v", err)
	}
	if hit.TotalCount < 1 {
		t.Fatalf("SearchObjects TotalCount = %d, want >= 1", hit.TotalCount)
	}

	empty, err := e.SearchObjects(ctx, "Supplier", spi.SearchQuery{Query: "   ", Limit: 10})
	if err != nil {
		t.Fatalf("SearchObjects blank err = %v", err)
	}
	if empty.TotalCount != 0 {
		t.Fatalf("blank query TotalCount = %d, want 0", empty.TotalCount)
	}
}

func TestEngine_GetLinks_InboundOutbound(t *testing.T) {
	rec := memory.New()
	e, err := New(rec, linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	ctx := tenantCtx("tnt")
	s, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("CreateObject Supplier err = %v", err)
	}
	p, err := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	if err != nil {
		t.Fatalf("CreateObject Part err = %v", err)
	}
	if _, err := e.CreateLink(ctx, "Supplies", s[spi.FieldID].(string), p[spi.FieldID].(string), nil); err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}

	out, err := e.GetLinks(ctx, s[spi.FieldID].(string), "Supplies", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks outbound err = %v", err)
	}
	if out.TotalCount != 1 {
		t.Fatalf("outbound TotalCount = %d, want 1", out.TotalCount)
	}
	in, err := e.GetLinks(ctx, p[spi.FieldID].(string), "Supplies", "inbound", nil)
	if err != nil {
		t.Fatalf("GetLinks inbound err = %v", err)
	}
	if in.TotalCount != 1 {
		t.Fatalf("inbound TotalCount = %d, want 1", in.TotalCount)
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
