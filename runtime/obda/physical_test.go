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
	if !contains(patient.Columns, "patient_name") {
		t.Fatalf("patient columns=%v want patient_name", patient.Columns)
	}
	if got, want := patient.Columns, []string{"id", "tenant_id", "patient_name", "version", "created_at", "updated_at", "deleted_at"}; !eqStrings(got, want) {
		t.Fatalf("patient columns=%v want %v", got, want)
	}
	admission := mustTable(t, got, "admission")
	if got, want := admission.Columns, []string{"id", "tenant_id", "from_id", "to_id", "version", "created_at", "updated_at", "deleted_at"}; !eqStrings(got, want) {
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
	blob := strings.Join(append(append([]string{}, patient.Columns...), admission.Columns...), " ")
	if strings.Contains(blob, "of_active") || strings.Contains(strings.ToLower(blob), "deleted_at is null") {
		t.Fatalf("dialect syntax leaked: %q", blob)
	}
}

func TestPhysicalSchemaOmitDeletedAt(t *testing.T) {
	got := obda.PhysicalSchema(hospitalCompiled(obda.OmitFlags{DeletedAt: true}, spi.CardinalityManyToOne))
	admission := mustTable(t, got, "admission")
	if contains(admission.Columns, "deleted_at") {
		t.Fatalf("deleted_at should be omitted: %v", admission.Columns)
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
	if contains(admission.Columns, "tenant_id") {
		t.Fatalf("columns=%v", admission.Columns)
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
	if got, want := got.Tables[0].Columns, []string{"id", "name", "updated_at", "deleted_at"}; !eqStrings(got, want) {
		t.Fatalf("columns=%v want %v", got, want)
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

func mustTable(t *testing.T, schema obda.Physical, name string) obda.PhysicalTable {
	t.Helper()
	for _, tbl := range schema.Tables {
		if tbl.Name == name {
			return tbl
		}
	}
	t.Fatalf("missing table %q in %+v", name, schema.Tables)
	return obda.PhysicalTable{}
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
