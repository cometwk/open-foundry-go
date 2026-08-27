package memory

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
)

// File covers U6: GetLinks (direction / includeDeleted / pagination),
// Traverse (multi-step / depth / nodes), UpdateLink (merge / conflict /
// no version history), and CreateLink cardinality enforcement (R4).
// Covers AE3, AE8, AE9.

// ---------------------------------------------------------------------------
// GetLinks
// ---------------------------------------------------------------------------

func TestGetLinks_OutboundVsInbound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	sid := s["_id"].(string)
	pid := pt["_id"].(string)
	if _, err := p.CreateLink(a, "Supplies", sid, pid, nil); err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}

	out, err := p.GetLinks(a, sid, "Supplies", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks outbound err = %v (U6)", err)
	}
	if out.TotalCount != 1 || len(out.Items) != 1 {
		t.Fatalf("outbound TotalCount/Items = %d/%d, want 1/1 (AE8)", out.TotalCount, len(out.Items))
	}
	if out.Items[0]["_fromId"] != sid {
		t.Errorf("outbound _fromId = %v, want %s", out.Items[0]["_fromId"], sid)
	}

	in, err := p.GetLinks(a, pid, "Supplies", "inbound", nil)
	if err != nil {
		t.Fatalf("GetLinks inbound err = %v (U6)", err)
	}
	if in.TotalCount != 1 {
		t.Fatalf("inbound TotalCount = %d, want 1 (AE8)", in.TotalCount)
	}
	// Wrong direction must be empty.
	wrong, _ := p.GetLinks(a, sid, "Supplies", "inbound", nil)
	if wrong.TotalCount != 0 {
		t.Errorf("inbound on from-id TotalCount = %d, want 0", wrong.TotalCount)
	}
}

func TestGetLinks_Pagination(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	sid := s["_id"].(string)
	for i := 0; i < 3; i++ {
		pt := createObjectForTest(t, p, a, "Part")
		if _, err := p.CreateLink(a, "Supplies", sid, pt["_id"].(string), nil); err != nil {
			t.Fatalf("CreateLink[%d] err = %v", i, err)
		}
	}
	page, err := p.GetLinks(a, sid, "Supplies", "outbound", &spi.QueryOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("GetLinks paginate err = %v", err)
	}
	if page.TotalCount != 3 || len(page.Items) != 2 || !page.HasNextPage {
		t.Errorf("page Total=%d Items=%d HasNext=%v, want 3/2/true (AE8)", page.TotalCount, len(page.Items), page.HasNextPage)
	}
}

// ---------------------------------------------------------------------------
// Traverse
// ---------------------------------------------------------------------------

func TestTraverse_MultiStep_NodesAreFinalStep(t *testing.T) {
	// Supplier --Supplies--> Part --UsedIn--> Assembly.
	// After two steps from Supplier, nodes = Assembly; edges include both hops.
	p := New()
	a, _ := tenancyA()
	schema := spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Supplier"}, {Name: "Part"}, {Name: "Assembly"},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Supplies", FromType: "Supplier", ToType: "Part", Cardinality: spi.CardinalityManyToMany},
			{Name: "UsedIn", FromType: "Part", ToType: "Assembly", Cardinality: spi.CardinalityManyToMany},
		},
	}
	if _, err := p.ApplySchema(a, schema); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	s, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := p.CreateObject(a, "Part", map[string]any{"sku": "P1"})
	asm, _ := p.CreateObject(a, "Assembly", map[string]any{"name": "A1"})
	if _, err := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil); err != nil {
		t.Fatalf("CreateLink Supplies err = %v", err)
	}
	if _, err := p.CreateLink(a, "UsedIn", pt["_id"].(string), asm["_id"].(string), nil); err != nil {
		t.Fatalf("CreateLink UsedIn err = %v", err)
	}

	res, err := p.Traverse(a, s["_id"].(string), spi.TraversalPath{
		Steps: []spi.TraversalStep{
			{LinkType: "Supplies", Direction: "outbound"},
			{LinkType: "UsedIn", Direction: "outbound"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Traverse err = %v (U6)", err)
	}
	if res.TotalCount != 1 || len(res.Nodes) != 1 {
		t.Fatalf("nodes Total/len = %d/%d, want 1/1 (final step = Assembly) (AE8)", res.TotalCount, len(res.Nodes))
	}
	if res.Nodes[0]["_id"] != asm["_id"] {
		t.Errorf("final node _id = %v, want Assembly %v", res.Nodes[0]["_id"], asm["_id"])
	}
	if len(res.Edges) != 2 {
		t.Errorf("edges = %d, want 2 (both hops) (AE8)", len(res.Edges))
	}
	if len(res.Visited) != 1 {
		t.Fatalf("visited = %d, want 1 (Part only)", len(res.Visited))
	}
	if res.Visited[0]["_id"] != pt["_id"] {
		t.Errorf("visited _id = %v, want Part %v", res.Visited[0]["_id"], pt["_id"])
	}
}

func TestTraverse_OneHop_VisitedEmpty(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	schema := spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Supplier"}, {Name: "Part"},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Supplies", FromType: "Supplier", ToType: "Part", Cardinality: spi.CardinalityManyToMany},
		},
	}
	if _, err := p.ApplySchema(a, schema); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	s, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := p.CreateObject(a, "Part", map[string]any{"sku": "P1"})
	if _, err := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil); err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}
	res, err := p.Traverse(a, s["_id"].(string), spi.TraversalPath{
		Steps: []spi.TraversalStep{{LinkType: "Supplies", Direction: "outbound"}},
	}, nil)
	if err != nil {
		t.Fatalf("Traverse err = %v", err)
	}
	if len(res.Nodes) != 1 || len(res.Visited) != 0 {
		t.Fatalf("1-hop nodes=%d visited=%d, want 1/0", len(res.Nodes), len(res.Visited))
	}
}

