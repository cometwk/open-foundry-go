// Package e2e holds the supply-chain Gold Path integration: load the
// real domain pack, project to a storage schema, apply it through the
// memory provider, then walk the Phase 3 verb surface end-to-end
// against the actual ontology types (Supplier, Product, and the
// SuppliesProduct link). This is the Phase 3 acceptance for F8 / R11 /
// AE11 — the single test that proves the Phase 1 → Phase 3 pipeline is
// wired correctly and the implemented SPI methods return zero
// ErrUnimplemented hits.
//
// The Gold Path does NOT copy domain-packs into runtime/. It loads
// the pack from the repo root via pack.SupplyChainDir(), so the
// supply-chain pack stays the source of truth.
package e2e_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/action"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// TestGoldPath_SupplyChain_F8 is the Phase 3 acceptance test. It walks
// the full pipeline: pack load → IR → OntologySchema projection →
// memory.ApplySchema → Engine verbs plus query/traverse/transaction/
// soft-delete on real supply-chain types. Covers AE11 / F8 / R11.
func TestGoldPath_SupplyChain_F8(t *testing.T) {
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

	// (4) Engine.CreateObject("Supplier", …).
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

	// (5) Engine.CreateObject("Product", …).
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

	// F8: QueryObjects after create — Supplier filter by name.
	qpage, err := p.QueryObjects(ctx, "Supplier", spi.FilterExpression{
		Field: "name", Operator: "eq", Value: "Acme",
	}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryObjects after create err = %v, want nil (F8)", err)
	}
	if qpage.TotalCount < 1 {
		t.Fatalf("QueryObjects TotalCount = %d, want >=1 (F8)", qpage.TotalCount)
	}

	// (6) Engine.CreateLink("SuppliesProduct", s.id, p.id, {}).
	link, err := e.CreateLink(ctx, "SuppliesProduct", supplierID, productID, nil)
	if err != nil {
		t.Fatalf("CreateLink(SuppliesProduct) err = %v, want nil", err)
	}
	linkID := link["_id"].(string)
	if link["_type"] != "SuppliesProduct" {
		t.Errorf("CreateLink _type = %v, want SuppliesProduct", link["_type"])
	}

	// F8: GetLinks + Traverse after link.
	glinks, err := p.GetLinks(ctx, supplierID, "SuppliesProduct", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks err = %v, want nil (F8)", err)
	}
	if glinks.TotalCount != 1 {
		t.Fatalf("GetLinks TotalCount = %d, want 1 (F8)", glinks.TotalCount)
	}
	trav, err := p.Traverse(ctx, supplierID, spi.TraversalPath{
		Steps: []spi.TraversalStep{{LinkType: "SuppliesProduct", Direction: "outbound"}},
	}, nil)
	if err != nil {
		t.Fatalf("Traverse err = %v, want nil (F8)", err)
	}
	if trav.TotalCount != 1 {
		t.Fatalf("Traverse TotalCount = %d, want 1 (F8)", trav.TotalCount)
	}

	// F8: BeginTransaction + rollback once.
	tx, err := p.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("BeginTransaction err = %v, want nil (F8)", err)
	}
	tmp, err := tx.CreateObject("Supplier", map[string]any{
		"name":    "Temp",
		"code":    "TMP-001",
		"tier":    "APPROVED",
		"country": "US",
	})
	if err != nil {
		t.Fatalf("tx.CreateObject err = %v (F8)", err)
	}
	tmpID := tmp["_id"].(string)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback err = %v (F8)", err)
	}
	if _, err := e.GetObject(ctx, "Supplier", tmpID); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after tx rollback err = %v, want ErrObjectNotFound (F8)", err)
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
	if updatedSupplier["code"] != "ACME-001" {
		t.Errorf("UpdateObject dropped code = %v, want ACME-001 (merge)", updatedSupplier["code"])
	}

	// (9) Engine.UpdateLink + GetLink round-trip (Phase 3 verb).
	updatedLink, err := e.UpdateLink(ctx, "SuppliesProduct", linkID, map[string]any{"preferredSupplier": true}, nil)
	if err != nil {
		t.Fatalf("UpdateLink err = %v, want nil (F8)", err)
	}
	if updatedLink["preferredSupplier"] != true {
		t.Errorf("UpdateLink preferredSupplier = %v, want true", updatedLink["preferredSupplier"])
	}
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

	// F8: soft-delete Supplier then QueryObjects(includeDeleted:true).
	if err := e.DeleteObject(ctx, "Supplier", supplierID, "soft"); err != nil {
		t.Fatalf("DeleteObject(Supplier soft) err = %v, want nil (F8)", err)
	}
	if _, err := e.GetObject(ctx, "Supplier", supplierID); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after soft delete err = %v, want ErrObjectNotFound (F8)", err)
	}
	incl, err := p.QueryObjects(ctx, "Supplier", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("QueryObjects(includeDeleted) err = %v (F8)", err)
	}
	softSeen := false
	for _, item := range incl.Items {
		if item["_id"] == supplierID {
			softSeen = true
		}
	}
	if !softSeen {
		t.Fatal("QueryObjects(includeDeleted:true) missing soft-deleted Supplier (F8)")
	}

	// (12) Hard delete Product (Supplier already soft-deleted).
	if err := e.DeleteObject(ctx, "Product", productID, "hard"); err != nil {
		t.Fatalf("DeleteObject(Product) err = %v, want nil", err)
	}

	// Outcome: pipeline ran end-to-end with zero ErrUnimplemented hits
	// across the Phase 3 verb surface (query/traverse/tx/soft-delete/
	// UpdateLink included). Covers AE11 / F8 / R11.
}

