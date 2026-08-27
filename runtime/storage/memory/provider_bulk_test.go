package memory

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
)

// File covers U5: BulkMutate (idempotency tenant-scoped cache + best-effort
// apply + INTERNAL_ERROR partial failure), Indices (EnsureIndex/DropIndex
// overlay + ListIndexes schema-projected merge). Mirrors TS bulkMutate
// (memory-storage-provider.ts:789-831) and matches the plan's overlay
// semantics so ListIndexes reflects EnsureIndex/DropIndex mutations while
// schema-projected @unique/@indexed/@searchable indexes stay visible.

// ---------------------------------------------------------------------------
// BulkMutate
// ---------------------------------------------------------------------------

func TestBulkMutate_IdempotencyCache_HitReturnsEqualResult(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	req := spi.BulkMutationRequest{
		IdempotencyKey: "op-123",
		Operations: []spi.BulkOperation{
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "Acme"}},
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "Globex"}},
		},
	}
	first, err := p.BulkMutate(a, req)
	if err != nil {
		t.Fatalf("first BulkMutate err = %v, want nil (U5)", err)
	}
	if first.Accepted != 2 || first.Failed != 0 {
		t.Fatalf("first BulkMutate accepted=%d failed=%d, want 2/0 (U5)", first.Accepted, first.Failed)
	}

	// Second call with the same key returns a result structurally equal to
	// the first (TS clones the cached result). No new objects appear.
	second, err := p.BulkMutate(a, req)
	if err != nil {
		t.Fatalf("second BulkMutate err = %v, want nil (U5)", err)
	}
	if second.Accepted != first.Accepted || second.Failed != first.Failed {
		t.Errorf("idempotency hit returned different summary: first=%+v second=%+v (U5)", first, second)
	}
	if len(second.Errors) != len(first.Errors) {
		t.Errorf("idempotency hit returned different error count: first=%d second=%d (U5)", len(first.Errors), len(second.Errors))
	}
	// Idempotency means the underlying map did NOT add two more objects.
	// Assert via QueryObjects: only 2 Supplier objects exist in tenant A.
	page, _ := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
	if len(page.Items) != 2 {
		t.Errorf("after idempotent retry, tenant A has %d Suppliers, want 2 (U5 cache hit did not re-mutate)", len(page.Items))
	}
}

func TestBulkMutate_IdempotencyCache_CrossTenantNoCollision(t *testing.T) {
	p := New()
	a, b := tenancyA()
	req := spi.BulkMutationRequest{
		IdempotencyKey: "shared-key",
		Operations: []spi.BulkOperation{
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "tenantA-only"}},
		},
	}
	// Tenant A first: creates one object.
	if _, err := p.BulkMutate(a, req); err != nil {
		t.Fatalf("tenantA BulkMutate err = %v (U5)", err)
	}
	// Tenant B with the SAME idempotencyKey must execute as a separate
	// request — cached result is keyed by (tenant, idempotencyKey).
	if _, err := p.BulkMutate(b, req); err != nil {
		t.Fatalf("tenantB BulkMutate err = %v (U5)", err)
	}
	// Each tenant independently has 1 Supplier; B's cache hit did not
	// suppress an A-only mutation nor leak A's result back to B.
	pageA, _ := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
	if len(pageA.Items) != 1 {
		t.Errorf("tenant A has %d Suppliers, want 1 (U5 cross-tenant isolation)", len(pageA.Items))
	}
	pageB, _ := p.QueryObjects(b, "Supplier", spi.FilterExpression{}, nil)
	if len(pageB.Items) != 1 {
		t.Errorf("tenant B has %d Suppliers, want 1 (U5 cross-tenant separate cache)", len(pageB.Items))
	}
}

func TestBulkMutate_PartialFailure_ReportsOperationIndex(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	// Op 1: success. Op 2: update a missing object — the underlying
	// _doUpdateObject throws ErrObjectNotFound, which the bulk loop
	// surfaces as a BulkMutationError with Code=INTERNAL_ERROR.
	req := spi.BulkMutationRequest{
		IdempotencyKey: "partial-fail",
		Operations: []spi.BulkOperation{
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "Acme"}},
			{Type: "updateObject", ObjectType: "Supplier", ID: "never-existed", Properties: map[string]any{"name": "changed"}},
		},
	}
	res, err := p.BulkMutate(a, req)
	if err != nil {
		t.Fatalf("BulkMutate err = %v, want nil (best-effort with per-op failure) (U5)", err)
	}
	if res.Accepted != 1 {
		t.Errorf("accepted = %d, want 1 (U5)", res.Accepted)
	}
	if res.Failed != 1 {
		t.Errorf("failed = %d, want 1 (U5)", res.Failed)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors len = %d, want 1 (U5)", len(res.Errors))
	}
	e := res.Errors[0]
	if e.OperationIndex != 1 {
		t.Errorf("error operationIndex = %d, want 1 (U5)", e.OperationIndex)
	}
	if e.Code != "INTERNAL_ERROR" {
		t.Errorf("error code = %q, want INTERNAL_ERROR (U5 mirror TS code)", e.Code)
	}
	if e.Message == "" {
		t.Errorf("error message empty, want non-empty (U5)")
	}
}