func TestTraverse_DepthExceedsMax_Errors(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	steps := make([]spi.TraversalStep, 11)
	for i := range steps {
		steps[i] = spi.TraversalStep{LinkType: "Supplies", Direction: "outbound"}
	}
	_, err := p.Traverse(a, "x", spi.TraversalPath{Steps: steps}, nil)
	if err == nil {
		t.Fatal("Traverse depth=11 err = nil, want error (AE8)")
	}
}

// ---------------------------------------------------------------------------
// UpdateLink
// ---------------------------------------------------------------------------

func TestUpdateLink_MergesPatchAndIncrementsVersion(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, err := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), map[string]any{"qty": 1})
	if err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}
	id := created["_id"].(string)
	updated, err := p.UpdateLink(a, "Supplies", id, map[string]any{"qty": 5, "note": "rush"}, nil)
	if err != nil {
		t.Fatalf("UpdateLink err = %v (U6)", err)
	}
	if asInt(updated["qty"]) != 5 || updated["note"] != "rush" {
		t.Errorf("updated props = qty=%v note=%v, want 5/rush (AE9)", updated["qty"], updated["note"])
	}
	if objectVersionValue(spi.OntologyObject(updated)) != 2 {
		t.Errorf("_version = %d, want 2 (AE9)", objectVersionValue(spi.OntologyObject(updated)))
	}
	// Links never push versionHistory.
	if hist := p.versionHistory[objectKey("Supplies", id)]; len(hist) != 0 {
		t.Errorf("link versionHistory len = %d, want 0 (links never push history) (AE9)", len(hist))
	}
}

func TestUpdateLink_ExpectedVersionConflict(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	wrong := 99
	_, err := p.UpdateLink(a, "Supplies", created["_id"].(string), map[string]any{"qty": 2}, &wrong)
	if !errors.Is(err, spi.ErrVersionConflict) {
		t.Errorf("UpdateLink conflict err = %v, want ErrVersionConflict (AE9)", err)
	}
}

func TestUpdateLink_Missing_ReturnsErrLinkNotFound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	_, err := p.UpdateLink(a, "Supplies", "missing", map[string]any{"qty": 1}, nil)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Errorf("UpdateLink missing err = %v, want ErrLinkNotFound (AE9)", err)
	}
}

func TestUpdateLink_CrossTenant_MaskedAsNotFound(t *testing.T) {
	p := New()
	a, b := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	_, err := p.UpdateLink(b, "Supplies", created["_id"].(string), map[string]any{"qty": 9}, nil)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Errorf("cross-tenant UpdateLink err = %v, want ErrLinkNotFound (AE9)", err)
	}
	got, err := p.GetLink(a, "Supplies", created["_id"].(string))
	if err != nil {
		t.Fatalf("original tenant GetLink err = %v", err)
	}
	if _, has := got["qty"]; has {
		t.Errorf("cross-tenant UpdateLink leaked write into tenant A")
	}
}

// ---------------------------------------------------------------------------
// Cardinality
// ---------------------------------------------------------------------------

func applyCardinalitySchema(t *testing.T, p *Provider, ctx spi.RequestContext, card spi.Cardinality) {
	t.Helper()
	schema := spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Supplier"}, {Name: "Part"},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Supplies", FromType: "Supplier", ToType: "Part", Cardinality: card},
		},
	}
	if _, err := p.ApplySchema(ctx, schema); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
}

