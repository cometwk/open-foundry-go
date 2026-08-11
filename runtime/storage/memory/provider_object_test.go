package memory

import (
	"errors"
	"testing"
	"time"

	"github.com/openfoundry/runtime/spi"
)

func tenantCtx(tenant string) spi.RequestContext {
	return spi.RequestContext{TenantID: tenant, ActorID: "test"}
}

func tenancyA() (spi.RequestContext, spi.RequestContext) {
	return tenantCtx("tenantA"), tenantCtx("tenantB")
}

func TestCreateObject_ThenGet_RoundTrips(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, err := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("CreateObject err: %v", err)
	}
	if obj["_id"] == nil || obj["_id"] == "" {
		t.Errorf("CreateObject omitted _id: %v", obj)
	}
	if obj["_type"] != "Supplier" {
		t.Errorf("CreateObject _type = %v, want Supplier", obj["_type"])
	}
	if obj["_tenantId"] != "tenantA" {
		t.Errorf("CreateObject _tenantId = %v, want tenantA", obj["_tenantId"])
	}
	if obj["_createdAt"] == nil {
		t.Errorf("CreateObject omitted _createdAt")
	}
	if obj["_updatedAt"] == nil {
		t.Errorf("CreateObject omitted _updatedAt")
	}
	// Phase 3 (U1): CreateObject stamps authoritative _version:1. The
// returned object is a JSON clone (cloneObject), so int survives as
// float64 — accept either, matching the qty float64(3) convention and
// engine.checkScalarType's float64-with-integer-magnitude handling.
	switch v := obj["_version"].(type) {
	case int:
		if v != 1 {
			t.Errorf("CreateObject _version = %d, want 1 (U1)", v)
		}
	case float64:
		if v != 1 {
			t.Errorf("CreateObject _version = %v, want 1 (U1)", v)
		}
	default:
		t.Errorf("CreateObject _version has unexpected type %T = %v, want 1 (U1)", obj["_version"], obj["_version"])
	}

	got, err := p.GetObject(a, "Supplier", obj["_id"].(string))
	if err != nil {
		t.Fatalf("GetObject err: %v", err)
	}
	if got["name"] != "Acme" {
		t.Errorf("GetObject name = %v, want Acme", got["name"])
	}
	if got["_id"] != obj["_id"] {
		t.Errorf("GetObject _id = %v, want %v", got["_id"], obj["_id"])
	}
}

func TestGetObject_Missing_ReturnsObjectNotFound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	_, err := p.GetObject(a, "Supplier", "nope")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject(missing) err = %v, want ErrObjectNotFound", err)
	}
}

func TestGetObject_CrossTenant_Hidden(t *testing.T) {
	p := New()
	a, b := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	_, err := p.GetObject(b, "Supplier", obj["_id"].(string))
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject cross-tenant err = %v, want ErrObjectNotFound (no leak)", err)
	}
}

func TestGetObject_AfterHardDelete_NotFound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	if err := p.DeleteObject(a, "Supplier", id, "hard"); err != nil {
		t.Fatalf("DeleteObject hard err: %v", err)
	}
	_, err := p.GetObject(a, "Supplier", id)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after hard delete err = %v, want ErrObjectNotFound", err)
	}
}

func TestGetObject_AfterCrossTenantHardDelete_StillVisibleToOriginal(t *testing.T) {
	p := New()
	a, b := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	if err := p.DeleteObject(b, "Supplier", id, "hard"); err != nil {
		t.Fatalf("DeleteObject cross-tenant hard err: %v", err)
	}
	if _, err := p.GetObject(a, "Supplier", id); err != nil {
		t.Fatalf("cross-tenant hard delete must not remove object from original tenant, err = %v", err)
	}
}

