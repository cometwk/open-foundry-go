package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
	"github.com/stretchr/testify/require"
)

func testToolContext(skills ...engine.Skill) ToolContext {
	return ToolContext{
		Cwd:            "/tmp/project",
		Ctx:            context.Background(),
		PermissionMode: engine.PermissionModeDefault,
		GetState:       func() AppState { return CreateInitialState("/tmp/project") },
		SetState:       func(fn func(AppState) AppState) {},
		Skills:         skills,
		Extra:          ToolExtra{"custom": "value"},
	}
}

func TestTypesCreateInitialState(t *testing.T) {
	before := time.Now().UnixMilli()
	state := CreateInitialState("/work/project")
	after := time.Now().UnixMilli()

	require.Equal(t, "/work/project", state.Cwd)
	require.Equal(t, engine.PermissionModeDefault, state.PermissionMode)
	require.Equal(t, TokenUsage{Input: 0, Output: 0}, state.TotalTokens)
	require.Equal(t, 0, state.TurnCount)
	require.GreaterOrEqual(t, state.StartedAt, before)
	require.LessOrEqual(t, state.StartedAt, after)
	require.Empty(t, state.PermissionDenials)

	// ToolExtra: 任意键值 + createTools 回调约定
	toolsSet := aisdk.ToolSet{}
	extra := ToolExtra{
		"custom":      42,
		"createTools": func(ToolExtra) aisdk.ToolSet { return toolsSet },
	}
	require.Equal(t, 42, extra["custom"])
}

func TestSkillsToolExecute(t *testing.T) {
	skills := []engine.Skill{
		{Name: "code-review", Description: "Review code", Prompt: "You are a code reviewer..."},
		{Name: "deploy", Description: "Deploy", Prompt: "Deploy steps..."},
	}
	tool, err := CreateSkillsTool(testToolContext(skills...))
	require.NoError(t, err)

	// 元信息
	require.Equal(t, "Read a skill's instructions from SKILL.md.", tool.Description)
	require.NotNil(t, tool.InputSchema)
	require.NotNil(t, tool.Execute)

	execJSON := func(input string) json.RawMessage {
		t.Helper()
		out, err := tool.Execute(context.Background(), json.RawMessage(input), aisdk.ToolExecutionOptions{})
		require.NoError(t, err)
		return out
	}

	// 按名称查找: 返回 skill 的完整 prompt (JSON string)
	out := execJSON(`{"skill_id": "code-review"}`)
	var prompt string
	require.NoError(t, json.Unmarshal(out, &prompt))
	require.Equal(t, "You are a code reviewer...", prompt)

	// 带前导斜杠也能找到
	out = execJSON(`{"skill_id": "/deploy"}`)
	require.NoError(t, json.Unmarshal(out, &prompt))
	require.Equal(t, "Deploy steps...", prompt)

	// 未找到: {"error": "Skill xxx not found"}
	out = execJSON(`{"skill_id": "missing"}`)
	var errObj map[string]string
	require.NoError(t, json.Unmarshal(out, &errObj))
	require.Equal(t, "Skill missing not found", errObj["error"])
}

func TestSkillsToolEmptySkills(t *testing.T) {
	// 无 skills 时仍创建成功 (只记日志，不抛错)
	tool, err := CreateSkillsTool(testToolContext())
	require.NoError(t, err)
	require.NotNil(t, tool.Execute)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"skill_id": "any"}`), aisdk.ToolExecutionOptions{})
	require.NoError(t, err)
	require.True(t, strings.Contains(string(out), "not found"))
}
