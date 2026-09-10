package bootstrap_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
)

func TestSQLName(t *testing.T) {
	for _, name := range []string{"mysql", "sqlite"} {
		got, err := bootstrap.SQLName(name)
		if err != nil || got != name {
			t.Fatalf("%s: got %q err=%v", name, got, err)
		}
	}
	for _, name := range []string{"sqlite3", "postgres", "MySQL", ""} {
		if _, err := bootstrap.SQLName(name); err == nil {
			t.Fatalf("%q: want error", name)
		}
	}
}

func TestPrintPackDDL_MySQLDefault(t *testing.T) {
	dir := widgetPackDir(t)
	stmts, err := bootstrap.PrintPackDDL(dir, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, "ENGINE=InnoDB") {
		t.Fatalf("want mysql DDL: %s", joined)
	}
	if strings.Contains(joined, "CREATE UNIQUE INDEX IF NOT EXISTS") {
		t.Fatalf("sqlite syntax leaked: %s", joined)
	}
}

func TestPrintPackDDL_DialectOverride(t *testing.T) {
	dir := widgetPackDir(t)
	stmts, err := bootstrap.PrintPackDDL(dir, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, `CREATE TABLE IF NOT EXISTS "widget"`) {
		t.Fatalf("want sqlite DDL: %s", joined)
	}
	if strings.Contains(joined, "ENGINE=InnoDB") {
		t.Fatalf("mysql syntax leaked: %s", joined)
	}
}

func TestPrintPackDDL_LibraryPackMySQL(t *testing.T) {
	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := bootstrap.PrintPackDDL(dir, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, "`branch_id`") {
		t.Fatalf("missing branch_id:\n%s", joined)
	}
	if strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `registered_at`") {
		t.Fatalf("must not emit registered_at:\n%s", joined)
	}
	if !strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `borrows`") {
		t.Fatalf("missing borrows:\n%s", joined)
	}
	if !strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `available_at`") {
		t.Fatalf("missing available_at:\n%s", joined)
	}
}

func TestPrintPackDDL_LibraryPackSQLiteRefuses(t *testing.T) {
	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.PrintPackDDL(dir, "sqlite")
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
}

func TestPrintPackDDL_TwoMappings(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n  - obda/gadget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
		"obda/gadget.obda.yaml": modelMapping("Gadget", "gadget"),
	})
	if _, err := bootstrap.PrintPackDDL(dir, "sqlite"); err == nil || !strings.Contains(err.Error(), "mappings=2") {
		t.Fatalf("err=%v want mappings=2", err)
	}
}

func TestPrintPackDDL_ZeroMappings(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\n",
		"schema/models.odl": widgetODL,
	})
	if _, err := bootstrap.PrintPackDDL(dir, "sqlite"); err == nil || !strings.Contains(err.Error(), "mappings=0") {
		t.Fatalf("err=%v want mappings=0", err)
	}
}

func TestPrintPackDDL_UnknownDialect(t *testing.T) {
	dir := widgetPackDir(t)
	if _, err := bootstrap.PrintPackDDL(dir, "sqlite3"); err == nil {
		t.Fatal("want unsupported dialect")
	}
}

func TestPrintMappedDDL_NoURL(t *testing.T) {
	base := t.TempDir()
	writePackInto(t, base, "fixture")
	c := &bootstrap.Conf{BaseDir: base, DomainPacks: "fixture", DBDriver: "mysql"}
	stmts, err := bootstrap.PrintMappedDDL(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(stmts, "\n"), "ENGINE=InnoDB") {
		t.Fatalf("want mysql: %v", stmts)
	}
}

func TestMappedDDL_ForcePrependsDrop(t *testing.T) {
	dir := widgetPackDir(t)
	stmts, err := bootstrap.PackDDL(dir, "sqlite", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) < 2 {
		t.Fatalf("stmts=%v", stmts)
	}
	if stmts[0] != `DROP TABLE IF EXISTS "widget"` {
		t.Fatalf("first=%q", stmts[0])
	}
	if !strings.Contains(stmts[1], `CREATE TABLE IF NOT EXISTS "widget"`) {
		t.Fatalf("create missing: %v", stmts)
	}
}

func TestExecStatements_SQLiteCreateAndForce(t *testing.T) {
	base := t.TempDir()
	writePackInto(t, base, "fixture")
	dbPath := filepath.Join(t.TempDir(), "t.db")
	c := &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		DBDriver:    "sqlite",
		DBURL:       dbPath,
	}
	stmts, err := bootstrap.MappedDDL(c, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.ExecStatements(c, "", stmts); err != nil {
		t.Fatal(err)
	}
	db := openFileDB(t, dbPath)
	if _, err := db.Exec(`INSERT INTO widget (id, tenant_id, name, version, created_at, updated_at) VALUES ('1','t1','n',1,'a','b')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM widget`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	_ = db.Close()

	force, err := bootstrap.MappedDDL(c, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.ExecStatements(c, "", force); err != nil {
		t.Fatal(err)
	}
	db = openFileDB(t, dbPath)
	defer db.Close()
	if err := db.QueryRow(`SELECT COUNT(*) FROM widget`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after force count=%d err=%v", n, err)
	}
}

func TestExecStatements_NoURL(t *testing.T) {
	c := &bootstrap.Conf{DBDriver: "sqlite"}
	if err := bootstrap.ExecStatements(c, "", []string{"SELECT 1"}); err == nil || !strings.Contains(err.Error(), "DB_URL") {
		t.Fatalf("err=%v", err)
	}
}

func TestExecStatements_DialectMismatch(t *testing.T) {
	c := &bootstrap.Conf{DBDriver: "mysql", DBURL: "ignored"}
	err := bootstrap.ExecStatements(c, "sqlite", []string{"SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "cannot execute dialect") {
		t.Fatalf("err=%v", err)
	}
}

func openFileDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func widgetPackDir(t *testing.T) string {
	t.Helper()
	return writePack(t, map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
	})
}

func writePackInto(t *testing.T, base, name string) {
	t.Helper()
	dir := filepath.Join(base, "domain-packs", name)
	files := map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
