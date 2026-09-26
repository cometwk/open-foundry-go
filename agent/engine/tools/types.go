/**
 * Tool 类型系统 — 对标 Claude Code Tool.ts
 *
 * Round 3: 集成权限检查
 */
package tools

import (
	"context"
	"time"

	"github.com/openfoundry/agent/engine"
)

// ToolContext 工具执行上下文 — 对标 ToolUseContext
type ToolContext struct {
	/** Cwd 当前工作目录 */
	Cwd string
	/** Ctx 中止控制器 (TS 的 AbortController → Go 的 context.Context 取消机制) */
	Ctx context.Context
	/** AllowWrite 是否允许写操作 (从 permissionMode 派生) */
	AllowWrite bool
	/** AllowBash 是否允许执行命令 (从 permissionMode 派生) */
	AllowBash bool
	/** PermissionMode 权限模式 */
	PermissionMode engine.PermissionMode
	/** PermissionRules 自定义权限规则 */
	PermissionRules []engine.PermissionRule
	/** GetState 获取应用状态 */
	GetState func() AppState
	/** SetState 更新应用状态 */
	SetState func(fn func(AppState) AppState)
	/** Extra 扩展上下文 (nil = 未设置，对应 TS 的可选字段) */
	Extra ToolExtra
	/** Skills 技能列表 */
	Skills []engine.Skill
}

// AppState 应用状态 — 对标 AppState
type AppState struct {
	Cwd            string
	PermissionMode engine.PermissionMode
	TotalTokens    TokenUsage
	TurnCount      int
	StartedAt      int64 // UnixMilli，对应 Date.now()
	/** PermissionDenials 权限拒绝记录 — 对标 permissionDenials */
	PermissionDenials []PermissionDenial
}

// TokenUsage token 用量 (TS 的匿名对象 { input, output })
type TokenUsage struct {
	Input  int
	Output int
}

// PermissionDenial 一次权限拒绝记录
type PermissionDenial struct {
	Tool      string
	Reason    string
	Timestamp int64 // UnixMilli
}

// CreateInitialState 初始状态
func CreateInitialState(cwd string) AppState {
	return AppState{
		Cwd:               cwd,
		PermissionMode:    engine.PermissionModeDefault,
		TotalTokens:       TokenUsage{Input: 0, Output: 0},
		TurnCount:         0,
		StartedAt:         time.Now().UnixMilli(),
		PermissionDenials: []PermissionDenial{},
	}
}

// ToolExtra 扩展上下文 — 对应 TS 的 Record<string, any> & { createTools? }
// 可选键 "createTools" 可存放 func(ToolExtra) aisdk.ToolSet (对应 TS 的 createTools 回调)
type ToolExtra map[string]any
