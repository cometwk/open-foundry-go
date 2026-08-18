package action_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/action"
	"github.com/openfoundry/runtime/pack"
)

func TestParse_CreateOrderIgnoresOperationalFields(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "actions", "create-order.yaml"))
	if err != nil {
		t.Fatalf("read create-order.yaml: %v", err)
	}
	m, err := action.Parse(data)
	if err != nil {
		t.Fatalf("Parse err = %v, want nil", err)
	}
	if m.Action != "CreateOrder" {
		t.Errorf("Action = %q, want CreateOrder", m.Action)
	}
	if m.Version != 1 {
		t.Errorf("Version = %d, want 1", m.Version)
	}
	if m.Reversible {
		t.Errorf("Reversible = true, want false")
	}
	if m.SideEffects == nil {
		t.Error("SideEffects decoded nil, want ignored-but-present value")
	}
	if m.Rollback == nil {
		t.Error("Rollback decoded nil, want ignored-but-present value")
	}
	if len(m.Preconditions) != 3 {
		t.Fatalf("Preconditions = %d, want 3", len(m.Preconditions))
	}
}

func TestBind_UnknownActionFails(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir: %v", err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	m, err := action.Parse([]byte("action: NotARealAction\nversion: 1\n"))
	if err != nil {
		t.Fatalf("Parse err = %v, want nil", err)
	}
	if err := m.Bind(onto); err == nil {
		t.Fatal("Bind unknown action err = nil, want error")
	}
}

func TestBind_CreateOrderMatchesIR(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir: %v", err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "actions", "create-order.yaml"))
	if err != nil {
		t.Fatalf("read create-order.yaml: %v", err)
	}
	m, err := action.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := m.Bind(onto); err != nil {
		t.Fatalf("Bind CreateOrder err = %v, want nil", err)
	}
	if onto.ActionByName("CreateOrder") == nil {
		t.Fatal("IR missing CreateOrder ActionType")
	}
}
