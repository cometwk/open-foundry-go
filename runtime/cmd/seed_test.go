package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/spi"
)

func TestSeed_SQLiteIdempotent(t *testing.T) {
	base, dbPath := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		SeedTenant:  "default",
		DBDriver:    "sqlite",
		DBURL:       dbPath,
	}
	t.Cleanup(func() { conf = prev })

	out := filepath.Join(t.TempDir(), "schema.sql")
	if err := ddl("", out, true, false); err != nil {
		t.Fatal(err)
	}

	if err := seed(); err != nil {
		t.Fatal(err)
	}
	if got := queryWidgetNames(t, conf); got != "Ada" {
		t.Fatalf("after first seed name=%q", got)
	}

	if err := seed(); err != nil {
		t.Fatal(err)
	}
	if got := queryWidgetNames(t, conf); got != "Ada" {
		t.Fatalf("after second seed name=%q", got)
	}
}

func TestSeed_RequiresSchema(t *testing.T) {
	base, dbPath := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		SeedTenant:  "default",
		DBDriver:    "sqlite",
		DBURL:       dbPath,
	}
	t.Cleanup(func() { conf = prev })

	if err := seed(); err == nil {
		t.Fatal("seed without ddl err = nil, want schema verification failure")
	}
}

func TestSeed_NilConf(t *testing.T) {
	prev := conf
	conf = nil
	t.Cleanup(func() { conf = prev })
	if err := seed(); err == nil || !strings.Contains(err.Error(), "config required") {
		t.Fatalf("err=%v, want config required", err)
	}
}

func writeSeedFixture(t *testing.T) (base, dbPath string) {
	t.Helper()
	base = t.TempDir()
	dir := filepath.Join(base, "domain-packs", "fixture")
	files := map[string]string{
		"pack.yaml": `name: fixture
namespace: test.pack
schema:
  - schema/models.odl
obda:
  - obda/widget.obda.yaml
seed:
  - seeds/demo.yaml
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
		"seeds/demo.yaml": `objects:
  - type: Widget
    ref: widget-ada
    fields:
      name: "Ada"
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
	return base, filepath.Join(t.TempDir(), "t.db")
}

func queryWidgetNames(t *testing.T, c *bootstrap.Conf) string {
	t.Helper()
	b, err := bootstrap.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.DB.Close()
	if err := b.ApplySchema(); err != nil {
		t.Fatal(err)
	}
	page, err := b.SPI.QueryObjects(
		bootstrap.SeedContext(c.SeedTenant),
		"Widget",
		spi.FilterExpression{Field: "name", Operator: "eq", Value: "Ada"},
		&spi.QueryOptions{Limit: 5},
	)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 1 {
		t.Fatalf("Widget count=%d, want 1", page.TotalCount)
	}
	name, _ := page.Items[0]["name"].(string)
	return name
}
