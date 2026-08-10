package projection_test

import (
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
)

func TestProjectOmitsNonPersistentFields(t *testing.T) {
	o := &ir.Ontology{
		Objects: []ir.ObjectType{{
			Name: "Widget",
			Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty, Flags: ir.FieldFlags{Unique: true, Indexed: true}},
				{Name: "bins", Type: ir.TypeRef{Name: "Bin", IsList: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "InBin"}},
				{Name: "count", Type: ir.TypeRef{Name: "Int"}, Role: ir.RoleComputed, Computed: &ir.ComputedRef{Fn: "countLinks"}},
				{Name: "notes", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty, Flags: ir.FieldFlags{Searchable: true}},
			},
		}},
		Actions: []ir.ActionType{{Name: "CreateWidget", Fields: []ir.Field{{Name: "name", Role: ir.RoleParam}}}},
		Enums:   []ir.EnumType{{Name: "Status", Values: []ir.EnumValue{{Name: "OPEN"}}}},
	}

	schema := projection.ProjectStorage(o)
	if schema.Version != 1 {
		t.Fatalf("version %d", schema.Version)
	}
	if len(schema.ObjectTypes) != 1 {
		t.Fatalf("objects %d", len(schema.ObjectTypes))
	}
	props := map[string]spi.PropertyDefinition{}
	for _, p := range schema.ObjectTypes[0].Properties {
		props[p.Name] = p
	}
	if _, ok := props["id"]; ok {
		t.Fatal("primary should be omitted")
	}
	if _, ok := props["bins"]; ok {
		t.Fatal("link nav should be omitted")
	}
	if _, ok := props["count"]; ok {
		t.Fatal("computed should be omitted")
	}
	if !props["name"].Required {
		t.Fatal("name required")
	}
	if len(schema.ObjectTypes[0].Indexes) < 2 {
		t.Fatalf("indexes: %+v", schema.ObjectTypes[0].Indexes)
	}
	// Actions/enums must not appear on storage schema
	if len(schema.LinkTypes) != 0 {
		t.Fatalf("unexpected links")
	}
}

func TestProjectLinkProperties(t *testing.T) {
	o := &ir.Ontology{
		Links: []ir.LinkType{{
			Name:        "InBin",
			From:        "Widget",
			To:          "Bin",
			Cardinality: ir.CardinalityManyToOne,
			Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "since", Type: ir.TypeRef{Name: "DateTime", NonNull: true}, Role: ir.RoleProperty},
			},
		}},
	}
	schema := projection.ProjectStorage(o)
	if len(schema.LinkTypes) != 1 || len(schema.LinkTypes[0].Properties) != 2 {
		t.Fatalf("%+v", schema.LinkTypes)
	}
}
