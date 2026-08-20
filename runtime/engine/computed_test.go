package engine

import (
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func facilityOntology(t *testing.T) *ir.Ontology {
	t.Helper()
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test"},
		Objects: []ir.ObjectType{
			{
				Name: "Facility",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
					{
						Name: "currentUtilization",
						Type: ir.TypeRef{Name: "Int"},
						Role: ir.RoleComputed,
						Computed: &ir.ComputedRef{
							Fn:    "countLinks",
							Args:  map[string]any{"type": "InventoryAt"},
							Cache: "LAZY",
						},
					},
				},
			},
			{
				Name: "InventoryRecord",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "sku", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
					{Name: "facility", Type: ir.TypeRef{Name: "Facility", NonNull: true}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "InventoryAt", From: "InventoryRecord", To: "Facility", Cardinality: ir.CardinalityManyToOne},
		},
	}
}

func TestGetObjectOpts_CountLinks_ZeroWithoutLinks(t *testing.T) {
	e, err := New(memory.New(), facilityOntology(t))
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	ctx := tenantCtx("tnt")
	fac, err := e.CreateObject(ctx, "Facility", map[string]any{"name": "WH-1"})
	if err != nil {
		t.Fatalf("CreateObject Facility err = %v", err)
	}
	fid := fac[spi.FieldID].(string)

	got, err := e.GetObjectOpts(ctx, "Facility", fid, &GetObjectOpts{ComputedFields: []string{}})
	if err != nil {
		t.Fatalf("GetObjectOpts err = %v", err)
	}
	n, _ := toInt(got["currentUtilization"])
	if n != 0 {
		t.Fatalf("currentUtilization = %v, want 0 (no InventoryAt links)", got["currentUtilization"])
	}
}

func TestGetObjectOpts_CountLinks_FKWithoutCreateLink_StaysZero(t *testing.T) {
	e, err := New(memory.New(), facilityOntology(t))
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	ctx := tenantCtx("tnt")
	fac, err := e.CreateObject(ctx, "Facility", map[string]any{"name": "WH-1"})
	if err != nil {
		t.Fatalf("CreateObject Facility err = %v", err)
	}
	fid := fac[spi.FieldID].(string)
	if _, err := e.CreateObject(ctx, "InventoryRecord", map[string]any{
		"sku":      "IR-1",
		"facility": fid,
	}); err != nil {
		t.Fatalf("CreateObject InventoryRecord err = %v", err)
	}

	got, err := e.GetObjectOpts(ctx, "Facility", fid, &GetObjectOpts{ComputedFields: []string{}})
	if err != nil {
		t.Fatalf("GetObjectOpts err = %v", err)
	}
	n, _ := toInt(got["currentUtilization"])
	if n != 0 {
		t.Fatalf("currentUtilization = %v, want 0 (FK without CreateLink)", got["currentUtilization"])
	}
}

func TestGetObjectOpts_CountLinks_NActiveLinks(t *testing.T) {
	e, err := New(memory.New(), facilityOntology(t))
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	ctx := tenantCtx("tnt")
	fac, err := e.CreateObject(ctx, "Facility", map[string]any{"name": "WH-1"})
	if err != nil {
		t.Fatalf("CreateObject Facility err = %v", err)
	}
	fid := fac[spi.FieldID].(string)
	for i := 0; i < 3; i++ {
		rec, err := e.CreateObject(ctx, "InventoryRecord", map[string]any{
			"sku":      "IR",
			"facility": fid,
		})
		if err != nil {
			t.Fatalf("CreateObject InventoryRecord %d err = %v", i, err)
		}
		if _, err := e.CreateLink(ctx, "InventoryAt", rec[spi.FieldID].(string), fid, nil); err != nil {
			t.Fatalf("CreateLink InventoryAt %d err = %v", i, err)
		}
	}

	got, err := e.GetObjectOpts(ctx, "Facility", fid, &GetObjectOpts{ComputedFields: []string{}})
	if err != nil {
		t.Fatalf("GetObjectOpts err = %v", err)
	}
	n, _ := toInt(got["currentUtilization"])
	if n != 3 {
		t.Fatalf("currentUtilization = %v, want 3", got["currentUtilization"])
	}

	plain, err := e.GetObject(ctx, "Facility", fid)
	if err != nil {
		t.Fatalf("GetObject err = %v", err)
	}
	if _, ok := plain["currentUtilization"]; ok {
		t.Fatalf("GetObject without opts must not evaluate computed, got %v", plain["currentUtilization"])
	}
}

func TestGetObjectOpts_UnknownComputedFn_Errors(t *testing.T) {
	e := newEngine(t)
	ctx := tenantCtx("tnt")
	obj, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("CreateObject err = %v", err)
	}
	id := obj[spi.FieldID].(string)
	_, err = e.GetObjectOpts(ctx, "Supplier", id, &GetObjectOpts{ComputedFields: []string{"activeOrders"}})
	if err == nil {
		t.Fatal("GetObjectOpts unknown fn = nil err, want error")
	}
}
