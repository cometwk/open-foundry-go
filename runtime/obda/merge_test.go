package obda_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
)

func TestMergeDocuments_SingleReturnedAsIs(t *testing.T) {
	doc, err := obda.Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	got, err := obda.MergeDocuments([]*obda.Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	if got != doc {
		t.Fatal("single document should be returned as-is")
	}
}

func TestMergeDocuments_Empty(t *testing.T) {
	_, err := obda.MergeDocuments(nil)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestMergeDocuments_UnionsModelsAndLinks(t *testing.T) {
	patient, err := obda.Parse([]byte(patientOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	admission, err := obda.Parse([]byte(admittedToOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	got, err := obda.MergeDocuments([]*obda.Document{patient, admission})
	if err != nil {
		t.Fatal(err)
	}
	if got == patient {
		t.Fatal("merged document must be a new value")
	}
	if got.Metadata.Name != "hospital" {
		t.Fatalf("metadata.name=%q", got.Metadata.Name)
	}
	if _, ok := got.Models["Patient"]; !ok {
		t.Fatalf("missing Patient: %#v", got.Models)
	}
	if _, ok := got.Links["AdmittedTo"]; !ok {
		t.Fatalf("missing AdmittedTo: %#v", got.Links)
	}
}

func TestMergeDocuments_DuplicateModel(t *testing.T) {
	a, err := obda.Parse([]byte(patientOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	b, err := obda.Parse([]byte(patientOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.MergeDocuments([]*obda.Document{a, b})
	if !errors.Is(err, spi.ErrInvalidMapping) || !strings.Contains(err.Error(), "duplicate model") {
		t.Fatalf("err=%v", err)
	}
}

func TestMergeDocuments_DuplicateTable(t *testing.T) {
	a, err := obda.Parse([]byte(patientOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(patientOnlyYAML, "Patient:", "Ward:", 1)
	raw = strings.Replace(raw, "name: hospital", "name: wards", 1)
	b, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.MergeDocuments([]*obda.Document{a, b})
	if !errors.Is(err, spi.ErrInvalidMapping) || !strings.Contains(err.Error(), "duplicate relation table") {
		t.Fatalf("err=%v", err)
	}
}

func TestMergeDocuments_SchemaMismatch(t *testing.T) {
	a, err := obda.Parse([]byte(patientOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(admittedToOnlyYAML, "schema:\n  namespace: nhs.acute", "schema:\n  namespace: other.ns", 1)
	b, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.MergeDocuments([]*obda.Document{a, b})
	if !errors.Is(err, spi.ErrInvalidMapping) || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileAll_CrossFileInline(t *testing.T) {
	models, err := obda.Parse([]byte(libraryModelsYAML))
	if err != nil {
		t.Fatal(err)
	}
	links, err := obda.Parse([]byte(libraryInlineLinkYAML))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obda.Compile(librarySchema(t, identityOnlyOwnedBy(), false), links); err == nil {
		t.Fatal("per-file compile of inline link should fail without host model")
	}
	compiled, err := obda.CompileAll(librarySchema(t, identityOnlyOwnedBy(), false), []*obda.Document{models, links})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Models["Book"] == nil || compiled.Models["Member"] == nil {
		t.Fatalf("models=%v", compiled.Models)
	}
	link := compiled.Links["OwnedBy"]
	if link == nil || !link.Inline {
		t.Fatalf("OwnedBy=%#v", link)
	}
	if link.HostModel != "Book" || link.FKColumn != "owner_id" {
		t.Fatalf("host=%q fk=%q", link.HostModel, link.FKColumn)
	}
}

const patientOnlyYAML = `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata:
  name: hospital
  namespace: nhs.acute
  version: 1
schema:
  namespace: nhs.acute
  version: 1
models:
  Patient:
    relation:
      kind: table
      name: patient
    access: readWrite
    identity:
      strategy: direct
      columns: [id]
      insert: generated
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
    fields:
      name:
        column: patient_name
`

const admittedToOnlyYAML = `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata:
  name: admissions
  namespace: nhs.acute
  version: 1
schema:
  namespace: nhs.acute
  version: 1
links:
  AdmittedTo:
    relation:
      kind: table
      name: admission
    access: readWrite
    identity:
      strategy: direct
      columns: [id]
      insert: generated
    from:
      object: Patient
      columns: [patient_id]
    to:
      object: Ward
      columns: [ward_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
`

const libraryModelsYAML = `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: library-models, namespace: example.library, version: 1}
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
`

const libraryInlineLinkYAML = `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: library-links, namespace: example.library, version: 1}
schema: {namespace: example.library, version: 1}
links:
  OwnedBy:
    relation: {kind: inline}
    access: readWrite
    from: {object: Book}
    to: {object: Member, columns: [owner_id]}
`