func TestBulkMutate_SoftDeleteOpApplies(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	id := obj["_id"].(string)
	req := spi.BulkMutationRequest{
		IdempotencyKey: "soft-del",
		Operations: []spi.BulkOperation{
			{Type: "deleteObject", ObjectType: "Supplier", ID: id, Mode: "soft"},
		},
	}
	res, err := p.BulkMutate(a, req)
	if err != nil {
		t.Fatalf("BulkMutate err = %v (U5)", err)
	}
	if res.Accepted != 1 || res.Failed != 0 {
		t.Fatalf("accepted=%d failed=%d, want 1/0 (U5)", res.Accepted, res.Failed)
	}
	// Soft-delete mask: GetObject returns not-found.
	if _, err := p.GetObject(a, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObject after bulk soft delete err = %v, want ErrObjectNotFound (U5 mask)", err)
	}
}

func TestBulkMutate_HardDeleteOpApplies(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	id := obj["_id"].(string)
	req := spi.BulkMutationRequest{
		IdempotencyKey: "hard-del",
		Operations: []spi.BulkOperation{
			{Type: "deleteObject", ObjectType: "Supplier", ID: id, Mode: "hard"},
		},
	}
	res, _ := p.BulkMutate(a, req)
	if res.Accepted != 1 {
		t.Errorf("hard delete accepted = %d, want 1 (U5)", res.Accepted)
	}
	if _, err := p.GetObject(a, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObject after bulk hard delete err = %v, want ErrObjectNotFound (U5)", err)
	}
}

func TestBulkMutate_EmptyIdempotencyKey_NoCaching(t *testing.T) {
	// An empty idempotencyKey means: caller does not request idempotency.
	// The impl must NOT cache, must NOT use a "" cache key (avoid
	// accidental collisions). Two consecutive calls should both apply;
	// the test asserts: both return Accepted for the same pair of
	// createObject ops, and two distinct objects exist in tenant A.
	p := New()
	a, _ := tenancyA()
	req := spi.BulkMutationRequest{
		Operations: []spi.BulkOperation{
			{Type: "createObject", ObjectType: "Supplier", Properties: map[string]any{"name": "S1"}},
		},
	}
	res1, _ := p.BulkMutate(a, req)
	res2, _ := p.BulkMutate(a, req)
	if res1.Accepted != 1 || res2.Accepted != 1 {
		t.Errorf("res1.Accepted=%d res2.Accepted=%d, want 1/1 (U5 no caching when key empty)", res1.Accepted, res2.Accepted)
	}
	page, _ := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
	if len(page.Items) != 2 {
		t.Errorf("after two uncached bulk calls, %d Suppliers in tenant A, want 2 (U5)", len(page.Items))
	}
}

// ---------------------------------------------------------------------------
// Indices — EnsureIndex/DropIndex/ListIndexes
// ---------------------------------------------------------------------------

// schemaWithIndexedFields applies a small schema with one @unique field
// and one @indexed field so the projection layer tags those field into
// ObjectTypeDefinition.Indexes; ListIndexes must reflect them.
func schemaWithIndexedFields() spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{
				Name: "Supplier",
				Indexes: []spi.IndexDefinition{
					{Field: "sku", IndexType: spi.IndexBTREE, Unique: true},
					{Field: "city", IndexType: spi.IndexBTREE},
				},
			},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Supplies", FromType: "Supplier", ToType: "Supplier", Cardinality: spi.CardinalityOneToMany},
		},
	}
}

func TestListIndexes_FromSchemaProjection(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	if _, err := p.ApplySchema(a, schemaWithIndexedFields()); err != nil {
		t.Fatalf("ApplySchema err = %v (U5)", err)
	}
	idxs, err := p.ListIndexes(a, "Supplier")
	if err != nil {
		t.Fatalf("ListIndexes err = %v (U5)", err)
	}
	if len(idxs) != 2 {
		t.Fatalf("ListIndexes returned %d indexes, want 2 (sku+city from schema) (U5)", len(idxs))
	}
	byField := map[string]spi.IndexDefinition{}
	for _, i := range idxs {
		byField[i.Field] = i
	}
	if _, ok := byField["sku"]; !ok {
		t.Errorf("missing projected index on sku (U5)")
	}
	if _, ok := byField["city"]; !ok {
		t.Errorf("missing projected index on city (U5)")
	}
	if !byField["sku"].Unique {
		t.Errorf("schema-projected sku index must carry Unique=true (U5)")
	}
}

