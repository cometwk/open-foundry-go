// Package e2e holds the supply-chain Gold Path integration: load the
// real domain pack, project to a storage schema, apply it through the
// memory provider, then walk the six implemented verbs end-to-end
// against the actual ontology types (Supplier, Product, and the
// SuppliesProduct link). This is the Phase 2 acceptance for F7 / R16 /
// AE9 — the single test that proves the Phase 1 → Phase 2 pipeline is
// wired correctly and the seven implemented SPI methods return zero
// ErrUnimplemented hits.
//
// The Gold Path does NOT copy domain-packs into runtime/. It loads
// the pack from the repo root via pack.SupplyChainDir(), so the
// supply-chain pack stays the source of truth.
package e2e_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// TestGoldPath_SupplyChain_F7 is the Phase 2 acceptance test. It walks
// the full pipeline: pack load → IR → OntologySchema projection →
// memory.ApplySchema → six Engine verbs exercised on real
// supply-chain types. Covers AE9 / F7 / R16.
func TestGoldPath_SupplyChain_F7(t *testing.T) {
	// (1) pack.Load → IR. Loaded from the repo-rooted pack dir, not a
	// copy under runtime/.
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("pack.SupplyChainDir err = %v, want nil", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("pack.LoadDir(supply-chain) err = %v, want nil", err)
	}
	if o.Namespace == nil || o.Namespace.Name != "supply.chain" {
		t.Fatalf("loaded namespace = %+v, want supply.chain", o.Namespace)
	}

	// (2) projection.ProjectStorage → OntologySchema.
	schema := projection.ProjectStorage(o)
	if len(schema.ObjectTypes) == 0 {
		t.Fatal("ProjectStorage produced zero object types")
	}

	// (3) memory.ApplySchema → MigrationResult. Engine binds the same
	// IR through engine.New (which calls ir.Validate once more as a
	// TBox gate).
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "gold-path", ActorID: "test"}
	mr, err := p.ApplySchema(ctx, schema)
	if err != nil {
		t.Fatalf("ApplySchema err = %v, want nil", err)
	}
	if !mr.Success {
		t.Fatalf("ApplySchema result = %+v, want Success=true", mr)
	}

	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v, want nil", err)
	}

	// (4) Engine.CreateObject("Supplier", {name:"Acme"}).
	supplier, err := e.CreateObject(ctx, "Supplier", map[string]any{
		"name":    "Acme",
		"code":    "ACME-001",
		"tier":    "STRATEGIC",
		"country": "US",
	})
	if err != nil {
		t.Fatalf("CreateObject(Supplier) err = %v, want nil", err)
	}
	supplierID := supplier["_id"].(string)

	// (5) Engine.CreateObject("Product", {sku:"P1"}). The Product
	// object type carries several NonNull fields (sku, name, category,
	// unitOfMeasure, reorderPoint, reorderQuantity); supply all of
	// them so the Engine validator passes.
	product, err := e.CreateObject(ctx, "Product", map[string]any{
		"sku":             "P1",
		"name":            "Widget",
		"category":        "Hardware",
		"unitOfMeasure":   "each",
		"reorderPoint":    5,
		"reorderQuantity": 50,
	})
	if err != nil {
		t.Fatalf("CreateObject(Product) err = %v, want nil", err)
	}
	productID := product["_id"].(string)

	// (6) Engine.CreateLink("SuppliesProduct", s.id, p.id, {}).
	link, err := e.CreateLink(ctx, "SuppliesProduct", supplierID, productID, nil)
	if err != nil {
		t.Fatalf("CreateLink(SuppliesProduct) err = %v, want nil", err)
	}
	linkID := link["_id"].(string)
	if link["_type"] != "SuppliesProduct" {
		t.Errorf("CreateLink _type = %v, want SuppliesProduct", link["_type"])
	}

	// (7) Engine.GetObject("Supplier", s.id) returns s.
	gotSupplier, err := e.GetObject(ctx, "Supplier", supplierID)
	if err != nil {
		t.Fatalf("GetObject(Supplier) err = %v, want nil", err)
	}
	if gotSupplier["name"] != "Acme" {
		t.Errorf("GetObject(Supplier).name = %v, want Acme", gotSupplier["name"])
	}

	// (8) Engine.UpdateObject("Supplier", s.id, {name:"Acme Corp"}).
	updatedSupplier, err := e.UpdateObject(ctx, "Supplier", supplierID, map[string]any{"name": "Acme Corp"}, nil)
	if err != nil {
		t.Fatalf("UpdateObject(Supplier) err = %v, want nil", err)
	}
	if updatedSupplier["name"] != "Acme Corp" {
		t.Errorf("UpdateObject name = %v, want Acme Corp", updatedSupplier["name"])
	}
	// Merge preserves unpatched fields.
	if updatedSupplier["code"] != "ACME-001" {
		t.Errorf("UpdateObject dropped code = %v, want ACME-001 (merge)", updatedSupplier["code"])
	}

	// (9) Engine.GetLink("SuppliesProduct", l.id) returns l.
	gotLink, err := e.GetLink(ctx, "SuppliesProduct", linkID)
	if err != nil {
		t.Fatalf("GetLink err = %v, want nil", err)
	}
	if gotLink["_id"] != linkID {
		t.Errorf("GetLink _id = %v, want %v", gotLink["_id"], linkID)
	}

	// (10) Engine.DeleteLink("SuppliesProduct", l.id).
	if err := e.DeleteLink(ctx, "SuppliesProduct", linkID); err != nil {
		t.Fatalf("DeleteLink err = %v, want nil", err)
	}

	// (11) Engine.GetLink → not-found.
	if _, err := e.GetLink(ctx, "SuppliesProduct", linkID); !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink after delete err = %v, want ErrLinkNotFound", err)
	}

	// (12) Engine.DeleteObject hard on Supplier and Product.
	if err := e.DeleteObject(ctx, "Supplier", supplierID, "hard"); err != nil {
		t.Fatalf("DeleteObject(Supplier) err = %v, want nil", err)
	}
	if err := e.DeleteObject(ctx, "Product", productID, "hard"); err != nil {
		t.Fatalf("DeleteObject(Product) err = %v, want nil", err)
	}

	// (13) Engine.GetObject("Supplier", s.id) → not-found.
	if _, err := e.GetObject(ctx, "Supplier", supplierID); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after hard delete err = %v, want ErrObjectNotFound", err)
	}

	// Outcome assertion: the pipeline ran end-to-end without error and
	// every implemented SPI method returned its real behaviour, not
	// ErrUnimplemented. The seven implemented methods exercised here
	// (CreateObject, GetObject, UpdateObject, DeleteObject, CreateLink,
	// GetLink, DeleteLink) all returned non-ErrUnimplemented outcomes
	// — proven by the absence of ErrUnimplemented checks above and the
	// explicit err == nil / typed-Err assertions throughout. This test
	// would fail loudly if any of them hit the unimplemented floor.
}
