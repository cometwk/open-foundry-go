package pack_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
)

const widgetODL = `extend schema @namespace(name: "test.pack", version: "0.1.0")

type Widget @objectType {
  id: ID! @primary
  name: String
}

type Gadget @objectType {
  id: ID! @primary
  name: String
}
`

func modelMapping(name, table string) string {
	return `apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata:
  name: ` + strings.ToLower(name) + `
  namespace: test.pack
  version: 1
schema:
  namespace: test.pack
  version: 1
sources:
  primary:
    kind: sql
    dialect: sqlite
    connection:
      dsnRef: secret://test/sqlite-dsn
models:
  ` + name + `:
    sourceRef: primary
    relation:
      kind: table
      name: ` + table + `
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
`
}

func writePack(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func loadOnto(t *testing.T, dir string) *ir.Ontology {
	t.Helper()
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	return onto
}

func schemaPackYAML(obdaBlock string) string {
	base := "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\n"
	if obdaBlock == "" {
		return base
	}
	return base + obdaBlock
}

func TestLoadMappings_UndeclaredNoObdaDir(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         schemaPackYAML(""),
		"schema/models.odl": widgetODL,
	})
	got, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestLoadMappings_ObdaDirExistsButUndeclared(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":              schemaPackYAML(""),
		"schema/models.odl":      widgetODL,
		"obda/ignored.obda.yaml": modelMapping("Widget", "widget"),
	})
	got, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got %#v, want nil (undeclared obda/ must not be scanned)", got)
	}
}

func TestLoadMappings_EmptyList(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         schemaPackYAML("obda: []\n"),
		"schema/models.odl": widgetODL,
	})
	_, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err == nil {
		t.Fatal("err = nil, want empty obda list error")
	}
	if !strings.Contains(err.Error(), "empty obda list") {
		t.Fatalf("err = %v, want empty obda list", err)
	}
}

func TestLoadMappings_MissingFile(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         schemaPackYAML("obda:\n  - obda/x.obda.yaml\n"),
		"schema/models.odl": widgetODL,
	})
	_, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err == nil {
		t.Fatal("err = nil, want missing file error")
	}
	if !strings.Contains(err.Error(), "obda/x.obda.yaml") {
		t.Fatalf("err = %v, want path obda/x.obda.yaml", err)
	}
}

func TestLoadMappings_PlaintextDSN(t *testing.T) {
	raw := strings.Replace(modelMapping("Widget", "widget"), "dsnRef: secret://test/sqlite-dsn", "dsn: file:secret.db", 1)
	dir := writePack(t, map[string]string{
		"pack.yaml":             schemaPackYAML("obda:\n  - obda/widget.obda.yaml\n"),
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": raw,
	})
	_, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err = %v, want ErrInvalidMapping", err)
	}
}

func TestLoadMappings_UnknownModel(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":            schemaPackYAML("obda:\n  - obda/ghost.obda.yaml\n"),
		"schema/models.odl":    widgetODL,
		"obda/ghost.obda.yaml": modelMapping("Ghost", "ghost"),
	})
	_, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err = %v, want ErrInvalidMapping", err)
	}
}

func TestLoadMappings_DeclarationOrder(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":             schemaPackYAML("obda:\n  - obda/gadget.obda.yaml\n  - obda/widget.obda.yaml\n"),
		"schema/models.odl":     widgetODL,
		"obda/gadget.obda.yaml": modelMapping("Gadget", "gadget"),
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
	})
	got, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Path != "obda/gadget.obda.yaml" || got[1].Path != "obda/widget.obda.yaml" {
		t.Fatalf("order = %q, %q", got[0].Path, got[1].Path)
	}
	if len(got[0].Raw) == 0 || len(got[1].Raw) == 0 {
		t.Fatal("Raw must be non-empty")
	}
	if got[0].Doc == nil || got[0].Doc.Models["Gadget"].Relation.Name != "gadget" {
		t.Fatalf("first Doc = %#v", got[0].Doc)
	}
	if got[1].Doc == nil || got[1].Doc.Models["Widget"].Relation.Name != "widget" {
		t.Fatalf("second Doc = %#v", got[1].Doc)
	}
}

func TestLoadMappings_DuplicateModel(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         schemaPackYAML("obda:\n  - obda/a.obda.yaml\n  - obda/b.obda.yaml\n"),
		"schema/models.odl": widgetODL,
		"obda/a.obda.yaml":  modelMapping("Widget", "widget_a"),
		"obda/b.obda.yaml":  modelMapping("Widget", "widget_b"),
	})
	_, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err == nil {
		t.Fatal("err = nil, want duplicate model error")
	}
	if !strings.Contains(err.Error(), "Widget") {
		t.Fatalf("err = %v, want model Widget", err)
	}
	if !strings.Contains(err.Error(), "obda/b.obda.yaml") {
		t.Fatalf("err = %v, want file obda/b.obda.yaml", err)
	}
}

func TestLoadMappings_DuplicateRelationTable(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         schemaPackYAML("obda:\n  - obda/a.obda.yaml\n  - obda/b.obda.yaml\n"),
		"schema/models.odl": widgetODL,
		"obda/a.obda.yaml":  modelMapping("Widget", "shared"),
		"obda/b.obda.yaml":  modelMapping("Gadget", "shared"),
	})
	_, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err == nil {
		t.Fatal("err = nil, want duplicate relation table error")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Fatalf("err = %v, want table shared", err)
	}
}

func TestLoadSupplyChainMappings(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	got, err := pack.LoadMappings(dir, onto)
	if err != nil {
		t.Fatalf("LoadMappings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Path != "obda/supply-chain.obda.yaml" {
		t.Fatalf("Path = %q", got[0].Path)
	}
	if got[0].Doc == nil {
		t.Fatal("Doc is nil")
	}
	if n := len(got[0].Doc.Models); n != 6 {
		t.Fatalf("models = %d, want 6", n)
	}
	if n := len(got[0].Doc.Links); n != 7 {
		t.Fatalf("links = %d, want 7", n)
	}
}

func TestLoadMappings_NestedPath(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":               schemaPackYAML("obda:\n  - obda/nested/x.obda.yaml\n"),
		"schema/models.odl":       widgetODL,
		"obda/nested/x.obda.yaml": modelMapping("Widget", "widget"),
	})
	got, err := pack.LoadMappings(dir, loadOnto(t, dir))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 1 || got[0].Path != "obda/nested/x.obda.yaml" {
		t.Fatalf("got %#v", got)
	}
	if got[0].Doc == nil || got[0].Doc.Models["Widget"].Relation.Name != "widget" {
		t.Fatalf("Doc = %#v", got[0].Doc)
	}
}
