package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/openfoundry/runtime/internal/sqlopen"
)

func TestGoldPath_REST(t *testing.T) {
	env := setupGoldHTTP(t)
	ts := env.Server
	ids := env.IDs
	t.Logf("storage backend = %s", env.Backend)

	t.Run("two hop follow", func(t *testing.T) {
		sqlopen.LogSQL = true
		// Both backends rebuild the hop tree: Traverse returns per-hop Edges
		// and the engine batch-hydrates intermediates from them.
		code, body := rest(t, ts.URL+"/api/v1/book/"+ids.tb2+"/follow?path=branches,readers", "gold")
		if code != 200 {
			t.Fatalf("follow status = %d body = %s", code, body)
		}
		var follow struct {
			Nodes []map[string]any `json:"nodes"`
		}
		if err := json.Unmarshal(body, &follow); err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, n := range follow.Nodes {
			if _, ok := n["_id"]; ok {
				t.Fatal("follow exposed _id")
			}
			name, _ := n["name"].(string)
			names[name] = true
		}
		if !names["小明"] || !names["老王"] || len(names) != 2 {
			t.Fatalf("follow nodes = %v, want 小明 and 老王", follow.Nodes)
		}
		// assertTwoHopSQLBaseline(t, env.Backend)
	})

	t.Run("AE7 REST book", func(t *testing.T) {
		code, body := rest(t, ts.URL+"/api/v1/book/"+ids.sapiens, "gold")
		if code != 200 {
			t.Fatalf("REST GET status = %d body = %s", code, body)
		}
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		if obj["title"] != "人类简史" || obj["id"] != ids.sapiens {
			t.Fatalf("REST = %v", obj)
		}
		if _, ok := obj["_id"]; ok {
			t.Fatal("REST exposed _id")
		}
		code, body = rest(t, ts.URL+"/api/v1/book/missing", "gold")
		if code != 404 || !bytes.Contains(body, []byte("OBJECT_NOT_FOUND")) {
			t.Fatalf("REST miss status = %d body = %s", code, body)
		}
	})

	t.Run("cross tenant", func(t *testing.T) {
		code, _ := rest(t, ts.URL+"/api/v1/book/"+ids.sapiens, "other")
		if code != 404 {
			t.Fatalf("cross-tenant REST status = %d, want 404", code)
		}
	})

	t.Run("missing tenant", func(t *testing.T) {
		code, body := rest(t, ts.URL+"/api/v1/book/"+ids.sapiens, "")
		if code != 400 || !bytes.Contains(body, []byte("MISSING_TENANT")) {
			t.Fatalf("missing tenant REST = %d %s", code, body)
		}
	})
}

func rest(t *testing.T, url, tenant string) (int, []byte) {
	t.Helper()
	resetSQLCount()
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
