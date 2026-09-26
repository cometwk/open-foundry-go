package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandsIsCommand(t *testing.T) {
	require.True(t, IsCommand("/help"))
	require.True(t, IsCommand("  /help  ")) // 先 trim
	require.True(t, IsCommand("/"))         // 只有斜杠也算 (会走 unknown 分支)
	require.False(t, IsCommand("help"))
	require.False(t, IsCommand(""))
	require.False(t, IsCommand("use /help please"))
}

func TestCommandsExecute_Builtin(t *testing.T) {
	// 非命令
	require.Equal(t, CommandResult{}, ExecuteCommand("hello"))

	// help
	res := ExecuteCommand("/help")
	require.True(t, res.Handled)
	require.Contains(t, res.Message, "**Available commands:**")
	require.Contains(t, res.Message, "**/resume** — Resume a previous session")
	require.Contains(t, res.Message, "**Keyboard shortcuts:**")

	// compact / clear / cost / diff
	res = ExecuteCommand("/compact")
	require.Equal(t, CommandResult{Handled: true, Action: ActionCompact, Message: "⏳ Compacting conversation..."}, res)

	res = ExecuteCommand("/clear")
	require.Equal(t, CommandResult{Handled: true, Action: ActionClear, Message: "Conversation cleared."}, res)

	res = ExecuteCommand("/cost")
	require.Equal(t, CommandResult{Handled: true, Action: ActionCost}, res) // message 由调用方填入

	res = ExecuteCommand("/diff")
	require.Equal(t, CommandResult{Handled: true, Action: ActionDiff}, res)

	// 权限模式切换: message + data
	cases := []struct {
		cmd  string
		mode PermissionMode
		msg  string
	}{
		{"/plan", PermissionModePlan, "Switched to **plan mode** — only read-only tools (file_read, glob, grep).\nUse `/auto` to return to full mode."},
		{"/auto", PermissionModeAuto, "Switched to **auto mode** — all tools available, no confirmation needed."},
		{"/default", PermissionModeDefault, "Switched to **default mode** — dangerous operations require confirmation."},
	}
	for _, c := range cases {
		res := ExecuteCommand(c.cmd)
		require.True(t, res.Handled)
		require.Equal(t, c.msg, res.Message)
		require.Equal(t, map[string]any{"permissionMode": string(c.mode)}, res.Data)
	}
}

func TestCommandsExecute_Resume(t *testing.T) {
	// 无参数 -> idx -1
	res := ExecuteCommand("/resume")
	require.Equal(t, ActionResume, res.Action)
	require.Equal(t, float64(-1), toFloat(res.Data["idx"]))

	// 数字参数
	res = ExecuteCommand("/resume 2")
	require.Equal(t, float64(2), toFloat(res.Data["idx"]))

	// 非数字: TS 的 parseInt 返回 NaN，Go 按 -1 处理 (文档化差异)
	res = ExecuteCommand("/resume abc")
	require.Equal(t, float64(-1), toFloat(res.Data["idx"]))

	// 带空白的参数
	res = ExecuteCommand("  /resume    3  ")
	require.Equal(t, float64(3), toFloat(res.Data["idx"]))
}

func TestCommandsExecute_MCP(t *testing.T) {
	// 无参数: usage
	res := ExecuteCommand("/mcp")
	require.True(t, res.Handled)
	require.Equal(t, "Usage: `/mcp <sse-url>` — Connect to an MCP server\nExample: `/mcp http://localhost:3001/sse`", res.Message)
	require.Nil(t, res.Data)

	// 有参数
	res = ExecuteCommand("/mcp http://localhost:3001/sse")
	require.Equal(t, ActionMCP, res.Action)
	require.Equal(t, map[string]any{"mcpUrl": "http://localhost:3001/sse"}, res.Data)
	require.Equal(t, "Connecting to MCP server: `http://localhost:3001/sse`...", res.Message)
}

func TestCommandsExecute_Unknown(t *testing.T) {
	res := ExecuteCommand("/nope")
	require.True(t, res.Handled)
	require.Equal(t, "Unknown command: `/nope`\nType `/help` for available commands.", res.Message)

	// 命令名 + 未知参数仍路由到命令本身
	res = ExecuteCommand("/compact now")
	require.Equal(t, ActionCompact, res.Action)
}

func TestCommandsGetCommandNames(t *testing.T) {
	require.Equal(t, []string{
		"help", "compact", "clear", "cost", "resume",
		"plan", "auto", "default", "diff", "mcp",
	}, GetCommandNames()) // 注册顺序，对标 Object.keys

	for _, name := range GetCommandNames() {
		require.True(t, strings.HasPrefix(name, ""), "sanity")
	}
}

// toFloat 统一 int/float64 比较 (Data 中存的是 int)
func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	return -999
}
