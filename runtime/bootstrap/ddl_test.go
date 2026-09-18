package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/pack"
)

func TestSQLName(t *testing.T) {
	got, err := bootstrap.SQLName("mysql")
	if err != nil || got != "mysql" {
		t.Fatalf("mysql: got %q err=%v", got, err)
	}
	// sqlite died with the sqliteobda provider; it must fail like any other
	// unknown dialect.
	for _, name := range []string{"sqlite", "sqlite3", "postgres", "MySQL", "memory", ""} {
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
		t.Fatalf("stale dialect syntax leaked: %s", joined)
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

func TestPrintPackDDL_TwoMappings(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n  - obda/gadget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "prefix_widget"),
		"obda/gadget.obda.yaml": modelMapping("Gadget", "gadget"),
	})
	stmts, err := bootstrap.PrintPackDDL(dir, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `prefix_widget`") {
		t.Fatalf("missing widget:\n%s", joined)
	}
	if !strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `gadget`") {
		t.Fatalf("missing gadget:\n%s", joined)
	}
	// fmt.Println("--------------------------------")
	// fmt.Println(modelMapping("Widget", "prefix_widget"))
	// fmt.Println("--------------------------------")
	// fmt.Println(stmts)
}

func TestPrintPackDDL_ZeroMappings(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\n",
		"schema/models.odl": widgetODL,
	})
	if _, err := bootstrap.PrintPackDDL(dir, "mysql"); err == nil || !strings.Contains(err.Error(), "mappings=0") {
		t.Fatalf("err=%v want mappings=0", err)
	}
}

func TestPrintPackDDL_UnknownDialect(t *testing.T) {
	dir := widgetPackDir(t)
	for _, name := range []string{"sqlite", "sqlite3"} {
		if _, err := bootstrap.PrintPackDDL(dir, name); err == nil {
			t.Fatalf("%q: want unsupported dialect", name)
		}
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
	stmts, err := bootstrap.PackDDL(dir, "mysql", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) < 2 {
		t.Fatalf("stmts=%v", stmts)
	}
	if stmts[0] != "DROP TABLE IF EXISTS `widget`" {
		t.Fatalf("first=%q", stmts[0])
	}
	if !strings.Contains(stmts[1], "CREATE TABLE IF NOT EXISTS `widget`") {
		t.Fatalf("create missing: %v", stmts)
	}
}

func TestExecStatements_MySQLSmoke(t *testing.T) {
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		t.Skip("TEST_DB_URL unset")
	}
	base := t.TempDir()
	writePackInto(t, base, "fixture")
	c := &bootstrap.Conf{BaseDir: base, DomainPacks: "fixture", DBDriver: "mysql", DBURL: dsn}
	if err := bootstrap.ExecStatements(c, "", []string{"SELECT 1"}); err != nil {
		t.Fatal(err)
	}
}

func TestExecStatements_NoURL(t *testing.T) {
	c := &bootstrap.Conf{DBDriver: "mysql"}
	if err := bootstrap.ExecStatements(c, "", []string{"SELECT 1"}); err == nil || !strings.Contains(err.Error(), "DB_URL") {
		t.Fatalf("err=%v", err)
	}
}

func TestExecStatements_UnknownDialect(t *testing.T) {
	c := &bootstrap.Conf{DBDriver: "mysql", DBURL: "ignored"}
	err := bootstrap.ExecStatements(c, "sqlite", []string{"SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "unsupported dialect") {
		t.Fatalf("err=%v", err)
	}
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
