package sqlite_test

import (
	"errors"
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

func TestPhysicalSchemaOmitsDialectSyntax(t *testing.T) {
	c := hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne)
	expect := obda.PhysicalSchema(c)
	for _, tbl := range expect.Tables {
		for _, col := range tbl.Columns {
			if col.Name == "of_active" || strings.Contains(strings.ToLower(col.Name), "is null") {
				t.Fatalf("dialect syntax in expectation: %q", col.Name)
			}
		}
	}
	stmts, err := sqlitedialect.MappedTableStatements(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(stmts, "\n"), `WHERE "deleted_at" IS NULL`) {
		t.Fatal("sqlite DDL must still emit partial unique")
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

func TestMappedTableStatementsRejectsInline(t *testing.T) {
	c := hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne)
	c.Links["OwnedBy"] = &obda.CompiledLink{Name: "OwnedBy", Table: "patient", Inline: true, HostModel: "Patient", FKColumn: "owner_id"}
	_, err := sqlitedialect.MappedTableStatements(c)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "OwnedBy") {
		t.Fatalf("err=%v want OwnedBy", err)
	}
}

func TestDropTableStatementsReverseOrder(t *testing.T) {
	stmts, err := sqlitedialect.DropTableStatements(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`DROP TABLE IF EXISTS "admission"`,
		`DROP TABLE IF EXISTS "ward"`,
		`DROP TABLE IF EXISTS "patient"`,
	}
	if len(stmts) != len(want) {
		t.Fatalf("got %v", stmts)
	}
	for i := range want {
		if stmts[i] != want[i] {
			t.Fatalf("stmts[%d]=%q want %q", i, stmts[i], want[i])
		}
	}
}

func TestDropTableStatementsRejectsInline(t *testing.T) {
	c := hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne)
	c.Links["OwnedBy"] = &obda.CompiledLink{Name: "OwnedBy", Table: "patient", Inline: true, HostModel: "Patient", FKColumn: "owner_id"}
	_, err := sqlitedialect.DropTableStatements(c)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
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
