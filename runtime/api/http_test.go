package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTP_GraphQLAndRESTProduct(t *testing.T) {
	s, ids := seedSupplyChain(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	gql := graphqlPOST(t, ts.URL, "gold", `{ product(id: "`+ids.product+`") { id sku name } }`, "")
	if len(gql.Errors) > 0 {
		t.Fatalf("graphql errors = %v", gql.Errors)
	}
	prod := gql.Data["product"].(map[string]any)
	if prod["sku"] != "P1" {
		t.Fatalf("graphql sku = %v", prod["sku"])
	}

	code, body := restGET(t, ts.URL+"/api/v1/product/"+ids.product, "gold", "")
	if code != 200 {
		t.Fatalf("REST GET status = %d body = %s", code, body)
	}
	var rest map[string]any
	if err := json.Unmarshal(body, &rest); err != nil {
		t.Fatalf("REST JSON: %v", err)
	}
	if rest["id"] != ids.product || rest["sku"] != "P1" {
		t.Fatalf("REST body = %v", rest)
	}
	if _, ok := rest["_id"]; ok {
		t.Fatal("REST exposed _id")
	}

	code, body = restGET(t, ts.URL+"/api/v1/product/no-such", "gold", "")
	if code != 404 {
		t.Fatalf("unknown id status = %d, want 404", code)
	}
	if !bytes.Contains(body, []byte(`OBJECT_NOT_FOUND`)) {
		t.Fatalf("404 body = %s", body)
	}
}

func TestHTTP_TenantHeader(t *testing.T) {
	s, ids := seedSupplyChain(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	for _, tenant := range []string{"", "   "} {
		code, body := restGET(t, ts.URL+"/api/v1/product/"+ids.product, tenant, "")
		if code != 400 {
			t.Fatalf("tenant %q status = %d, want 400", tenant, code)
		}
		if !bytes.Contains(body, []byte(`MISSING_TENANT`)) {
			t.Fatalf("tenant %q body = %s", tenant, body)
		}
		gql := graphqlPOST(t, ts.URL, tenant, `{ product(id: "x") { id } }`, "")
		if gql.HTTPStatus != 400 {
			t.Fatalf("graphql tenant %q status = %d, want 400", tenant, gql.HTTPStatus)
		}
	}

	withAuth := graphqlPOST(t, ts.URL, "gold", `{ product(id: "`+ids.product+`") { sku } }`, "Bearer ignored")
	if len(withAuth.Errors) > 0 {
		t.Fatalf("Authorization should be ignored, errors = %v", withAuth.Errors)
	}
	if withAuth.Data["product"].(map[string]any)["sku"] != "P1" {
		t.Fatalf("with Authorization data = %v", withAuth.Data)
	}
}

func TestHTTP_RESTInventoryRecordAndFacilityComputed(t *testing.T) {
	s, ids := seedSupplyChain(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	code, body := restGET(t, ts.URL+"/api/v1/inventoryRecord/"+ids.inventory, "gold", "")
	if code != 200 {
		t.Fatalf("inventoryRecord status = %d body = %s", code, body)
	}

	code, body = restGET(t, ts.URL+"/api/v1/facility/"+ids.facility, "gold", "")
	if code != 200 {
		t.Fatalf("facility status = %d body = %s", code, body)
	}
	var fac map[string]any
	_ = json.Unmarshal(body, &fac)
	if fac["currentUtilization"].(float64) != 1 {
		t.Fatalf("REST currentUtilization = %v, want 1", fac["currentUtilization"])
	}

	code, _ = restGET(t, ts.URL+"/api/v1/products/"+ids.product, "gold", "")
	if code != 404 {
		t.Fatalf("plural products path status = %d, want 404 (not mounted)", code)
	}
}

type gqlHTTP struct {
	HTTPStatus int
	Data       map[string]any
	Errors     []any
}

func graphqlPOST(t *testing.T, base, tenant, query, auth string) gqlHTTP {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"query": query})
	req, err := http.NewRequest(http.MethodPost, base+"/graphql", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("X-OpenFoundry-Tenant", tenant)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := gqlHTTP{HTTPStatus: resp.StatusCode}
	if resp.StatusCode != 200 {
		return out
	}
	var parsed struct {
		Data   map[string]any `json:"data"`
		Errors []any          `json:"errors"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("graphql response %s: %v", raw, err)
	}
	out.Data = parsed.Data
	out.Errors = parsed.Errors
	return out
}

func restGET(t *testing.T, url, tenant, auth string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tenant != "" {
		req.Header.Set("X-OpenFoundry-Tenant", tenant)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}
