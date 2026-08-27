package odl_test

import (
	"strings"
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/odl"
)

func TestLowerObjectRolesAndFlags(t *testing.T) {
	src := `
extend schema @namespace(name: "test", version: "0.1.0")

type Widget @objectType {
  id: ID! @primary
  code: String! @unique @indexed @immutable
  title: String
}
`
	o, err := odl.ParseAndLower(odl.Source{Name: "w.odl", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	if o.Namespace == nil || o.Namespace.Name != "test" {
		t.Fatalf("namespace: %+v", o.Namespace)
	}
	if len(o.Objects) != 1 {
		t.Fatalf("objects: %d", len(o.Objects))
	}
	w := o.Objects[0]
	byName := map[string]ir.Field{}
	for _, f := range w.Fields {
		byName[f.Name] = f
	}
	if byName["id"].Role != ir.RolePrimary {
		t.Fatalf("id role: %v", byName["id"].Role)
	}
	if byName["code"].Role != ir.RoleProperty || !byName["code"].Flags.Unique || !byName["code"].Flags.Indexed || !byName["code"].Flags.Immutable {
		t.Fatalf("code flags: %+v", byName["code"])
	}
	if !byName["code"].Type.NonNull || byName["code"].Type.Name != "String" {
		t.Fatalf("code type: %+v", byName["code"].Type)
	}
}

func TestLowerLinkType(t *testing.T) {
	src := `
type Widget @objectType { id: ID! @primary }
type Bin @objectType { id: ID! @primary }
type InBin @linkType(from: "Widget", to: "Bin", cardinality: MANY_TO_ONE) {
  id: ID! @primary
  since: DateTime!
}
`
	o, err := odl.ParseAndLower(odl.Source{Name: "l.odl", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Links) != 1 {
		t.Fatalf("links: %d objects=%d", len(o.Links), len(o.Objects))
	}
	l := o.Links[0]
	if l.Name != "InBin" || l.From != "Widget" || l.To != "Bin" || l.Cardinality != ir.CardinalityManyToOne {
		t.Fatalf("%+v", l)
	}
}

func TestLowerActionParams(t *testing.T) {
	src := `
type Widget @objectType { id: ID! @primary }
type CreateWidget @actionType {
  name: String! @param
  notes: String @param
}
`
	o, err := odl.ParseAndLower(odl.Source{Name: "a.odl", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Actions) != 1 {
		t.Fatalf("actions: %d", len(o.Actions))
	}
	for _, f := range o.Actions[0].Fields {
		if f.Role != ir.RoleParam {
			t.Fatalf("field %s role %v", f.Name, f.Role)
		}
	}
}

func TestLowerLinkNavAndComputed(t *testing.T) {
	src := `
type Widget @objectType {
  id: ID! @primary
  bins: [Bin!]! @link(type: "InBin", direction: OUTBOUND)
  count: Int @computed(fn: "countLinks", args: { type: "InBin" }, cache: LAZY)
}
type Bin @objectType { id: ID! @primary }
type InBin @linkType(from: "Widget", to: "Bin", cardinality: MANY_TO_MANY) {
  id: ID! @primary
}
`
	o, err := odl.ParseAndLower(odl.Source{Name: "c.odl", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	w := o.Objects[0]
	byName := map[string]ir.Field{}
	for _, f := range w.Fields {
		byName[f.Name] = f
	}
	if byName["bins"].Role != ir.RoleLinkNav || byName["bins"].Link == nil || byName["bins"].Link.Type != "InBin" {
		t.Fatalf("bins: %+v", byName["bins"])
	}
	if byName["count"].Role != ir.RoleComputed || byName["count"].Computed == nil || byName["count"].Computed.Fn != "countLinks" {
		t.Fatalf("count: %+v", byName["count"])
	}
}

func TestParseSyntaxError(t *testing.T) {
	_, err := odl.ParseAndLower(odl.Source{Name: "bad.odl", Content: "type Widget @objectType {"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad.odl") && !strings.Contains(err.Error(), "parse") {
		t.Fatalf("error should mention parse/source: %v", err)
	}
}
