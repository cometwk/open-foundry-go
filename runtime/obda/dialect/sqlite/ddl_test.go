package sqlite_test

import (
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/spi"
)

func TestMappedTableStatementsNoOfPrefix(t *testing.T) {
	stmts, err := sqlitedialect.MappedTableStatements(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if strings.Contains(joined, "of_") {
		t.Fatalf("sidecar leaked: %s", joined)
	}
	if !strings.Contains(joined, `CREATE TABLE IF NOT EXISTS "patient"`) {
		t.Fatalf("missing patient table: %s", joined)
	}
	if !strings.Contains(joined, `CREATE TABLE IF NOT EXISTS "admission"`) {
		t.Fatalf("missing admission table: %s", joined)
	}
}

func TestMappedTableStatementsManyToOnePartialUnique(t *testing.T) {
	stmts, err := sqlitedialect.MappedTableStatements(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, `CREATE UNIQUE INDEX IF NOT EXISTS "admission_from_active"`) {
		t.Fatalf("missing unique: %s", joined)
	}
	if !strings.Contains(joined, `ON "admission" ("tenant_id", "from_id") WHERE "deleted_at" IS NULL`) {
		t.Fatalf("missing partial unique: %s", joined)
	}
}

func TestMappedTableStatementsOmitDeletedAtUnique(t *testing.T) {
	stmts, err := sqlitedialect.MappedTableStatements(hospitalCompiled(obda.OmitFlags{DeletedAt: true}, spi.CardinalityManyToOne))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if strings.Contains(joined, "deleted_at") {
		t.Fatalf("deleted_at should be omitted: %s", joined)
	}
	if !strings.Contains(joined, `ON "admission" ("tenant_id", "from_id")`) {
		t.Fatalf("missing unique cols: %s", joined)
	}
	if strings.Contains(joined, "WHERE") {
		t.Fatalf("unique must not use deleted_at predicate: %s", joined)
	}
}

func TestMappedTableStatementsRejectsIllegalTable(t *testing.T) {
	c := hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne)
	c.Models["Patient"].Table = "patient;drop"
	if _, err := sqlitedialect.MappedTableStatements(c); err == nil {
		t.Fatal("expected reject")
	}
}

func hospitalCompiled(omit obda.OmitFlags, card spi.Cardinality) *obda.Compiled {
	return &obda.Compiled{
		Models: map[string]*obda.CompiledModel{
			"Patient": {
				Name:            "Patient",
				Table:           "patient",
				IdentityColumns: []string{"id"},
				TenantColumn:    "tenant_id",
				Omit:            omit,
				Fields:          []obda.CompiledField{{Logical: "name", Column: "patient_name"}},
				PropertyTypes:   map[string]string{"name": "String"},
			},
			"Ward": {
				Name:            "Ward",
				Table:           "ward",
				IdentityColumns: []string{"id"},
				TenantColumn:    "tenant_id",
				Omit:            omit,
				Fields:          []obda.CompiledField{{Logical: "name", Column: "ward_name"}},
				PropertyTypes:   map[string]string{"name": "String"},
			},
		},
		Links: map[string]*obda.CompiledLink{
			"AdmittedTo": {
				Name:            "AdmittedTo",
				Table:           "admission",
				IdentityColumns: []string{"id"},
				FromColumns:     []string{"from_id"},
				ToColumns:       []string{"to_id"},
				TenantColumn:    "tenant_id",
				Cardinality:     card,
				Omit:            omit,
			},
		},
	}
}
