package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
)

func TestRun_NilConf(t *testing.T) {
	prev := conf
	conf = nil
	t.Cleanup(func() { conf = prev })
	if _, _, err := openAPI(); err == nil || !strings.Contains(err.Error(), "config required") {
		t.Fatalf("err=%v, want config required", err)
	}
}

func TestRun_ServesGraphQL(t *testing.T) {
	base, dbPath := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		SeedTenant:  "default",
		DBDriver:    "sqlite",
		DBURL:       dbPath,
	}
	t.Cleanup(func() { conf = prev })

	if err := ddl("", "", true, false); err != nil {
		t.Fatal(err)
	}
	if err := seed(); err != nil {
		t.Fatal(err)
	}

	h, closeDB, err := openAPI()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeDB)

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	payload, _ := json.Marshal(map[string]any{
		"query": `{ widgets { totalCount edges { node { name } } } }`,
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/graphql", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenFoundry-Tenant", "default")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Widgets struct {
				TotalCount float64 `json:"totalCount"`
			} `json:"widgets"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) > 0 {
		t.Fatalf("errors=%v", out.Errors)
	}
	if out.Data.Widgets.TotalCount != 1 {
		t.Fatalf("totalCount=%v, want 1", out.Data.Widgets.TotalCount)
	}
}
