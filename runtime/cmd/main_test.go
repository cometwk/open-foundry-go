package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
)

func TestWriteDDL_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")
	if err := writeDDL([]string{"CREATE TABLE book (id TEXT)", "CREATE TABLE member (id TEXT);"}, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE book (id TEXT);\nCREATE TABLE member (id TEXT);\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWriteDDL_EmptyStmts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sql")
	if err := writeDDL(nil, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want empty file", got)
	}
}

func TestWriteDDL_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "nested", "schema.sql")
	if err := writeDDL([]string{"CREATE TABLE book (id TEXT);"}, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CREATE TABLE book (id TEXT);\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteDDL_MultilineEndsWithSemi(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")
	stmt := "CREATE TABLE IF NOT EXISTS `book` (\n  `id` VARCHAR(255) PRIMARY KEY\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	if err := writeDDL([]string{stmt}, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := stmt + ";\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveOutputPath_UsesCWD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := resolveOutputPath("1.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "1.sql")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveOutputPath_FallsBackToBaseDir(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(root, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	prev := conf
	conf = &bootstrap.Conf{BaseDir: root}
	t.Cleanup(func() { conf = prev })

	got, err := resolveOutputPath("1.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "1.sql")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDDL_ExecuteForceSQLite(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "domain-packs", "fixture")
	files := map[string]string{
		"pack.yaml": `name: fixture
namespace: test.pack
schema:
  - schema/models.odl
obda:
  - obda/widget.obda.yaml
`,
		"schema/models.odl": `extend schema @namespace(name: "test.pack", version: "0.1.0")

type Widget @objectType {
  id: ID! @primary
  name: String
}
`,
		"obda/widget.obda.yaml": `apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata:
  name: widget
  namespace: test.pack
  version: 1
schema:
  namespace: test.pack
  version: 1
models:
  Widget:
    relation:
      kind: table
      name: widget
    access: readWrite
    identity:
      strategy: direct
      columns: [id]
      insert: generated
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
    fields:
      name:
        column: name
`,
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
	dbPath := filepath.Join(t.TempDir(), "t.db")
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		DBDriver:    "sqlite",
		DBURL:       dbPath,
	}
	t.Cleanup(func() { conf = prev })

	out := filepath.Join(t.TempDir(), "schema.sql")
	if err := ddl("", out, true, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `CREATE TABLE IF NOT EXISTS "widget"`) {
		t.Fatalf("got %s", body)
	}

	if err := ddl("", out, true, true); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `DROP TABLE IF EXISTS "widget";`) {
		t.Fatalf("force missing drop: %s", body)
	}
}
