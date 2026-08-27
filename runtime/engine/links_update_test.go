package engine

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// Covers AE9 / R7: Engine.UpdateLink happy path, missing reject-before-SPI,
// and expectedVersion passthrough.

func TestEngine_UpdateLink_HappyPath(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	link, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), map[string]any{"qty": 1})
	if err != nil {
		t.Fatalf("CreateLink err = %v", err)
	}
	updated, err := e.UpdateLink(ctx, "Supplies", link["_id"].(string), map[string]any{"qty": 7}, nil)
	if err != nil {
		t.Fatalf("UpdateLink err = %v, want nil (AE9)", err)
	}
	if asIntAny(updated["qty"]) != 7 {
		t.Errorf("updated qty = %v, want 7", updated["qty"])
	}
	got, err := e.GetLink(ctx, "Supplies", link["_id"].(string))
	if err != nil {
		t.Fatalf("GetLink after update err = %v", err)
	}
	if asIntAny(got["qty"]) != 7 {
		t.Errorf("GetLink qty = %v, want 7", got["qty"])
	}
}

func TestEngine_UpdateLink_Missing_RejectsBeforeSPI(t *testing.T) {
	rec := &recordingProvider{inner: memory.New()}
	e, err := New(rec, linkOntology(t))
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	ctx := tenantCtx("tnt")
	_, err = e.UpdateLink(ctx, "Supplies", "missing-link", map[string]any{"qty": 1}, nil)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Errorf("UpdateLink missing err = %v, want ErrLinkNotFound (AE9)", err)
	}
	if rec.updateLinkCalls != 0 {
		t.Errorf("updateLinkCalls = %d, want 0 (reject before storage.UpdateLink) (AE9)", rec.updateLinkCalls)
	}
}

func TestEngine_UpdateLink_ExpectedVersionPassthrough(t *testing.T) {
	e := newLinkEngine(t)
	ctx := tenantCtx("tnt")
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})
	link, _ := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	wrong := 0
	_, err := e.UpdateLink(ctx, "Supplies", link["_id"].(string), map[string]any{"qty": 2}, &wrong)
	if !errors.Is(err, spi.ErrVersionConflict) {
		t.Errorf("UpdateLink expectedVersion=0 err = %v, want ErrVersionConflict (passthrough AE9)", err)
	}
	match := 1
	updated, err := e.UpdateLink(ctx, "Supplies", link["_id"].(string), map[string]any{"qty": 3}, &match)
	if err != nil {
		t.Fatalf("UpdateLink matching version err = %v, want nil", err)
	}
	if asIntAny(updated["qty"]) != 3 {
		t.Errorf("updated qty = %v, want 3", updated["qty"])
	}
}

// asIntAny coerces JSON-cloned numbers for engine test assertions.
func asIntAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return -1
}
