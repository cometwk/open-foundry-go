/**
 * FileStore 双文件布局实现 — 对标 Claude Code history.jsonl
 *
 *   - .agent/sessions/{id}.json  Session 结构 (metadata + systemPrompt 快照)，小文件可重写
 *   - .agent/sessions/{id}.jsonl 消息日志，一行一条 UIMessage，append-only
 *   - 旧格式 (messages 内嵌 {id}.json) 不兼容: 仅元数据可读，消息视为空
 */
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aisdk "github.com/grafana/ai-sdk"
)

// FileStore 双文件布局: 元数据 json + 消息 jsonl
type FileStore struct{}

// NewFileStore 创建双文件布局 Store
func NewFileStore() *FileStore { return &FileStore{} }

func getSessionsDir(cwd string) string {
	return filepath.Join(cwd, ".agent", "sessions")
}

// SaveSession 增量追加 messages 到 {id}.jsonl，重写 {id}.json 元数据 (不含消息)
func (s *FileStore) SaveSession(ctx context.Context, cwd, sessionId string, messages []aisdk.UIMessage) (*Session, error) {
	if sessionId == "" {
		return nil, fmt.Errorf("sessionId is required")
	}
	dir := getSessionsDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	now := utcNow()

	// 已有会话: 沿用 createdAt，title 保持稳定；新会话: 首次创建
	meta := nextMetadata(loadSessionMetadata(cwd, sessionId), sessionId, cwd, now, messages)
	systemPrompt := snapshotSystemPrompt(ctx, cwd, messages)

	// 先追加消息 (append-only)，再写元数据——中途失败时元数据不会指向未落盘的消息
	if err := appendMessagesJSONL(filepath.Join(dir, sessionId+".jsonl"), messages); err != nil {
		return nil, err
	}

	session := &Session{
		Metadata:     meta,
		Messages:     messages,
		SystemPrompt: systemPrompt,
	}
	// 落盘的 {id}.json 不含消息 (messages 只存 {id}.jsonl)
	disk := *session
	disk.Messages = nil
	raw, err := json.MarshalIndent(&disk, "", "  ") // JSON.stringify(session, null, 2)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, sessionId+".json"), raw, 0o644); err != nil {
		return nil, err
	}
	return session, nil
}

// appendMessagesJSONL 以 append 模式把消息逐条写入 .jsonl (一行一条)，不重写旧内容
func appendMessagesJSONL(path string, messages []aisdk.UIMessage) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return nil
}

// loadSessionMetadata 只读 {id}.json 的元数据 (不读消息日志)，供增量更新用
func loadSessionMetadata(cwd, sessionId string) *SessionMetadata {
	raw, err := os.ReadFile(filepath.Join(getSessionsDir(cwd), sessionId+".json"))
	if err != nil {
		return nil
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil
	}
	return &session.Metadata
}

// ListSessions 列出所有会话 (最新在前)，目录不存在返回 (nil, nil)；
// 单个损坏文件跳过不中断列举
func (s *FileStore) ListSessions(cwd string) ([]SessionMetadata, error) {
	dir := getSessionsDir(cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []SessionMetadata
	for _, entry := range entries {
		// 只认 {id}.json；.jsonl (消息日志) 与其他文件不收录
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
	return sessions, nil
}

// LoadSession 加载指定会话，会话不存在返回 (nil, nil)，元数据损坏返回 (nil, err)。
// Metadata/SystemPrompt 来自 {id}.json，Messages 来自 {id}.jsonl 合并还原；
// .jsonl 缺失时消息为空——不回退读 {id}.json 内嵌的旧格式消息。
//
// UIMessage JSON 反序列化能力: aisdk.UIMessage 实现了 UnmarshalJSON
// (ai-sdk message_json.go)，会根据每个 part 的 "type" 字段还原为具体的
// Part 类型 (TextPart / ToolInvocationPart / DynamicToolUIPart / ...)，
// 因此含工具调用的 messages 也能完整 round-trip，无需额外处理。
func (s *FileStore) LoadSession(cwd, sessionId string) (*Session, error) {
	raw, err := os.ReadFile(filepath.Join(getSessionsDir(cwd), sessionId+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 会话不存在不是错误
		}
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, fmt.Errorf("session %s metadata corrupt: %w", sessionId, err)
	}
	session.Messages = readMessagesJSONL(filepath.Join(getSessionsDir(cwd), sessionId+".jsonl"))
	return &session, nil
}

// readMessagesJSONL 逐行解码 .jsonl 中的消息；文件不存在返回 nil，
// 空行与损坏行跳过 (对标 Claude Code history.jsonl 的容错读取)
func readMessagesJSONL(path string) []aisdk.UIMessage {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var messages []aisdk.UIMessage
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		var m aisdk.UIMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue // skip corrupt lines
		}
		messages = append(messages, m)
	}
	return messages
}

// LoadSessionByIndex 按 ListSessions 的顺序 (最新在前) 加载会话，越界返回 (nil, nil)
func (s *FileStore) LoadSessionByIndex(cwd string, idx int) (*Session, error) {
	return loadSessionByIndex(s, cwd, idx)
}
