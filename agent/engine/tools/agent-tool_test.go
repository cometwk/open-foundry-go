package tools

import (
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/require"
)

func TestAgentToolCreate(t *testing.T) {
	tool, err := CreateAgentTool(t.TempDir(), aisdk.ToolSet{})
	require.NoError(t, err)
	require.NotEmpty(t, tool.Description)
	require.NotNil(t, tool.InputSchema)
	require.NotNil(t, tool.Execute)
}

// TestAgentTool_Integration 真实调用子 agent (默认模型，无子工具，单步即答)
func TestAgentTool_Integration(t *testing.T) {
	require.NoError(t, env.LoadEnv(""))

	tool, err := CreateAgentTool(t.TempDir(), aisdk.ToolSet{})
	require.NoError(t, err)

	res := execTool(t, tool, `{
		"prompt": "What is 2+2? Reply with just the number.",
		"description": "math check"
	}`)

	require.Equal(t, "completed", res["status"])
	require.Equal(t, "math check", res["description"])
	require.GreaterOrEqual(t, int(res["steps"].(float64)), 1)
	require.NotEmpty(t, res["result"])
	require.NotEqual(t, "(no text output)", res["result"])
	require.Contains(t, res["summary"], `"math check" completed in`)

	testutil.PrintPretty(res)
}
