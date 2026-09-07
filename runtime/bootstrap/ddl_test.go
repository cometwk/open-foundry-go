package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
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
