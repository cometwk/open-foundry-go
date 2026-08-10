package ir_test

import (
	"testing"

	"github.com/openfoundry/runtime/ir"
)

func validMinimal() *ir.Ontology {
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test", Version: "0.1.0"},
		Objects: []ir.ObjectType{
			{
				Name: "Widget",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
			{
				Name: "Bin",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				},
			},
		},
		Links: []ir.LinkType{
			{
				Name:        "InBin",
				From:        "Widget",
				To:          "Bin",
				Cardinality: ir.CardinalityManyToOne,
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				},
			},
		},
	}
}

func TestValidateHappyPath(t *testing.T) {
	if err := ir.Validate(validMinimal()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMissingLinkEndpoint(t *testing.T) {
	o := validMinimal()
	o.Links[0].To = "Missing"
	err := ir.Validate(o)
	if err == nil {
		t.Fatal("expected error")
	}
	if ve, ok := err.(*ir.Error); !ok || ve.Code != "LINK_ENDPOINT" {
		t.Fatalf("got %v", err)
	}
}

func TestValidatePrimaryCount(t *testing.T) {
	o := validMinimal()
	o.Objects[0].Fields = []ir.Field{
		{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
	}
	err := ir.Validate(o)
	if err == nil {
		t.Fatal("expected error")
	}
	if ve, ok := err.(*ir.Error); !ok || ve.Code != "PRIMARY_COUNT" {
		t.Fatalf("got %v", err)
	}
}

func TestValidateActionParam(t *testing.T) {
	o := validMinimal()
	o.Actions = []ir.ActionType{{
		Name: "DoThing",
		Fields: []ir.Field{
			{Name: "x", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
		},
	}}
	err := ir.Validate(o)
	if err == nil {
		t.Fatal("expected error")
	}
	if ve, ok := err.(*ir.Error); !ok || ve.Code != "ACTION_PARAM" {
		t.Fatalf("got %v", err)
	}
}
