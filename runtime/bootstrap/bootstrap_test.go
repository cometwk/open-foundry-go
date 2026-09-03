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
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
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

func loadPack(t *testing.T, dir string) (*ir.Ontology, []pack.Mapping) {
	t.Helper()
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	mappings, err := pack.LoadMappings(dir, onto)
	if err != nil {
		t.Fatalf("LoadMappings: %v", err)
	}
	return onto, mappings
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

func mustInitMappings(t *testing.T, db *sql.DB, onto *ir.Ontology, mappings []pack.Mapping) {
	t.Helper()
	schema := projection.ProjectStorage(onto)
	for _, m := range mappings {
		compiled, err := obda.Compile(schema, m.Doc)
		if err != nil {
			t.Fatalf("Compile %s: %v", m.Path, err)
		}
		if err := sqliteobda.InitMappedSchema(db, compiled); err != nil {
			t.Fatalf("InitMappedSchema %s: %v", m.Path, err)
		}
	}
}

func widgetPack(t *testing.T) (string, *ir.Ontology, []pack.Mapping) {
	t.Helper()
	dir := writePack(t, map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
	})
	onto, mappings := loadPack(t, dir)
	return dir, onto, mappings
}

func TestOpenSQLite_RoundTrip(t *testing.T) {
	_, onto, mappings := widgetPack(t)
	db := openDB(t)
	mustInitMappings(t, db, onto, mappings)

	p, err := bootstrap.OpenSQLite(bootstrap.Config{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		DSNRefs:  map[string]string{"secret://test/sqlite-dsn": "ignored"},
		TenantID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	got, err := p.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ObjectTypes) == 0 {
		t.Fatalf("GetSchema empty: %+v", got)
	}

	created, err := p.CreateObject(ctx, "Widget", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := created[spi.FieldID].(string)
	if id == "" {
		t.Fatalf("missing id: %#v", created)
	}
	fetched, err := p.GetObject(ctx, "Widget", id)
	if err != nil {
		t.Fatal(err)
	}
	if fetched["name"] != "Ada" {
		t.Fatalf("name=%v", fetched["name"])
	}
}

func TestOpenSQLite_MergesTwoMappings(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n  - obda/gadget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
		"obda/gadget.obda.yaml": modelMapping("Gadget", "gadget"),
	})
	onto, mappings := loadPack(t, dir)
	if len(mappings) != 2 {
		t.Fatalf("mappings=%d", len(mappings))
	}
	db := openDB(t)
	mustInitMappings(t, db, onto, mappings)

	p, err := bootstrap.OpenSQLite(bootstrap.Config{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		DSNRefs:  map[string]string{"secret://test/sqlite-dsn": "ignored"},
		TenantID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	w, err := p.CreateObject(ctx, "Widget", map[string]any{"name": "W"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := p.CreateObject(ctx, "Gadget", map[string]any{"name": "G"})
	if err != nil {
		t.Fatal(err)
	}
	gotW, err := p.GetObject(ctx, "Widget", w[spi.FieldID].(string))
	if err != nil {
		t.Fatal(err)
	}
	gotG, err := p.GetObject(ctx, "Gadget", g[spi.FieldID].(string))
	if err != nil {
		t.Fatal(err)
	}
	if gotW["name"] != "W" || gotG["name"] != "G" {
		t.Fatalf("widget=%v gadget=%v", gotW["name"], gotG["name"])
	}
}

func TestOpenSQLite_MissingDSNRef(t *testing.T) {
	_, onto, mappings := widgetPack(t)
	db := openDB(t)
	_, err := bootstrap.OpenSQLite(bootstrap.Config{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		TenantID: "t1",
	})
	if err == nil {
		t.Fatal("err = nil, want unresolved dsnRef")
	}
}

func TestOpenSQLite_EmptyTenant(t *testing.T) {
	_, onto, mappings := widgetPack(t)
	db := openDB(t)
	_, err := bootstrap.OpenSQLite(bootstrap.Config{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		DSNRefs:  map[string]string{"secret://test/sqlite-dsn": "ignored"},
	})
	if !errors.Is(err, spi.ErrTenantRequired) {
		t.Fatalf("err = %v, want ErrTenantRequired", err)
	}
}

func TestOpenSQLite_NoMapping(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\n",
		"schema/models.odl": widgetODL,
	})
	onto, mappings := loadPack(t, dir)
	if mappings != nil {
		t.Fatalf("undeclared obda: got %#v", mappings)
	}
	db := openDB(t)
	_, err := bootstrap.OpenSQLite(bootstrap.Config{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		DSNRefs:  map[string]string{"secret://test/sqlite-dsn": "ignored"},
		TenantID: "t1",
	})
	if err == nil || !strings.Contains(err.Error(), "no OBDA mapping") {
		t.Fatalf("err = %v, want no OBDA mapping", err)
	}
}

func TestOpenSQLite_SupplyChainRoundTrip(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	mappings, err := pack.LoadMappings(dir, onto)
	if err != nil {
		t.Fatalf("LoadMappings: %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("mappings=%d", len(mappings))
	}
	db := openDB(t)
	mustInitMappings(t, db, onto, mappings)

	p, err := bootstrap.OpenSQLite(bootstrap.Config{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		DSNRefs:  map[string]string{"secret://supply-chain/sqlite-dsn": "ignored"},
		TenantID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	schema, err := p.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.ObjectTypes) != 6 {
		t.Fatalf("object types=%d want 6", len(schema.ObjectTypes))
	}

	created, err := p.CreateObject(ctx, "Supplier", map[string]any{
		"name":    "Acme",
		"code":    "ACME",
		"tier":    "STRATEGIC",
		"country": "US",
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := created[spi.FieldID].(string)
	if id == "" {
		t.Fatalf("missing id: %#v", created)
	}
	got, err := p.GetObject(ctx, "Supplier", id)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Acme" || got["code"] != "ACME" {
		t.Fatalf("got %#v", got)
	}
}
