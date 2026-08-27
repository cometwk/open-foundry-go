package pack_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/action"
	"github.com/openfoundry/runtime/pack"
)

func TestLoadActions_CreateOrderBoundToIR(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	manifests, err := pack.LoadActions(dir, onto)
	if err != nil {
		t.Fatalf("LoadActions err = %v, want nil", err)
	}
	m := action.Lookup(manifests, "CreateOrder")
	if m == nil {
		t.Fatal("LoadActions missing CreateOrder manifest")
	}
	if onto.ActionByName("CreateOrder") == nil {
		t.Fatal("IR missing CreateOrder ActionType")
	}
	if m.SideEffects == nil || m.Rollback == nil {
		t.Error("CreateOrder sideEffects/rollback must parse (ignored at eval)")
	}
	if m.Reversible {
		t.Error("CreateOrder reversible = true, want false")
	}
}

func TestLoadActions_UnknownActionFailsBind(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "actions"), 0o755); err != nil {
		t.Fatal(err)
	}
	packYAML := []byte("name: fixture\nnamespace: test\nactions:\n  - actions/nope.yaml\n")
	if err := os.WriteFile(filepath.Join(tmp, "pack.yaml"), packYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "actions", "nope.yaml"), []byte("action: NotARealAction\nversion: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = pack.LoadActions(tmp, onto)
	if err == nil {
		t.Fatal("LoadActions unknown action err = nil, want bind failure")
	}
}