// TestDeleteObject_SoftMode_StampsDeletedAt is the Phase 3 flip of the
// Phase 2 "soft mode unimplemented" contract: soft delete now stamps
// _deletedAt, increments _version, and re-stamps _updatedAt without
// removing the object from the map (R3 write side, AE2 write side). The
// read-path mask (GetObject returns ErrObjectNotFound for soft-deleted)
// lands in U3 with its own dedicated test. Here, we assert the write
// invariants directly via a parallel direct-read of the stored object;
// for U2 the public GetObject still returns the soft-deleted object
// until U3 adds the mask.
func TestDeleteObject_SoftMode_StampsDeletedAt(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	if err := p.DeleteObject(a, "Supplier", id, "soft"); err != nil {
		t.Fatalf("DeleteObject(soft) err = %v, want nil (U2 soft delete implemented)", err)
	}
	// Read the stored object directly under lock — bypassing the public
	// GetObject path that U3 will mask. Stamp invariants belong to U2.
	p.mu.Lock()
	stored, ok := p.objects[objectKey("Supplier", id)]
	p.mu.Unlock()
	if !ok {
		t.Fatalf("soft delete must NOT remove the object from the map (U2), but it is gone")
	}
	if stored["_deletedAt"] == nil {
		t.Errorf("soft delete must stamp _deletedAt, got nil (U2)")
	}
	if v := objectVersionValue(stored); v != 2 {
		t.Errorf("soft delete must increment _version to 2, got %v (U2)", v)
	}
	if stored["_updatedAt"] == nil {
		t.Errorf("soft delete must re-stamp _updatedAt (U2)")
	}
	// User fields preserved: hard-delete semantics would drop the entry.
	if stored["name"] != "x" {
		t.Errorf("soft delete must preserve user fields: name = %v, want x (U2)", stored["name"])
	}
	// Cross-tenant soft delete is a no-op (idempotent, original tenant keeps).
	_, b := tenancyA() // b is tenantB (the second return value)
	obj2, _ := p.CreateObject(b, "Supplier", map[string]any{"name": "y"})
	id2 := obj2["_id"].(string)
	if err := p.DeleteObject(a, "Supplier", id2, "soft"); err != nil {
		t.Errorf("cross-tenant soft delete err = %v, want nil (U2 idempotent no-op)", err)
	}
	p.mu.Lock()
	stored2, ok2 := p.objects[objectKey("Supplier", id2)]
	p.mu.Unlock()
	if !ok2 {
		t.Fatalf("cross-tenant soft delete removed the other tenant's object (U2 no-leak)")
	}
	if stored2["_deletedAt"] != nil {
		t.Errorf("cross-tenant soft delete must not stamp _deletedAt on other tenant's object (U2 no-leak)")
	}
}

func TestDeleteObject_Hard_Idempotent(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	if err := p.DeleteObject(a, "Supplier", id, "hard"); err != nil {
		t.Fatalf("first hard delete err: %v", err)
	}
	if err := p.DeleteObject(a, "Supplier", id, "hard"); err != nil {
		t.Fatalf("second hard delete (idempotent) err: %v", err)
	}
	if err := p.DeleteObject(a, "Supplier", "never-existed", "hard"); err != nil {
		t.Fatalf("hard delete of never-existed id err: %v, want nil (idempotent)", err)
	}
}

func TestUpdateObject_MergesPatch(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x", "city": "NYC"})
	id := obj["_id"].(string)
	created := obj["_createdAt"]
	time.Sleep(2 * time.Millisecond) // ensure _updatedAt advances
	updated, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "y"}, nil)
	if err != nil {
		t.Fatalf("UpdateObject err: %v", err)
	}
	if updated["name"] != "y" {
		t.Errorf("UpdateObject name = %v, want y (patch)", updated["name"])
	}
	if updated["city"] != "NYC" {
		t.Errorf("UpdateObject must merge, city = %v, want NYC", updated["city"])
	}
	if updated["_createdAt"] != created {
		t.Errorf("UpdateObject must preserve _createdAt, got %v want %v", updated["_createdAt"], created)
	}
	if updated["_id"] != id {
		t.Errorf("UpdateObject must preserve _id, got %v want %v", updated["_id"], id)
	}
	if updated["_type"] != "Supplier" {
		t.Errorf("UpdateObject must preserve _type, got %v", updated["_type"])
	}
	if updated["_updatedAt"] == created {
		t.Errorf("UpdateObject must advance _updatedAt")
	}
}

func TestUpdateObject_SystemFieldsInPatch_AreIgnoredFromPatchAndRestamped(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	original := obj["_createdAt"]
	// Patch trying to overwrite system fields.
	updated, err := p.UpdateObject(a, "Supplier", id, map[string]any{
		"name":       "y",
		"_id":        "INTRUDER",
		"_type":      "INTRUDER",
		"_tenantId":  "INTRUDER",
		"_createdAt": "INTRUDER",
	}, nil)
	if err != nil {
		t.Fatalf("UpdateObject err: %v", err)
	}
	if updated["_id"] != id {
		t.Errorf("patched _id = %v, must keep %v", updated["_id"], id)
	}
	if updated["_type"] != "Supplier" {
		t.Errorf("patched _type = %v, must keep Supplier", updated["_type"])
	}
	if updated["_tenantId"] != "tenantA" {
		t.Errorf("patched _tenantId = %v, must keep tenantA", updated["_tenantId"])
	}
	if updated["_createdAt"] != original {
		t.Errorf("patched _createdAt = %v, must keep %v", updated["_createdAt"], original)
	}
	if v, ok := updated["name"].(string); !ok || v != "y" {
		t.Errorf("user property name must be updated, got %v", updated["name"])
	}
}

func TestUpdateObject_Missing_ReturnsObjectNotFound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	_, err := p.UpdateObject(a, "Supplier", "nope", map[string]any{"name": "y"}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("UpdateObject(missing) err = %v, want ErrObjectNotFound", err)
	}
}

