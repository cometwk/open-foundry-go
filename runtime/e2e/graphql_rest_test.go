package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestGoldPath_GraphQLREST_HTTP(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir err = %v", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir err = %v", err)
	}
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "gold", ActorID: "test"}
	mr, err := p.ApplySchema(ctx, projection.ProjectStorage(o))
	if err != nil || !mr.Success {
		t.Fatalf("ApplySchema err = %v result = %+v", err, mr)
	}
	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	ids := seedAll(t, e, ctx)
	srv, err := api.New(e)
	if err != nil {
		t.Fatalf("api.New err = %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	t.Run("AE1 get six types", func(t *testing.T) {
		queries := []string{
			`{ product(id: "` + ids.product + `") { id sku name } }`,
			`{ supplier(id: "` + ids.supplier + `") { id name code } }`,
			`{ facility(id: "` + ids.facility + `") { id name } }`,
			`{ purchaseOrder(id: "` + ids.order + `") { id orderNumber } }`,
			`{ shipment(id: "` + ids.shipment + `") { id status } }`,
			`{ inventoryRecord(id: "` + ids.inventory + `") { id quantity } }`,
		}
		for _, q := range queries {
			res := gql(t, ts.URL, "gold", q)
			if len(res.Errors) > 0 {
				t.Fatalf("query %s errors = %v", q, res.Errors)
			}
		}
	})

	t.Run("AE2 suppliers nested", func(t *testing.T) {
		res := gql(t, ts.URL, "gold", `{ product(id: "`+ids.product+`") { suppliers { name } } }`)
		if len(res.Errors) > 0 {
			t.Fatalf("errors = %v", res.Errors)
		}
		list := res.Data["product"].(map[string]any)["suppliers"].([]any)
		if len(list) != 1 || list[0].(map[string]any)["name"] != "Acme" {
			t.Fatalf("suppliers = %v", list)
		}
		bad := gql(t, ts.URL, "gold", `{ product(id: "`+ids.product+`") { suppliers { sku } } }`)
		if len(bad.Errors) == 0 {
			t.Fatal("expected schema error for suppliers { sku }")
		}
	})

	t.Run("list search aggregate", func(t *testing.T) {
		list := gql(t, ts.URL, "gold", `{ products(first: 1) { totalCount edges { node { sku } } pageInfo { hasNextPage } } }`)
		if len(list.Errors) > 0 {
			t.Fatalf("list errors = %v", list.Errors)
		}
		if list.Data["products"].(map[string]any)["totalCount"].(float64) < 1 {
			t.Fatalf("list = %v", list.Data)
		}
		search := gql(t, ts.URL, "gold", `{ searchProducts(query: "Widget") { totalCount hits { node { sku } } } }`)
		if len(search.Errors) > 0 {
			t.Fatalf("search errors = %v", search.Errors)
		}
		if search.Data["searchProducts"].(map[string]any)["totalCount"].(float64) < 1 {
			t.Fatalf("search = %v", search.Data)
		}
		blank := gql(t, ts.URL, "gold", `{ searchProducts(query: "  ") { totalCount } }`)
		if blank.Data["searchProducts"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("blank search = %v", blank.Data)
		}
		agg := gql(t, ts.URL, "gold", `{ productAggregate(fields: [{ field: "*", fn: COUNT, alias: "n" }]) { totalGroups groups { values } } }`)
		if len(agg.Errors) > 0 {
			t.Fatalf("aggregate errors = %v", agg.Errors)
		}
	})

	t.Run("nested lazy and FKs", func(t *testing.T) {
		inv := gql(t, ts.URL, "gold", `{ inventoryRecord(id: "`+ids.inventory+`") { facility { name currentUtilization } } }`)
		if len(inv.Errors) > 0 {
			t.Fatalf("inventoryRecord errors = %v", inv.Errors)
		}
		util := inv.Data["inventoryRecord"].(map[string]any)["facility"].(map[string]any)["currentUtilization"].(float64)
		if util != 1 {
			t.Fatalf("nested currentUtilization = %v, want 1", util)
		}
		sh := gql(t, ts.URL, "gold", `{
			shipment(id: "`+ids.shipment+`") {
				order { orderNumber }
				origin { name }
				destination { name }
			}
		}`)
		if len(sh.Errors) > 0 {
			t.Fatalf("shipment errors = %v", sh.Errors)
		}
		sm := sh.Data["shipment"].(map[string]any)
		if sm["order"].(map[string]any)["orderNumber"] != "PO-1" {
			t.Fatalf("order = %v", sm["order"])
		}
		if sm["origin"].(map[string]any)["name"] != "WH-1" {
			t.Fatalf("origin = %v", sm["origin"])
		}
		if sm["destination"].(map[string]any)["name"] != "WH-1" {
			t.Fatalf("destination = %v", sm["destination"])
		}
	})

	t.Run("AE7 REST product", func(t *testing.T) {
		code, body := rest(t, ts.URL+"/api/v1/product/"+ids.product, "gold")
		if code != 200 {
			t.Fatalf("REST GET status = %d body = %s", code, body)
		}
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		if obj["sku"] != "P1" || obj["id"] != ids.product {
			t.Fatalf("REST = %v", obj)
		}
		code, body = rest(t, ts.URL+"/api/v1/product/missing", "gold")
		if code != 404 || !bytes.Contains(body, []byte("OBJECT_NOT_FOUND")) {
			t.Fatalf("REST miss status = %d body = %s", code, body)
		}
	})

	t.Run("cross tenant", func(t *testing.T) {
		miss := gql(t, ts.URL, "other", `{ product(id: "`+ids.product+`") { sku } }`)
		if miss.Data["product"] != nil {
			t.Fatalf("cross-tenant get = %v, want null", miss.Data["product"])
		}
		list := gql(t, ts.URL, "other", `{ products { totalCount edges { node { id } } } }`)
		if list.Data["products"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("cross-tenant list = %v", list.Data)
		}
		search := gql(t, ts.URL, "other", `{ searchProducts(query: "Widget") { totalCount } }`)
		if search.Data["searchProducts"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("cross-tenant search = %v", search.Data)
		}
		code, _ := rest(t, ts.URL+"/api/v1/product/"+ids.product, "other")
		if code != 404 {
			t.Fatalf("cross-tenant REST status = %d, want 404", code)
		}
	})

	t.Run("missing tenant and auth ignored", func(t *testing.T) {
		code, body := rest(t, ts.URL+"/api/v1/product/"+ids.product, "")
		if code != 400 || !bytes.Contains(body, []byte("MISSING_TENANT")) {
			t.Fatalf("missing tenant REST = %d %s", code, body)
		}
		res := gql(t, ts.URL, "gold", `{ product(id: "`+ids.product+`") { sku } }`)
		if res.Data["product"].(map[string]any)["sku"] != "P1" {
			t.Fatalf("auth-ignored graphql = %v", res.Data)
		}
	})
}

