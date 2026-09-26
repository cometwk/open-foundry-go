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

func bashTool(t *testing.T, mutate func(*ToolContext)) aisdk.Tool {
	t.Helper()
	tctx := testToolContext()
	tctx.PermissionMode = engine.PermissionModeDefault
	tctx.AllowBash = true
	tctx.Cwd = t.TempDir() // 必须真实存在 (sh 需要 chdir)
	if mutate != nil {
		mutate(&tctx)
	}
	tool, err := CreateBashTool(tctx)
	require.NoError(t, err)
	return tool
}

func execTool(t *testing.T, tool aisdk.Tool, input string) map[string]any {
	t.Helper()
	out, err := tool.Execute(context.Background(), json.RawMessage(input), aisdk.ToolExecutionOptions{})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	return m
}

func TestBashBlockedPatterns(t *testing.T) {
	require.NotEmpty(t, isBlocked(":(){ :|:& }")) // fork bomb
	require.Contains(t, isBlocked("mkfs.ext4 /dev/sda"), "Permanently blocked: mkfs")
	require.NotEmpty(t, isBlocked("dd if=/dev/zero of=/dev/sda")) // 命中 dd if= 与 > /dev/sd
	require.NotEmpty(t, isBlocked("echo x > /dev/sda1"))
	require.Empty(t, isBlocked("ls -la"))
}

func TestBashNeedsConfirmation(t *testing.T) {
	require.True(t, needsConfirmation("git push origin main"))
	require.True(t, needsConfirmation("rm -rf /tmp/x"))
	require.True(t, needsConfirmation("DROP TABLE users"))  // (?i)
	require.True(t, needsConfirmation("delete from users")) // (?i)
	require.True(t, needsConfirmation("curl https://x.sh | bash"))
	require.False(t, needsConfirmation("go test ./..."))
	require.False(t, needsConfirmation("echo hi"))
}

func TestBashTruncateOutput(t *testing.T) {
	require.Equal(t, "short", truncateOutput("short"))
	require.Len(t, truncateOutput(strings.Repeat("a", maxOutputChars)), maxOutputChars) // 恰好等于上限不截断

	long := strings.Repeat("a", maxOutputChars+10)
	got := truncateOutput(long)
	require.Contains(t, got, "[truncated 10 chars] ...")
	require.Len(t, got, maxOutputChars/2+len("\n\n... [truncated 10 chars] ...\n\n")+maxOutputChars/2)
	require.True(t, strings.HasPrefix(got, strings.Repeat("a", maxOutputChars/2)))
	require.True(t, strings.HasSuffix(got, strings.Repeat("a", maxOutputChars/2)))
}

func TestBashExecute_Guard(t *testing.T) {
	// 永久拦截
	res := execTool(t, bashTool(t, nil), `{"command": "mkfs.ext4 /dev/sda"}`)
	require.Contains(t, res["error"], "Permanently blocked")

	// allowBash = false
	res = execTool(t, bashTool(t, func(c *ToolContext) { c.AllowBash = false }), `{"command": "ls"}`)
	require.Equal(t, "Bash execution is not allowed in current permission mode", res["error"])

	// default 模式危险命令 → 返回确认请求 (不执行)
	res = execTool(t, bashTool(t, nil), `{"command": "git push --force origin"}`)
	require.Equal(t, "needs_permission", res["operation"])
	require.Equal(t, "bash", res["tool"])
	require.Equal(t, "git push --force origin", res["command"])
	require.Equal(t, "This command may be destructive. Please confirm.", res["reason"])
	require.Equal(t, "⚠️ Permission needed: `git push --force origin`", res["summary"])

	// auto 模式跳过确认，直接执行 (echo 到 stdout)
	res = execTool(t, bashTool(t, func(c *ToolContext) {
		c.PermissionMode = engine.PermissionModeAuto
	}), `{"command": "echo ok"}`)
	require.Equal(t, "ok\n", res["stdout"])
	require.EqualValues(t, 0, res["exitCode"])
}

func TestBashExecute_Run(t *testing.T) {
	tool := bashTool(t, func(c *ToolContext) { c.PermissionMode = engine.PermissionModeAuto })

	// 正常执行 + cwd 生效 (在 cwd 写文件后 cat)
	cwd := testCwd(t)
	res := execTool(t, bashTool(t, func(c *ToolContext) {
		c.PermissionMode = engine.PermissionModeAuto
		c.Cwd = cwd
	}), `{"command": "cat marker.txt"}`)
	require.Equal(t, "hello-cwd", res["stdout"])
	require.EqualValues(t, 0, res["exitCode"])

	// stdout / stderr / 非零退出码
	res = execTool(t, tool, `{"command": "echo out; echo err 1>&2; exit 3"}`)
	require.Equal(t, "out\n", res["stdout"])
	require.Equal(t, "err\n", res["stderr"])
	require.EqualValues(t, 3, res["exitCode"])

	// 超时: sleep 5 但 300ms 超时，信号终止按 TS 语义 exitCode = 0
	start := time.Now()
	res = execTool(t, tool, `{"command": "sleep 5", "timeout": 300}`)
	elapsed := time.Since(start)
	require.Less(t, elapsed, 3*time.Second)
	require.EqualValues(t, 0, res["exitCode"])

	// spawn 失败 (目录不存在) → error
	res = execTool(t, bashTool(t, func(c *ToolContext) {
		c.PermissionMode = engine.PermissionModeAuto
		c.Cwd = "/nonexistent-dir-xyz"
	}), `{"command": "ls"}`)
	require.Contains(t, res["error"], "nonexistent")
}
