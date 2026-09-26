/**
 * 会话持久化 — 对标 Claude Code history.ts + commands/resume/
 *
 * Claude Code 会话存储:
 *   - ~/.claude/history.jsonl (JSONL, 含 prompt + pastedContents + timestamp)
 *   - sessionStorage.js 管理完整会话 (messages + metadata)
 *   - /resume 命令恢复会话
 *
 * 简化版:
 *   - .agent/sessions/{id}.json (JSON, 含 messages + metadata)
 *   - 自动保存 (每次 agent 回复后)
 *   - /resume 列出并选择会话
 */
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aisdk "github.com/grafana/ai-sdk"
)

// SessionMetadata 会话元信息
type SessionMetadata struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	MessageCount int    `json:"messageCount"`
	Cwd          string `json:"cwd"`
}

// Session 一次完整会话
type Session struct {
	Metadata SessionMetadata   `json:"metadata"`
	Messages []aisdk.UIMessage `json:"messages"`
	// SystemPrompt 保存时的系统提示词快照, 用于调试 (运行时由 agent 每轮重新构建)
	SystemPrompt []string `json:"systemPrompt,omitempty"`
}

func getSessionsDir(cwd string) string {
	return filepath.Join(cwd, ".agent", "sessions")
}

// generateID 生成会话 ID: {毫秒时间戳}-{6 位 base36 随机串}，
// 对标 TS 的 `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
func generateID() string {
	const charset = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[rand.IntN(len(charset))]
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), b)
}

// joinTextParts 拼接消息中的 text part (对应 TS 的 filter(p => p.type === "text"))
func joinTextParts(m aisdk.UIMessage) string {
	texts := make([]string, 0, len(m.Parts))
	for _, part := range m.Parts {
		if tp, ok := part.(aisdk.TextPart); ok {
			texts = append(texts, tp.Text)
		}
	}
	return strings.Join(texts, " ")
}

// extractTitle 从消息中提取标题 (第一条用户消息文本的前 50 个字符)
func extractTitle(messages []aisdk.UIMessage) string {
	for _, m := range messages {
		if m.Role == aisdk.RoleUser {
			if title := truncateRunes(joinTextParts(m), 50); title != "" {
				return title
			}
			break
		}
	}
	return "Untitled session"
}

// getLastUserMessage 取最后一条用户消息的文本
func getLastUserMessage(messages []aisdk.UIMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == aisdk.RoleUser {
			return joinTextParts(messages[i])
		}
	}
	return ""
}

// SaveSession 保存会话到磁盘 (自动保存: 每次 agent 回复后调用)，返回保存的会话。
// sessionId 可选，传入时为覆盖保存 (resume 场景)。
//
// 注意: 会调用 BuildSystemPrompt 做系统提示词快照，AGENT_MEMORY=on 时
// 可能触发记忆召回的 LLM 调用。
func SaveSession(ctx context.Context, cwd string, messages []aisdk.UIMessage, sessionId ...string) (*Session, error) {
	dir := getSessionsDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	id := generateID()
	if len(sessionId) > 0 && sessionId[0] != "" {
		id = sessionId[0]
	}
	// 对标 new Date().toISOString() (UTC + 毫秒精度)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 保存时的系统提示词快照 (记忆召回依赖最后一条用户消息)
	systemPrompt := BuildSystemPrompt(ctx, cwd, getLastUserMessage(messages))

	session := &Session{
		Metadata: SessionMetadata{
			ID:           id,
			Title:        extractTitle(messages),
			CreatedAt:    now,
			UpdatedAt:    now,
			MessageCount: len(messages),
			Cwd:          cwd,
		},
		Messages:     messages,
		SystemPrompt: systemPrompt,
	}

	raw, err := json.MarshalIndent(session, "", "  ") // JSON.stringify(session, null, 2)
	if err != nil {
		return nil, err
	}
	filePath := filepath.Join(dir, id+".json")
	if err := os.WriteFile(filePath, raw, 0o644); err != nil {
		return nil, err
	}
	return session, nil
}

// ListSessions 列出所有会话 (最新在前)，跳过损坏文件；目录不存在返回空
func ListSessions(cwd string) []SessionMetadata {
	dir := getSessionsDir(cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var sessions []SessionMetadata
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		// UIMessage 反序列化说明见 LoadSession
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // skip corrupt files
		}
		var session Session
		if err := json.Unmarshal(raw, &session); err != nil {
			continue // skip corrupt files
		}
		sessions = append(sessions, session.Metadata)
	}

	sort.Slice(sessions, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, sessions[i].UpdatedAt)
		tj, _ := time.Parse(time.RFC3339, sessions[j].UpdatedAt)
		return tj.Before(ti) // updatedAt 倒序
	})
	return sessions
}

// LoadSession 加载指定会话，文件不存在或损坏返回 nil
//
// UIMessage JSON 反序列化能力: aisdk.UIMessage 实现了 UnmarshalJSON
// (ai-sdk message_json.go)，会根据每个 part 的 "type" 字段还原为具体的
// Part 类型 (TextPart / ToolInvocationPart / DynamicToolUIPart / ...)，
// 因此含工具调用的 messages 也能完整 round-trip，无需额外处理。
func LoadSession(cwd, sessionId string) *Session {
	raw, err := os.ReadFile(filepath.Join(getSessionsDir(cwd), sessionId+".json"))
	if err != nil {
		return nil
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil
	}
	return &session
}

// LoadSessionByIndex 按 ListSessions 的顺序 (最新在前) 加载会话，越界返回 nil
func LoadSessionByIndex(cwd string, idx int) *Session {
	sessions := ListSessions(cwd)
	if idx < 0 || idx >= len(sessions) {
		return nil
	}
	return LoadSession(cwd, sessions[idx].ID)
}
