package tools

import (
	"context"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
	"github.com/stretchr/testify/require"
)

// fullToolContext 可读可写的 ToolContext (默认走全量组装路径)
func fullToolContext(t *testing.T, skills ...engine.Skill) ToolContext {
	t.Helper()
	tctx := testToolContext(skills...)
	tctx.Cwd = t.TempDir()
	tctx.PermissionMode = engine.PermissionModeDefault
	tctx.AllowWrite = true
	tctx.AllowBash = true
	return tctx
}

func TestAssembleToolsFull(t *testing.T) {
	forceToolsEnv(t)
	toolSet, err := AssembleTools(fullToolContext(t))
	require.NoError(t, err)
	// 全量模式: 顶层只有 agent/ask_user/read_skill；
	// baseTools (file_read/bash/file_edit/file_write) 只传给子 agent，不进顶层集合
	require.ElementsMatch(t, []string{"agent", "ask_user", "read_skill"}, keysOf(toolSet))
}

// plan 模式 (allowWrite/allowBash 均为 false): 只返回只读工具
func TestAssembleToolsReadOnly(t *testing.T) {
	forceToolsEnv(t)
	tctx := testToolContext()
	tctx.Cwd = t.TempDir()
	tctx.PermissionMode = engine.PermissionModePlan
	tctx.AllowWrite = false
	tctx.AllowBash = false

	toolSet, err := AssembleTools(tctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"file_read"}, keysOf(toolSet)) // glob/grep 暂缺
}

// AGENT_TOOLS=off: 只保留 ask_user + read_skill
func TestAssembleToolsDisabled(t *testing.T) {
	forceToolsEnv(t)
	t.Setenv("AGENT_TOOLS", "off")

	toolSet, err := AssembleTools(fullToolContext(t))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ask_user", "read_skill"}, keysOf(toolSet))
}

// extra.createTools 扩展工具合并 (TS: extra?.createTools?.(extra) || {})
func TestAssembleToolsExtra(t *testing.T) {
	forceToolsEnv(t)
	custom, err := CreateAskUserTool() // 任意一个工具充当扩展
	require.NoError(t, err)

	tctx := fullToolContext(t)
	tctx.Extra = ToolExtra{
		"createTools": func(ToolExtra) aisdk.ToolSet {
			return aisdk.ToolSet{"custom_tool": custom}
		},
	}
	toolSet, err := AssembleTools(tctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"agent", "ask_user", "read_skill", "custom_tool",
	}, keysOf(toolSet))
}

// TestWireInstallAssembleTools 验证依赖倒置接线: 注入后 engine 钩子可用
func TestWireInstallAssembleTools(t *testing.T) {
	forceToolsEnv(t)
	InstallAssembleTools()
	t.Cleanup(func() { engine.ToolsAssembler = nil })

	require.NotNil(t, engine.ToolsAssembler)

	toolSet, err := engine.ToolsAssembler(context.Background(), t.TempDir(), engine.PermissionModeDefault, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"agent", "ask_user", "read_skill"}, keysOf(toolSet))
}

// forceToolsEnv 屏蔽宿主环境变量，恢复 EnvConfig 默认状态
func forceToolsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_TOOLS", "")
	t.Setenv("AGENT_CONTEXT_GIT", "")
	t.Setenv("AGENT_MEMORY", "")
}

func keysOf(toolSet aisdk.ToolSet) []string {
	keys := make([]string, 0, len(toolSet))
	for k := range toolSet {
		keys = append(keys, k)
	}
	return keys
}
