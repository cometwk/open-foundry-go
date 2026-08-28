package mysql_test

import (
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/spi"
)

func compiledFixture(card spi.Cardinality) *obda.Compiled {
	return &obda.Compiled{
		Models: map[string]*obda.CompiledModel{
			"Patient": {
				Name:            "Patient",
				Table:           "patient",
				IdentityColumns: []string{"id"},
				TenantColumn:    "tenant_id",
				Fields: []obda.CompiledField{
					{Logical: "name", Column: "patient_name"},
					{Logical: "age", Column: "age"},
				},
				PropertyTypes: map[string]string{"name": "String", "age": "Integer"},
			},
		},
		Links: map[string]*obda.CompiledLink{
			"AdmittedTo": {
				Name:            "AdmittedTo",
				Table:           "admission",
				IdentityColumns: []string{"id"},
				FromObject:      "Patient",
				FromColumns:     []string{"from_id"},
				ToObject:        "Ward",
				ToColumns:       []string{"to_id"},
				TenantColumn:    "tenant_id",
				Cardinality:     card,
			},
		},
	}
}

func TestMappedTableStatementsModel(t *testing.T) {
	stmts, err := mysqldialect.MappedTableStatements(compiledFixture(spi.CardinalityManyToOne))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) < 3 {
		t.Fatalf("stmts=%v", stmts)
	}
	var create string
	for _, s := range stmts {
		if strings.HasPrefix(s, "CREATE TABLE IF NOT EXISTS `patient`") {
			create = s
		}
	}
	if create == "" {
		t.Fatalf("patient CREATE missing: %v", stmts)
	}
	for _, want := range []string{
		"`id` VARCHAR(255) PRIMARY KEY",
		"`patient_name` TEXT",
		"`age` BIGINT",
		"`version` BIGINT NOT NULL DEFAULT 1",
		"`deleted_at` VARCHAR(64)",
		"ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
	} {
		if !strings.Contains(create, want) {
			t.Fatalf("patient CREATE missing %q:\n%s", want, create)
		}
	}
	if strings.Contains(create, "of_active") {
		t.Fatalf("model table must not carry of_active:\n%s", create)
	}
}

func TestMappedTableStatementsLinkActiveKey(t *testing.T) {
	stmts, err := mysqldialect.MappedTableStatements(compiledFixture(spi.CardinalityManyToOne))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	for _, want := range []string{
		"GENERATED ALWAYS AS (IF(`deleted_at` IS NULL, 1, NULL)) VIRTUAL",
		"CREATE UNIQUE INDEX `admission_from_active`",
		"(`tenant_id`, `from_id`, `of_active`)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stmts missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "WHERE `deleted_at` IS NULL") {
		t.Fatalf("MySQL DDL must not emit sqlite partial-index predicates:\n%s", joined)
	}
}

func TestMappedTableStatementsOmitDeletedAtDropsActiveKey(t *testing.T) {
	c := compiledFixture(spi.CardinalityManyToOne)
	link := c.Links["AdmittedTo"]
	link.Omit = obda.OmitFlags{DeletedAt: true}
	stmts, err := mysqldialect.MappedTableStatements(c)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if strings.Contains(joined, "of_active") {
		t.Fatalf("omitted deletedAt must not emit of_active:\n%s", joined)
	}
	if !strings.Contains(joined, "CREATE UNIQUE INDEX `admission_from_active`\n  ON `admission` (`tenant_id`, `from_id`)") {
		t.Fatalf("plain unique index missing:\n%s", joined)
	}
}

func TestMappedTableStatementsManyToManyHasNoIndex(t *testing.T) {
	stmts, err := mysqldialect.MappedTableStatements(compiledFixture(spi.CardinalityManyToMany))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if strings.Contains(s, "UNIQUE INDEX") {
			t.Fatalf("ManyToMany must not emit unique indexes: %v", stmts)
		}
		if strings.Contains(s, "of_active") && strings.HasPrefix(s, "CREATE TABLE") {
			t.Fatalf("ManyToMany must not emit of_active: %v", stmts)
		}
	}
}

func TestHasUniqueIndex(t *testing.T) {
	indexes := []mysqldialect.Index{
		{Name: "PRIMARY", Unique: true, Columns: []string{"id"}},
		{Name: "admission_from_active", Unique: true, Columns: []string{"tenant_id", "from_id", "of_active"}},
		{Name: "plain", Unique: false, Columns: []string{"tenant_id", "from_id", "of_active"}},
	}
	if !mysqldialect.HasUniqueIndex(indexes, []string{"tenant_id", "from_id"}, true) {
		t.Fatal("active-key unique index should match")
	}
	if mysqldialect.HasUniqueIndex(indexes, []string{"tenant_id", "from_id"}, false) {
		t.Fatal("plain columns must not match when index carries of_active")
	}
	if mysqldialect.HasUniqueIndex(indexes[:1], []string{"tenant_id", "from_id"}, true) {
		t.Fatal("missing index must not match")
	}
}
