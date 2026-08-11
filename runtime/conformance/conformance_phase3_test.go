package conformance_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// setupProvider returns Engine + memory.Provider with the fixture schema
// applied so SPI methods that read LinkTypeDefinition / Indexes (CreateLink
// fromType, cardinality, ListIndexes, Traverse target typing) see a real
// projected schema. Covers Phase 3 U8 positive conformance (AE11).
func setupProvider(t *testing.T) (*engine.Engine, *memory.Provider, spi.RequestContext) {
	t.Helper()
	p := memory.New()
	o := fixtureOntology()
	ctx := spi.RequestContext{TenantID: "conformance-tenant", ActorID: "test"}
	if _, err := p.ApplySchema(ctx, projection.ProjectStorage(o)); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	return e, p, ctx
}

func TestConformance_QueryObjects(t *testing.T) {
	_, p, ctx := setupProvider(t)
	if _, err := p.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme", "tier": "Gold"}); err != nil {
		t.Fatalf("CreateObject err = %v", err)
	}
	if _, err := p.CreateObject(ctx, "Supplier", map[string]any{"name": "Beta", "tier": "Silver"}); err != nil {
		t.Fatalf("CreateObject err = %v", err)
	}
	page, err := p.QueryObjects(ctx, "Supplier", spi.FilterExpression{
		Field: "tier", Operator: "eq", Value: "Gold",
	}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryObjects err = %v (AE4)", err)
	}
	if page.TotalCount != 1 || len(page.Items) != 1 {
		t.Fatalf("QueryObjects Total/Items = %d/%d, want 1/1 (AE4)", page.TotalCount, len(page.Items))
	}
	if page.Items[0]["name"] != "Acme" {
		t.Errorf("QueryObjects name = %v, want Acme", page.Items[0]["name"])
	}
}

func TestConformance_AggregateObjects(t *testing.T) {
	_, p, ctx := setupProvider(t)
	for _, tier := range []string{"Gold", "Gold", "Silver"} {
		if _, err := p.CreateObject(ctx, "Supplier", map[string]any{"name": tier, "tier": tier}); err != nil {
			t.Fatalf("CreateObject err = %v", err)
		}
	}
	res, err := p.AggregateObjects(ctx, "Supplier", spi.AggregateQuery{
		GroupBy: []string{"tier"},
		Fields: []spi.AggregateField{
			{Alias: "n", Fn: "count", Field: "*"},
		},
	})
	if err != nil {
		t.Fatalf("AggregateObjects err = %v (AE5)", err)
	}
	if len(res.Groups) < 2 {
		t.Fatalf("AggregateObjects groups = %d, want >=2 (AE5)", len(res.Groups))
	}
}

func TestConformance_SearchObjects(t *testing.T) {
	_, p, ctx := setupProvider(t)
	if _, err := p.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme Corp"}); err != nil {
		t.Fatalf("CreateObject err = %v", err)
	}
	res, err := p.SearchObjects(ctx, "Supplier", spi.SearchQuery{Query: "acme", Limit: 10})
	if err != nil {
		t.Fatalf("SearchObjects err = %v (AE5)", err)
	}
	if res.TotalCount < 1 || len(res.Hits) < 1 {
		t.Fatalf("SearchObjects Total/Hits = %d/%d, want >=1 (AE5)", res.TotalCount, len(res.Hits))
	}
}

func TestConformance_BulkMutate(t *testing.T) {
	_, p, ctx := setupProvider(t)
	res, err := p.BulkMutate(ctx, spi.BulkMutationRequest{
		IdempotencyKey: "k1",
		Operations: []spi.BulkOperation{
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "Bulk"}},
		},
	})
	if err != nil {
		t.Fatalf("BulkMutate err = %v (AE6)", err)
	}
	if res.Accepted != 1 || res.Failed != 0 {
		t.Fatalf("BulkMutate accepted/failed = %d/%d, want 1/0 (AE6)", res.Accepted, res.Failed)
	}
	again, err := p.BulkMutate(ctx, spi.BulkMutationRequest{
		IdempotencyKey: "k1",
		Operations: []spi.BulkOperation{
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "Bulk"}},
		},
	})
	if err != nil {
		t.Fatalf("BulkMutate replay err = %v (AE6)", err)
	}
	if again.Accepted != res.Accepted {
		t.Errorf("idempotent replay Accepted = %d, want %d (AE6)", again.Accepted, res.Accepted)
	}
}

func TestConformance_GetLinks(t *testing.T) {
	e, p, ctx := setupProvider(t)
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	if _, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil); err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}
	page, err := p.GetLinks(ctx, s["_id"].(string), "Supplies", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks err = %v (AE8)", err)
	}
	if page.TotalCount != 1 {
		t.Fatalf("GetLinks TotalCount = %d, want 1 (AE8)", page.TotalCount)
	}
}

func TestConformance_Traverse(t *testing.T) {
	e, p, ctx := setupProvider(t)
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	if _, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil); err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}
	res, err := p.Traverse(ctx, s["_id"].(string), spi.TraversalPath{
		Steps: []spi.TraversalStep{{LinkType: "Supplies", Direction: "outbound"}},
	}, nil)
	if err != nil {
		t.Fatalf("Traverse err = %v (AE8)", err)
	}
	if res.TotalCount != 1 || len(res.Nodes) != 1 {
		t.Fatalf("Traverse nodes = %d/%d, want 1/1 (AE8)", res.TotalCount, len(res.Nodes))
	}
	if res.Nodes[0]["_id"] != pt["_id"] {
		t.Errorf("Traverse node = %v, want Part %v", res.Nodes[0]["_id"], pt["_id"])
	}
}

