package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAskUserTool(t *testing.T) {
	tool, err := CreateAskUserTool()
	require.NoError(t, err)
	require.NotEmpty(t, tool.Description)
	require.NotNil(t, tool.InputSchema)

	res := execTool(t, tool, `{
		"questions": [{
			"question": "使用哪个库?",
			"header": "Lib",
			"multiSelect": false,
			"options": [
				{"label": "Gin", "description": "高性能路由"},
				{"label": "Echo"}
			]
		}]
	}`)

	require.Equal(t, "ask_user", res["operation"])
	require.Equal(t, "Asked 1 question(s) — waiting for user response", res["summary"])

	questions := res["questions"].([]any)
	require.Len(t, questions, 1)
	q := questions[0].(map[string]any)
	require.Equal(t, "使用哪个库?", q["question"])
	require.Equal(t, "Lib", q["header"])
	require.Equal(t, false, q["multiSelect"])

	options := q["options"].([]any)
	require.Len(t, options, 2)
	require.Equal(t, map[string]any{"label": "Gin", "description": "高性能路由"}, options[0])
	require.Equal(t, map[string]any{"label": "Echo"}, options[1])

	// header 未设置时省略该键
	res = execTool(t, tool, `{
		"questions": [{
			"question": "继续吗?",
			"options": [{"label": "是"}, {"label": "否"}]
		}]
	}`)
	q = res["questions"].([]any)[0].(map[string]any)
	require.NotContains(t, q, "header")
	require.Equal(t, false, q["multiSelect"]) // zod default(false)
}
