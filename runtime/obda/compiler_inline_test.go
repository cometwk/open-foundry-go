package obda_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
)

const libraryInlineYAML = `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: library, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
models:
  Book:
    relation: {kind: table, name: book}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields:
      title: {column: title}
  Member:
    relation: {kind: table, name: member}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields:
      name: {column: name}
links:
  OwnedBy:
    relation: {kind: inline}
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [owner_id]}
`

func librarySchema(t *testing.T, link spi.LinkTypeDefinition, ownerNonNull bool) spi.OntologySchema {
	t.Helper()
	return spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{
			{
				Name: "Book",
				Properties: []spi.PropertyDefinition{
					{Name: "title", Type: "String", Required: true},
				},
				Navigations: []spi.LinkNavigation{{
					Field: "owner", LinkType: "OwnedBy", Direction: "OUTBOUND", NonNull: ownerNonNull,
				}},
			},
			{
				Name: "Member",
				Properties: []spi.PropertyDefinition{
					{Name: "name", Type: "String", Required: true},
				},
				Navigations: []spi.LinkNavigation{{
					Field: "ownedBooks", LinkType: "OwnedBy", Direction: "INBOUND", NonNull: true,
				}},
			},
		},
		LinkTypes: []spi.LinkTypeDefinition{link},
	}
}

func identityOnlyOwnedBy() spi.LinkTypeDefinition {
	return spi.LinkTypeDefinition{
		Name:        "OwnedBy",
		FromType:    "Book",
		ToType:      "Member",
		Cardinality: spi.CardinalityManyToOne,
		Properties:  []spi.PropertyDefinition{{Name: "id", Type: "ID", Required: true}},
	}
}

func TestCompileInlineOmitKind(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: library, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
models:
  Book:
    relation: {kind: table, name: book}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {title: {column: title}}
  Member:
    relation: {kind: table, name: member}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {name: {column: name}}
links:
  OwnedBy:
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [owner_id]}
`
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), doc)
	if err != nil {
		t.Fatal(err)
	}
	l := compiled.Links["OwnedBy"]
	if !l.Inline || l.HostModel != "Book" || l.FKColumn != "owner_id" || l.Table != "book" {
		t.Fatalf("%+v", l)
	}
	if !l.FKNullable || l.HostNavField != "owner" {
		t.Fatalf("nav %+v", l)
	}
}

func TestCompileInlineExplicitKindEquivalent(t *testing.T) {
	doc, err := obda.Parse([]byte(libraryInlineYAML))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), doc)
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.Links["OwnedBy"].Inline {
		t.Fatal("want inline")
	}
}

func TestCompileInlineKindTableStaysJunction(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: library, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
models:
  Book:
    relation: {kind: table, name: book}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {title: {column: title}}
  Member:
    relation: {kind: table, name: member}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {name: {column: name}}
links:
  OwnedBy:
    relation: {kind: table, name: owned_by}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    from: {object: Book, columns: [from_id]}
    to: {object: Member, columns: [to_id]}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
`
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), doc)
	if err != nil {
		t.Fatal(err)
	}
	l := compiled.Links["OwnedBy"]
	if l.Inline || l.Table != "owned_by" {
		t.Fatalf("%+v", l)
	}
}

func TestCompileInlineRejectsBusinessProperties(t *testing.T) {
	doc, err := obda.Parse([]byte(libraryInlineYAML))
	if err != nil {
		t.Fatal(err)
	}
	def := identityOnlyOwnedBy()
	def.Properties = append(def.Properties, spi.PropertyDefinition{Name: "borrowedAt", Type: "DateTime"})
	_, err = obda.Compile(librarySchema(t, def, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineRejectsManyToMany(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: x, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
models:
  Book:
    relation: {kind: table, name: book}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {title: {column: title}}
  Member:
    relation: {kind: table, name: member}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {name: {column: name}}
links:
  Tagged:
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [tag_id]}
`
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	def := spi.LinkTypeDefinition{
		Name: "Tagged", FromType: "Book", ToType: "Member",
		Cardinality: spi.CardinalityManyToMany,
		Properties:  []spi.PropertyDefinition{{Name: "id", Type: "ID", Required: true}},
	}
	schema := librarySchema(t, def, false)
	schema.ObjectTypes[0].Navigations[0].LinkType = "Tagged"
	schema.LinkTypes = []spi.LinkTypeDefinition{def}
	_, err = obda.Compile(schema, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineRequiredNavNotNullable(t *testing.T) {
	doc, err := obda.Parse([]byte(libraryInlineYAML))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(librarySchema(t, identityOnlyOwnedBy(), true), doc)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Links["OwnedBy"].FKNullable {
		t.Fatal("want NOT NULL")
	}
}

func TestCompileInlineO2ORequiresHost(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: x, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
models:
  Book:
    relation: {kind: table, name: book}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {title: {column: title}}
  Member:
    relation: {kind: table, name: member}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {name: {column: name}}
links:
  OwnedBy:
    relation: {kind: inline}
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [owner_id]}
`
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	def := identityOnlyOwnedBy()
	def.Cardinality = spi.CardinalityOneToOne
	_, err = obda.Compile(librarySchema(t, def, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineFKCollision(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: library, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
models:
  Book:
    relation: {kind: table, name: book}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {title: {column: title}}
  Member:
    relation: {kind: table, name: member}
    access: readWrite
    identity: {strategy: direct, columns: [id], insert: generated}
    tenant: {strategy: column, column: tenant_id}
    system: {strategy: native}
    fields: {name: {column: name}}
links:
  OwnedBy:
    relation: {kind: inline}
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [title]}
`
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateInlineBothEndpoints(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: x}
links:
  OwnedBy:
    relation: {kind: inline}
    access: readWrite
    from: {object: Book, columns: [a]}
    to: {object: Member, columns: [b]}
`
	mustInvalid(t, raw)
}