func TestConformance_UpdateLink(t *testing.T) {
	e, _, ctx := setupProvider(t)
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	link, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), map[string]any{"qty": 1})
	if err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}
	updated, err := e.UpdateLink(ctx, "Supplies", link["_id"].(string), map[string]any{"qty": 4}, nil)
	if err != nil {
		t.Fatalf("UpdateLink err = %v (AE9)", err)
	}
	switch v := updated["qty"].(type) {
	case int:
		if v != 4 {
			t.Errorf("qty = %d, want 4", v)
		}
	case float64:
		if int(v) != 4 {
			t.Errorf("qty = %v, want 4", v)
		}
	default:
		t.Errorf("qty type %T = %v, want 4", updated["qty"], updated["qty"])
	}
}

func TestConformance_Transaction_Commit_Rollback(t *testing.T) {
	_, p, ctx := setupProvider(t)
	tx, err := p.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("BeginTransaction err = %v (AE10)", err)
	}
	obj, err := tx.CreateObject("Supplier", map[string]any{"name": "Tx"})
	if err != nil {
		t.Fatalf("tx.CreateObject err = %v", err)
	}
	id := obj["_id"].(string)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v (AE10)", err)
	}
	if _, err := p.GetObject(ctx, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObject after rollback err = %v, want ErrObjectNotFound (AE10)", err)
	}

	tx2, _ := p.BeginTransaction(ctx)
	obj2, _ := tx2.CreateObject("Supplier", map[string]any{"name": "Keep"})
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit err = %v (AE10)", err)
	}
	if _, err := p.GetObject(ctx, "Supplier", obj2["_id"].(string)); err != nil {
		t.Errorf("GetObject after commit err = %v, want nil (AE10)", err)
	}
}

func TestConformance_SoftDelete(t *testing.T) {
	e, p, ctx := setupProvider(t)
	obj, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Gone"})
	id := obj["_id"].(string)
	if err := e.DeleteObject(ctx, "Supplier", id, "soft"); err != nil {
		t.Fatalf("DeleteObject soft err = %v (AE2)", err)
	}
	if _, err := e.GetObject(ctx, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObject after soft err = %v, want ErrObjectNotFound (AE2)", err)
	}
	incl, err := p.QueryObjects(ctx, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("QueryObjects includeDeleted err = %v", err)
	}
	found := false
	for _, item := range incl.Items {
		if item["_id"] == id {
			found = true
		}
	}
	if !found {
		t.Error("QueryObjects(includeDeleted:true) missing soft-deleted object (AE2)")
	}
	excl, _ := p.QueryObjects(ctx, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: false})
	for _, item := range excl.Items {
		if item["_id"] == id {
			t.Error("QueryObjects(includeDeleted:false) leaked soft-deleted object (AE2)")
		}
	}
}

func TestConformance_VersionConflict(t *testing.T) {
	e, _, ctx := setupProvider(t)
	obj, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	id := obj["_id"].(string)
	wrong := 0
	_, err := e.UpdateObject(ctx, "Supplier", id, map[string]any{"tier": "Gold"}, &wrong)
	if !errors.Is(err, spi.ErrVersionConflict) {
		t.Errorf("UpdateObject conflict err = %v, want ErrVersionConflict (AE1)", err)
	}
	match := 1
	updated, err := e.UpdateObject(ctx, "Supplier", id, map[string]any{"tier": "Gold"}, &match)
	if err != nil {
		t.Fatalf("UpdateObject matching version err = %v (AE1)", err)
	}
	if updated["tier"] != "Gold" {
		t.Errorf("tier = %v, want Gold", updated["tier"])
	}
}

func TestConformance_CardinalityViolation(t *testing.T) {
	// ONE_TO_ONE fixture so the second same-from outbound CreateLink rejects.
	o := &ir.Ontology{
		Namespace: &ir.Namespace{Name: "conformance"},
		Objects: []ir.ObjectType{
			{
				Name: "Supplier",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
			{
				Name: "Part",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "sku", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "Supplies", From: "Supplier", To: "Part", Cardinality: ir.CardinalityOneToOne},
		},
	}
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "conformance-tenant", ActorID: "test"}
	if _, err := p.ApplySchema(ctx, projection.ProjectStorage(o)); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	p1, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	p2, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P2"})
	if _, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), p1["_id"].(string), nil); err != nil {
		t.Fatalf("first CreateLink err = %v", err)
	}
	_, err = e.CreateLink(ctx, "Supplies", s["_id"].(string), p2["_id"].(string), nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Errorf("second ONE_TO_ONE CreateLink err = %v, want ErrCardinalityViolation (AE3)", err)
	}
}

func TestConformance_Capabilities_FinalState(t *testing.T) {
	caps := memory.New().Capabilities()
	if !caps.SupportsTransactions || !caps.SupportsTemporalQueries || !caps.SupportsFullTextSearch ||
		!caps.SupportsGraphTraversal || !caps.SupportsBulkMutations {
		t.Errorf("final Capabilities missing true flags: %+v (R9/AE11)", caps)
	}
	if caps.SupportsGeoQueries {
		t.Error("SupportsGeoQueries = true, want false (R9)")
	}
	if caps.MaxTraversalDepth != 10 {
		t.Errorf("MaxTraversalDepth = %d, want 10 (R9)", caps.MaxTraversalDepth)
	}
	if caps.ReplicationSupport != spi.ReplicationNone {
		t.Errorf("ReplicationSupport = %v, want ReplicationNone (R9)", caps.ReplicationSupport)
	}
}
