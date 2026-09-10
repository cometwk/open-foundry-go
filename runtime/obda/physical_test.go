package obda_test

import (
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
)

func TestPhysicalSchemaManyToOne(t *testing.T) {
	got := obda.PhysicalSchema(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne))
	patient := mustTable(t, got, "patient")
	if !contains(patient.ColumnNames(), "patient_name") {
		t.Fatalf("patient columns=%v want patient_name", patient.ColumnNames())
	}
	if got, want := patient.ColumnNames(), []string{"id", "tenant_id", "patient_name", "version", "created_at", "updated_at", "deleted_at"}; !eqStrings(got, want) {
		t.Fatalf("patient columns=%v want %v", got, want)
	}
	if !mustColumn(t, patient, "deleted_at").Nullable {
		t.Fatal("deleted_at must be nullable")
	}
	admission := mustTable(t, got, "admission")
	if got, want := admission.ColumnNames(), []string{"id", "tenant_id", "from_id", "to_id", "version", "created_at", "updated_at", "deleted_at"}; !eqStrings(got, want) {
		t.Fatalf("admission columns=%v want %v", got, want)
	}
	if len(admission.Uniques) != 1 {
		t.Fatalf("uniques=%d want 1", len(admission.Uniques))
	}
	u := admission.Uniques[0]
	if !eqStrings(u.Columns, []string{"tenant_id", "from_id"}) {
		t.Fatalf("unique cols=%v", u.Columns)
	}
	if !u.ExcludeSoftDeleted {
		t.Fatal("want ExcludeSoftDeleted")
	}
	blob := strings.Join(append(append([]string{}, patient.ColumnNames()...), admission.ColumnNames()...), " ")
	if strings.Contains(blob, "of_active") || strings.Contains(strings.ToLower(blob), "deleted_at is null") {
		t.Fatalf("dialect syntax leaked: %q", blob)
	}
}

func TestPhysicalSchemaOmitDeletedAt(t *testing.T) {
	got := obda.PhysicalSchema(hospitalCompiled(obda.OmitFlags{DeletedAt: true}, spi.CardinalityManyToOne))
	admission := mustTable(t, got, "admission")
	if contains(admission.ColumnNames(), "deleted_at") {
		t.Fatalf("deleted_at should be omitted: %v", admission.ColumnNames())
	}
	if len(admission.Uniques) != 1 || admission.Uniques[0].ExcludeSoftDeleted {
		t.Fatalf("uniques=%+v", admission.Uniques)
	}
}

func TestPhysicalSchemaOneToOneAndManyToMany(t *testing.T) {
	o2o := mustTable(t, obda.PhysicalSchema(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityOneToOne)), "admission")
	if len(o2o.Uniques) != 2 {
		t.Fatalf("o2o uniques=%d want 2", len(o2o.Uniques))
	}
	if !eqStrings(o2o.Uniques[0].Columns, []string{"tenant_id", "from_id"}) {
		t.Fatalf("o2o from=%v", o2o.Uniques[0].Columns)
	}
	if !eqStrings(o2o.Uniques[1].Columns, []string{"tenant_id", "to_id"}) {
		t.Fatalf("o2o to=%v", o2o.Uniques[1].Columns)
	}
	m2m := mustTable(t, obda.PhysicalSchema(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToMany)), "admission")
	if len(m2m.Uniques) != 0 {
		t.Fatalf("m2m uniques=%+v", m2m.Uniques)
	}
}

func TestPhysicalSchemaNoTenant(t *testing.T) {
	c := hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne)
	c.Links["AdmittedTo"].TenantColumn = ""
	admission := mustTable(t, obda.PhysicalSchema(c), "admission")
	if contains(admission.ColumnNames(), "tenant_id") {
		t.Fatalf("columns=%v", admission.ColumnNames())
	}
	if !eqStrings(admission.Uniques[0].Columns, []string{"from_id"}) {
		t.Fatalf("unique=%v", admission.Uniques[0].Columns)
	}
}

func TestPhysicalSchemaModelsOnlyOmitSystem(t *testing.T) {
	c := &obda.Compiled{
		Models: map[string]*obda.CompiledModel{
			"Widget": {
				Name:            "Widget",
				Table:           "widget",
				IdentityColumns: []string{"id"},
				Omit:            obda.OmitFlags{Version: true, CreatedAt: true},
				Fields:          []obda.CompiledField{{Logical: "name", Column: "name"}},
			},
		},
	}
	got := obda.PhysicalSchema(c)
	if len(got.Tables) != 1 {
		t.Fatalf("tables=%d want 1", len(got.Tables))
	}
	if got, want := got.Tables[0].ColumnNames(), []string{"id", "name", "updated_at", "deleted_at"}; !eqStrings(got, want) {
		t.Fatalf("columns=%v want %v", got, want)
	}
}

func TestPhysicalSchemaFulltextColumns(t *testing.T) {
	c := &obda.Compiled{
		Models: map[string]*obda.CompiledModel{
			"Patient": {
				Name:             "Patient",
				Table:            "patient",
				IdentityColumns:  []string{"id"},
				TenantColumn:      "tenant_id",
				Fields:            []obda.CompiledField{{Logical: "name", Column: "patient_name"}},
				PropertyTypes:     map[string]string{"name": "String"},
				SearchableFields:  []string{"patient_name"},
			},
		},
	}
	got := obda.PhysicalSchema(c)
	patient := mustTable(t, got, "patient")
	if !eqStrings(patient.FulltextColumns, []string{"patient_name"}) {
		t.Fatalf("FulltextColumns=%v want [patient_name]", patient.FulltextColumns)
	}
}

