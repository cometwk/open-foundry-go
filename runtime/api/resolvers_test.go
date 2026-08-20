package api

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	projgql "github.com/openfoundry/runtime/projection/graphql"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestNew_SupplyChainSchemaParses(t *testing.T) {
	s := newSupplyChainAPI(t)
	if s.Schema() == nil {
		t.Fatal("Schema is nil")
	}
}

func TestNode_CoversSupplyChainFields(t *testing.T) {
	o := loadSupplyChainIR(t)
	nt := reflect.TypeOf((*node)(nil))
	for _, obj := range o.Objects {
		for _, f := range obj.Fields {
			if !nodeResolves(nt, f.Name) {
				t.Errorf("node missing resolver for %s.%s", obj.Name, f.Name)
			}
		}
	}
}

func nodeResolves(t reflect.Type, gqlName string) bool {
	want := strings.ReplaceAll(gqlName, "_", "")
	for i := 0; i < t.NumMethod(); i++ {
		got := strings.ReplaceAll(t.Method(i).Name, "_", "")
		if strings.EqualFold(want, got) {
			return true
		}
	}
	return false
}

func TestExec_ProductGetAndMiss(t *testing.T) {
	s, ids := seedSupplyChain(t)
	rc := tenantRC("gold")
	res := s.Exec(context.Background(), rc, `{ product(id: "`+ids.product+`") { id sku name } }`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("product get errors = %v", res.Errors)
	}
	data := decodeData(t, res.Data)
	p := data["product"].(map[string]any)
	if p["sku"] != "P1" || p["name"] != "Widget" {
		t.Fatalf("product = %+v", p)
	}
	if p["id"] != ids.product {
		t.Fatalf("product.id = %v want %s", p["id"], ids.product)
	}

	miss := s.Exec(context.Background(), rc, `{ product(id: "bogus") { id sku } }`, nil)
	if len(miss.Errors) > 0 {
		t.Fatalf("bogus get errors = %v, want none", miss.Errors)
	}
	missData := decodeData(t, miss.Data)
	if missData["product"] != nil {
		t.Fatalf("bogus product = %v, want null", missData["product"])
	}
}

func TestExec_ProductSuppliersLink(t *testing.T) {
	s, ids := seedSupplyChain(t)
	rc := tenantRC("gold")

	empty := s.Exec(context.Background(), rc, `{ product(id: "`+ids.productNoLink+`") { suppliers { name } } }`, nil)
	if len(empty.Errors) > 0 {
		t.Fatalf("empty suppliers errors = %v", empty.Errors)
	}
	emptyData := decodeData(t, empty.Data)
	suppliers := emptyData["product"].(map[string]any)["suppliers"].([]any)
	if len(suppliers) != 0 {
		t.Fatalf("empty suppliers = %v, want []", suppliers)
	}

	got := s.Exec(context.Background(), rc, `{ product(id: "`+ids.product+`") { suppliers { name code } } }`, nil)
	if len(got.Errors) > 0 {
		t.Fatalf("suppliers errors = %v", got.Errors)
	}
	gotData := decodeData(t, got.Data)
	list := gotData["product"].(map[string]any)["suppliers"].([]any)
	if len(list) != 1 {
		t.Fatalf("suppliers len = %d, want 1", len(list))
	}
	name := list[0].(map[string]any)["name"]
	if name != "Acme" {
		t.Fatalf("supplier.name = %v, want Acme", name)
	}

	bad := s.Exec(context.Background(), rc, `{ product(id: "`+ids.product+`") { suppliers { sku } } }`, nil)
	if len(bad.Errors) == 0 {
		t.Fatal("suppliers { sku } expected schema error (sku is not a Supplier field)")
	}
}

