package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissionAutoMode(t *testing.T) {
	// 全部允许，包括 bash 危险命令
	dec := CheckPermission("bash", map[string]any{"command": "rm -rf /"}, PermissionModeAuto)
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "auto mode"}, dec)

	dec = CheckPermission("file_edit", nil, PermissionModeAuto)
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "auto mode"}, dec)
}

func TestPermissionPlanMode(t *testing.T) {
	// 只读工具允许
	for _, tool := range []string{"file_read", "glob", "grep"} {
		dec := CheckPermission(tool, nil, PermissionModePlan)
		require.Equal(t, PermissionDecision{Allowed: true, Reason: "read-only tool in plan mode"}, dec)
	}

	// 其余工具拒绝
	dec := CheckPermission("file_edit", nil, PermissionModePlan)
	require.Equal(t, PermissionDecision{
		Allowed: false,
		Reason:  `Tool "file_edit" not allowed in plan mode (read-only)`,
	}, dec)
	dec = CheckPermission("bash", map[string]any{"command": "ls"}, PermissionModePlan)
	require.False(t, dec.Allowed)
}

func TestPermissionDefaultMode(t *testing.T) {
	// 默认允许 (含安全 bash 命令)
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "default allow"},
		CheckPermission("file_edit", map[string]any{"path": "a.go"}, PermissionModeDefault))
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "default allow"},
		CheckPermission("bash", map[string]any{"command": "go test ./..."}, PermissionModeDefault))

	// 危险命令拦截 (含大小写不敏感的 SQL 模式)
	blocked := []string{
		"rm -rf /tmp/x",
		"rm  -rf /tmp",              // \s+ 匹配多空格
		"git push --force origin",   // git\s+push\s+--force
		"git reset --hard HEAD~2",   // git\s+reset\s+--hard
		"echo x; DROP TABLE users;", // (?i)
		"delete from users where 1", // (?i)
	}
	for _, cmd := range blocked {
		dec := CheckPermission("bash", map[string]any{"command": cmd}, PermissionModeDefault)
		require.False(t, dec.Allowed, "should block: %s", cmd)
		require.Equal(t, "Blocked dangerous command: "+truncateRunes(cmd, 50)+"...", dec.Reason)
	}

	// 命令总是追加 "..."，即使不足 50 字符
	dec := CheckPermission("bash", map[string]any{"command": "rm -rf /"}, PermissionModeDefault)
	require.Equal(t, "Blocked dangerous command: rm -rf /...", dec.Reason)

	// 长命令截断到 50 字符
	long := "git push --force " + strings.Repeat("x", 100)
	dec = CheckPermission("bash", map[string]any{"command": long}, PermissionModeDefault)
	require.Len(t, dec.Reason, len("Blocked dangerous command: ")+50+len("..."))

	// 非 bash 工具不做危险命令检查; command 非字符串也不检查
	require.True(t, CheckPermission("web_fetch", map[string]any{"command": "rm -rf /"}, PermissionModeDefault).Allowed)
	require.True(t, CheckPermission("bash", map[string]any{"command": 42}, PermissionModeDefault).Allowed)
	require.True(t, CheckPermission("bash", nil, PermissionModeDefault).Allowed)
}

func TestPermissionCustomRules(t *testing.T) {
	// 精确匹配 + 自定义 reason
	dec := CheckPermission("file_edit", nil, PermissionModeDefault, PermissionRule{Tool: "file_edit", Allow: true, Reason: "trusted editor"})
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "trusted editor"}, dec)

	// 通配符匹配 (TS 的 replace("*", ".*")，只替换第一个 *)
	dec = CheckPermission("web_fetch", nil, PermissionModeDefault, PermissionRule{Tool: "web_*", Allow: true})
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "Custom rule: web_*"}, dec)

	// 拒绝规则优先于危险命令检查
	dec = CheckPermission("bash", map[string]any{"command": "ls"}, PermissionModeDefault, PermissionRule{Tool: "bash", Allow: false})
	require.Equal(t, PermissionDecision{Allowed: false, Reason: "Custom rule: bash"}, dec)

	// 允许规则同样优先 (危险命令也被放行)
	dec = CheckPermission("bash", map[string]any{"command": "rm -rf /"}, PermissionModeDefault, PermissionRule{Tool: "bash", Allow: true, Reason: "ops trusted"})
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "ops trusted"}, dec)

	// 规则按顺序匹配，第一条命中即返回
	rules := []PermissionRule{
		{Tool: "bash", Allow: true, Reason: "first"},
		{Tool: "bash", Allow: false, Reason: "second"},
	}
	dec = CheckPermission("bash", nil, PermissionModeDefault, rules...)
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "first"}, dec)

	// 不匹配的规则不影响
	dec = CheckPermission("file_write", nil, PermissionModeDefault, PermissionRule{Tool: "file_edit", Allow: false})
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "default allow"}, dec)

	// 非法正则模式被跳过 (TS 会 throw，Go 跳过)，落入默认允许
	dec = CheckPermission("bash", nil, PermissionModeDefault, PermissionRule{Tool: "bash(", Allow: false})
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "default allow"}, dec)
}

func TestPermissionCheckerClosure(t *testing.T) {
	// 中间件捕获 mode + rules
	checker := CreatePermissionChecker(PermissionModeDefault, PermissionRule{Tool: "bash", Allow: false})
	require.False(t, checker("bash", map[string]any{"command": "ls"}).Allowed)
	require.True(t, checker("file_read", nil).Allowed)

	auto := CreatePermissionChecker(PermissionModeAuto)
	require.Equal(t, PermissionDecision{Allowed: true, Reason: "auto mode"}, auto("bash", map[string]any{"command": "rm -rf /"}))

	plan := CreatePermissionChecker(PermissionModePlan)
	require.True(t, plan("grep", nil).Allowed)
	require.False(t, plan("file_edit", nil).Allowed)
}

// TestPermissionReasonFormat 校验 reason 的引号格式与 TS 的 JSON.stringify 风格一致
func TestPermissionReasonFormat(t *testing.T) {
	dec := CheckPermission("agent", nil, PermissionModePlan)
	require.Equal(t, fmt.Sprintf("Tool %q not allowed in plan mode (read-only)", "agent"), dec.Reason)
}
