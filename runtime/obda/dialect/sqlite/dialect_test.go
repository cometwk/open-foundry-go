package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestQuoteIdentifier(t *testing.T) {
	d := sqlitedialect.New()
	got, err := d.QuoteIdentifier(sqlast.Identifier{Name: "patient"})
	if err != nil || got != `"patient"` {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := d.QuoteIdentifier(sqlast.Identifier{Name: "patient;drop"}); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := d.QuoteIdentifier(sqlast.Identifier{Name: `pa"tient`}); err == nil {
		t.Fatal("expected reject quote")
	}
}

func TestRenderSelectExecutes(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`CREATE TABLE patient (patient_id TEXT, tenant_id TEXT, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO patient VALUES ('p1', 't1', 'Ada')`); err != nil {
		t.Fatal(err)
	}
	sel, args, err := obda.PlanGetObject(obda.ObjectBinding{
		Table:           "patient",
		TenantColumn:    "tenant_id",
		IdentityColumns: []string{"patient_id"},
		SelectColumns:   []string{"name"},
	}, "t1", []any{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := sqlitedialect.New().Render(sel)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stmt.SQL, "Ada") {
		t.Fatalf("value leaked into SQL: %s", stmt.SQL)
	}
	var name string
	if err := db.QueryRow(stmt.SQL, args...).Scan(&name); err != nil {
		t.Fatalf("sql=%s args=%v err=%v", stmt.SQL, args, err)
	}
	if name != "Ada" {
		t.Fatalf("name=%q", name)
	}
}

func TestIntrospectFingerprint(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE patient (patient_id TEXT, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	a, err := sqlitedialect.InspectTable(ctx, db, sqlast.Identifier{Name: "patient"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := sqlitedialect.InspectTable(ctx, db, sqlast.Identifier{Name: "patient"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatalf("hash a=%s b=%s", a.Hash, b.Hash)
	}
	if _, err := db.Exec(`ALTER TABLE patient ADD COLUMN extra TEXT`); err != nil {
		t.Fatal(err)
	}
	c, err := sqlitedialect.InspectTable(ctx, db, sqlast.Identifier{Name: "patient"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Hash == a.Hash {
		t.Fatal("fingerprint should change after ALTER")
	}
}

func TestNormalizeBool(t *testing.T) {
	d := sqlitedialect.New()
	v, err := d.NormalizeValue("Boolean", int64(1))
	if err != nil {
		t.Fatal(err)
	}
	if v != true {
		t.Fatalf("got=%v (%T)", v, v)
	}
}

func TestDSNNotInQuoteError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.db")
	_, err := sqlitedialect.New().QuoteIdentifier(sqlast.Identifier{Name: "x;drop"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("error leaked path: %v", err)
	}
}
