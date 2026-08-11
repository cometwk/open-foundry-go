package engine

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// objectOntology is the U4 fixture TBox: Supplier (required name) + Part
// (required sku), a Supplies link Supplier→Part, plus a Supplier
// LinkNav field (currentShipments) and Computed field (activeOrders)
// so role-rejection can be exercised. ir.Validate accepts exactly one
// Primary per object type and the link endpoints land on object types,
// so this fixture passes construction-time validation.
func objectOntology(t *testing.T) *ir.Ontology {
	t.Helper()
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test"},
		Objects: []ir.ObjectType{
			{
				Name: "Supplier",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
					{Name: "currentShipments", Type: ir.TypeRef{Name: "Supplies", IsList: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "Supplies", Direction: ir.DirectionOutbound}},
					{Name: "activeOrders", Type: ir.TypeRef{Name: "Int"}, Role: ir.RoleComputed, Computed: &ir.ComputedRef{Fn: "countActiveOrders"}},
				},
			},
			{
				Name: "Part",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "sku", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
					{Name: "qty", Type: ir.TypeRef{Name: "Int"}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "Supplies", From: "Supplier", To: "Part", Cardinality: ir.CardinalityOneToMany},
		},
	}
}

// tenantCtx returns a RequestContext for the given tenant id.
func tenantCtx(tenant string) spi.RequestContext {
	return spi.RequestContext{TenantID: tenant, ActorID: "test"}
}

// newEngine wires a fresh memory.Provider + Engine over the U4 fixture.
func newEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(memory.New(), objectOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	return e
}

func TestEngine_CreateObject_Get_RoundTrip_StampsSystemFields(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	obj, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("CreateObject err = %v, want nil", err)
	}
	if obj["_id"] == nil || obj["_id"] == "" {
		t.Errorf("CreateObject _id = %v, want non-empty", obj["_id"])
	}
	if obj["_type"] != "Supplier" {
		t.Errorf("CreateObject _type = %v, want Supplier", obj["_type"])
	}
	if obj["_createdAt"] == nil {
		t.Errorf("CreateObject _createdAt = nil, want a value")
	}
	if obj["_tenantId"] != "tnt" {
		t.Errorf("CreateObject _tenantId = %v, want tnt", obj["_tenantId"])
	}
	if obj["name"] != "Acme" {
		t.Errorf("CreateObject user prop name = %v, want Acme", obj["name"])
	}
	id := obj["_id"].(string)
	got, err := e.GetObject(ctx, "Supplier", id)
	if err != nil {
		t.Fatalf("GetObject err = %v, want nil", err)
	}
	if got["name"] != "Acme" {
		t.Errorf("GetObject name = %v, want Acme", got["name"])
	}
	if got["_id"] != id {
		t.Errorf("GetObject _id = %v, want %v", got["_id"], id)
	}
}

func TestEngine_CreateObject_UnknownType_RejectsBeforeStorage(t *testing.T) {
	// Capture storage calls to prove the validator short-circuits before
	// the SPI is touched.
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, objectOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	_, err = e.CreateObject(tenantCtx("tnt"), "Nonexistent", map[string]any{"name": "x"})
	if !errors.Is(err, spi.ErrInvalidObjectType) {
		t.Fatalf("CreateObject(unknown type) err = %v, want ErrInvalidObjectType", err)
	}
	if rec.createCalls > 0 {
		t.Errorf("CreateObject(unknown type) called storage.CreateObject %d times, want 0 (validator must reject before SPI)", rec.createCalls)
	}
}

func TestEngine_CreateObject_MissingRequired_Rejects(t *testing.T) {
	e := newEngine(t)
	// Supplier.name is NonNull with no default.
	_, err := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{})
	if err == nil {
		t.Fatal("CreateObject(missing required name) = nil err, want validation error")
	}
}

func TestEngine_CreateObject_RequiredWithDefault_Accepts(t *testing.T) {
	// A NonNull field carrying a @default directive should not require a
	// payload value. We mutate a copy of the fixture TBox so other tests
	// see the unadorned IR.
	o := objectOntology(t)
	for i := range o.Objects {
		if o.Objects[i].Name == "Supplier" {
			for j := range o.Objects[i].Fields {
				if o.Objects[i].Fields[j].Name == "name" {
					o.Objects[i].Fields[j].Flags.Default = "Acme"
				}
			}
		}
	}
	e, err := New(memory.New(), o)
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	obj, err := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{})
	if err != nil {
		t.Fatalf("CreateObject(Non-null field with default, no payload) err = %v, want nil", err)
	}
	if obj["_id"] == nil || obj["_id"] == "" {
		t.Errorf("CreateObject _id = %v, want non-empty", obj["_id"])
	}
}