type goldIDs struct {
	product, supplier, facility, order, shipment, inventory string
}

func seedAll(t *testing.T, e *engine.Engine, ctx spi.RequestContext) goldIDs {
	t.Helper()
	supplier, err := e.CreateObject(ctx, "Supplier", map[string]any{
		"name": "Acme", "code": "ACME-001", "tier": "STRATEGIC", "country": "US",
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := e.CreateObject(ctx, "Product", map[string]any{
		"sku": "P1", "name": "Widget", "category": "Hardware",
		"unitOfMeasure": "each", "reorderPoint": 5, "reorderQuantity": 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateLink(ctx, "SuppliesProduct", supplier["_id"].(string), product["_id"].(string), map[string]any{
		"leadTimeDays": 7, "unitCost": 1.5,
	}); err != nil {
		t.Fatal(err)
	}
	fac, err := e.CreateObject(ctx, "Facility", map[string]any{
		"name": "WH-1", "code": "WH1", "type": "WAREHOUSE", "status": "OPERATIONAL",
		"country": "US", "capacity": 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := e.CreateObject(ctx, "InventoryRecord", map[string]any{
		"quantity": 10, "reservedQuantity": 0, "stockLevel": "ADEQUATE",
		"product": product["_id"], "facility": fac["_id"],
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateLink(ctx, "InventoryAt", inv["_id"].(string), fac["_id"].(string), nil); err != nil {
		t.Fatal(err)
	}
	order, err := e.CreateObject(ctx, "PurchaseOrder", map[string]any{
		"orderNumber": "PO-1", "status": "SUBMITTED",
		"supplier": supplier["_id"], "product": product["_id"],
		"quantity": 5, "unitCost": 2.0, "currency": "USD",
		"requestedDeliveryDate": "2026-09-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	ship, err := e.CreateObject(ctx, "Shipment", map[string]any{
		"status": "PENDING", "transportMode": "ROAD", "quantity": 5,
		"order": order["_id"], "origin": fac["_id"], "destination": fac["_id"],
	})
	if err != nil {
		t.Fatal(err)
	}
	return goldIDs{
		product:   product["_id"].(string),
		supplier:  supplier["_id"].(string),
		facility:  fac["_id"].(string),
		order:     order["_id"].(string),
		shipment:  ship["_id"].(string),
		inventory: inv["_id"].(string),
	}
}

type gqlRes struct {
	Data   map[string]any `json:"data"`
	Errors []any          `json:"errors"`
}

func gql(t *testing.T, base, tenant, query string) gqlRes {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"query": query})
	req, err := http.NewRequest(http.MethodPost, base+"/graphql", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenFoundry-Tenant", tenant)
	req.Header.Set("Authorization", "Bearer ignored")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("graphql HTTP %d body %s", resp.StatusCode, raw)
	}
	var out gqlRes
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func rest(t *testing.T, url, tenant string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tenant != "" {
		req.Header.Set("X-OpenFoundry-Tenant", tenant)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}
