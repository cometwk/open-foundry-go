package tools

import (
	"context"
	"encoding/json"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/foundry"
	"github.com/stretchr/testify/require"
)

func TestGqlExec(t *testing.T) {
	gql := `
	query {
	  books(first: 3) {
		totalCount
		edges {
		  node {
			title
			branches {
			  name
			  readers {
				name
				id
			  }
			}
		  }
		}
		pageInfo {
		  hasNextPage
		}
	  }
	}`

	foundry.CreateFoundryServer()
	tool, err := CreateGqlExecTool(nil)
	require.NoError(t, err)

	input := execInput{
		Query: gql,
	}
	json, err := json.Marshal(input)
	require.NoError(t, err)

	out, err := tool.Execute(context.Background(), []byte(json), aisdk.ToolExecutionOptions{})
	require.NoError(t, err)
	t.Logf("out: %s", string(out))
}