func TestExec_ListFilterPagination(t *testing.T) {
	s, _ := seedSupplyChain(t)
	rc := tenantRC("gold")
	res := s.Exec(context.Background(), rc, `{
		products(filter: { name: { eq: "Widget" } }, first: 1) {
			totalCount
			edges { node { sku } cursor }
			pageInfo { hasNextPage hasPreviousPage }
		}
	}`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("products errors = %v", res.Errors)
	}
	data := decodeData(t, res.Data)
	conn := data["products"].(map[string]any)
	if conn["totalCount"].(float64) < 1 {
		t.Fatalf("totalCount = %v", conn["totalCount"])
	}
	edges := conn["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}

	bad := s.Exec(context.Background(), rc, `{ products(after: "not-a-cursor") { totalCount } }`, nil)
	if len(bad.Errors) == 0 {
		t.Fatal("invalid cursor expected error")
	}
}

func TestExec_AggregateAndSearch(t *testing.T) {
	s, _ := seedSupplyChain(t)
	rc := tenantRC("gold")
	agg := s.Exec(context.Background(), rc, `{
		productAggregate(fields: [{ field: "*", fn: COUNT, alias: "n" }]) {
			totalGroups
			groups { values }
		}
	}`, nil)
	if len(agg.Errors) > 0 {
		t.Fatalf("aggregate errors = %v", agg.Errors)
	}
	aggData := decodeData(t, agg.Data)
	groups := aggData["productAggregate"].(map[string]any)["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("groups = %d", len(groups))
	}

	hit := s.Exec(context.Background(), rc, `{ searchProducts(query: "Widget") { totalCount hits { node { sku } } } }`, nil)
	if len(hit.Errors) > 0 {
		t.Fatalf("search errors = %v", hit.Errors)
	}
	hitData := decodeData(t, hit.Data)
	if hitData["searchProducts"].(map[string]any)["totalCount"].(float64) < 1 {
		t.Fatalf("search totalCount = %v", hitData["searchProducts"])
	}

	blank := s.Exec(context.Background(), rc, `{ searchProducts(query: "  ") { totalCount hits { node { id } } } }`, nil)
	if len(blank.Errors) > 0 {
		t.Fatalf("blank search errors = %v", blank.Errors)
	}
	blankData := decodeData(t, blank.Data)
	if blankData["searchProducts"].(map[string]any)["totalCount"].(float64) != 0 {
		t.Fatalf("blank search want 0 hits, got %v", blankData["searchProducts"])
	}
}

func TestExec_PurchaseOrderFK(t *testing.T) {
	s, ids := seedSupplyChain(t)
	rc := tenantRC("gold")
	res := s.Exec(context.Background(), rc, `{
		purchaseOrder(id: "`+ids.order+`") { orderNumber supplier { name } product { sku } }
	}`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("purchaseOrder errors = %v", res.Errors)
	}
	data := decodeData(t, res.Data)
	po := data["purchaseOrder"].(map[string]any)
	if po["supplier"].(map[string]any)["name"] != "Acme" {
		t.Fatalf("supplier = %v", po["supplier"])
	}
	if po["product"].(map[string]any)["sku"] != "P1" {
		t.Fatalf("product = %v", po["product"])
	}

	missing := s.Exec(context.Background(), rc, `{
		purchaseOrder(id: "`+ids.orderMissingFK+`") { supplier { name } }
	}`, nil)
	if len(missing.Errors) > 0 {
		t.Fatalf("missing FK errors = %v", missing.Errors)
	}
	missData := decodeData(t, missing.Data)
	if missData["purchaseOrder"].(map[string]any)["supplier"] != nil {
		t.Fatalf("missing FK supplier = %v, want null", missData["purchaseOrder"])
	}
}

func TestExec_ComputedSelectionAware(t *testing.T) {
	rec := &getLinksCounter{inner: memory.New()}
	s, ids := seedSupplyChainOn(t, rec)
	rc := tenantRC("gold")

	noSel := s.Exec(context.Background(), rc, `{ facility(id: "`+ids.facility+`") { name } }`, nil)
	if len(noSel.Errors) > 0 {
		t.Fatalf("facility name errors = %v", noSel.Errors)
	}
	if rec.n != 0 {
		t.Fatalf("GetLinks called %d times without selecting currentUtilization, want 0", rec.n)
	}

	sel := s.Exec(context.Background(), rc, `{ facility(id: "`+ids.facility+`") { currentUtilization } }`, nil)
	if len(sel.Errors) > 0 {
		t.Fatalf("currentUtilization errors = %v", sel.Errors)
	}
	if rec.n == 0 {
		t.Fatal("GetLinks not called when currentUtilization selected")
	}
	data := decodeData(t, sel.Data)
	util := data["facility"].(map[string]any)["currentUtilization"].(float64)
	if util != 1 {
		t.Fatalf("currentUtilization = %v, want 1", util)
	}
}

func TestExec_OneHopLink_GetLinksNotTraverse(t *testing.T) {
	rec := &getLinksCounter{inner: memory.New()}
	s, ids := seedSupplyChainOn(t, rec)
	rc := tenantRC("gold")
	rec.n, rec.t = 0, 0
	res := s.Exec(context.Background(), rc, `{ product(id: "`+ids.product+`") { suppliers { name } } }`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	if rec.n != 1 || rec.t != 0 {
		t.Fatalf("GetLinks/Traverse = %d/%d, want 1/0", rec.n, rec.t)
	}
}

func TestExec_TwoHopLink_TraverseNotGetLinks(t *testing.T) {
	rec := &getLinksCounter{inner: memory.New()}
	s, ids := seedSupplyChainOn(t, rec)
	e := s.Engine()
	if _, err := e.CreateLink(tenantRC("gold"), "InventoryOf", ids.inventory, ids.product, nil); err != nil {
		t.Fatalf("CreateLink InventoryOf err = %v", err)
	}
	rc := tenantRC("gold")
	rec.n, rec.t = 0, 0
	res := s.Exec(context.Background(), rc, `{
		facility(id: "`+ids.facility+`") {
			inventoryRecords { quantity trackedProduct { name } }
		}
	}`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	if rec.t < 1 {
		t.Fatalf("Traverse = %d, want >= 1", rec.t)
	}
	if rec.n != 0 {
		t.Fatalf("GetLinks = %d, want 0 for nested @link path", rec.n)
	}
	data := decodeData(t, res.Data)
	recs := data["facility"].(map[string]any)["inventoryRecords"].([]any)
	if len(recs) != 1 {
		t.Fatalf("inventoryRecords = %v", recs)
	}
	tp := recs[0].(map[string]any)["trackedProduct"].(map[string]any)
	if tp["name"] != "Widget" {
		t.Fatalf("trackedProduct = %v", tp)
	}
	rec.n, rec.t = 0, 0
	again := s.Exec(context.Background(), rc, `{
		facility(id: "`+ids.facility+`") {
			inventoryRecords { trackedProduct { name } }
		}
	}`, nil)
	if len(again.Errors) > 0 {
		t.Fatalf("second exec errors = %v", again.Errors)
	}
	if rec.t != 1 {
		t.Fatalf("child resolvers must not re-Traverse; Traverse = %d, want 1", rec.t)
	}
}

func TestExec_TrackedProduct_RequiresLinkNotFK(t *testing.T) {
	s, ids := seedSupplyChain(t)
	rc := tenantRC("gold")
	res := s.Exec(context.Background(), rc, `{
		inventoryRecord(id: "`+ids.inventory+`") {
			product { name }
			trackedProduct { name }
		}
	}`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	data := decodeData(t, res.Data)
	inv := data["inventoryRecord"].(map[string]any)
	if inv["product"].(map[string]any)["name"] != "Widget" {
		t.Fatalf("FK product = %v", inv["product"])
	}
	if inv["trackedProduct"] != nil {
		t.Fatalf("trackedProduct = %v, want null without InventoryOf", inv["trackedProduct"])
	}
}

func TestExec_CycleAssemblesByLayer(t *testing.T) {
	s, ids := seedSupplyChain(t)
	rc := tenantRC("gold")
	res := s.Exec(context.Background(), rc, `{
		product(id: "`+ids.product+`") {
			sku
			suppliers { products { sku } }
		}
	}`, nil)
	if len(res.Errors) > 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	data := decodeData(t, res.Data)
	suppliers := data["product"].(map[string]any)["suppliers"].([]any)
	products := suppliers[0].(map[string]any)["products"].([]any)
	if len(products) != 1 || products[0].(map[string]any)["sku"] != "P1" {
		t.Fatalf("cycle products = %v, want original P1 at second layer", products)
	}
}

func TestExec_SyntheticForkAndMixed(t *testing.T) {
	rec := &getLinksCounter{inner: memory.New()}
	s, ids := seedSynthetic(t, rec)
	rc := tenantRC("syn")

	rec.n, rec.t = 0, 0
	fork := s.Exec(context.Background(), rc, `{ a(id: "`+ids.a+`") { b { c { name } d { name } } } }`, nil)
	if len(fork.Errors) > 0 {
		t.Fatalf("fork errors = %v", fork.Errors)
	}
	if rec.t != 2 || rec.n != 0 {
		t.Fatalf("fork GetLinks/Traverse = %d/%d, want 0/2", rec.n, rec.t)
	}
	data := decodeData(t, fork.Data)
	b := data["a"].(map[string]any)["b"].([]any)
	if len(b) != 1 {
		t.Fatalf("fork b = %v", b)
	}
	bm := b[0].(map[string]any)
	if len(bm["c"].([]any)) != 1 || len(bm["d"].([]any)) != 1 {
		t.Fatalf("fork c/d = %v", bm)
	}

	rec.n, rec.t = 0, 0
	mixed := s.Exec(context.Background(), rc, `{ a(id: "`+ids.a+`") { leaf { name } b { c { name } } } }`, nil)
	if len(mixed.Errors) > 0 {
		t.Fatalf("mixed errors = %v", mixed.Errors)
	}
	if rec.n != 1 || rec.t != 1 {
		t.Fatalf("mixed GetLinks/Traverse = %d/%d, want 1/1", rec.n, rec.t)
	}
}

func TestSDL_NoRootTraverseOrFollow(t *testing.T) {
	o := loadSupplyChainIR(t)
	sdl := projgql.Generate(o, spi.StorageCapabilities{SupportsFullTextSearch: true})
	if strings.Contains(sdl, "traverse(") || strings.Contains(sdl, "follow(") {
		t.Fatalf("SDL must not expose root traverse/follow")
	}
}

func seedSynthetic(t *testing.T, store spi.StorageProvider) (*Server, struct{ a string }) {
	t.Helper()
	ont := &ir.Ontology{
		Namespace: &ir.Namespace{Name: "syn"},
		Objects: []ir.ObjectType{
			{Name: "A", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				{Name: "b", Type: ir.TypeRef{Name: "B", IsList: true, NonNull: true, ListElementNonNull: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "AB", Direction: ir.DirectionOutbound}},
				{Name: "leaf", Type: ir.TypeRef{Name: "L", IsList: true, NonNull: true, ListElementNonNull: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "AL", Direction: ir.DirectionOutbound}},
			}},
			{Name: "B", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				{Name: "c", Type: ir.TypeRef{Name: "C", IsList: true, NonNull: true, ListElementNonNull: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "BC", Direction: ir.DirectionOutbound}},
				{Name: "d", Type: ir.TypeRef{Name: "D", IsList: true, NonNull: true, ListElementNonNull: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "BD", Direction: ir.DirectionOutbound}},
			}},
			{Name: "C", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
			}},
			{Name: "D", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
			}},
			{Name: "L", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID", NonNull: true}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
			}},
		},
		Links: []ir.LinkType{
			{Name: "AB", From: "A", To: "B", Cardinality: ir.CardinalityOneToMany},
			{Name: "AL", From: "A", To: "L", Cardinality: ir.CardinalityOneToMany},
			{Name: "BC", From: "B", To: "C", Cardinality: ir.CardinalityOneToMany},
			{Name: "BD", From: "B", To: "D", Cardinality: ir.CardinalityOneToMany},
		},
	}
	ctx := tenantRC("syn")
	if _, err := store.ApplySchema(ctx, projection.ProjectStorage(ont)); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	e, err := engine.New(store, ont)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	srv, err := New(e)
	if err != nil {
		t.Fatalf("api.New err = %v", err)
	}
	a, _ := e.CreateObject(ctx, "A", map[string]any{"name": "root"})
	b, _ := e.CreateObject(ctx, "B", map[string]any{"name": "B1"})
	c, _ := e.CreateObject(ctx, "C", map[string]any{"name": "C1"})
	d, _ := e.CreateObject(ctx, "D", map[string]any{"name": "D1"})
	l, _ := e.CreateObject(ctx, "L", map[string]any{"name": "L1"})
	if _, err := e.CreateLink(ctx, "AB", a[spi.FieldID].(string), b[spi.FieldID].(string), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateLink(ctx, "AL", a[spi.FieldID].(string), l[spi.FieldID].(string), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateLink(ctx, "BC", b[spi.FieldID].(string), c[spi.FieldID].(string), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateLink(ctx, "BD", b[spi.FieldID].(string), d[spi.FieldID].(string), nil); err != nil {
		t.Fatal(err)
	}
	return srv, struct{ a string }{a: a[spi.FieldID].(string)}
}

type seedIDs struct {
	product, productNoLink, supplier, order, orderMissingFK, facility, inventory string
}

func tenantRC(tenant string) spi.RequestContext {
	return spi.RequestContext{TenantID: tenant, ActorID: "test", TraceID: "test"}
}

func newSupplyChainAPI(t *testing.T) *Server {
	t.Helper()
	p := memory.New()
	e := newSupplyChainEngine(t, p)
	s, err := New(e)
	if err != nil {
		t.Fatalf("api.New err = %v", err)
	}
	return s
}

func seedSupplyChain(t *testing.T) (*Server, seedIDs) {
	t.Helper()
	return seedSupplyChainOn(t, memory.New())
}

func seedSupplyChainOn(t *testing.T, store spi.StorageProvider) (*Server, seedIDs) {
	t.Helper()
	e := newSupplyChainEngine(t, store)
	s, err := New(e)
	if err != nil {
		t.Fatalf("api.New err = %v", err)
	}
	ids := seedGold(t, e, tenantRC("gold"))
	return s, ids
}

func newSupplyChainEngine(t *testing.T, store spi.StorageProvider) *engine.Engine {
	t.Helper()
	o := loadSupplyChainIR(t)
	schema := projection.ProjectStorage(o)
	ctx := tenantRC("gold")
	mr, err := store.ApplySchema(ctx, schema)
	if err != nil || !mr.Success {
		t.Fatalf("ApplySchema err = %v result = %+v", err, mr)
	}
	e, err := engine.New(store, o)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	return e
}

func loadSupplyChainIR(t *testing.T) *ir.Ontology {
	t.Helper()
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir err = %v", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir err = %v", err)
	}
	return o
}

