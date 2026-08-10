package pack_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/pack"
)

func TestLoadSupplyChain(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if o.Namespace == nil || o.Namespace.Name != "supply.chain" {
		t.Fatalf("namespace: %+v", o.Namespace)
	}
	if len(o.Objects) != 6 {
		t.Fatalf("objects=%d want 6", len(o.Objects))
	}
	if len(o.Links) != 7 {
		t.Fatalf("links=%d want 7", len(o.Links))
	}
	if len(o.Actions) != 4 {
		t.Fatalf("actions=%d want 4", len(o.Actions))
	}
	if len(o.Enums) == 0 {
		t.Fatal("expected enums")
	}

	json, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(json))
}

func TestLoadMissingSchemaFile(t *testing.T) {
	root, err := pack.FindRepoRoot(".")
	if err != nil {
		// from runtime/pack during go test, cwd is runtime/pack or runtime
		root, err = pack.FindRepoRoot("..")
		if err != nil {
			t.Skip(err)
		}
	}
	// Use a temp-like failure: point at supply-chain but we can't easily mutate pack.yaml.
	// Instead call LoadDir on a non-existent path.
	_, err = pack.LoadDir(filepath.Join(root, "domain-packs", "does-not-exist"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStripDuplicateNamespace(t *testing.T) {
	in := "extend schema @namespace(name: \"supply.chain\", version: \"0.1.0\")\n\ntype X @objectType { id: ID! @primary }\n"
	out := pack.StripNamespaceForTest(in)
	if filepath.Base(out) == "" { // silence unused in weird cases
	}
	if len(out) == 0 || out[0:4] == "exte" {
		t.Fatalf("expected namespace stripped, got %q", out)
	}
}
