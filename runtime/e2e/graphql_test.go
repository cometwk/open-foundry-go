package e2e_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/spi"
)

func TestGoldPath_GraphQL(t *testing.T) {
	env := setupGoldAPI(t)
	srv := env.API
	ids := env.IDs
	t.Logf("storage backend = %s", env.Backend)

	t.Run("AE1 get three types", func(t *testing.T) {
		queries := []string{
			`{ reader(id: "` + ids.xiaoHong + `") { id name } }`,
			`{ book(id: "` + ids.sapiens + `") { id title isbn } }`,
			`{ branch(id: "` + ids.west + `") { id name } }`,
		}
		for _, q := range queries {
			res := gql(t, srv, "gold", q)
			if len(res.Errors) > 0 {
				t.Fatalf("query %s errors = %v", q, res.Errors)
			}
		}
	})

	t.Run("AE2 borrowers nested", func(t *testing.T) {
		res := gql(t, srv, "gold", `{ book(id: "`+ids.tb1+`") { borrowers { name } } }`)
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
		bad := gql(t, srv, "gold", `{ book(id: "`+ids.tb1+`") { borrowers { isbn } } }`)
		if len(bad.Errors) == 0 {
			t.Fatal("expected schema error for borrowers { isbn }")
		}
	})

	t.Run("two hop branches readers", func(t *testing.T) {
		// Both backends rebuild the hop tree: Traverse returns per-hop Edges
		// and the engine batch-hydrates intermediates from them.
		res := gql(t, srv, "gold", `{
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
		gqlNames := map[string]bool{}
		for _, item := range readers {
			gqlNames[item.(map[string]any)["name"].(string)] = true
		}
		if !gqlNames["小明"] || !gqlNames["老王"] || len(gqlNames) != 2 {
			t.Fatalf("readers = %v, want 小明 and 老王", readers)
		}
	})

	t.Run("borrowers empty without Borrows", func(t *testing.T) {
		res := gql(t, srv, "gold", `{
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
		list := gql(t, srv, "gold", `{ books(first: 1) { totalCount edges { node { title } } pageInfo { hasNextPage } } }`)
		if len(list.Errors) > 0 {
			t.Fatalf("list errors = %v", list.Errors)
		}
		if list.Data["books"].(map[string]any)["totalCount"].(float64) < 1 {
			t.Fatalf("list = %v", list.Data)
		}

		blank := gql(t, srv, "gold", `{ searchBooks(query: "  ") { totalCount } }`)
		if len(blank.Errors) > 0 {
			t.Fatalf("blank search errors = %v", blank.Errors)
		}
		if blank.Data["searchBooks"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("blank search = %v", blank.Data)
		}

		// library.obda.yaml does not declare Book search.fields yet, so MySQL
		// FULLTEXT search is unsupported; memory still does substring match.
		if env.Backend == backendMemory {
			search := gql(t, srv, "gold", `{ searchBooks(query: "人类简史") { totalCount hits { node { title } } } }`)
			if len(search.Errors) > 0 {
				t.Fatalf("search errors = %v", search.Errors)
			}
			if search.Data["searchBooks"].(map[string]any)["totalCount"].(float64) < 1 {
				t.Fatalf("search = %v", search.Data)
			}
		} else {
			t.Log("skip non-blank FTS hit assert on mysql (no search.fields in library.obda.yaml)")
		}

		agg := gql(t, srv, "gold", `{ bookAggregate(fields: [{ field: "*", fn: COUNT, alias: "n" }]) { totalGroups groups { values } } }`)
		if len(agg.Errors) > 0 {
			t.Fatalf("aggregate errors = %v", agg.Errors)
		}
	})

	t.Run("nested registered_at and borrows", func(t *testing.T) {
		res := gql(t, srv, "gold", `{
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

		sapiens := gql(t, srv, "gold", `{ book(id: "`+ids.sapiens+`") { branches { name } } }`)
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

	t.Run("cross tenant", func(t *testing.T) {
		miss := gql(t, srv, "other", `{ book(id: "`+ids.sapiens+`") { title } }`)
		if miss.Data["book"] != nil {
			t.Fatalf("cross-tenant get = %v, want null", miss.Data["book"])
		}
		list := gql(t, srv, "other", `{ books { totalCount edges { node { id } } } }`)
		if list.Data["books"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("cross-tenant list = %v", list.Data)
		}
		// Blank search is empty on both backends; non-blank FTS differs (see above).
		search := gql(t, srv, "other", `{ searchBooks(query: "  ") { totalCount } }`)
		if len(search.Errors) > 0 {
			t.Fatalf("cross-tenant blank search errors = %v", search.Errors)
		}
		if search.Data["searchBooks"].(map[string]any)["totalCount"].(float64) != 0 {
			t.Fatalf("cross-tenant search = %v", search.Data)
		}
		if env.Backend == backendMemory {
			hit := gql(t, srv, "other", `{ searchBooks(query: "人类简史") { totalCount } }`)
			if hit.Data["searchBooks"].(map[string]any)["totalCount"].(float64) != 0 {
				t.Fatalf("cross-tenant search hit = %v", hit.Data)
			}
		}
	})
}

type gqlRes struct {
	Data   map[string]any
	Errors []any
}

func gql(t *testing.T, srv *api.Server, tenant, query string) gqlRes {
	t.Helper()
	rc := spi.RequestContext{TenantID: tenant, ActorID: "test"}
	res := srv.Exec(context.Background(), rc, query, nil)
	out := gqlRes{}
	if len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &out.Data); err != nil {
			t.Fatalf("decode %s: %v", res.Data, err)
		}
	}
	for _, e := range res.Errors {
		out.Errors = append(out.Errors, e)
	}
	return out
}
