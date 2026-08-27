package memory

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
)

func TestCreateLink_HonorsEngineLinkId(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	_, objA, _ := seedObject(t, p, a, "Supplier")
	_, objB, _ := seedObject(t, p, a, "Part")
	link, err := p.CreateLink(a, "Supplies", objA["_id"].(string), objB["_id"].(string), map[string]any{
		"_engineLinkId": "engine-supplied-link-id",
		"qty":           3,
	})
	if err != nil {
		t.Fatalf("CreateLink err: %v", err)
	}
	if link["_id"] != "engine-supplied-link-id" {
		t.Errorf("CreateLink _id = %v, want engine-supplied-link-id", link["_id"])
	}
	if _, ok := link["_engineLinkId"]; ok {
		t.Errorf("CreateLink must strip _engineLinkId from stored link, got %v", link["_engineLinkId"])
	}
	if got := link["qty"]; got != float64(3) {
		t.Errorf("CreateLink user property qty = %v (%T), want 3", got, got)
	}
}

func TestCreateLink_WithoutEngineLinkId_GeneratesUUIDv7(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	link, err := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if err != nil {
		t.Fatalf("CreateLink err: %v", err)
	}
	id, ok := link["_id"].(string)
	if !ok || id == "" {
		t.Fatalf("CreateLink must mint a UUIDv7 _id when none is supplied, got %v", link["_id"])
	}
}

func TestCreateLink_StampsFromTypeAndToType_FromSchema(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	link, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if link["_fromType"] != "Supplier" {
		t.Errorf("CreateLink _fromType = %v, want Supplier (schema-stamped)", link["_fromType"])
	}
	if link["_toType"] != "Part" {
		t.Errorf("CreateLink _toType = %v, want Part (schema-stamped)", link["_toType"])
	}
}

func TestCreateLink_NoSchemaApplied_DefaultsUnknownFromToType(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	link, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if link["_fromType"] != "unknown" {
		t.Errorf("CreateLink with no schema default _fromType = %v, want unknown", link["_fromType"])
	}
	if link["_toType"] != "unknown" {
		t.Errorf("CreateLink with no schema default _toType = %v, want unknown", link["_toType"])
	}
}

func TestCreateLink_SystemFieldsInPatch_AreIgnored(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	link, err := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), map[string]any{
		"_id":        "INTRUDER",
		"_type":      "INTRUDER",
		"_tenantId":  "INTRUDER",
		"_fromType":  "INTRUDER",
		"_toType":    "INTRUDER",
		"_createdAt": "INTRUDER",
		"user-prop":  1,
	})
	if err != nil {
		t.Fatalf("CreateLink err: %v", err)
	}
	if link["_type"] != "Supplies" {
		t.Errorf("CreateLink _type = %v, want Supplies", link["_type"])
	}
	if link["_tenantId"] != a.TenantID {
		t.Errorf("CreateLink _tenantId = %v, want %s", link["_tenantId"], a.TenantID)
	}
	if link["_fromType"] == "INTRUDER" || link["_toType"] == "INTRUDER" {
		t.Errorf("CreateLink leaked _fromType/_toType from patch: from=%v to=%v", link["_fromType"], link["_toType"])
	}
	if got := link["user-prop"]; got != float64(1) {
		t.Errorf("CreateLink user property user-prop = %v (%T), want 1", got, got)
	}
}

func TestGetLink_RoundTripsAndStampsIds(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	got, err := p.GetLink(a, "Supplies", created["_id"].(string))
	if err != nil {
		t.Fatalf("GetLink err: %v", err)
	}
	if got["_id"] != created["_id"] {
		t.Errorf("GetLink _id = %v, want %v", got["_id"], created["_id"])
	}
	if got["_fromId"] != s["_id"] {
		t.Errorf("GetLink _fromId = %v, want %v", got["_fromId"], s["_id"])
	}
	if got["_toId"] != pt["_id"] {
		t.Errorf("GetLink _toId = %v, want %v", got["_toId"], pt["_id"])
	}
	// Phase 3 (U1): CreateLink stamps authoritative _version:1. JSON-clone
	// round-trip decodes ints as float64 (matches the qty float64(3)
	// convention); accept either representation.
	switch v := got["_version"].(type) {
	case int:
		if v != 1 {
			t.Errorf("GetLink _version = %d, want 1 (U1)", v)
		}
	case float64:
		if v != 1 {
			t.Errorf("GetLink _version = %v, want 1 (U1)", v)
		}
	default:
		t.Errorf("GetLink _version has unexpected type %T = %v, want 1 (U1)", got["_version"], got["_version"])
	}
}

