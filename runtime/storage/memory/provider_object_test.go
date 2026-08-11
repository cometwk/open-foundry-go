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

func tenancyA() (spi.RequestContext, spi.RequestContext) { return tenantCtx("tenantA"), tenantCtx("tenantB") }

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
	if obj["_version"] != nil {
		t.Errorf("CreateObject must not store _version in Phase 2, got %v", obj["_version"])
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

func TestDeleteObject_SoftMode_Unimplemented(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	err := p.DeleteObject(a, "Supplier", id, "soft")
	if !errors.Is(err, spi.ErrUnimplemented) {
		t.Fatalf("DeleteObject(soft) err = %v, want ErrUnimplemented", err)
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

func TestUpdateObject_ExpectedVersionArgument_SilentlyIgnored(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "x"})
	id := obj["_id"].(string)
	// Phase 2 does not store _version; passing an expected version must not
	// reject the update. Use values a real version-check would reject.
	v := 999
	if _, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "y"}, &v); err != nil {
		t.Fatalf("UpdateObject with non-nil expectedVersion err = %v, want nil (versioning deferred)", err)
	}
}

func TestUnimplementedFloor_RemaingObjectSPI(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	cases := []struct {
		name string
		fn  func() error
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
		{"GetObjectAtVersion", func() error {
			_, err := p.GetObjectAtVersion(a, "Supplier", "x", 1)
			return err
		}},
		{"GetObjectAtTime", func() error {
			_, err := p.GetObjectAtTime(a, "Supplier", "x", time.Now())
			return err
		}},
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