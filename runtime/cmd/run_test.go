package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/internal/serve"
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
	// Full-stack flow spans two processes-in-one (seed() then openAPI()), each
	// doing its own bootstrap.Open — only a persistent backend shares state.
	// Memory full-stack coverage lives in runtime/e2e instead.
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		t.Skip("TEST_DB_URL unset; MySQL full-stack test skipped")
	}
	base, _ := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		SeedTenant:  "default",
		DBDriver:    "mysql",
		DBURL:       dsn,
	}
	t.Cleanup(func() { conf = prev })

	if err := seed(); err != nil {
		t.Fatal(err)
	}

	srv, closeDB, err := openAPI()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeDB)

	e := serve.NewEcho()
	srv.Handler(e.Group(""))
	ts := httptest.NewServer(e)
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
