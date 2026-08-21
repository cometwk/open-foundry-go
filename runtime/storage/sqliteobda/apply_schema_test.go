package sqliteobda_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
)

func TestApplySchemaActivates(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustExec(t, db, `CREATE TABLE patient (patient_id TEXT, tenant_id TEXT, patient_name TEXT)`)
	schema := patientSchema()
	ctx := spi.RequestContext{TenantID: "t1"}
	res, err := p.ApplySchema(ctx, schema)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.ToVersion != 1 {
		t.Fatalf("%+v", res)
	}
	got, err := p.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ObjectTypes) != 1 || got.ObjectTypes[0].Name != "Patient" {
		t.Fatalf("%+v", got)
	}
	var cols int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('patient')`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 3 {
		t.Fatalf("business table altered: cols=%d", cols)
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
	mustExec(t, db, `CREATE TABLE patient (patient_id TEXT, tenant_id TEXT, patient_name TEXT)`)
	_, err := p.ApplySchema(spi.RequestContext{}, spi.OntologySchema{Version: 1, ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient"}}})
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

func TestBackfillCopiesBusinessTenant(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustExec(t, db, `CREATE TABLE patient (patient_id TEXT, tenant_id TEXT, patient_name TEXT)`)
	mustExec(t, db, `INSERT INTO patient VALUES ('p1','alpha','Ada'),('p2','beta','Bob')`)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "intruder"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT tenant_id FROM of_object_meta ORDER BY physical_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tenants []string
	for rows.Next() {
		var tnt string
		if err := rows.Scan(&tnt); err != nil {
			t.Fatal(err)
		}
		tenants = append(tenants, tnt)
	}
	if len(tenants) != 2 || tenants[0] != "alpha" || tenants[1] != "beta" {
		t.Fatalf("tenants=%v", tenants)
	}
}

func TestApplySchemaCASConflict(t *testing.T) {
	db := openDB(t)
	mustExec(t, db, `CREATE TABLE patient (patient_id TEXT, tenant_id TEXT, patient_name TEXT)`)
	raw := testdata(t, "patient.obda.yaml")
	opts := sqliteobda.Options{DSNRefs: map[string]string{"secret://hospital/sqlite-dsn": "ignored"}}
	p1, err := sqliteobda.Open(db, raw, opts)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := sqliteobda.Open(db, raw, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p1.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	_, err = p2.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema())
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("loser err=%v", err)
	}
}

func TestHealthCheckDriftFailClosed(t *testing.T) {
	p, db := openProvider(t, testdata(t, "patient.obda.yaml"))
	mustExec(t, db, `CREATE TABLE patient (patient_id TEXT, tenant_id TEXT, patient_name TEXT)`)
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
				{Name: "patientId", Type: "String"},
				{Name: "name", Type: "String"},
			}},
		},
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
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
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

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}
