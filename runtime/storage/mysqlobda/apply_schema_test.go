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
	p, db := openProvider(t, testdata(t, "library.obda.yaml"))
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema())
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v want ErrInvalidMapping", err)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Reader", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v want ErrMappingNotActive", err)
	}
	assertNoOfTables(t, db)
}

func TestApplySchemaGeneratedDDLNotExecutedFails(t *testing.T) {
	p, _ := openProvider(t, testdata(t, "library.obda.yaml"))
	compiled := compileMapping(t, testdata(t, "library.obda.yaml"), readerSchema())
	stmts, err := mysqldialect.MappedTableStatements(compiled)
	if err != nil || len(stmts) == 0 {
		t.Fatalf("stmts=%v err=%v", stmts, err)
	}
	_, err = p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema())
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v want ErrInvalidMapping", err)
	}
}

func TestApplySchemaAfterHelperSucceeds(t *testing.T) {
	p, db := openProvider(t, testdata(t, "library.obda.yaml"))
	mustInit(t, db, testdata(t, "library.obda.yaml"), readerSchema())
	res, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema())
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
	if len(got.ObjectTypes) != 1 || got.ObjectTypes[0].Name != "Reader" {
		t.Fatalf("%+v", got)
	}
	other := 999
	if _, err := p.GetSchema(spi.RequestContext{TenantID: "t1"}, &other); !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v want ErrMappingNotActive", err)
	}
	assertNoOfTables(t, db)
}

func TestApplySchemaFulltextVerify(t *testing.T) {
	raw := testdata(t, "library_search.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, bookSchema())
	// Init + ApplySchema should succeed — FULLTEXT index exists and is verified.
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, bookSchema()); err != nil {
		t.Fatal(err)
	}
	// Drop the FULLTEXT index; ApplySchema must catch the drift. Covers AE8.
	if _, err := db.Exec("DROP INDEX `ft_book` ON `book`"); err != nil {
		t.Fatal(err)
	}
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, bookSchema())
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("missing FULLTEXT should return ErrSourceSchemaDrift, got %v", err)
	}
}

func TestApplySchemaInlineHostFK(t *testing.T) {
	p, db := openProvider(t, testdata(t, "library_inline.obda.yaml"))
	mustInit(t, db, testdata(t, "library_inline.obda.yaml"), inlineSchema())
	res, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, inlineSchema())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("%+v", res)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'registered_at'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("registered_at must not exist")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'reader' AND COLUMN_NAME = 'branch_id'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("reader.branch_id missing")
	}
}

func TestApplySchemaMissingUniqueFails(t *testing.T) {
	p, db := openProvider(t, testdata(t, "library_links.obda.yaml"))
	mustExec(t, db, `CREATE TABLE reader (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), name TEXT, version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE book (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), title TEXT, version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE borrows (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), from_id VARCHAR(255), to_id VARCHAR(255), version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE branch (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), name TEXT, version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	mustExec(t, db, `CREATE TABLE available_at (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255), from_id VARCHAR(255), to_id VARCHAR(255), version BIGINT, created_at VARCHAR(64), updated_at VARCHAR(64), deleted_at VARCHAR(64))`)
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, librarySchema(spi.CardinalityManyToOne))
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Reader", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplySchemaMissingColumnIsDrift(t *testing.T) {
	p, db := openProvider(t, testdata(t, "library.obda.yaml"))
	mustExec(t, db, `CREATE TABLE reader (id VARCHAR(255) PRIMARY KEY, tenant_id VARCHAR(255))`)
	_, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema())
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
}

func TestCreateBeforeActivate(t *testing.T) {
	p, _ := openProvider(t, testdata(t, "library.obda.yaml"))
	_, err := p.CreateObject(spi.RequestContext{TenantID: "t1"}, "Reader", nil)
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Reader", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplySchemaRequiresTenant(t *testing.T) {
	p, db := openProvider(t, testdata(t, "library.obda.yaml"))
	mustInit(t, db, testdata(t, "library.obda.yaml"), readerSchema())
	_, err := p.ApplySchema(spi.RequestContext{}, readerSchema())
	if !errors.Is(err, spi.ErrTenantRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestHealthCheckOmitsPath(t *testing.T) {
	p, _ := openProvider(t, testdata(t, "library.obda.yaml"))
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
	p, db := openProvider(t, testdata(t, "library.obda.yaml"))
	mustInit(t, db, testdata(t, "library.obda.yaml"), readerSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema()); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `ALTER TABLE reader ADD COLUMN extra TEXT`)
	st, err := p.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if st.Healthy {
		t.Fatal("expected degraded health after drift")
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "t1"}, "Reader", "x")
	if !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
}

// readerSchema is the simplified library-pack Reader: fields identical to the
// full case, per domain-packs/library-pack/library-pack.md (简化版).
func readerSchema() spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Reader", Properties: []spi.PropertyDefinition{
				{Name: "name", Type: "String"},
				{Name: "membershipLevel", Type: "String"},
				{Name: "currentBorrowCount", Type: "Integer"},
				{Name: "registeredDays", Type: "Integer"},
			}},
		},
	}
}

// bookSchema is the simplified library-pack Book field set.
func bookSchema() spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Book", Properties: []spi.PropertyDefinition{
				{Name: "title", Type: "String"},
				{Name: "isbn", Type: "String"},
				{Name: "daysOnShelf", Type: "Integer"},
			}},
		},
	}
}

// branchSchema is the simplified library-pack Branch field set.
func branchSchema() spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Branch", Properties: []spi.PropertyDefinition{
				{Name: "name", Type: "String"},
				{Name: "maxBorrowPerReader", Type: "Integer"},
				{Name: "newBookProtectionDays", Type: "Integer"},
				{Name: "allowInterLibraryLoan", Type: "Boolean"},
			}},
		},
	}
}

// librarySchema is the simplified library-pack graph: Reader/Book/Branch with
// Borrows (cardinality injected per test) and AvailableAt.
func librarySchema(card spi.Cardinality) spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Reader", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}},
			{Name: "Book", Properties: []spi.PropertyDefinition{{Name: "title", Type: "String"}}},
			{Name: "Branch", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "Borrows", FromType: "Reader", ToType: "Book", Cardinality: card},
			{Name: "AvailableAt", FromType: "Book", ToType: "Branch", Cardinality: spi.CardinalityManyToMany},
		},
	}
}

// inlineSchema is RegisteredAt (Reader→Branch, MANY_TO_ONE) as a host-table FK.
func inlineSchema() spi.OntologySchema {
	return spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{
				Name:       "Reader",
				Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}},
				Navigations: []spi.LinkNavigation{{
					Field: "branch", LinkType: "RegisteredAt", Direction: "OUTBOUND",
				}},
			},
			{
				Name:       "Branch",
				Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}},
				Navigations: []spi.LinkNavigation{{
					Field: "readers", LinkType: "RegisteredAt", Direction: "INBOUND",
				}},
			},
		},
		LinkTypes: []spi.LinkTypeDefinition{{
			Name: "RegisteredAt", FromType: "Reader", ToType: "Branch",
			Cardinality: spi.CardinalityManyToOne,
			Properties:  []spi.PropertyDefinition{{Name: "id", Type: "ID", Required: true}},
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
	p, err := mysqlobda.Open(db, mapping, mysqlobda.Options{})
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
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + `"`)
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
