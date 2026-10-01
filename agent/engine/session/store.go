/**
 * 会话持久化接口 — 对标 Claude Code history.ts + commands/resume/
 *
 * Claude Code 会话存储:
 *   - ~/.claude/history.jsonl (JSONL, 含 prompt + pastedContents + timestamp)
 *   - sessionStorage.js 管理完整会话 (messages + metadata)
 *   - /resume 命令恢复会话
 */
package session

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
)

// Store 会话持久化接口；实现必须为无状态并发安全对象 (cwd 由各方法参数传入)。
// 错误语义: 不存在/越界不是错误 (返回 nil, nil，由调用方判 nil)；
// IO 失败或元数据损坏返回 (nil, err)。
type Store interface {
	// SaveSession 保存会话到磁盘 (自动保存: 每次 agent 回复后调用)，返回保存的会话。
	// sessionId 必填 (新建/续存由调用方决定 id)；messages 为本次新增的消息，
	// 只追加，不重写既有消息。返回的 Session.Messages 即本次追加的批次。
	//
	// 注意: 实现会调用 BuildSystemPrompt 做系统提示词快照，AGENT_MEMORY=on 时
	// 可能触发记忆召回的 LLM 调用。
	SaveSession(ctx context.Context, cwd, sessionId string, messages []aisdk.UIMessage) (*Session, error)

	// LoadSession 加载指定会话，会话不存在返回 (nil, nil)，读取失败或损坏返回 (nil, err)
	LoadSession(cwd, sessionId string) (*Session, error)

	// LoadSessionByIndex 按 ListSessions 的顺序 (最新在前) 加载会话，越界返回 (nil, nil)
	LoadSessionByIndex(cwd string, idx int) (*Session, error)

	// ListSessions 列出所有会话元数据 (最新在前)，无会话返回 (nil, nil)；
	// 单个损坏条目跳过不中断列举
	ListSessions(cwd string) ([]SessionMetadata, error)
}

// NewFromEnv 按环境变量 AGENT_SESSION_STORE 选择实现:
// "sqlite" -> SQLiteStore，其余/未设置 -> FileStore (默认)
func NewFromEnv() Store {
	if strings.EqualFold(os.Getenv("AGENT_SESSION_STORE"), "sqlite") {
		return NewSQLiteStore()
	}
	return NewFileStore()
}

// loadSessionByIndex Store.LoadSessionByIndex 的通用实现 (按 ListSessions 顺序取 id 再加载)
func loadSessionByIndex(s Store, cwd string, idx int) (*Session, error) {
	sessions, err := s.ListSessions(cwd)
	if err != nil {
		return nil, err
	}
	if idx < 0 || idx >= len(sessions) {
		return nil, nil
	}
	return s.LoadSession(cwd, sessions[idx].ID)
}

// utcNow 对标 new Date().toISOString() (UTC + 毫秒精度)
func utcNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// nextMetadata 依据已有元数据与本次追加批次计算新元数据:
// 首次创建定 createdAt/title，之后保持稳定 (首批无用户消息时后续批次补全 title)；
// updatedAt 刷新。消息数不持久化 (派生值 len(Session.Messages))。
func nextMetadata(prev *SessionMetadata, sessionId, cwd, now string, messages []aisdk.UIMessage) SessionMetadata {
	meta := SessionMetadata{
		ID:        sessionId,
		Title:     extractTitle(messages),
		CreatedAt: now,
		Cwd:       cwd,
	}
	if prev != nil {
		meta = *prev
		if meta.Title == "" || meta.Title == "Untitled session" {
			if title := extractTitle(messages); title != "Untitled session" {
				meta.Title = title
			}
		}
	}
	meta.ID = sessionId
	meta.Cwd = cwd
	meta.UpdatedAt = now
	return meta
}

// snapshotSystemPrompt 保存时的系统提示词快照 (记忆召回依赖最后一条用户消息)
func snapshotSystemPrompt(ctx context.Context, cwd string, messages []aisdk.UIMessage) []string {
	return engine.BuildSystemPrompt(ctx, cwd, getLastUserMessage(messages))
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

// truncateRunes 按字符 (rune) 截断，避免像 TS 的 UTF-16 slice 那样切断多字节字符
// (与 engine/context.go 的同名实现保持一致)
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
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
