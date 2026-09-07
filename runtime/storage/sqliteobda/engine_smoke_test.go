package sqliteobda_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

func TestCapabilitiesTemporalAndBulkOff(t *testing.T) {
	p, _ := activatePatient(t)
	caps := p.Capabilities()
	if caps.SupportsTemporalQueries || caps.SupportsBulkMutations {
		t.Fatalf("%+v", caps)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.GetObjectAtVersion(ctx, "Patient", "x", 1); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("GetObjectAtVersion err=%v", err)
	}
	if _, err := p.BulkMutate(ctx, spi.BulkMutationRequest{}); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("BulkMutate err=%v", err)
	}
}

func TestEngineSmokeRawIDs(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ont := hospitalIR()
	e, err := engine.New(p, ont)
	if err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	obj, err := e.CreateObject(ctx, "Patient", map[string]any{"name": "Eve"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := obj[spi.FieldID].(string)
	if len(id) != 36 || id[14] != '7' {
		t.Fatalf("object id=%q want UUIDv7", id)
	}
	link, err := e.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
	if err != nil {
		t.Fatal(err)
	}
	lid, _ := link[spi.FieldID].(string)
	if len(lid) != 36 || lid[14] != '7' {
		t.Fatalf("link id=%q want UUIDv7", lid)
	}
}

func hospitalIR() *ir.Ontology {
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "nhs.acute"},
		Objects: []ir.ObjectType{
			{
				Name: "Patient",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
			{
				Name: "Ward",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "AdmittedTo", From: "Patient", To: "Ward", Cardinality: ir.CardinalityManyToMany},
		},
	}
}
