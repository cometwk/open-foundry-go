package tools

import (
	"context"

	"github.com/openfoundry/agent/foundry"
	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/spi"

	aisdk "github.com/grafana/ai-sdk"
)

func gql_exec(srv *api.Server, query string) any {
	rc := spi.RequestContext{TenantID: "gold", ActorID: "test"}
	res := srv.Exec(context.Background(), rc, query, nil)
	return res
}

type execInput struct {
	Query string `json:"query" jsonschema:"description=GraphQL query to execute"`
}

func CreateGqlExecTool(_ any) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[execInput, any]{
		Name:        "gql_exec",
		Description: "Execute a GraphQL query on the OpenFoundry server.",
		Execute: func(_ context.Context, input execInput, _ aisdk.ToolExecutionOptions) (any, error) {
			srv := foundry.GetFoundryServer().Srv
			return gql_exec(srv, input.Query), nil
		},
	})
}
