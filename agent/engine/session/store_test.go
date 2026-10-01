package session

import (
	"context"
	"encoding/json"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/stretchr/testify/require"
)

// forceDefaultEnv 恢复 EnvConfig 的默认状态 (tools/git on, memory off)，
// 屏蔽宿主环境变量 (与 engine 包测试同名助手一致)
func forceDefaultEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_TOOLS", "")
	t.Setenv("AGENT_CONTEXT_GIT", "")
	t.Setenv("AGENT_MEMORY", "")
}

func userMsg(id, text string) aisdk.UIMessage {
	return aisdk.UIMessage{ID: id, Role: aisdk.RoleUser, Parts: []aisdk.Part{aisdk.TextPart{Text: text}}}
}

func assistantMsg(id string, parts ...aisdk.Part) aisdk.UIMessage {
	if len(parts) == 0 {
		parts = []aisdk.Part{aisdk.TextPart{Text: "ok"}}
	}
	return aisdk.UIMessage{ID: id, Role: aisdk.RoleAssistant, Parts: parts}
}

// suiteMessages 含 text part 与工具调用的消息，用于验证完整 round-trip。
// UIMessage 自带 JSON marshal/unmarshal 适配 (ai-sdk message_json.go):
// 序列化时按 "type" 信封封包，反序列化时还原为具体的 Part 类型，
// 因此含工具调用的 messages 保存/加载后类型与内容完整保留。
func suiteMessages() []aisdk.UIMessage {
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

// storeFixture 行为套件夹具: setUpdatedAt 直接改写会话的 updatedAt (实现相关)，
// 用于控制 ListSessions 排序——同毫秒多次保存无法靠时间差区分
type storeFixture struct {
	store        Store
	setUpdatedAt func(t *testing.T, cwd, id, ts string)
}

// testStoreBehavior Store 契约套件: 两种实现必须通过同一组行为测试
func testStoreBehavior(t *testing.T, f storeFixture) {
	t.Helper()
	forceDefaultEnv(t)

	t.Run("SaveAndLoad", func(t *testing.T) {
		cwd := t.TempDir()
		ctx := context.Background()

		const sid = "2-abc"
		saved, err := f.store.SaveSession(ctx, cwd, sid, suiteMessages())
		require.NoError(t, err)

		// 元信息 (消息数为派生值 len(Messages)，不持久化)
		require.Equal(t, sid, saved.Metadata.ID)
		require.Equal(t, "帮我修复 payments-api 的 500 错误", saved.Metadata.Title)
		require.Equal(t, cwd, saved.Metadata.Cwd)
		require.Equal(t, saved.Metadata.CreatedAt, saved.Metadata.UpdatedAt)
		require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, saved.Metadata.UpdatedAt)

		// 系统提示词快照 (memory off + tools on -> 1 段)
		require.Len(t, saved.SystemPrompt, 1)
		require.Contains(t, saved.SystemPrompt[0], "## Tools")

		// 加载 round-trip: 消息、part 类型、工具调用字段完整还原
		loaded, err := f.store.LoadSession(cwd, sid)
		require.NoError(t, err)
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

		// 不存在 -> (nil, nil)
		missing, err := f.store.LoadSession(cwd, "missing-id")
		require.NoError(t, err)
		require.Nil(t, missing)
	})

	t.Run("Append", func(t *testing.T) {
		cwd := t.TempDir()
		ctx := context.Background()

		const sid = "fixed-session"
		first, err := f.store.SaveSession(ctx, cwd, sid, []aisdk.UIMessage{userMsg("u1", "第一轮对话内容")})
		require.NoError(t, err)

		// 同一 sessionId 追加增量 (调用方只传新消息，不重传历史)
		_, err = f.store.SaveSession(ctx, cwd, sid, []aisdk.UIMessage{
			assistantMsg("a1", aisdk.TextPart{Text: "done"}),
			userMsg("u2", "第二轮"),
		})
		require.NoError(t, err)

		loaded, err := f.store.LoadSession(cwd, sid)
		require.NoError(t, err)
		require.NotNil(t, loaded)
		// 追加语义: 3 条消息 (1 + 2)，createdAt/title 首次创建后保持稳定
		require.Equal(t, "第一轮对话内容", loaded.Metadata.Title)
		require.Equal(t, first.Metadata.CreatedAt, loaded.Metadata.CreatedAt)
		require.Len(t, loaded.Messages, 3)
		require.Equal(t, []string{"u1", "a1", "u2"},
			[]string{loaded.Messages[0].ID, loaded.Messages[1].ID, loaded.Messages[2].ID})
	})

	t.Run("RequiresID", func(t *testing.T) {
		cwd := t.TempDir()

		_, err := f.store.SaveSession(context.Background(), cwd, "", suiteMessages())
		require.ErrorContains(t, err, "sessionId is required")

		// 空校验先于任何写入，不产生会话
		sessions, err := f.store.ListSessions(cwd)
		require.NoError(t, err)
		require.Empty(t, sessions)
	})

	t.Run("ListAndLoadByIndex", func(t *testing.T) {
		cwd := t.TempDir()
		ctx := context.Background()

		// 目录不存在 -> (nil, nil)
		empty, err := f.store.ListSessions(cwd)
		require.NoError(t, err)
		require.Empty(t, empty)

		mk := func(id, text string) {
			t.Helper()
			_, err := f.store.SaveSession(ctx, cwd, id, []aisdk.UIMessage{userMsg("u1", text)})
			require.NoError(t, err)
		}
		mk("old", "旧会话")
		mk("mid", "中间会话")
		mk("new", "最新会话")

		// 改写 updatedAt 为递增时间，排序确定
		f.setUpdatedAt(t, cwd, "old", "2026-09-25T10:00:00.000Z")
		f.setUpdatedAt(t, cwd, "mid", "2026-09-25T11:00:00.000Z")
		f.setUpdatedAt(t, cwd, "new", "2026-09-25T12:00:00.000Z")

		sessions, err := f.store.ListSessions(cwd)
		require.NoError(t, err)
		require.Len(t, sessions, 3)
		require.Equal(t, []string{"new", "mid", "old"},
			[]string{sessions[0].ID, sessions[1].ID, sessions[2].ID}) // 最新在前

		// 按序号加载: 0 = 最新
		latest, err := f.store.LoadSessionByIndex(cwd, 0)
		require.NoError(t, err)
		require.NotNil(t, latest)
		require.Equal(t, "new", latest.Metadata.ID)
		require.Equal(t, "最新会话", latest.Metadata.Title)

		// 越界 -> (nil, nil)
		for _, idx := range []int{-1, 3} {
			out, err := f.store.LoadSessionByIndex(cwd, idx)
			require.NoError(t, err)
			require.Nil(t, out)
		}
	})

	// Store 无状态 (每次操作重开存储)，跨实例读回验证持久化与实现无关
	t.Run("PersistAcrossInstances", func(t *testing.T) {
		cwd := t.TempDir()

		_, err := f.store.SaveSession(context.Background(), cwd, "s1", []aisdk.UIMessage{userMsg("u1", "跨实例")})
		require.NoError(t, err)

		sessions, err := f.store.ListSessions(cwd)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		loaded, err := f.store.LoadSession(cwd, "s1")
		require.NoError(t, err)
		require.NotNil(t, loaded)
		require.Equal(t, "跨实例", loaded.Metadata.Title)
	})
}

// NewFromEnv 按 AGENT_SESSION_STORE 选择实现
func TestNewFromEnv(t *testing.T) {
	t.Setenv("AGENT_SESSION_STORE", "")
	require.IsType(t, &FileStore{}, NewFromEnv())
	t.Setenv("AGENT_SESSION_STORE", "sqlite")
	require.IsType(t, &SQLiteStore{}, NewFromEnv())
	t.Setenv("AGENT_SESSION_STORE", "SQLITE")
	require.IsType(t, &SQLiteStore{}, NewFromEnv())
	t.Setenv("AGENT_SESSION_STORE", "file")
	require.IsType(t, &FileStore{}, NewFromEnv())
}