func TestEngine_CreateObject_LinkNavRoleInPayload_Rejects(t *testing.T) {
	e := newEngine(t)
	_, err := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{
		"name":             "Acme",
		"currentShipments": []any{"x"},
	})
	if err == nil {
		t.Fatal("CreateObject with LinkNav field in payload = nil err, want role rejection")
	}
}

func TestEngine_CreateObject_ComputedRoleInPayload_Rejects(t *testing.T) {
	e := newEngine(t)
	_, err := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{
		"name":         "Acme",
		"activeOrders": 3,
	})
	if err == nil {
		t.Fatal("CreateObject with Computed field in payload = nil err, want role rejection")
	}
}

func TestEngine_CreateObject_PrimaryRoleInPayload_Rejects(t *testing.T) {
	e := newEngine(t)
	_, err := e.CreateObject(tenantCtx("tnt"), "Supplier", map[string]any{
		"name": "Acme",
		"id":   "intruder",
	})
	if err == nil {
		t.Fatal("CreateObject with Primary field in payload = nil err, want role rejection")
	}
}

func TestEngine_CreateObject_ScalarTypeMismatch_Rejects(t *testing.T) {
	e := newEngine(t)
	// Part.qty is an Int; supplying a string must fail the type check.
	_, err := e.CreateObject(tenantCtx("tnt"), "Part", map[string]any{
		"sku": "P1",
		"qty": "not-an-int",
	})
	if err == nil {
		t.Fatal("CreateObject(scalar type mismatch) = nil err, want type error")
	}
}

func TestEngine_GetObject_Missing_TypedNotFound(t *testing.T) {
	e := newEngine(t)
	_, err := e.GetObject(tenantCtx("tnt"), "Supplier", "nope")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject(missing) err = %v, want ErrObjectNotFound", err)
	}
}

func TestEngine_UpdateObject_ReflectsPatch_MergesNotReplaces(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	obj, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1", "qty": 1})
	id := obj["_id"].(string)
	updated, err := e.UpdateObject(ctx, "Part", id, map[string]any{"qty": 9}, nil)
	if err != nil {
		t.Fatalf("UpdateObject err = %v, want nil", err)
	}
	if updated["sku"] != "P1" {
		t.Errorf("UpdateObject dropped unpatched field sku = %v, want P1 (merge)", updated["sku"])
	}
	if got := updated["qty"]; got != float64(9) {
		t.Errorf("UpdateObject qty = %v (%T), want 9", got, got)
	}
	if updated["_updatedAt"] == nil {
		t.Errorf("UpdateObject _updatedAt = nil, want storage-advanced value")
	}
	if updated["_createdAt"] != obj["_createdAt"] {
		t.Errorf("UpdateObject _createdAt drifted = %v, want %v", updated["_createdAt"], obj["_createdAt"])
	}
}

func TestEngine_UpdateObject_MissingId_TypedNotFoundBeforeWrite(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, objectOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	_, err = e.UpdateObject(tenantCtx("tnt"), "Supplier", "nope", map[string]any{"name": "x"}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("UpdateObject(missing id) err = %v, want ErrObjectNotFound", err)
	}
	if rec.updateCalls > 0 {
		t.Errorf("UpdateObject(missing id) called storage.UpdateObject %d times, want 0 (must reject before SPI)", rec.updateCalls)
	}
}

func TestEngine_UpdateObject_SystemFieldsInPatch_StrippedFromValidation(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	obj, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	id := obj["_id"].(string)
	// Supplying a Primary field id in a patch would normally be a
	// role-rejection, but system fields are stripped from the merged
	// payload before validation, so the update must succeed.
	_, err := e.UpdateObject(ctx, "Part", id, map[string]any{
		"_id": "intruder",
		"sku": "P2",
	}, nil)
	if err != nil {
		t.Fatalf("UpdateObject with system field in patch err = %v, want nil (system fields stripped)", err)
	}
}

func TestEngine_DeleteObject_Hard_RemovesAndPostGetNotFound(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	obj, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	id := obj["_id"].(string)
	if err := e.DeleteObject(ctx, "Supplier", id, "hard"); err != nil {
		t.Fatalf("DeleteObject(hard) err = %v, want nil", err)
	}
	if _, err := e.GetObject(ctx, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after hard delete err = %v, want ErrObjectNotFound", err)
	}
}