// TestGoldPath_CreateOrder_F1 is the Phase 4 acceptance: load CreateOrder
// YAML from the real pack, resolve Supplier/Product, pass CEL preconditions,
// then the caller hand-writes CreateObject. Evaluation and CreateObject are
// two steps — no PurchaseOrder exists between them. Covers AE6 / F1.
func TestGoldPath_CreateOrder_F1(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("pack.SupplyChainDir err = %v, want nil", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("pack.LoadDir(supply-chain) err = %v, want nil", err)
	}
	manifests, err := pack.LoadActions(dir, o)
	if err != nil {
		t.Fatalf("pack.LoadActions err = %v, want nil", err)
	}

	p := memory.New()
	ctx := spi.RequestContext{TenantID: "gold-path-order", ActorID: "test"}
	if _, err := p.ApplySchema(ctx, projection.ProjectStorage(o)); err != nil {
		t.Fatalf("ApplySchema err = %v, want nil", err)
	}
	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v, want nil", err)
	}

	supplier, err := e.CreateObject(ctx, "Supplier", map[string]any{
		"name":    "Acme",
		"code":    "ACME-F1",
		"tier":    "STRATEGIC",
		"country": "US",
	})
	if err != nil {
		t.Fatalf("CreateObject(Supplier) err = %v, want nil", err)
	}
	product, err := e.CreateObject(ctx, "Product", map[string]any{
		"sku":             "P-F1",
		"name":            "Widget",
		"category":        "Hardware",
		"unitOfMeasure":   "each",
		"reorderPoint":    5,
		"reorderQuantity": 50,
	})
	if err != nil {
		t.Fatalf("CreateObject(Product) err = %v, want nil", err)
	}

	params := map[string]any{
		"supplier":              supplier["_id"].(string),
		"product":               product["_id"].(string),
		"orderNumber":           "PO-F1-001",
		"quantity":              12,
		"unitCost":              9.5,
		"currency":              "USD",
		"requestedDeliveryDate": "2026-09-01T00:00:00Z",
	}
	result, err := action.Evaluate(ctx, e, o, manifests, action.Request{
		Name:   "CreateOrder",
		Params: params,
		Actor:  action.Actor{ID: "buyer-1", Type: "user", Roles: []string{"procurement_manager"}},
	})
	if err != nil {
		t.Fatalf("Evaluate(CreateOrder) err = %v, want nil", err)
	}

	page, err := p.QueryObjects(ctx, "PurchaseOrder", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatalf("QueryObjects after eval err = %v", err)
	}
	if page.TotalCount != 0 {
		t.Fatalf("PurchaseOrder count after Evaluate = %d, want 0 (eval must not write)", page.TotalCount)
	}

	po, err := e.CreateObject(ctx, "PurchaseOrder", map[string]any{
		"orderNumber":           params["orderNumber"],
		"status":                "DRAFT",
		"supplier":              result.Objects["supplier"]["_id"],
		"product":               result.Objects["product"]["_id"],
		"quantity":              params["quantity"],
		"unitCost":              params["unitCost"],
		"currency":              params["currency"],
		"requestedDeliveryDate": params["requestedDeliveryDate"],
	})
	if err != nil {
		t.Fatalf("CreateObject(PurchaseOrder) err = %v, want nil", err)
	}
	got, err := e.GetObject(ctx, "PurchaseOrder", po["_id"].(string))
	if err != nil {
		t.Fatalf("GetObject(PurchaseOrder) err = %v, want nil", err)
	}
	if got["status"] != "DRAFT" {
		t.Errorf("status = %v, want DRAFT", got["status"])
	}
	if got["supplier"] != supplier["_id"] {
		t.Errorf("supplier = %v, want %v", got["supplier"], supplier["_id"])
	}
	if got["product"] != product["_id"] {
		t.Errorf("product = %v, want %v", got["product"], product["_id"])
	}
	if got["orderNumber"] != "PO-F1-001" {
		t.Errorf("orderNumber = %v, want PO-F1-001", got["orderNumber"])
	}
	if got["quantity"] != 12 && got["quantity"] != float64(12) {
		t.Errorf("quantity = %v (%T), want 12", got["quantity"], got["quantity"])
	}
	if got["currency"] != "USD" {
		t.Errorf("currency = %v, want USD", got["currency"])
	}
}
