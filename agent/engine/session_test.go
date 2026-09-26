package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/stretchr/testify/require"
)

func TestSessionHelpers(t *testing.T) {
	// generateID: {毫秒}-{6位base36}
	id1, id2 := generateID(), generateID()
	require.NotEqual(t, id1, id2)
	require.Regexp(t, `^\d+-[0-9a-z]{6}$`, id1)

	// extractTitle
	msgs := []aisdk.UIMessage{
		assistantMsg("a1", aisdk.TextPart{Text: "hi"}),
		userMsg("u1", "帮我修复 payments-api 的 500 错误"),
		userMsg("u2", "第二条用户消息"),
	}
	require.Equal(t, "帮我修复 payments-api 的 500 错误", extractTitle(msgs))
	require.Equal(t, strings.Repeat("a", 50), extractTitle([]aisdk.UIMessage{
		userMsg("u1", strings.Repeat("a", 80)), // 截断到 50
	}))
	require.Equal(t, "Untitled session", extractTitle([]aisdk.UIMessage{
		assistantMsg("a1", aisdk.TextPart{Text: "only assistant"}),
	}))
	require.Equal(t, "Untitled session", extractTitle([]aisdk.UIMessage{
		{ID: "u1", Role: aisdk.RoleUser, Parts: []aisdk.Part{}}, // 第一条 user 无 text part
		userMsg("u2", "later user"),
	}))

	// getLastUserMessage: 最后一条 user 的 text part 用空格拼接
	require.Equal(t, "第二条用户消息", getLastUserMessage(msgs))
	multi := []aisdk.UIMessage{
		userMsg("u1", "a"),
		assistantMsg("a1", aisdk.TextPart{Text: "mid"}),
		{ID: "u2", Role: aisdk.RoleUser, Parts: []aisdk.Part{
			aisdk.TextPart{Text: "b"},
			aisdk.ToolInvocationPart{ToolCallID: "c1", ToolName: "bash", State: aisdk.ToolStateOutputAvailable},
			aisdk.TextPart{Text: "c"},
		}},
	}
	require.Equal(t, "b c", getLastUserMessage(multi))
	require.Empty(t, getLastUserMessage([]aisdk.UIMessage{assistantMsg("a1", aisdk.TextPart{Text: "x"})}))
}

// sessionMessages 含 text part 与工具调用的消息，用于验证完整 round-trip
func sessionMessages() []aisdk.UIMessage {
	return []aisdk.UIMessage{
		{ID: "m1", Role: aisdk.RoleUser, Parts: []aisdk.Part{aisdk.TextPart{Text: "帮我修复 payments-api 的 500 错误"}}},
		{ID: "m2", Role: aisdk.RoleAssistant, Parts: []aisdk.Part{
			aisdk.TextPart{Text: "已定位问题"},
			aisdk.ToolInvocationPart{
				ToolCallID: "c1",
				ToolName:   "file_edit",
				State:      aisdk.ToolStateOutputAvailable,
				Input:      json.RawMessage(`{"path":"a.go"}`),
				Output:     json.RawMessage(`{"ok":true}`),
			},
		}},
	}
}

// UIMessage 自带 JSON marshal/unmarshal 适配 (ai-sdk message_json.go):
// 序列化时按 "type" 信封封包，反序列化时还原为具体的 Part 类型，
// 因此含工具调用的 messages 保存/加载后类型与内容完整保留。
func TestSessionSaveAndLoad(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()
	ctx := context.Background()

	msgs := sessionMessages()
	saved, err := SaveSession(ctx, cwd, msgs)
	require.NoError(t, err)

	// 元信息
	require.NotEmpty(t, saved.Metadata.ID)
	require.Equal(t, "帮我修复 payments-api 的 500 错误", saved.Metadata.Title)
	require.Equal(t, 2, saved.Metadata.MessageCount)
	require.Equal(t, cwd, saved.Metadata.Cwd)
	require.Equal(t, saved.Metadata.CreatedAt, saved.Metadata.UpdatedAt)
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, saved.Metadata.UpdatedAt)

	// 系统提示词快照 (memory off + tools on -> 1 段)
	require.Len(t, saved.SystemPrompt, 1)
	require.Contains(t, saved.SystemPrompt[0], "## Tools")

	// 文件落盘
	raw, err := os.ReadFile(filepath.Join(cwd, ".agent", "sessions", saved.Metadata.ID+".json"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "\"systemPrompt\"")

	// 加载 round-trip: 消息、part 类型、工具调用字段完整还原
	loaded := LoadSession(cwd, saved.Metadata.ID)
	require.NotNil(t, loaded)
	require.Equal(t, saved.Metadata, loaded.Metadata)
	require.Len(t, loaded.Messages, 2)

	require.Equal(t, aisdk.RoleUser, loaded.Messages[0].Role)
	tp, ok := loaded.Messages[0].Parts[0].(aisdk.TextPart)
	require.True(t, ok, "part 应还原为 TextPart")
	require.Equal(t, "帮我修复 payments-api 的 500 错误", tp.Text)

	tool, ok := loaded.Messages[1].Parts[1].(aisdk.ToolInvocationPart)
	require.True(t, ok, "part 应还原为 ToolInvocationPart")
	require.Equal(t, "file_edit", tool.ToolName)
	require.Equal(t, "c1", tool.ToolCallID)
	require.Equal(t, aisdk.ToolStateOutputAvailable, tool.State)
	require.JSONEq(t, `{"path":"a.go"}`, string(tool.Input))
	require.JSONEq(t, `{"ok":true}`, string(tool.Output))

	// 不存在 / 损坏 -> nil
	require.Nil(t, LoadSession(cwd, "missing-id"))
	corrupt := filepath.Join(cwd, ".agent", "sessions", "corrupt.json")
	require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o644))
	require.Nil(t, LoadSession(cwd, "corrupt"))
}

