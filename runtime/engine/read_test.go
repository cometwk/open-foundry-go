package engine

import (
	"testing"

	"github.com/openfoundry/runtime/ir"
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

func TestEngine_Traverse_TwoHopVisitedAndCrossTenant(t *testing.T) {
	rec := memory.New()
	ont := traverseOntology(t)
	e, err := New(rec, ont)
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	a := tenantCtx("a")
	b := tenantCtx("b")
	if _, err := rec.ApplySchema(a, spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Supplier"}, {Name: "Part"}, {Name: "Assembly"},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Supplies", FromType: "Supplier", ToType: "Part", Cardinality: spi.CardinalityManyToMany},
			{Name: "UsedIn", FromType: "Part", ToType: "Assembly", Cardinality: spi.CardinalityManyToMany},
		},
	}); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	s, err := e.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("CreateObject Supplier err = %v", err)
	}
	pt, err := e.CreateObject(a, "Part", map[string]any{"sku": "P1"})
	if err != nil {
		t.Fatalf("CreateObject Part err = %v", err)
	}
	asm, err := e.CreateObject(a, "Assembly", map[string]any{"name": "A1"})
	if err != nil {
		t.Fatalf("CreateObject Assembly err = %v", err)
	}
	if _, err := e.CreateLink(a, "Supplies", s[spi.FieldID].(string), pt[spi.FieldID].(string), nil); err != nil {
		t.Fatalf("CreateLink Supplies err = %v", err)
	}
	if _, err := e.CreateLink(a, "UsedIn", pt[spi.FieldID].(string), asm[spi.FieldID].(string), nil); err != nil {
		t.Fatalf("CreateLink UsedIn err = %v", err)
	}
	if _, err := e.CreateObject(b, "Part", map[string]any{"sku": "OTHER"}); err != nil {
		t.Fatalf("CreateObject other-tenant Part err = %v", err)
	}

	direct, err := rec.Traverse(a, s[spi.FieldID].(string), spi.TraversalPath{
		Steps: []spi.TraversalStep{
			{LinkType: "Supplies", Direction: "outbound"},
			{LinkType: "UsedIn", Direction: "outbound"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("storage Traverse err = %v", err)
	}
	got, err := e.Traverse(a, s[spi.FieldID].(string), spi.TraversalPath{
		Steps: []spi.TraversalStep{
			{LinkType: "Supplies", Direction: "outbound"},
			{LinkType: "UsedIn", Direction: "outbound"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Engine Traverse err = %v", err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0][spi.FieldID] != asm[spi.FieldID] {
		t.Fatalf("nodes = %+v, want Assembly %v", got.Nodes, asm[spi.FieldID])
	}
	if len(got.Visited) != 1 || got.Visited[0][spi.FieldID] != pt[spi.FieldID] {
		t.Fatalf("visited = %+v, want Part %v", got.Visited, pt[spi.FieldID])
	}
	if len(got.Nodes) != len(direct.Nodes) || len(got.Visited) != len(direct.Visited) || len(got.Edges) != len(direct.Edges) {
		t.Fatalf("engine result nodes/visited/edges = %d/%d/%d, storage = %d/%d/%d",
			len(got.Nodes), len(got.Visited), len(got.Edges),
			len(direct.Nodes), len(direct.Visited), len(direct.Edges))
	}
	for _, n := range got.Nodes {
		if n[spi.FieldTenantID] != "a" {
			t.Fatalf("leaked tenant %v in nodes", n[spi.FieldTenantID])
		}
	}
	for _, n := range got.Visited {
		if n[spi.FieldTenantID] != "a" {
			t.Fatalf("leaked tenant %v in visited", n[spi.FieldTenantID])
		}
	}
}

func traverseOntology(t *testing.T) *ir.Ontology {
	t.Helper()
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test"},
		Objects: []ir.ObjectType{
			{Name: "Supplier", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
			}},
			{Name: "Part", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "sku", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
			}},
			{Name: "Assembly", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
			}},
		},
		Links: []ir.LinkType{
			{Name: "Supplies", From: "Supplier", To: "Part", Cardinality: ir.CardinalityManyToMany},
			{Name: "UsedIn", From: "Part", To: "Assembly", Cardinality: ir.CardinalityManyToMany},
		},
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
