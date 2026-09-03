package mysqlobda_test

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
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
	stmts, err := mysqldialect.MappedTableStatements(compiled)
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
		t.Fatalf("err=%v want ErrMappingNotActive", err)
	}
	assertNoOfTables(t, db)
}

func TestApplySchemaMissingUniqueFails(t *testing.T) {
	p, db := openProvider(t, testdata(t, "hospital.obda.yaml"))
	mustExec(t, db, `CREATE TABLE patient (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), patient_name TEXT, version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE ward (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), ward_name TEXT, version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE admission (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), from_id VARCHAR(255), to_id VARCHAR(255), version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE trust (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), trust_name TEXT, version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE ward_trust (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), from_id VARCHAR(255), to_id VARCHAR(255), version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
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
	mustExec(t, db, `CREATE TABLE patient (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255))`)
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

func TestOpenRejectsSQLiteDialect(t *testing.T) {
	db := openDB(t)
	raw := testdata(t, "patient.obda.yaml")
	raw = []byte(strings.Replace(string(raw), "dialect: mysql", "dialect: sqlite", 1))
	_, err := mysqlobda.Open(db, raw, mysqlobda.Options{DSNRefs: map[string]string{"secret://hospital/mysql-dsn": "x"}})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v want ErrInvalidMapping", err)
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
		if s != "" && (strings.Contains(s, "of_test_") || strings.Contains(s, "tcp(")) {
			t.Fatalf("details leaked dsn: %#v", st.Details)
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
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
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
			{Name: "Trust", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "AdmittedTo", FromType: "Patient", ToType: "Ward", Cardinality: card},
			{Name: "BelongsTo", FromType: "Ward", ToType: "Trust", Cardinality: spi.CardinalityManyToMany},
		},
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
	if err := mysqlobda.InitMappedSchema(db, compileMapping(t, mapping, schema)); err != nil {
		t.Fatal(err)
	}
}

func assertNoOfTables(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME LIKE 'of\_%'`)
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

func openProvider(t *testing.T, mapping []byte) (*mysqlobda.Provider, *sql.DB) {
	t.Helper()
	db := openDB(t)
	p, err := mysqlobda.Open(db, mapping, mysqlobda.Options{
		DSNRefs: map[string]string{"secret://hospital/mysql-dsn": "ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, db
}

// openDB connects to the TEST_DB_URL MySQL server and creates an isolated
// per-test database. Tests skip when TEST_DB_URL is unset.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("TEST_DB_URL")
	if base == "" {
		t.Skip("TEST_DB_URL not set; MySQL integration tests skipped")
	}
	cfg, err := mysql.ParseDSN(base)
	if err != nil {
		t.Fatalf("parse TEST_DB_URL: %v", err)
	}
	admin, err := sql.Open("mysql", base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("of_test_%d_%s", time.Now().UnixNano(), randHex(4))
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		_ = admin.Close()
		t.Fatalf("create database %s: %v", name, err)
	}
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		_ = admin.Close()
	})
	return db
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
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
