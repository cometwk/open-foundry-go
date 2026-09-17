package bootstrap_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/runtime/bootstrap"
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
models:
  ` + name + `:
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

func memoryConf(base string) *bootstrap.Conf {
	return &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		DBDriver:    bootstrap.BackendMemory,
	}
}

func TestOpen_MemoryRoundTrip(t *testing.T) {
	base := t.TempDir()
	writePackInto(t, base, "fixture")
	b, err := bootstrap.Open(memoryConf(base))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if err := b.ApplySchema(); err != nil {
		t.Fatal(err)
	}

	ctx := spi.RequestContext{TenantID: "t1"}
	got, err := b.SPI.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ObjectTypes) == 0 {
		t.Fatalf("GetSchema empty: %+v", got)
	}

	created, err := b.SPI.CreateObject(ctx, "Widget", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := created[spi.FieldID].(string)
	if id == "" {
		t.Fatalf("missing id: %#v", created)
	}
	fetched, err := b.SPI.GetObject(ctx, "Widget", id)
	if err != nil {
		t.Fatal(err)
	}
	if fetched["name"] != "Ada" {
		t.Fatalf("name=%v", fetched["name"])
	}
}

func TestOpen_MemorySupplyChainRoundTrip(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	// SupplyChainDir = <repo-root>/domain-packs/supply-chain; packDir joins
	// BaseDir/domain-packs/<name>, so BaseDir is the repo root.
	c := &bootstrap.Conf{
		BaseDir:     filepath.Dir(filepath.Dir(dir)),
		DomainPacks: filepath.Base(dir),
		TenantID:    "t1",
		DBDriver:    bootstrap.BackendMemory,
	}
	b, err := bootstrap.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if err := b.ApplySchema(); err != nil {
		t.Fatal(err)
	}

	ctx := spi.RequestContext{TenantID: "t1"}
	schema, err := b.SPI.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.ObjectTypes) != 6 {
		t.Fatalf("object types=%d want 6", len(schema.ObjectTypes))
	}

	created, err := b.SPI.CreateObject(ctx, "Supplier", map[string]any{
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
	got, err := b.SPI.GetObject(ctx, "Supplier", id)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Acme" || got["code"] != "ACME" {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadConfig(t *testing.T) {
	conf, err := bootstrap.LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("conf", conf)
}

func TestLoadConfig2(t *testing.T) {
	v, ok := os.LookupEnv("DB_DEBUG")
	fmt.Println("v", v, ok)
	env.LoadEnv("")
	v, ok = os.LookupEnv("DB_DEBUG")
	fmt.Println("v", v, ok)
}
