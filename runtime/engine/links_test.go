package engine

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// linkOntology is the U5 fixture TBox: Supplier + Part object types
// plus a Supplies link Supplier→Part (one-to-many). Reuses the U4
// objectOntology shape minus the role-rejection fields for focus.
func linkOntology(t *testing.T) *ir.Ontology {
	t.Helper()
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test"},
		Objects: []ir.ObjectType{
			{
				Name: "Supplier",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
				},
			},
			{
				Name: "Part",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "sku", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "Supplies", From: "Supplier", To: "Part", Cardinality: ir.CardinalityOneToMany},
		},
	}
}

func newLinkEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(memory.New(), linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	return e
}

func TestEngine_CreateLink_ReturnsLinkWithSystemFields(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	link, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if err != nil {
		t.Fatalf("CreateLink err = %v, want nil", err)
	}
	if link["_id"] == nil || link["_id"] == "" {
		t.Errorf("CreateLink _id = %v, want non-empty", link["_id"])
	}
	if link["_type"] != "Supplies" {
		t.Errorf("CreateLink _type = %v, want Supplies", link["_type"])
	}
	if link["_fromId"] != s["_id"] {
		t.Errorf("CreateLink _fromId = %v, want %v", link["_fromId"], s["_id"])
	}
	if link["_toId"] != pt["_id"] {
		t.Errorf("CreateLink _toId = %v, want %v", link["_toId"], pt["_id"])
	}
	if link["_tenantId"] != "tnt" {
		t.Errorf("CreateLink _tenantId = %v, want tnt", link["_tenantId"])
	}
}

func TestEngine_CreateLink_UnknownLinkType_RejectsBeforeStorage(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	s, _ := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(tenantCtx("tnt"), "Part", map[string]any{"sku": "P1"})
	_, err = e.CreateLink(tenantCtx("tnt"), "Nonexistent", s["_id"].(string), pt["_id"].(string), nil)
	if !errors.Is(err, spi.ErrInvalidLinkType) {
		t.Fatalf("CreateLink(unknown type) err = %v, want ErrInvalidLinkType", err)
	}
	if rec.createLinkCalls > 0 {
		t.Errorf("CreateLink(unknown type) called storage.CreateLink %d times, want 0 (validator must reject before SPI)", rec.createLinkCalls)
	}
}

func TestEngine_CreateLink_FromObjectMissing_TypedNotFoundBeforeWrite(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	pt, _ := e.CreateObject(tenantCtx("tnt"), "Part", map[string]any{"sku": "P1"})
	_, err = e.CreateLink(tenantCtx("tnt"), "Supplies", "missing-from", pt["_id"].(string), nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("CreateLink(missing from) err = %v, want ErrObjectNotFound", err)
	}
	if rec.createLinkCalls > 0 {
		t.Errorf("CreateLink(missing from) called storage.CreateLink %d times, want 0 (must reject before SPI)", rec.createLinkCalls)
	}
}

func TestEngine_CreateLink_ToObjectMissing_TypedNotFoundBeforeWrite(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	s, _ := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{"name": "Acme"})
	_, err = e.CreateLink(tenantCtx("tnt"), "Supplies", s["_id"].(string), "missing-to", nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("CreateLink(missing to) err = %v, want ErrObjectNotFound", err)
	}
	if rec.createLinkCalls > 0 {
		t.Errorf("CreateLink(missing to) called storage.CreateLink %d times, want 0 (must reject before SPI)", rec.createLinkCalls)
	}
}

func TestEngine_CreateLink_EngineGeneratesUUIDv7_StorageIdHonorsIt(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	link, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if err != nil {
		t.Fatalf("CreateLink err = %v, want nil", err)
	}
	id := link["_id"].(string)
	if !looksLikeUUIDv7(id) {
		t.Errorf("CreateLink _id = %q, want a UUIDv7-shaped id", id)
	}
	// GetLink must return the same _id — proves memory honored
	// _engineLinkId rather than minting its own.
	got, err := e.GetLink(ctx, "Supplies", id)
	if err != nil {
		t.Fatalf("GetLink err = %v, want nil", err)
	}
	if got["_id"] != id {
		t.Errorf("GetLink _id = %v, want %v (memory must honor _engineLinkId)", got["_id"], id)
	}
}

func TestEngine_CreateLink_DoesNotMutateCallerProperties(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	props := map[string]any{"qty": 3}
	if _, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), props); err != nil {
		t.Fatalf("CreateLink err = %v, want nil", err)
	}
	if _, ok := props["_engineLinkId"]; ok {
		t.Errorf("CreateLink mutated caller properties map: _engineLinkId = %v, want absent", props["_engineLinkId"])
	}
}

func TestEngine_GetLink_RoundTrips(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	created, _ := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	got, err := e.GetLink(ctx, "Supplies", created["_id"].(string))
	if err != nil {
		t.Fatalf("GetLink err = %v, want nil", err)
	}
	if got["_id"] != created["_id"] {
		t.Errorf("GetLink _id = %v, want %v", got["_id"], created["_id"])
	}
}

func TestEngine_GetLink_Missing_TypedNotFound(t *testing.T) {
	e := newLinkEngine(t)
	_, err := e.GetLink(tenantCtx("tnt"), "Supplies", "nope")
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink(missing) err = %v, want ErrLinkNotFound", err)
	}
}

func TestEngine_DeleteLink_RemovesAndPostGetNotFound(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	created, _ := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	id := created["_id"].(string)
	if err := e.DeleteLink(ctx, "Supplies", id); err != nil {
		t.Fatalf("DeleteLink err = %v, want nil", err)
	}
	if _, err := e.GetLink(ctx, "Supplies", id); !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink after delete err = %v, want ErrLinkNotFound", err)
	}
}

func TestEngine_DeleteLink_Missing_TypedNotFoundBeforeWrite(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	err = e.DeleteLink(tenantCtx("tnt"), "Supplies", "nope")
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("DeleteLink(missing) err = %v, want ErrLinkNotFound", err)
	}
	if rec.deleteLinkCalls > 0 {
		t.Errorf("DeleteLink(missing) called storage.DeleteLink %d times, want 0 (must reject before SPI)", rec.deleteLinkCalls)
	}
}

// looksLikeUUIDv7 asserts the id carries the v7 version nibble in the
// canonical third group. The uuidv7 façade produces 8-4-4-4-12 form;
// the third group must start with "7" per RFC 9562.
func looksLikeUUIDv7(id string) bool {
	if len(id) != 36 {
		return false
	}
	// positions 14-18 are the third group "7xxx".
	return id[14] == '7'
}
