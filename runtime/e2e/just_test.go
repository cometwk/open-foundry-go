package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/openfoundry/runtime/internal/sqlopen"
)

func Test_JustTest(t *testing.T) {
	env := setupGoldAPI(t)
	srv := env.API
	ids := env.IDs
	t.Logf("storage backend = %s", env.Backend)

	t.Run("just test 1", func(t *testing.T) {
		res := gql(t, srv, "gold", `{ book(id: "`+ids.tb1+`") { borrowers { name } } }`)
		if len(res.Errors) > 0 {
			t.Fatalf("errors = %v", res.Errors)
		}
		// to json string
		jsonStr, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal res = %v", err)
		}
		t.Logf("res = %s", string(jsonStr))
	})

	t.Run("two hop branches readers", func(t *testing.T) {
		sqlopen.LogSQL = true
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
		// to json string
		jsonStr, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			t.Fatalf("marshal res = %v", err)
		}
		t.Logf("res = %s", string(jsonStr))
	})
}
