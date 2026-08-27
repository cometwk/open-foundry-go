package action_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/action"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

type evalFixture struct {
	ctx       spi.RequestContext
	engine    *engine.Engine
	provider  *memory.Provider
	onto      *ir.Ontology
	manifests []action.Manifest
}

func setupEval(t *testing.T) evalFixture {
	t.Helper()
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir: %v", err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	manifests, err := pack.LoadActions(dir, onto)
	if err != nil {
		t.Fatalf("LoadActions: %v", err)
	}
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "tnt", ActorID: "test"}
	if _, err := p.ApplySchema(ctx, projection.ProjectStorage(onto)); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	e, err := engine.New(p, onto)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	return evalFixture{ctx: ctx, engine: e, provider: p, onto: onto, manifests: manifests}
}

func createSupplier(t *testing.T, f evalFixture, code, tier string) string {
	t.Helper()
	obj, err := f.engine.CreateObject(f.ctx, "Supplier", map[string]any{
		"name": code, "code": code, "tier": tier, "country": "US",
	})
	if err != nil {
		t.Fatalf("CreateObject(Supplier) err = %v", err)
	}
	return obj["_id"].(string)
}

func createProduct(t *testing.T, f evalFixture, sku string) string {
	t.Helper()
	obj, err := f.engine.CreateObject(f.ctx, "Product", map[string]any{
		"sku": sku, "name": "Widget", "category": "Hardware",
		"unitOfMeasure": "each", "reorderPoint": 5, "reorderQuantity": 50,
	})
	if err != nil {
		t.Fatalf("CreateObject(Product) err = %v", err)
	}
	return obj["_id"].(string)
}

func orderParams(supplierID, productID string, quantity int) map[string]any {
	return map[string]any{
		"supplier":              supplierID,
		"product":               productID,
		"orderNumber":           "PO-1",
		"quantity":              quantity,
		"unitCost":              1.5,
		"currency":              "USD",
		"requestedDeliveryDate": "2026-09-01T00:00:00Z",
	}
}

func managerActor() action.Actor {
	return action.Actor{ID: "u1", Type: "user", Roles: []string{"procurement_manager"}}
}

func viewerActor() action.Actor {
	return action.Actor{ID: "u1", Type: "user", Roles: []string{"viewer"}}
}

func countPurchaseOrders(t *testing.T, f evalFixture) int {
	t.Helper()
	page, err := f.provider.QueryObjects(f.ctx, "PurchaseOrder", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatalf("QueryObjects(PurchaseOrder): %v", err)
	}
	return page.TotalCount
}

func evaluate(f evalFixture, params map[string]any, actor action.Actor) (*action.Result, error) {
	return action.Evaluate(f.ctx, f.engine, f.onto, f.manifests, action.Request{
		Name:   "CreateOrder",
		Params: params,
		Actor:  actor,
	})
}

func TestEvaluate_UnknownSupplierIsObjectNotFound(t *testing.T) {
	f := setupEval(t)
	productID := createProduct(t, f, "P-missing-supplier")
	_, err := evaluate(f, orderParams("no-such-supplier", productID, 10), managerActor())
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
	if errors.Is(err, spi.ErrPreconditionFailed) {
		t.Fatal("unknown supplier must not be ErrPreconditionFailed")
	}
}

func TestEvaluate_MissingRoleIsPreconditionFailed(t *testing.T) {
	f := setupEval(t)
	sid := createSupplier(t, f, "ACME-ROLE", "STRATEGIC")
	pid := createProduct(t, f, "P-role")
	_, err := evaluate(f, orderParams(sid, pid, 10), viewerActor())
	if !errors.Is(err, spi.ErrPreconditionFailed) {
		t.Fatalf("err = %v, want ErrPreconditionFailed", err)
	}
	want := "Only procurement managers or supply chain admins can create orders"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want message %q", err, want)
	}
}

func TestEvaluate_SuccessDoesNotWritePurchaseOrder(t *testing.T) {
	f := setupEval(t)
	sid := createSupplier(t, f, "ACME-OK", "STRATEGIC")
	pid := createProduct(t, f, "P-ok")
	before := countPurchaseOrders(t, f)
	result, err := evaluate(f, orderParams(sid, pid, 10), managerActor())
	if err != nil {
		t.Fatalf("Evaluate err = %v, want nil", err)
	}
	if result == nil || result.Objects["supplier"] == nil || result.Objects["product"] == nil {
		t.Fatalf("Result.Objects missing supplier/product: %+v", result)
	}
	if result.Objects["supplier"]["_id"] != sid {
		t.Errorf("resolved supplier _id = %v, want %s", result.Objects["supplier"]["_id"], sid)
	}
	if countPurchaseOrders(t, f) != before {
		t.Fatal("successful Evaluate must not CreateObject PurchaseOrder")
	}
}

func TestEvaluate_ProbationSupplier(t *testing.T) {
	f := setupEval(t)
	sid := createSupplier(t, f, "ACME-PROB", "PROBATION")
	pid := createProduct(t, f, "P-prob")
	_, err := evaluate(f, orderParams(sid, pid, 10), managerActor())
	if !errors.Is(err, spi.ErrPreconditionFailed) {
		t.Fatalf("err = %v, want ErrPreconditionFailed", err)
	}
	want := "Cannot place orders with suppliers on probation"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want message %q", err, want)
	}
}

func TestEvaluate_ZeroQuantity(t *testing.T) {
	f := setupEval(t)
	sid := createSupplier(t, f, "ACME-QTY", "STRATEGIC")
	pid := createProduct(t, f, "P-qty")
	_, err := evaluate(f, orderParams(sid, pid, 0), managerActor())
	if !errors.Is(err, spi.ErrPreconditionFailed) {
		t.Fatalf("err = %v, want ErrPreconditionFailed", err)
	}
	want := "Order quantity must be greater than zero"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want message %q", err, want)
	}
}

func TestEvaluate_ProbationAndMissingRoleReturnsFirstManifestError(t *testing.T) {
	f := setupEval(t)
	sid := createSupplier(t, f, "ACME-BOTH", "PROBATION")
	pid := createProduct(t, f, "P-both")
	_, err := evaluate(f, orderParams(sid, pid, 10), viewerActor())
	if !errors.Is(err, spi.ErrPreconditionFailed) {
		t.Fatalf("err = %v, want ErrPreconditionFailed", err)
	}
	if !strings.Contains(err.Error(), "Cannot place orders with suppliers on probation") {
		t.Errorf("err = %v, want first manifest error (probation)", err)
	}
	if strings.Contains(err.Error(), "Only procurement managers") {
		t.Errorf("err = %v, must not skip to the role error", err)
	}
}

func TestEvaluate_SupplierMissingTierIsCelEval(t *testing.T) {
	f := setupEval(t)
	sid := createSupplier(t, f, "ACME-NOTIER", "STRATEGIC")
	pid := createProduct(t, f, "P-notier")
	if _, err := f.provider.UpdateObject(f.ctx, "Supplier", sid, map[string]any{"tier": nil}, nil); err != nil {
		t.Fatalf("UpdateObject to clear tier: %v", err)
	}
	_, err := evaluate(f, orderParams(sid, pid, 10), managerActor())
	if !errors.Is(err, spi.ErrCelEval) {
		t.Fatalf("err = %v, want ErrCelEval", err)
	}
	if errors.Is(err, spi.ErrPreconditionFailed) {
		t.Fatal("missing tier must not be the probation precondition")
	}
	if strings.Contains(err.Error(), "Cannot place orders with suppliers on probation") {
		t.Errorf("err = %v, must not carry probation manifest text", err)
	}
}
