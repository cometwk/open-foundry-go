package sqliteobda_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
)

func TestApplySchemaEmptyDatabaseFails(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema())
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v want ErrInvalidMapping", err)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Patient", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v want ErrMappingNotActive", err)
	}
	assertNoOfTables(t, db)
}

func TestApplySchemaGeneratedDDLNotExecutedFails(t *testing.T) {
	p, _ := openProvider(t, testdata(t, "patient.obda.yaml"))
	compiled := compileMapping(t, testdata(t, "patient.obda.yaml"), patientSchema())
	stmts, err := sqlitedialect.MappedTableStatements(compiled)
	if err != nil || len(stmts) == 0 {
		t.Fatalf("stmts=%v err=%v", stmts, err)
	}
	_, err = p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema())
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v want ErrInvalidMapping", err)
	}
}

func TestApplySchemaAfterHelperSucceeds(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustInit(t, db, testdata(t, "patient.obda.yaml"), patientSchema())
	res, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.ToVersion != 1 {
		t.Fatalf("%+v", res)
	}
	got, err := p.GetSchema(spi.RequestContext{TenantID: "t1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ObjectTypes) != 1 || got.ObjectTypes[0].Name != "Patient" {
		t.Fatalf("%+v", got)
	}
	other := 999
	if _, err := p.GetSchema(spi.RequestContext{TenantID: "t1"}, &other); !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
	assertNoOfTables(t, db)
}

func TestApplySchemaMissingUniqueFails(t *testing.T) {
	p, db := openProvider(t, testdata(t, "hospital.obda.yaml"))
	mustExec(t, db, `CREATE TABLE patient (id TEXT PRIMARY KEY, tenant_id TEXT, patient_name TEXT, version INTEGER, created_at TEXT, updated_at TEXT, deleted_at TEXT)`)
	mustExec(t, db, `CREATE TABLE ward (id TEXT PRIMARY KEY, tenant_id TEXT, ward_name TEXT, version INTEGER, created_at TEXT, updated_at TEXT, deleted_at TEXT)`)
	mustExec(t, db, `CREATE TABLE admission (id TEXT PRIMARY KEY, tenant_id TEXT, from_id TEXT, to_id TEXT, version INTEGER, created_at TEXT, updated_at TEXT, deleted_at TEXT)`)
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, hospitalSchema(spi.CardinalityManyToOne))
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Patient", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplySchemaMissingColumnIsDrift(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustExec(t, db, `CREATE TABLE patient (id TEXT PRIMARY KEY, tenant_id TEXT)`)
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema())
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
}

func TestCreateBeforeActivate(t *testing.T) {
	p, _ := openProvider(t, testdata(t, "patient.obda.yaml"))
	_, err := p.CreateObject(spi.RequestContext{TenantID: "t1"}, "Patient", nil)
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Patient", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplySchemaRequiresTenant(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustInit(t, db, testdata(t, "patient.obda.yaml"), patientSchema())
	_, err := p.ApplySchema(spi.RequestContext{}, patientSchema())
	if !errors.Is(err, spi.ErrTenantRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenRejectsMySQLDialect(t *testing.T) {
	db := openDB(t)
	raw := testdata(t, "patient.obda.yaml")
	raw = []byte(strings.Replace(string(raw), "dialect: sqlite", "dialect: mysql", 1))
	_, err := sqliteobda.Open(db, raw, sqliteobda.Options{DSNRefs: map[string]string{"secret://hospital/sqlite-dsn": "x"}})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestHealthCheckOmitsPath(t *testing.T) {
	p, _ := openProvider(t, testdata(t, "patient.obda.yaml"))
	st, err := p.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range st.Details {
		s, _ := v.(string)
		if s != "" && (strings.Contains(s, ".db") || strings.Contains(s, "Temp")) {
			t.Fatalf("details leaked path: %#v", st.Details)
		}
	}
}

func TestHealthCheckDriftFailClosed(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustInit(t, db, testdata(t, "patient.obda.yaml"), patientSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `ALTER TABLE patient ADD COLUMN extra TEXT`)
	st, err := p.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if st.Healthy {
		t.Fatal("expected degraded health after drift")
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Patient", "x")
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v", err)
	}
}

func patientSchema() spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Patient", Properties: []spi.PropertyDefinition{
				{Name: "name", Type: "String"},
			}},
		},
	}
}

func hospitalSchema(card spi.Cardinality) spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Patient", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}},
			{Name: "Ward", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}},
		},
		LinkTypes: []spi.LinkTypeDefinition{{
			Name: "AdmittedTo", FromType: "Patient", ToType: "Ward", Cardinality: card,
		}},
	}
}

func compileMapping(t *testing.T, mapping []byte, schema spi.OntologySchema) *obda.Compiled {
	t.Helper()
	doc, err := obda.Parse(mapping)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(schema, doc)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func mustInit(t *testing.T, db *sql.DB, mapping []byte, schema spi.OntologySchema) {
	t.Helper()
	if err := sqliteobda.InitMappedSchema(db, compileMapping(t, mapping, schema)); err != nil {
		t.Fatal(err)
	}
}

func assertNoOfTables(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type IN ('table','index','view') AND name LIKE 'of_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if len(names) != 0 {
		t.Fatalf("sidecar objects present: %v", names)
	}
}

func openProvider(t *testing.T, mapping []byte) (*sqliteobda.Provider, *sql.DB) {
	t.Helper()
	db := openDB(t)
	p, err := sqliteobda.Open(db, mapping, sqliteobda.Options{
		DSNRefs: map[string]string{"secret://hospital/sqlite-dsn": "ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, db
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db")+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
