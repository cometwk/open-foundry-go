package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
)

func TestSeed_MemoryCreates(t *testing.T) {
	base, _ := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		SeedTenant:  "default",
		DBDriver:    bootstrap.BackendMemory,
	}
	t.Cleanup(func() { conf = prev })

	// Memory is per-process: each seed() opens a fresh instance, so the
	// observable contract is the seed report. Skip-existing idempotence on
	// one instance is covered by bootstrap.TestApplySeeds_LibraryDemoIdempotent.
	out := captureStdout(t, func() {
		if err := seed(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Seed: created 1 object(s) + 0 link(s), skipped 0 existing") {
		t.Fatalf("seed report: %s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
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