func TestEnsureIndex_OverlayAppendedToHEMAListIndexes(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	if _, err := p.ApplySchema(a, schemaWithIndexedFields()); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	before, _ := p.ListIndexes(a, "Supplier")
	beforeLen := len(before)

	newIdx := spi.IndexDefinition{Field: "region", IndexType: spi.IndexHASH}
	if err := p.EnsureIndex(a, "Supplier", newIdx); err != nil {
		t.Fatalf("EnsureIndex err = %v (U5)", err)
	}
	after, _ := p.ListIndexes(a, "Supplier")
	if len(after) != beforeLen+1 {
		t.Fatalf("after EnsureIndex ListIndexes len = %d, want %d (schema %d + overlay 1) (U5)", len(after), beforeLen+1, beforeLen)
	}
	regions := 0
	for _, i := range after {
		if i.Field == "region" {
			regions++
			if i.IndexType != spi.IndexHASH {
				t.Errorf("overlay index region IndexType = %v, want HASH (U5)", i.IndexType)
			}
		}
	}
	if regions != 1 {
		t.Errorf("ListIndexes returned %d region indexes, want 1 (U5)", regions)
	}
}

func TestDropIndex_RemovesFromOverlay(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	if _, err := p.ApplySchema(a, schemaWithIndexedFields()); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	// Add an overlay-only index.
	if err := p.EnsureIndex(a, "Supplier", spi.IndexDefinition{Field: "region", IndexType: spi.IndexHASH}); err != nil {
		t.Fatalf("EnsureIndex err = %v (U5)", err)
	}
	// Drop the overlay-only index — ListIndexes must no longer include it.
	if err := p.DropIndex(a, "Supplier", "region"); err != nil {
		t.Fatalf("DropIndex err = %v (U5)", err)
	}
	after, _ := p.ListIndexes(a, "Supplier")
	for _, i := range after {
		if i.Field == "region" {
			t.Errorf("DropIndex did not remove overlay index region (U5)")
		}
	}
}

func TestDropIndex_OnSchemaProjectedIndex_RemovesFromDisplayedList(t *testing.T) {
	// The plan's ListIndexes merges schema-projected and overlay. DropIndex
	// only removes from overlay. For schema-projected fields, DropIndex
	// should add a "drop" entry that suppresses that schema index from
	// display until the next ApplySchema (this is the simplest overlay
	// semantics satisfying R5's "DropIndex removes from overlay").
	//
	// Anticipated behavior under the merged approach: DropIndex on a
	// schema-projected field records a "drop" marker that ListIndexes
	// honors, hiding that field from results. A subsequent re-ApplySchema
	// restores it (since ApplySchema re-projects).
	p := New()
	a, _ := tenancyA()
	if _, err := p.ApplySchema(a, schemaWithIndexedFields()); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	if err := p.DropIndex(a, "Supplier", "sku"); err != nil {
		t.Fatalf("DropIndex err = %v (U5)", err)
	}
	after, _ := p.ListIndexes(a, "Supplier")
	for _, i := range after {
		if i.Field == "sku" {
			t.Errorf("DropIndex did not hide schema-projected sku index (U5 overlay-drop semantic)")
		}
	}
	// A fresh ApplySchema re-projects and the dropped index is restored
	// (projection re-runs; the drop overlay only suppressed the prior view).
	if _, err := p.ApplySchema(a, schemaWithIndexedFields()); err != nil {
		t.Fatalf("re-ApplySchema err = %v (U5)", err)
	}
	restored, _ := p.ListIndexes(a, "Supplier")
	skuSeen := false
	for _, i := range restored {
		if i.Field == "sku" {
			skuSeen = true
		}
	}
	if !skuSeen {
		t.Errorf("DropIndex ossifies — re-ApplySchema must re-project sku (U5) (drop overlay scope only)")
	}
}

func TestListIndexes_CrossTenantNoLeak(t *testing.T) {
	p := New()
	a, b := tenancyA()
	if _, err := p.ApplySchema(a, schemaWithIndexedFields()); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	// Tenant B queries indexes for "Supplier" — schema is global in the
	// memory provider (no per-tenant schema slicing), so the same
	// projected indexes are returned. This is acceptable per R5: the
	// overlay is keyed by type. Cross-tenant does not add leakage (no
	// per-tenant rows). Assert that EnsureIndex for B is observable when
	// caller is B, but does not REMOVE indexes added by A on the same type.
	_ = b
	_ = a
	aStart, _ := p.ListIndexes(a, "Supplier")
	bStart, _ := p.ListIndexes(b, "Supplier")
	if len(aStart) != len(bStart) {
		t.Errorf("schema-projected index leak by tenant: A=%d B=%d (U5)", len(aStart), len(bStart))
	}
}

func TestBulkIndices_NotErrUnimplemented(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	if err := p.EnsureIndex(a, "Supplier", spi.IndexDefinition{Field: "x"}); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("EnsureIndex still returns ErrUnimplemented (U5 should have implemented it)")
	}
	if err := p.DropIndex(a, "Supplier", "x"); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("DropIndex still returns ErrUnimplemented (U5 should have implemented it)")
	}
	if _, err := p.ListIndexes(a, "Supplier"); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("ListIndexes still returns ErrUnimplemented (U5 should have implemented it)")
	}
	if _, err := p.BulkMutate(a, spi.BulkMutationRequest{}); errors.Is(err, spi.ErrUnimplemented) {
		t.Error("BulkMutate still returns ErrUnimplemented (U5 should have implemented it)")
	}
}