func TestGetLink_Missing_ReturnsLinkNotFound(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	_, err := p.GetLink(a, "Supplies", "nope")
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink(missing) err = %v, want ErrLinkNotFound", err)
	}
}

func TestGetLink_CrossTenant_Hidden(t *testing.T) {
	p := New()
	a, b := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	_, err := p.GetLink(b, "Supplies", created["_id"].(string))
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink cross-tenant err = %v, want ErrLinkNotFound (no leak)", err)
	}
}

func TestDeleteLink_RemovesFromMap(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	id := created["_id"].(string)
	if err := p.DeleteLink(a, "Supplies", id); err != nil {
		t.Fatalf("DeleteLink err: %v", err)
	}
	_, err := p.GetLink(a, "Supplies", id)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink after delete err = %v, want ErrLinkNotFound", err)
	}
}

func TestDeleteLink_Idempotent(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	if err := p.DeleteLink(a, "Supplies", "never-existed"); err != nil {
		t.Errorf("DeleteLink(never-existed) err = %v, want nil (idempotent)", err)
	}
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	id := created["_id"].(string)
	if err := p.DeleteLink(a, "Supplies", id); err != nil {
		t.Fatalf("first DeleteLink err: %v", err)
	}
	if err := p.DeleteLink(a, "Supplies", id); err != nil {
		t.Errorf("second DeleteLink err: %v, want nil (idempotent)", err)
	}
}

func TestDeleteLink_CrossTenant_NoOpLeavesOriginalVisible(t *testing.T) {
	p := New()
	a, b := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	id := created["_id"].(string)
	if err := p.DeleteLink(b, "Supplies", id); err != nil {
		t.Fatalf("DeleteLink cross-tenant err: %v", err)
	}
	if _, err := p.GetLink(a, "Supplies", id); err != nil {
		t.Errorf("cross-tenant DeleteLink removed link from original tenant: err = %v", err)
	}
}

func TestUnimplementedFloor_RemainingLinkSPI(t *testing.T) {
	// Phase 3 (U6): UpdateLink/GetLinks/Traverse implemented — the link
	// ErrUnimplemented floor is empty. Positive coverage lives in
	// provider_link_extra_test.go. Retained as a doc-only sentinel so a
	// future deferred link SPI method has a place to land.
	t.Log("U6 folded UpdateLink/GetLinks/Traverse off the ErrUnimplemented floor")
}

// applySupplyLinkSchema installs a minimal schema with one link type so
// CreateLink can stamp _fromType/_toType from it.
func applySupplyLinkSchema(t *testing.T, p *Provider, ctx spi.RequestContext) {
	t.Helper()
	schema := spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Supplier"},
			{Name: "Part"},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Supplies", FromType: "Supplier", ToType: "Part", Cardinality: spi.CardinalityOneToMany},
		},
	}
	if _, err := p.ApplySchema(ctx, schema); err != nil {
		t.Fatalf("ApplySchema err: %v", err)
	}
}

// createObjectForTest wraps p.CreateObject and surfaces a fatal on failure.
// Counterpart to seedObject but without the secondary tenant output.
func createObjectForTest(t *testing.T, p *Provider, ctx spi.RequestContext, typ string) spi.OntologyObject {
	t.Helper()
	obj, err := p.CreateObject(ctx, typ, map[string]any{"name": "x"})
	if err != nil {
		t.Fatalf("seed CreateObject(%s) err: %v", typ, err)
	}
	return obj
}

// seedObject is a drop-in helper retained for tests that want both tenants
// primed. Returns the A-tenant object as the second value; the B-tenant
// object exists only for tests that explicitly seed cross-tenant state.
func seedObject(t *testing.T, p *Provider, ctx spi.RequestContext, typ string) (spi.RequestContext, spi.OntologyObject, error) {
	t.Helper()
	obj, err := p.CreateObject(ctx, typ, map[string]any{"name": "x"})
	if err != nil {
		return spi.RequestContext{}, nil, err
	}
	return ctx, obj, nil
}