func TestSessionSaveOverwrite(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()
	ctx := context.Background()

	const sid = "fixed-session"
	first, err := SaveSession(ctx, cwd, []aisdk.UIMessage{userMsg("u1", "第一轮对话内容")}, sid)
	require.NoError(t, err)
	require.Equal(t, sid, first.Metadata.ID)

	// 同一 sessionId 再保存: 覆盖同一文件，元信息刷新 (与 TS 行为一致，createdAt 也被刷新)
	_, err = SaveSession(ctx, cwd, []aisdk.UIMessage{
		userMsg("u1", "第一轮对话内容"),
		assistantMsg("a1", aisdk.TextPart{Text: "done"}),
		userMsg("u2", "第二轮"),
	}, sid)
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(cwd, ".agent", "sessions"))
	require.NoError(t, err)
	require.Len(t, entries, 1) // 只有一个文件

	loaded := LoadSession(cwd, sid)
	require.NotNil(t, loaded)
	require.Equal(t, 3, loaded.Metadata.MessageCount)
	require.Equal(t, "第一轮对话内容", loaded.Metadata.Title)
}

// rewriteUpdatedAt 直接改写会话文件中的 updatedAt，控制 ListSessions 排序
func rewriteUpdatedAt(t *testing.T, cwd, id, ts string) {
	t.Helper()
	path := filepath.Join(cwd, ".agent", "sessions", id+".json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var s Session
	require.NoError(t, json.Unmarshal(raw, &s))
	s.Metadata.UpdatedAt = ts
	out, err := json.MarshalIndent(&s, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, out, 0o644))
}

func TestSessionListAndLoadByIndex(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()
	ctx := context.Background()

	// 目录不存在 -> 空
	require.Empty(t, ListSessions(cwd))

	mk := func(id, text string) {
		t.Helper()
		_, err := SaveSession(ctx, cwd, []aisdk.UIMessage{userMsg("u1", text)}, id)
		require.NoError(t, err)
	}
	mk("old", "旧会话")
	mk("mid", "中间会话")
	mk("new", "最新会话")

	// 同毫秒保存无法区分，改写 updatedAt 为递增时间
	rewriteUpdatedAt(t, cwd, "old", "2026-09-25T10:00:00.000Z")
	rewriteUpdatedAt(t, cwd, "mid", "2026-09-25T11:00:00.000Z")
	rewriteUpdatedAt(t, cwd, "new", "2026-09-25T12:00:00.000Z")

	// 损坏与非 .json 文件应被跳过
	dir := filepath.Join(cwd, ".agent", "sessions")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("skip"), 0o644))

	sessions := ListSessions(cwd)
	require.Len(t, sessions, 3)
	require.Equal(t, []string{"new", "mid", "old"},
		[]string{sessions[0].ID, sessions[1].ID, sessions[2].ID}) // 最新在前

	// 按序号加载: 0 = 最新
	latest := LoadSessionByIndex(cwd, 0)
	require.NotNil(t, latest)
	require.Equal(t, "new", latest.Metadata.ID)
	require.Equal(t, "最新会话", latest.Metadata.Title)

	// 越界 -> nil
	require.Nil(t, LoadSessionByIndex(cwd, -1))
	require.Nil(t, LoadSessionByIndex(cwd, 3))
}
