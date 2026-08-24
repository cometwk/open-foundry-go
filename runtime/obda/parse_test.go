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
	if doc.Sources["primary"].Dialect != "sqlite" {
		t.Fatalf("dialect=%q", doc.Sources["primary"].Dialect)
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
sources:
  primary:
    dialect: sqlite
    connection:
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
sources:
  primary:
    dialect: sqlite
    connection:
      dsnRef: secret://x
      password: hunter2
`
	_, err := obda.Parse([]byte(raw))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseDialectMySQLIsOpaque(t *testing.T) {
	raw := strings.Replace(validYAML, "dialect: sqlite", "dialect: mysql", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := obda.Validate(doc); err != nil {
		t.Fatal(err)
	}
	if doc.Sources["primary"].Dialect != "mysql" {
		t.Fatalf("dialect=%q", doc.Sources["primary"].Dialect)
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
sources:
  primary:
    kind: sql
    dialect: sqlite
    connection:
      dsnRef: secret://hospital/sqlite-dsn
models:
  Patient:
    sourceRef: primary
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
    sourceRef: primary
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