func TestPhysicalSchemaNoFulltextWhenNoSearch(t *testing.T) {
	got := obda.PhysicalSchema(hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne))
	patient := mustTable(t, got, "patient")
	if len(patient.FulltextColumns) != 0 {
		t.Fatalf("FulltextColumns should be empty, got %v", patient.FulltextColumns)
	}
}

func TestPhysicalSchemaInlineM2ONoJunction(t *testing.T) {
	got := obda.PhysicalSchema(libraryInlineCompiled(t, spi.CardinalityManyToOne, true))
	if _, ok := tableNamed(got, "owned_by"); ok {
		t.Fatal("inline must not emit owned_by")
	}
	borrowed := mustTable(t, got, "borrowed_by")
	if !contains(borrowed.ColumnNames(), "from_id") {
		t.Fatalf("borrowed_by=%v", borrowed.ColumnNames())
	}
	book := mustTable(t, got, "book")
	col := mustColumn(t, book, "owner_id")
	if !col.Nullable {
		t.Fatal("optional owner_id must be nullable")
	}
	if len(book.Uniques) != 0 {
		t.Fatalf("M2O inline must not add UNIQUE: %+v", book.Uniques)
	}
}

func TestPhysicalSchemaInlineRequiredNotNull(t *testing.T) {
	book := mustTable(t, obda.PhysicalSchema(libraryInlineCompiled(t, spi.CardinalityManyToOne, false)), "book")
	col := mustColumn(t, book, "owner_id")
	if col.Nullable {
		t.Fatal("required owner_id must be NOT NULL")
	}
}

func TestPhysicalSchemaInlineO2OUniqueOnHost(t *testing.T) {
	book := mustTable(t, obda.PhysicalSchema(libraryInlineCompiled(t, spi.CardinalityOneToOne, true)), "book")
	if len(book.Uniques) != 1 {
		t.Fatalf("uniques=%+v", book.Uniques)
	}
	if !eqStrings(book.Uniques[0].Columns, []string{"tenant_id", "owner_id"}) {
		t.Fatalf("unique=%v", book.Uniques[0].Columns)
	}
	if !book.Uniques[0].ExcludeSoftDeleted {
		t.Fatal("want ExcludeSoftDeleted")
	}
}

func TestPhysicalSchemaInlineO2MNoUnique(t *testing.T) {
	book := mustTable(t, obda.PhysicalSchema(libraryInlineCompiled(t, spi.CardinalityOneToMany, true)), "book")
	if _, ok := columnNamed(book, "owner_id"); !ok {
		t.Fatalf("columns=%v", book.ColumnNames())
	}
	if len(book.Uniques) != 0 {
		t.Fatalf("O2M inline must not add UNIQUE: %+v", book.Uniques)
	}
}

func libraryInlineCompiled(t *testing.T, card spi.Cardinality, nullable bool) *obda.Compiled {
	t.Helper()
	c := &obda.Compiled{
		Models: map[string]*obda.CompiledModel{
			"Book": {
				Name:            "Book",
				Table:           "book",
				IdentityColumns: []string{"id"},
				TenantColumn:    "tenant_id",
				Fields:          []obda.CompiledField{{Logical: "title", Column: "title"}},
			},
			"Member": {
				Name:            "Member",
				Table:           "member",
				IdentityColumns: []string{"id"},
				TenantColumn:    "tenant_id",
				Fields:          []obda.CompiledField{{Logical: "name", Column: "name"}},
			},
		},
		Links: map[string]*obda.CompiledLink{
			"OwnedBy": {
				Name:         "OwnedBy",
				Table:        "book",
				FromObject:   "Book",
				ToObject:     "Member",
				ToColumns:    []string{"owner_id"},
				TenantColumn: "tenant_id",
				Cardinality:  card,
				Inline:       true,
				HostModel:    "Book",
				HostNavField: "owner",
				FKColumn:     "owner_id",
				FKNullable:   nullable,
			},
			"BorrowedBy": {
				Name:            "BorrowedBy",
				Table:           "borrowed_by",
				IdentityColumns: []string{"id"},
				FromColumns:     []string{"from_id"},
				ToColumns:       []string{"to_id"},
				TenantColumn:    "tenant_id",
				Cardinality:     spi.CardinalityManyToOne,
			},
		},
	}
	return c
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

func mustTable(t *testing.T, schema obda.Physical, name string) obda.PhysicalTable {
	t.Helper()
	tbl, ok := tableNamed(schema, name)
	if !ok {
		t.Fatalf("missing table %q in %+v", name, schema.Tables)
	}
	return tbl
}

func tableNamed(schema obda.Physical, name string) (obda.PhysicalTable, bool) {
	for _, tbl := range schema.Tables {
		if tbl.Name == name {
			return tbl, true
		}
	}
	return obda.PhysicalTable{}, false
}

func mustColumn(t *testing.T, tbl obda.PhysicalTable, name string) obda.PhysicalColumn {
	t.Helper()
	col, ok := columnNamed(tbl, name)
	if !ok {
		t.Fatalf("missing column %q in %v", name, tbl.ColumnNames())
	}
	return col
}

func columnNamed(tbl obda.PhysicalTable, name string) (obda.PhysicalColumn, bool) {
	for _, c := range tbl.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return obda.PhysicalColumn{}, false
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
