/**
 * Permission System — 对标 Claude Code hooks/toolPermission/
 *
 * Claude Code 权限架构:
 *   - 3 层 handler: coordinator → interactive → swarmWorker
 *   - 每层: hooks (fast) → classifier (inference) → user dialog
 *   - PermissionMode: default | auto | plan | bypass
 *   - 工具自声明 checkPermissions()
 *   - resolveOnce 防止重复决策
 *
 * 简化版实现:
 *   - 3 种模式: auto (全部允许) | plan (只读) | default (危险操作需确认)
 *   - 规则表: 按工具名 + 输入内容判断
 *   - 无交互确认 (Web UI 暂不支持)，用 allowlist 代替
 */
package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// PermissionMode 权限模式
type PermissionMode string

const (
	PermissionModeAuto    PermissionMode = "auto"
	PermissionModePlan    PermissionMode = "plan"
	PermissionModeDefault PermissionMode = "default"
)

// PermissionRule 自定义权限规则
type PermissionRule struct {
	/** Tool 工具名匹配 (支持 * 通配) */
	Tool string
	/** Allow 是否允许 */
	Allow bool
	/** Reason 原因 */
	Reason string
}

// PermissionDecision 权限判定结果
type PermissionDecision struct {
	Allowed bool
	Reason  string
}

// readOnlyTools 只读工具 — plan 模式允许
var readOnlyTools = map[string]struct{}{
	"file_read": {},
	"glob":      {},
	"grep":      {},
}

// dangerousBashPatterns 危险命令模式 — default 模式拦截
// (后两条带 (?i)，对应 TS 正则的 /i 标志)
var dangerousBashPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rm\s+-rf`),
	regexp.MustCompile(`git\s+push\s+--force`),
	regexp.MustCompile(`git\s+reset\s+--hard`),
	regexp.MustCompile(`(?i)DROP\s+TABLE`),
	regexp.MustCompile(`(?i)DELETE\s+FROM`),
}

// CheckPermission 检查工具权限 — 对标 canUseTool()
//
// Claude Code 完整流程:
//  1. validateInput() — 输入合法性
//  2. checkPermissions() — 工具自检
//  3. handler chain — coordinator/interactive/swarm
//
// 我们简化为: mode + rules 表。
// customRules 可选，对应 TS 的默认参数 []。
func CheckPermission(toolName string, input map[string]any, mode PermissionMode, customRules ...PermissionRule) PermissionDecision {
	// Auto mode: 全部允许 (对标 bypassPermissions)
	if mode == PermissionModeAuto {
		return PermissionDecision{Allowed: true, Reason: "auto mode"}
	}

	// Plan mode: 只允许只读工具 (对标 plan mode)
	if mode == PermissionModePlan {
		if _, ok := readOnlyTools[toolName]; ok {
			return PermissionDecision{Allowed: true, Reason: "read-only tool in plan mode"}
		}
		return PermissionDecision{Allowed: false, Reason: fmt.Sprintf("Tool %q not allowed in plan mode (read-only)", toolName)}
	}

	// Default mode: 检查自定义规则 + 危险命令拦截
	// 1. 自定义规则优先
	for _, rule := range customRules {
		// TS 的 String.replace("*", ".*") 只替换第一个 *
		pattern := strings.Replace(rule.Tool, "*", ".*", 1)
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			continue // 非法模式跳过 (TS 会 throw，Go 选择跳过以保持权限链可用)
		}
		if re.MatchString(toolName) {
			reason := rule.Reason
			if reason == "" {
				reason = fmt.Sprintf("Custom rule: %s", rule.Tool)
			}
			return PermissionDecision{Allowed: rule.Allow, Reason: reason}
		}
	}

	// 2. Bash 危险命令检查
	if toolName == "bash" {
		if command, ok := input["command"].(string); ok {
			for _, pattern := range dangerousBashPatterns {
				if pattern.MatchString(command) {
					return PermissionDecision{
						Allowed: false,
						Reason:  "Blocked dangerous command: " + truncateRunes(command, 50) + "...",
					}
				}
			}
		}
	}

	// 3. 默认允许
	return PermissionDecision{Allowed: true, Reason: "default allow"}
}

// CreatePermissionChecker 创建权限检查中间件 — 包装 tool execute
func CreatePermissionChecker(mode PermissionMode, rules ...PermissionRule) func(toolName string, input map[string]any) PermissionDecision {
	return func(toolName string, input map[string]any) PermissionDecision {
		return CheckPermission(toolName, input, mode, rules...)
	}
}