func seedGold(t *testing.T, e *engine.Engine, ctx spi.RequestContext) seedIDs {
	t.Helper()
	supplier, err := e.CreateObject(ctx, "Supplier", map[string]any{
		"name": "Acme", "code": "ACME-001", "tier": "STRATEGIC", "country": "US",
	})
	if err != nil {
		t.Fatalf("CreateObject Supplier err = %v", err)
	}
	product, err := e.CreateObject(ctx, "Product", map[string]any{
		"sku": "P1", "name": "Widget", "category": "Hardware",
		"unitOfMeasure": "each", "reorderPoint": 5, "reorderQuantity": 50,
	})
	if err != nil {
		t.Fatalf("CreateObject Product err = %v", err)
	}
	product2, err := e.CreateObject(ctx, "Product", map[string]any{
		"sku": "P2", "name": "Gadget", "category": "Hardware",
		"unitOfMeasure": "each", "reorderPoint": 1, "reorderQuantity": 10,
	})
	if err != nil {
		t.Fatalf("CreateObject Product P2 err = %v", err)
	}
	if _, err := e.CreateLink(ctx, "SuppliesProduct", supplier[spi.FieldID].(string), product[spi.FieldID].(string), map[string]any{
		"leadTimeDays": 7, "unitCost": 1.5,
	}); err != nil {
		t.Fatalf("CreateLink SuppliesProduct err = %v", err)
	}
	fac, err := e.CreateObject(ctx, "Facility", map[string]any{
		"name": "WH-1", "code": "WH1", "type": "WAREHOUSE", "status": "OPERATIONAL",
		"country": "US", "capacity": 100,
	})
	if err != nil {
		t.Fatalf("CreateObject Facility err = %v", err)
	}
	inv, err := e.CreateObject(ctx, "InventoryRecord", map[string]any{
		"quantity": 10, "reservedQuantity": 0, "stockLevel": "ADEQUATE",
		"product": product[spi.FieldID], "facility": fac[spi.FieldID],
	})
	if err != nil {
		t.Fatalf("CreateObject InventoryRecord err = %v", err)
	}
	if _, err := e.CreateLink(ctx, "InventoryAt", inv[spi.FieldID].(string), fac[spi.FieldID].(string), nil); err != nil {
		t.Fatalf("CreateLink InventoryAt err = %v", err)
	}
	order, err := e.CreateObject(ctx, "PurchaseOrder", map[string]any{
		"orderNumber": "PO-1", "status": "SUBMITTED",
		"supplier": supplier[spi.FieldID], "product": product[spi.FieldID],
		"quantity": 5, "unitCost": 2.0, "currency": "USD",
		"requestedDeliveryDate": "2026-09-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateObject PurchaseOrder err = %v", err)
	}
	order2, err := e.CreateObject(ctx, "PurchaseOrder", map[string]any{
		"orderNumber": "PO-2", "status": "DRAFT",
		"supplier": "missing-supplier", "product": product[spi.FieldID],
		"quantity": 1, "unitCost": 1.0, "currency": "USD",
		"requestedDeliveryDate": "2026-09-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateObject PurchaseOrder missing FK err = %v", err)
	}
	if _, err := e.CreateObject(ctx, "Shipment", map[string]any{
		"status": "PENDING", "transportMode": "ROAD", "quantity": 5,
		"order": order[spi.FieldID], "origin": fac[spi.FieldID], "destination": fac[spi.FieldID],
	}); err != nil {
		t.Fatalf("CreateObject Shipment err = %v", err)
	}
	return seedIDs{
		product:        product[spi.FieldID].(string),
		productNoLink:  product2[spi.FieldID].(string),
		supplier:       supplier[spi.FieldID].(string),
		order:          order[spi.FieldID].(string),
		orderMissingFK: order2[spi.FieldID].(string),
		facility:       fac[spi.FieldID].(string),
		inventory:      inv[spi.FieldID].(string),
	}
}

