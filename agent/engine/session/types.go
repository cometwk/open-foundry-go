// Package session 提供会话持久化的 Store 接口与多种实现。
//
// 布局 (见 docs/plans/2026-09-30-001-refactor-session-persistence-plan.md):
//   - FileStore: 双文件布局 —— .agent/sessions/{id}.json (元数据) + {id}.jsonl (消息, append-only)
//   - SQLiteStore: 单库 .agent/sessions.db (modernc.org/sqlite, 纯 Go 无 CGO)
//
// 共同语义:
//   - SaveSession 的 sessionId 必填，messages 为本次新增 (增量追加, 不重写旧消息)
//   - 元数据小体积可重写: createdAt/title 首次创建后稳定, updatedAt 每次刷新
//     (消息数为派生值 len(Session.Messages)，不持久化)
//   - LoadSession 合并元数据与消息还原完整 Session; 旧格式 (messages 内嵌 {id}.json) 不兼容
package session

import (
	aisdk "github.com/grafana/ai-sdk"
)

// SessionMetadata 会话元信息
type SessionMetadata struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Cwd       string `json:"cwd"`
}

// Session 一次完整会话
type Session struct {
	Metadata SessionMetadata   `json:"metadata"`
	Messages []aisdk.UIMessage `json:"messages"`
	// SystemPrompt 保存时的系统提示词快照, 用于调试 (运行时由 agent 每轮重新构建)
	SystemPrompt []string `json:"systemPrompt,omitempty"`
}