func TestUpdateObject_CrossTenant_NotFound_NoLeak(t *testing.T) {
	p := New()
	a, b := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	_, err := p.UpdateObject(b, "Supplier", id, map[string]any{"name": "y"}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("UpdateObject cross-tenant err = %v, want ErrObjectNotFound (no leak)", err)
	}
	// Original tenant still sees its data unchanged.
	got, _ := p.GetObject(a, "Supplier", id)
	if got["name"] != "x" {
		t.Errorf("cross-tenant UpdateObject corrupted original: name = %v", got["name"])
	}
}

// TestUpdateObject_ExpectedVersionConflict_MismatchesReject is the Phase 3
// flip of the Phase 2 "silently ignored" contract: a non-nil expectedVersion
// that does not match the stored _version now returns ErrVersionConflict
// before any write. A matching expectedVersion (or nil) accepts the update
// and increments _version. Covers R1, AE1.
func TestUpdateObject_ExpectedVersionConflict_MismatchesReject(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)

	// Mismatch: stored _version is 1; expect 999 — must reject before write.
	stale := 999
	_, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "y"}, &stale)
	if !errors.Is(err, spi.ErrVersionConflict) {
		t.Fatalf("UpdateObject(stale version) err = %v, want ErrVersionConflict (U2)", err)
	}
	// Reject-before-write: the stored object's name must be unchanged.
	got, _ := p.GetObject(a, "Supplier", id)
	if got["name"] != "x" {
		t.Errorf("UpdateObject(stale version) wrote anyway: name = %v, want x (U2 reject-before-write)", got["name"])
	}

	// Match: expectedVersion 1 accepts and increments _version to 2.
	match := 1
	updated, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "y"}, &match)
	if err != nil {
		t.Fatalf("UpdateObject(matching version) err = %v, want nil (U2)", err)
	}
	if updated["name"] != "y" {
		t.Errorf("UpdateObject name = %v, want y (U2)", updated["name"])
	}
	if v := objectVersionValue(updated); v != 2 {
		t.Errorf("UpdateObject _version = %v, want 2 (U2 increment-on-match)", v)
	}

	// nil expectedVersion skips the check; accepts at any current version and
	// still increments. Covers R1's nil-means-accept clause.
	if _, err := p.UpdateObject(a, "Supplier", id, map[string]any{"tier": "Gold"}, nil); err != nil {
		t.Fatalf("UpdateObject(nil expectedVersion) err = %v, want nil (U2)", err)
	}
	got2, _ := p.GetObject(a, "Supplier", id)
	if v := objectVersionValue(got2); v != 3 {
		t.Errorf("UpdateObject _version after nil-check = %v, want 3 (U2)", v)
	}
	if got2["tier"] != "Gold" {
		t.Errorf("UpdateObject tier = %v, want Gold (U2 merge)", got2["tier"])
	}
}

// objectVersionValue is a tiny test helper that coerces _version (int when
// read from the stored authoritative map under lock; float64 when read from
// a JSON-clone returned to the caller) to a plain int for assertions. Mirrors
// the coercion the provider's own conflict check (U2 objectVersionInt) uses.
func objectVersionValue(o spi.OntologyObject) int {
	switch v := o["_version"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return -1
}

func TestUnimplementedFloor_RemaingObjectSPI(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	cases := []struct {
		name string
		fn   func() error
	}{
		{"QueryObjects", func() error {
			_, err := p.QueryObjects(a, "Supplier", spi.FilterExpression{}, nil)
			return err
		}},
		{"AggregateObjects", func() error {
			_, err := p.AggregateObjects(a, "Supplier", spi.AggregateQuery{})
			return err
		}},
		{"SearchObjects", func() error {
			_, err := p.SearchObjects(a, "Supplier", spi.SearchQuery{})
			return err
		}},
		{"BulkMutate", func() error {
			_, err := p.BulkMutate(a, spi.BulkMutationRequest{})
			return err
		}},
		{"BeginTransaction", func() error {
			_, err := p.BeginTransaction(a)
			return err
		}},
		// Phase 3 (U2): GetObjectAtVersion/GetObjectAtTime implemented —
		// removed from the ErrUnimplemented floor. They have their own
		// positive tests in provider_version_test.go.
		{"EnsureIndex", func() error {
			return p.EnsureIndex(a, "Supplier", spi.IndexDefinition{})
		}},
		{"DropIndex", func() error {
			return p.DropIndex(a, "Supplier", "name")
		}},
		{"ListIndexes", func() error {
			_, err := p.ListIndexes(a, "Supplier")
			return err
		}},
	}
	for _, c := range cases {
		err := c.fn()
		if !errors.Is(err, spi.ErrUnimplemented) {
			t.Errorf("%s err = %v, want ErrUnimplemented", c.name, err)
		}
	}
}