func TestEngine_DeleteObject_MissingId_TypedNotFoundBeforeWrite(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, objectOntology(t))
	if err != nil {
		t.Fatalf("New err = %v, want nil", err)
	}
	err = e.DeleteObject(tenantCtx("tnt"), "Supplier", "nope", "hard")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("DeleteObject(missing id) err = %v, want ErrObjectNotFound", err)
	}
	if rec.deleteCalls > 0 {
		t.Errorf("DeleteObject(missing id) called storage.DeleteObject %d times, want 0 (must reject before SPI)", rec.deleteCalls)
	}
}

// TestEngine_DeleteObject_SoftMode_Succeeds is the Phase 3 flip of the
// Phase 2 "bubbles ErrUnimplemented" contract: soft delete now stamps
// _deletedAt via the SPI memory provider (U2) instead of rejecting, and
// the U3 read-path mask hides the soft-deleted object as ErrObjectNotFound
// when read through Engine.GetObject. Together U2+U3 complete AE2's
// Engine-level soft-delete visibility contract.
func TestEngine_DeleteObject_SoftMode_Succeeds(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	obj, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	id := obj["_id"].(string)
	if err := e.DeleteObject(ctx, "Supplier", id, "soft"); err != nil {
		t.Fatalf("DeleteObject(soft) err = %v, want nil (U2 soft delete implemented)", err)
	}
	// U3 read-path mask: Engine.GetObject surfaces the SPI's typed
	// not-found, identical to a hard-deleted or missing object. Callers
	// cannot distinguish "soft-deleted" from "never was" (R3 design).
	if _, err := e.GetObject(ctx, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("Engine.GetObject after soft delete err = %v, want ErrObjectNotFound (U3 mask)", err)
	}
}

// recordingProvider is a thin decorator over a StorageProvider that
// counts Create/Update/Delete Object (and Create/Delete Link) calls so
// tests can prove the Engine rejects before touching storage. It
// embeds the underlying memory provider so all other SPI methods pass
// through unchanged.
type recordingProvider struct {
	spi.UnimplementedStorageProvider
	inner           spi.StorageProvider
	createCalls     int
	updateCalls     int
	deleteCalls     int
	createLinkCalls int
	updateLinkCalls int
	deleteLinkCalls int
}

func (r *recordingProvider) ApplySchema(ctx spi.RequestContext, s spi.OntologySchema) (spi.MigrationResult, error) {
	return r.inner.ApplySchema(ctx, s)
}
func (r *recordingProvider) GetSchema(ctx spi.RequestContext, v *int) (spi.OntologySchema, error) {
	return r.inner.GetSchema(ctx, v)
}
func (r *recordingProvider) CreateObject(ctx spi.RequestContext, typ string, p map[string]any) (spi.OntologyObject, error) {
	r.createCalls++
	return r.inner.CreateObject(ctx, typ, p)
}
func (r *recordingProvider) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	return r.inner.GetObject(ctx, typ, id)
}
func (r *recordingProvider) UpdateObject(ctx spi.RequestContext, typ, id string, p map[string]any, ev *int) (spi.OntologyObject, error) {
	r.updateCalls++
	return r.inner.UpdateObject(ctx, typ, id, p, ev)
}
func (r *recordingProvider) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	r.deleteCalls++
	return r.inner.DeleteObject(ctx, typ, id, mode)
}
func (r *recordingProvider) CreateLink(ctx spi.RequestContext, typ, fromID, toID string, p map[string]any) (spi.OntologyLink, error) {
	r.createLinkCalls++
	return r.inner.CreateLink(ctx, typ, fromID, toID, p)
}
func (r *recordingProvider) GetLink(ctx spi.RequestContext, typ, linkID string) (spi.OntologyLink, error) {
	return r.inner.GetLink(ctx, typ, linkID)
}
func (r *recordingProvider) UpdateLink(ctx spi.RequestContext, typ, linkID string, p map[string]any, ev *int) (spi.OntologyLink, error) {
	r.updateLinkCalls++
	return r.inner.UpdateLink(ctx, typ, linkID, p, ev)
}
func (r *recordingProvider) DeleteLink(ctx spi.RequestContext, typ, linkID string) error {
	r.deleteLinkCalls++
	return r.inner.DeleteLink(ctx, typ, linkID)
}

// Compile-time assertion: Engine's verbs operate against the
// spi.StorageProvider interface. The Engine struct never references the
// concrete memory.Provider type — recordingProvider here is only used
// in tests and is satisfied via the interface.
var _ spi.StorageProvider = (*recordingProvider)(nil)