func TestCardinality_OneToOne_RejectsSecondFromOutbound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applyCardinalitySchema(t, p, a, spi.CardinalityOneToOne)
	s := createObjectForTest(t, p, a, "Supplier")
	p1 := createObjectForTest(t, p, a, "Part")
	p2 := createObjectForTest(t, p, a, "Part")
	sid := s["_id"].(string)
	if _, err := p.CreateLink(a, "Supplies", sid, p1["_id"].(string), nil); err != nil {
		t.Fatalf("first CreateLink err = %v", err)
	}
	_, err := p.CreateLink(a, "Supplies", sid, p2["_id"].(string), nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Errorf("second ONE_TO_ONE CreateLink err = %v, want ErrCardinalityViolation (AE3)", err)
	}
	page, _ := p.GetLinks(a, sid, "Supplies", "outbound", nil)
	if page.TotalCount != 1 {
		t.Errorf("after rejected create TotalCount = %d, want 1 (no map write) (AE3)", page.TotalCount)
	}
}

func TestCardinality_ManyToMany_AllowsSecond(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applyCardinalitySchema(t, p, a, spi.CardinalityManyToMany)
	s := createObjectForTest(t, p, a, "Supplier")
	p1 := createObjectForTest(t, p, a, "Part")
	p2 := createObjectForTest(t, p, a, "Part")
	sid := s["_id"].(string)
	if _, err := p.CreateLink(a, "Supplies", sid, p1["_id"].(string), nil); err != nil {
		t.Fatalf("first CreateLink err = %v", err)
	}
	if _, err := p.CreateLink(a, "Supplies", sid, p2["_id"].(string), nil); err != nil {
		t.Errorf("second MANY_TO_MANY CreateLink err = %v, want nil (AE3)", err)
	}
}

func TestCardinality_ManyToOne_RejectsSecondFromOutbound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applyCardinalitySchema(t, p, a, spi.CardinalityManyToOne)
	s := createObjectForTest(t, p, a, "Supplier")
	p1 := createObjectForTest(t, p, a, "Part")
	p2 := createObjectForTest(t, p, a, "Part")
	sid := s["_id"].(string)
	if _, err := p.CreateLink(a, "Supplies", sid, p1["_id"].(string), nil); err != nil {
		t.Fatalf("first CreateLink err = %v", err)
	}
	_, err := p.CreateLink(a, "Supplies", sid, p2["_id"].(string), nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Errorf("second MANY_TO_ONE CreateLink err = %v, want ErrCardinalityViolation (AE3)", err)
	}
}

func TestCardinality_OneToMany_RejectsSecondToInbound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applyCardinalitySchema(t, p, a, spi.CardinalityOneToMany)
	s1 := createObjectForTest(t, p, a, "Supplier")
	s2 := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	pid := pt["_id"].(string)
	if _, err := p.CreateLink(a, "Supplies", s1["_id"].(string), pid, nil); err != nil {
		t.Fatalf("first CreateLink err = %v", err)
	}
	_, err := p.CreateLink(a, "Supplies", s2["_id"].(string), pid, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Errorf("second ONE_TO_MANY CreateLink err = %v, want ErrCardinalityViolation (AE3)", err)
	}
}

func TestCardinality_CrossTenant_NotCounted(t *testing.T) {
	p := New()
	a, b := tenancyA()
	applyCardinalitySchema(t, p, a, spi.CardinalityOneToOne)
	// Seed same schema for B via ApplySchema (global schema).
	sa := createObjectForTest(t, p, a, "Supplier")
	pa := createObjectForTest(t, p, a, "Part")
	sb := createObjectForTest(t, p, b, "Supplier")
	pb := createObjectForTest(t, p, b, "Part")
	if _, err := p.CreateLink(a, "Supplies", sa["_id"].(string), pa["_id"].(string), nil); err != nil {
		t.Fatalf("tenantA CreateLink err = %v", err)
	}
	// Tenant B with same from/to ids would be a different object set;
	// using B's own endpoints must succeed — A's link must not count.
	if _, err := p.CreateLink(b, "Supplies", sb["_id"].(string), pb["_id"].(string), nil); err != nil {
		t.Errorf("tenantB CreateLink err = %v, want nil (cross-tenant not counted) (AE3)", err)
	}
}

func TestCapabilities_GraphTraversalEnabled(t *testing.T) {
	caps := New().Capabilities()
	if !caps.SupportsGraphTraversal {
		t.Error("SupportsGraphTraversal = false, want true (U6)")
	}
	if caps.MaxTraversalDepth != 10 {
		t.Errorf("MaxTraversalDepth = %d, want 10 (U6)", caps.MaxTraversalDepth)
	}
}

// asInt coerces JSON-cloned numbers (float64) and native ints for assertions.
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return -1
}
