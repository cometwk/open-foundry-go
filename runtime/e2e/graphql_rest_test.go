package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestGoldPath_GraphQLREST_HTTP(t *testing.T) {
	env := setupGoldHTTP(t)
	ts := env.Server
	ids := env.IDs
	t.Logf("storage backend = %s", env.Backend)

	t.Run("AE1 get three types", func(t *testing.T) {
		queries := []string{
			`{ reader(id: "` + ids.xiaoHong + `") { id name } }`,
			`{ book(id: "` + ids.sapiens + `") { id title isbn } }`,
			`{ branch(id: "` + ids.west + `") { id name } }`,
		}
		for _, q := range queries {
			res := gql(t, ts.URL, "gold", q)
			if len(res.Errors) > 0 {
				t.Fatalf("query %s errors = %v", q, res.Errors)
			}
		}
	})

	t.Run("AE2 borrowers nested", func(t *testing.T) {
		res := gql(t, ts.URL, "gold", `{ book(id: "`+ids.tb1+`") { borrowers { name } } }`)
		if len(res.Errors) > 0 {
			t.Fatalf("errors = %v", res.Errors)
		}
		list := res.Data["book"].(map[string]any)["borrowers"].([]any)
		names := map[string]bool{}
		for _, item := range list {
			names[item.(map[string]any)["name"].(string)] = true
		}
		if !names["小明"] || !names["老王"] || len(names) != 2 {
			t.Fatalf("borrowers = %v, want 小明 and 老王", list)
		}
		bad := gql(t, ts.URL, "gold", `{ book(id: "`+ids.tb1+`") { borrowers { isbn } } }`)
		if len(bad.Errors) == 0 {
			t.Fatal("expected schema error for borrowers { isbn }")
		}
	})

	t.Run("two hop branches readers", func(t *testing.T) {
		// mysqlobda Traverse returns terminal Nodes only (Edges/Visited empty).
		// query.assemblePath rebuilds the hop tree from Edges, so REST follow and
		// GraphQL nested @link both need the memory provider for this assertion.
		if env.Backend != backendMemory {
			t.Skip("mysql Traverse is terminal-only; follow/2-hop tree needs Edges/Visited")
		}

		res := gql(t, ts.URL, "gold", `{
			book(id: "`+ids.tb2+`") {
				branches { name readers { name id } }
			}
		}`)
		if len(res.Errors) > 0 {
			t.Fatalf("errors = %v", res.Errors)
		}
		branches := res.Data["book"].(map[string]any)["branches"].([]any)
		if len(branches) != 1 {
			t.Fatalf("branches = %v", branches)
		}
		br := branches[0].(map[string]any)
		if br["name"] != "主馆" {
			t.Fatalf("branch name = %v, want 主馆", br["name"])
		}
		readers := br["readers"].([]any)
		gqlIDs := map[string]bool{}
		gqlNames := map[string]bool{}
		for _, item := range readers {
			m := item.(map[string]any)
			gqlIDs[m["id"].(string)] = true
			gqlNames[m["name"].(string)] = true
		}
		if !gqlNames["小明"] || !gqlNames["老王"] || len(gqlNames) != 2 {
			t.Fatalf("readers = %v, want 小明 and 老王", readers)
		}

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
		if len(follow.Nodes) != 2 {
			t.Fatalf("follow nodes = %v, want 2", follow.Nodes)
		}
		for _, n := range follow.Nodes {
			id, _ := n["id"].(string)
			if !gqlIDs[id] {
				t.Fatalf("follow node id %s not in GraphQL readers %v", id, gqlIDs)
			}
		}
	})

	t.Run("borrowers empty without Borrows", func(t *testing.T) {
		res := gql(t, ts.URL, "gold", `{
			book(id: "`+ids.quantum+`") {
				title
				borrowers { name }
			}
		}`)
		if len(res.Errors) > 0 {
			t.Fatalf("errors = %v", res.Errors)
		}
		bk := res.Data["book"].(map[string]any)
		if bk["title"] != "量子纠缠导论" {
			t.Fatalf("title = %v", bk["title"])
		}
		list, _ := bk["borrowers"].([]any)
		if len(list) != 0 {
			t.Fatalf("borrowers = %v, want empty (no Borrows edge)", list)
		}
	})

	t.Run("list search aggregate", func(t *testing.T) {
		list := gql(t, ts.URL, "gold", `{ books(first: 1) { totalCount edges { node { title } } pageInfo { hasNextPage } } }`)
		if len(list.Errors) > 0 {
			t.Fatalf("list errors = %v", list.Errors)
		}
		if list.Data["books"].(map[string]any)["totalCount"].(float64) < 1 {
			t.Fatalf("list = %v", list.Data)
		}

		blank := gql(t, ts.URL, "gold", `{ searchBooks(query: "  ") { totalCount } }`)
		if len(blank.Errors) > 0 {
			t.Fatalf("blank search errors = %v", blank.Errors)
		}
		if blank.Data["searchBooks"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("blank search = %v", blank.Data)
		}

		// library.obda.yaml does not declare Book search.fields yet, so MySQL
		// FULLTEXT search is unsupported; memory still does substring match.
		if env.Backend == backendMemory {
			search := gql(t, ts.URL, "gold", `{ searchBooks(query: "人类简史") { totalCount hits { node { title } } } }`)
			if len(search.Errors) > 0 {
				t.Fatalf("search errors = %v", search.Errors)
			}
			if search.Data["searchBooks"].(map[string]any)["totalCount"].(float64) < 1 {
				t.Fatalf("search = %v", search.Data)
			}
		} else {
			t.Log("skip non-blank FTS hit assert on mysql (no search.fields in library.obda.yaml)")
		}

		agg := gql(t, ts.URL, "gold", `{ bookAggregate(fields: [{ field: "*", fn: COUNT, alias: "n" }]) { totalGroups groups { values } } }`)
		if len(agg.Errors) > 0 {
			t.Fatalf("aggregate errors = %v", agg.Errors)
		}
	})

	t.Run("nested registered_at and borrows", func(t *testing.T) {
		res := gql(t, ts.URL, "gold", `{
			reader(id: "`+ids.xiaoHong+`") {
				name
				branch { name }
				borrowedBooks { title }
			}
		}`)
		if len(res.Errors) > 0 {
			t.Fatalf("reader errors = %v", res.Errors)
		}
		rd := res.Data["reader"].(map[string]any)
		if rd["name"] != "小红" {
			t.Fatalf("name = %v", rd["name"])
		}
		if rd["branch"].(map[string]any)["name"] != "西区馆" {
			t.Fatalf("branch = %v, want 西区馆", rd["branch"])
		}
		books, _ := rd["borrowedBooks"].([]any)
		if len(books) != 0 {
			t.Fatalf("xiao_hong borrowedBooks = %v, want empty", books)
		}

		sapiens := gql(t, ts.URL, "gold", `{ book(id: "`+ids.sapiens+`") { branches { name } } }`)
		if len(sapiens.Errors) > 0 {
			t.Fatalf("sapiens errors = %v", sapiens.Errors)
		}
		brs := sapiens.Data["book"].(map[string]any)["branches"].([]any)
		brNames := map[string]bool{}
		for _, item := range brs {
			brNames[item.(map[string]any)["name"].(string)] = true
		}
		if !brNames["主馆"] || !brNames["西区馆"] || len(brNames) != 2 {
			t.Fatalf("sapiens branches = %v, want 主馆 and 西区馆", brs)
		}
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
		code, body = rest(t, ts.URL+"/api/v1/book/missing", "gold")
		if code != 404 || !bytes.Contains(body, []byte("OBJECT_NOT_FOUND")) {
			t.Fatalf("REST miss status = %d body = %s", code, body)
		}
	})

	t.Run("cross tenant", func(t *testing.T) {
		miss := gql(t, ts.URL, "other", `{ book(id: "`+ids.sapiens+`") { title } }`)
		if miss.Data["book"] != nil {
			t.Fatalf("cross-tenant get = %v, want null", miss.Data["book"])
		}
		list := gql(t, ts.URL, "other", `{ books { totalCount edges { node { id } } } }`)
		if list.Data["books"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("cross-tenant list = %v", list.Data)
		}
		// Blank search is empty on both backends; non-blank FTS differs (see above).
		search := gql(t, ts.URL, "other", `{ searchBooks(query: "  ") { totalCount } }`)
		if len(search.Errors) > 0 {
			t.Fatalf("cross-tenant blank search errors = %v", search.Errors)
		}
		if search.Data["searchBooks"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("cross-tenant search = %v", search.Data)
		}
		if env.Backend == backendMemory {
			hit := gql(t, ts.URL, "other", `{ searchBooks(query: "人类简史") { totalCount } }`)
			if hit.Data["searchBooks"].(map[string]any)["totalCount"].(float64) != 0 {
				t.Fatalf("cross-tenant search hit = %v", hit.Data)
			}
		}
		code, _ := rest(t, ts.URL+"/api/v1/book/"+ids.sapiens, "other")
		if code != 404 {
			t.Fatalf("cross-tenant REST status = %d, want 404", code)
		}
	})

	t.Run("missing tenant and auth ignored", func(t *testing.T) {
		code, body := rest(t, ts.URL+"/api/v1/book/"+ids.sapiens, "")
		if code != 400 || !bytes.Contains(body, []byte("MISSING_TENANT")) {
			t.Fatalf("missing tenant REST = %d %s", code, body)
		}
		res := gql(t, ts.URL, "gold", `{ book(id: "`+ids.sapiens+`") { title } }`)
		if res.Data["book"].(map[string]any)["title"] != "人类简史" {
			t.Fatalf("auth-ignored graphql = %v", res.Data)
		}
	})
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