func decodeData(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode data: %v (%s)", err, raw)
	}
	return data
}

type getLinksCounter struct {
	spi.UnimplementedStorageProvider
	inner spi.StorageProvider
	n     int
	t     int
}

func (c *getLinksCounter) ApplySchema(ctx spi.RequestContext, s spi.OntologySchema) (spi.MigrationResult, error) {
	return c.inner.ApplySchema(ctx, s)
}
func (c *getLinksCounter) Capabilities() spi.StorageCapabilities { return c.inner.Capabilities() }
func (c *getLinksCounter) CreateObject(ctx spi.RequestContext, typ string, p map[string]any) (spi.OntologyObject, error) {
	return c.inner.CreateObject(ctx, typ, p)
}
func (c *getLinksCounter) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	return c.inner.GetObject(ctx, typ, id)
}
func (c *getLinksCounter) CreateLink(ctx spi.RequestContext, typ, fromID, toID string, p map[string]any) (spi.OntologyLink, error) {
	return c.inner.CreateLink(ctx, typ, fromID, toID, p)
}
func (c *getLinksCounter) GetLinks(ctx spi.RequestContext, objectID, linkType, direction string, options *spi.QueryOptions) (spi.LinkPage, error) {
	c.n++
	return c.inner.GetLinks(ctx, objectID, linkType, direction, options)
}
func (c *getLinksCounter) Traverse(ctx spi.RequestContext, startID string, path spi.TraversalPath, options *spi.TraversalOptions) (spi.TraversalResult, error) {
	c.t++
	return c.inner.Traverse(ctx, startID, path, options)
}
func (c *getLinksCounter) QueryObjects(ctx spi.RequestContext, typ string, filter spi.FilterExpression, options *spi.QueryOptions) (spi.ObjectPage, error) {
	return c.inner.QueryObjects(ctx, typ, filter, options)
}
func (c *getLinksCounter) AggregateObjects(ctx spi.RequestContext, typ string, q spi.AggregateQuery) (spi.AggregateResult, error) {
	return c.inner.AggregateObjects(ctx, typ, q)
}
func (c *getLinksCounter) SearchObjects(ctx spi.RequestContext, typ string, q spi.SearchQuery) (spi.SearchResult, error) {
	return c.inner.SearchObjects(ctx, typ, q)
}
func (c *getLinksCounter) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	return c.inner.DeleteObject(ctx, typ, id, mode)
}
func (c *getLinksCounter) GetLink(ctx spi.RequestContext, typ, id string) (spi.OntologyLink, error) {
	return c.inner.GetLink(ctx, typ, id)
}
func (c *getLinksCounter) UpdateObject(ctx spi.RequestContext, typ, id string, p map[string]any, ev *int) (spi.OntologyObject, error) {
	return c.inner.UpdateObject(ctx, typ, id, p, ev)
}
func (c *getLinksCounter) UpdateLink(ctx spi.RequestContext, typ, id string, p map[string]any, ev *int) (spi.OntologyLink, error) {
	return c.inner.UpdateLink(ctx, typ, id, p, ev)
}
func (c *getLinksCounter) DeleteLink(ctx spi.RequestContext, typ, id string) error {
	return c.inner.DeleteLink(ctx, typ, id)
}
