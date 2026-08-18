package cel_test

import (
	"errors"
	"testing"

	runcel "github.com/openfoundry/runtime/cel"
	"github.com/openfoundry/runtime/spi"
)

func TestEval_ActorHasRole(t *testing.T) {
	vars := map[string]any{
		"actor": map[string]any{
			"id":    "user-001",
			"roles": []string{"procurement_manager", "viewer"},
			"type":  "user",
		},
	}
	got, err := runcel.Eval("actor.hasRole('procurement_manager')", vars)
	if err != nil {
		t.Fatalf("Eval hasRole(present) err = %v, want nil", err)
	}
	if got != true {
		t.Errorf("hasRole(present) = %v, want true", got)
	}

	got, err = runcel.Eval("actor.hasRole('supply_chain_admin')", vars)
	if err != nil {
		t.Fatalf("Eval hasRole(absent) err = %v, want nil", err)
	}
	if got != false {
		t.Errorf("hasRole(absent) = %v, want false", got)
	}
}

func TestEval_ParamsQuantity(t *testing.T) {
	vars := map[string]any{
		"params": map[string]any{"quantity": 3},
	}
	got, err := runcel.Eval("params.quantity > 0", vars)
	if err != nil {
		t.Fatalf("Eval err = %v, want nil", err)
	}
	if got != true {
		t.Errorf("params.quantity > 0 = %v, want true", got)
	}

	vars["params"] = map[string]any{"quantity": 0}
	got, err = runcel.Eval("params.quantity > 0", vars)
	if err != nil {
		t.Fatalf("Eval zero err = %v, want nil", err)
	}
	if got != false {
		t.Errorf("params.quantity > 0 (zero) = %v, want false", got)
	}
}

func TestEval_IllegalCELIsErrCelEval(t *testing.T) {
	_, err := runcel.Eval("this is not valid CEL !!!", map[string]any{})
	if err == nil {
		t.Fatal("Eval illegal CEL err = nil, want ErrCelEval")
	}
	if !errors.Is(err, spi.ErrCelEval) {
		t.Errorf("Eval illegal CEL err = %v, want errors.Is ErrCelEval", err)
	}
	if errors.Is(err, spi.ErrPreconditionFailed) {
		t.Errorf("illegal CEL must not be ErrPreconditionFailed")
	}
}
