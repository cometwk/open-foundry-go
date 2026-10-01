package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/stretchr/testify/require"
)

func TestFileStoreBehavior(t *testing.T) {
	testStoreBehavior(t, storeFixture{
		store: NewFileStore(),
		// 直接改写 {id}.json 中的 updatedAt，控制 ListSessions 排序
		setUpdatedAt: func(t *testing.T, cwd, id, ts string) {
			t.Helper()
			path := filepath.Join(getSessionsDir(cwd), id+".json")
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var s Session
			require.NoError(t, json.Unmarshal(raw, &s))
			s.Metadata.UpdatedAt = ts
			out, err := json.MarshalIndent(&s, "", "  ")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, out, 0o644))
		},
	})
}

// 双文件落盘: {id}.json 只含元数据 (messages 为 null)，消息在 {id}.jsonl
func TestFileStoreLayout(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()

	_, err := NewFileStore().SaveSession(t.Context(), cwd, "2-abc", suiteMessages())
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(cwd, ".agent", "sessions", "2-abc.json"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "\"systemPrompt\"")
	require.Contains(t, string(raw), "\"messages\": null")

	jsonl, err := os.ReadFile(filepath.Join(cwd, ".agent", "sessions", "2-abc.jsonl"))
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimRight(string(jsonl), "\n"), "\n"), 2)

	// 损坏与非 .json 文件 (含 .jsonl 消息日志) 不被 ListSessions 收录
	dir := filepath.Join(cwd, ".agent", "sessions")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("skip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2-abc.jsonl"), []byte("{}\n"), 0o644)) // 覆盖也无妨
	sessions, err := NewFileStore().ListSessions(cwd)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
}

// 旧格式 (messages 内嵌 {id}.json、无 .jsonl) 不兼容: 元数据可读，消息视为空
func TestFileStoreLoadLegacyNoFallback(t *testing.T) {
	cwd := t.TempDir()
	dir := getSessionsDir(cwd)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	legacy := &Session{
		Metadata: SessionMetadata{ID: "legacy", Title: "旧会话", CreatedAt: "2026-01-01T00:00:00.000Z", Cwd: cwd},
		Messages: suiteMessages(),
	}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "legacy.json"), raw, 0o644))

	loaded, err := NewFileStore().LoadSession(cwd, "legacy")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, "legacy", loaded.Metadata.ID)
	require.Empty(t, loaded.Messages) // 不回退读内嵌消息

	// 损坏的 {id}.json -> (nil, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o644))
	corrupt, err := NewFileStore().LoadSession(cwd, "corrupt")
	require.ErrorContains(t, err, "metadata corrupt")
	require.Nil(t, corrupt)
}

// .jsonl 空行/损坏行跳过，不阻断后续消息
func TestFileStoreReadMessagesJSONLCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	valid, err := json.Marshal(userMsg("u1", "ok"))
	require.NoError(t, err)
	content := "{not json\n" + string(valid) + "\n\n" + string(valid) + "\ntruncated{"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	msgs := readMessagesJSONL(path)
	require.Len(t, msgs, 2)
	require.Equal(t, "u1", msgs[0].ID)

	require.Nil(t, readMessagesJSONL(filepath.Join(dir, "missing.jsonl")))
}

// TestSessionHelpers 纯函数行为 (标题提取/末条用户消息/ID 生成)
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
