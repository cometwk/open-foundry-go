package obda_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
)

func TestParseValidDirectNative(t *testing.T) {
	doc, err := obda.Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := obda.Validate(doc); err != nil {
		t.Fatal(err)
	}
	if doc.Models["Patient"].Relation.Catalog != "" {
		t.Fatalf("catalog=%q want empty", doc.Models["Patient"].Relation.Catalog)
	}
}

func TestParseRejectsPlaintextDSN(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: x}
models:
  Patient:
    relation: {kind: table, name: patient}
    dsn: file:secret.db
`
	_, err := obda.Parse([]byte(raw))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseRejectsPassword(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: x}
models:
  Patient:
    relation: {kind: table, name: patient}
    password: hunter2
`
	_, err := obda.Parse([]byte(raw))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseRejectsTopLevelSources(t *testing.T) {
	raw := strings.Replace(validYAML, "models:", "sources:\n  primary: {}\nmodels:", 1)
	_, err := obda.Parse([]byte(raw))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "sources") {
		t.Fatalf("err=%v, want sources removed", err)
	}
}

func TestParseRejectsModelSourceRef(t *testing.T) {
	raw := strings.Replace(validYAML, "  Patient:\n    relation:", "  Patient:\n    sourceRef: primary\n    relation:", 1)
	_, err := obda.Parse([]byte(raw))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "sourceRef") {
		t.Fatalf("err=%v, want sourceRef removed", err)
	}
}

func TestParseRejectsLinkSourceRef(t *testing.T) {
	raw := strings.Replace(validYAML, "  AdmittedTo:\n    relation:", "  AdmittedTo:\n    sourceRef: primary\n    relation:", 1)
	_, err := obda.Parse([]byte(raw))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "sourceRef") {
		t.Fatalf("err=%v, want sourceRef removed", err)
	}
}

func TestParseAllowsFieldNamedSources(t *testing.T) {
	raw := strings.Replace(validYAML, "      name:\n        column: patient_name", "      sources:\n        column: patient_name", 1)
	if _, err := obda.Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestParseAllowsFieldNamedSourceRef(t *testing.T) {
	raw := strings.Replace(validYAML, "      name:\n        column: patient_name", "      sourceRef:\n        column: patient_name", 1)
	if _, err := obda.Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

const validYAML = `
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
