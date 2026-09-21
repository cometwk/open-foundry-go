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

func schemaWithOwnerID(t *testing.T, required, navNonNull bool) spi.OntologySchema {
	t.Helper()
	s := librarySchema(t, identityOnlyOwnedBy(), navNonNull)
	s.ObjectTypes[0].Properties = append(s.ObjectTypes[0].Properties, spi.PropertyDefinition{
		Name: "ownerId", Type: "ID", Required: required,
	})
	return s
}

func TestCompileInlineScalarProjection(t *testing.T) {
	doc, err := obda.Parse([]byte(libraryInlineYAML))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(schemaWithOwnerID(t, false, false), doc)
	if err != nil {
		t.Fatal(err)
	}
	l := compiled.Links["OwnedBy"]
	if l.ScalarField != "ownerId" {
		t.Fatalf("scalar=%q", l.ScalarField)
	}
	book := compiled.Models["Book"]
	if _, ok := book.FieldByLogical["ownerId"]; ok {
		t.Fatal("projection must not enter Fields")
	}
	if len(book.InlineFKs) != 1 || book.InlineFKs[0].Logical != "ownerId" || book.InlineFKs[0].Column != "owner_id" {
		t.Fatalf("InlineFKs=%+v", book.InlineFKs)
	}
	if err := book.RejectProjectionWrites(map[string]any{"ownerId": "x"}); !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("write reject err=%v", err)
	}
	if err := book.RejectProjectionWrites(map[string]any{"title": "t"}); err != nil {
		t.Fatal(err)
	}
}

func TestCompileInlineScalarOverride(t *testing.T) {
	raw := libraryInlineYAML + "    scalarField: registeredOwner\n"
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	s := librarySchema(t, identityOnlyOwnedBy(), false)
	s.ObjectTypes[0].Properties = append(s.ObjectTypes[0].Properties, spi.PropertyDefinition{
		Name: "registeredOwner", Type: "ID",
	})
	compiled, err := obda.Compile(s, doc)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Links["OwnedBy"].ScalarField != "registeredOwner" {
		t.Fatal(compiled.Links["OwnedBy"].ScalarField)
	}
}

func TestCompileInlineNoScalarOK(t *testing.T) {
	doc, err := obda.Parse([]byte(libraryInlineYAML))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), doc)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Links["OwnedBy"].ScalarField != "" {
		t.Fatal("want empty scalar")
	}
	fks := compiled.Models["Book"].InlineFKs
	if len(fks) != 1 || fks[0].Logical != "" || fks[0].Column != "owner_id" {
		t.Fatalf("InlineFKs=%+v", fks)
	}
}

func TestCompileInlineProjectScalarFalseUnmapped(t *testing.T) {
	raw := libraryInlineYAML + "    projectScalar: false\n"
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(schemaWithOwnerID(t, false, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineScalarNullabilityMismatch(t *testing.T) {
	doc, err := obda.Parse([]byte(libraryInlineYAML))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(schemaWithOwnerID(t, true, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineScalarAndProjectFalse(t *testing.T) {
	raw := libraryInlineYAML + "    scalarField: ownerId\n    projectScalar: false\n"
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(schemaWithOwnerID(t, false, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineDuplicateScalarProjection(t *testing.T) {
	raw := libraryInlineYAML + `  Other:
    relation: {kind: inline}
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [other_id]}
    scalarField: ownerId
`
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	s := schemaWithOwnerID(t, false, false)
	s.ObjectTypes[0].Navigations = append(s.ObjectTypes[0].Navigations, spi.LinkNavigation{
		Field: "other", LinkType: "Other", Direction: "OUTBOUND",
	})
	s.LinkTypes = append(s.LinkTypes, spi.LinkTypeDefinition{
		Name: "Other", FromType: "Book", ToType: "Member",
		Cardinality: spi.CardinalityManyToOne,
		Properties:  []spi.PropertyDefinition{{Name: "id", Type: "ID", Required: true}},
	})
	_, err = obda.Compile(s, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineScalarCollidesWithField(t *testing.T) {
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
    fields: {title: {column: title}, ownerId: {column: title}}
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
	_, err = obda.Compile(schemaWithOwnerID(t, false, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineScalarMissingWhenNamed(t *testing.T) {
	raw := libraryInlineYAML + "    scalarField: registeredOwner\n"
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineFieldsMapFKColumn(t *testing.T) {
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
    fields: {title: {column: title}, ownerId: {column: owner_id}}
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
	_, err = obda.Compile(schemaWithOwnerID(t, false, false), doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileInlineSearchFieldsRejectProjection(t *testing.T) {
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
    search: {fields: [ownerId]}
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
	_, err = obda.Compile(schemaWithOwnerID(t, false, false), doc)
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
