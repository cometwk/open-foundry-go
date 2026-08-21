package obda_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
)

func TestValidateReadWriteView(t *testing.T) {
	raw := strings.Replace(validYAML, "kind: table\n      name: patient", "kind: view\n      name: patient", 1)
	mustInvalid(t, raw)
}

func TestValidateHashTransform(t *testing.T) {
	raw := strings.Replace(validYAML, "column: patient_name", "column: patient_name\n        transform:\n          kind: hash", 1)
	mustInvalid(t, raw)
}

func TestValidateMissingSourceRef(t *testing.T) {
	raw := strings.Replace(validYAML, "sourceRef: primary\n    relation:\n      kind: table\n      name: patient", "relation:\n      kind: table\n      name: patient", 1)
	mustInvalid(t, raw)
}

func TestValidateEmptyIdentityColumns(t *testing.T) {
	raw := strings.Replace(validYAML, "strategy: sidecar\n      columns: [patient_id]", "strategy: sidecar\n      columns: []", 1)
	mustInvalid(t, raw)
}

func TestValidateViewWithoutTenant(t *testing.T) {
	raw := `
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata: {name: v}
sources:
  primary:
    dialect: sqlite
    connection: {dsnRef: secret://x}
models:
  Patient:
    sourceRef: primary
    relation: {kind: view, name: patient_v}
    access: read
    identity: {strategy: sidecar, columns: [id]}
    system: {strategy: sidecar}
`
	mustInvalid(t, raw)
}

func TestValidateTenantConnection(t *testing.T) {
	raw := strings.Replace(validYAML, "strategy: column\n      column: tenant_id", "strategy: connection", 1)
	mustInvalid(t, raw)
}

func TestValidateCatalogHospital(t *testing.T) {
	raw := strings.Replace(validYAML, "kind: table\n      name: patient", "kind: table\n      catalog: hospital\n      name: patient", 1)
	mustInvalid(t, raw)
}

func TestValidateCatalogMainOK(t *testing.T) {
	raw := strings.Replace(validYAML, "kind: table\n      name: patient", "kind: table\n      catalog: main\n      name: patient", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := obda.Validate(doc); err != nil {
		t.Fatal(err)
	}
}

func TestSentinelsAreDistinct(t *testing.T) {
	if errors.Is(spi.ErrReadOnlyMapping, spi.ErrInvalidMapping) {
		t.Fatal("sentinels must not wrap each other")
	}
	if !errors.Is(spi.ErrInvalidMapping, spi.ErrInvalidMapping) {
		t.Fatal("errors.Is self")
	}
}

func mustInvalid(t *testing.T, raw string) {
	t.Helper()
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		if !errors.Is(err, spi.ErrInvalidMapping) {
			t.Fatalf("parse err=%v want ErrInvalidMapping", err)
		}
		return
	}
	err = obda.Validate(doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("validate err=%v want ErrInvalidMapping", err)
	}
}
