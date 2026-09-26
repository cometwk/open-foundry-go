package engine

import (
	"context"
	"strings"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/require"
)

func initEnv(t *testing.T) {
	err := env.LoadEnv("")
	require.NoError(t, err)
}

func userMsg(id, text string) aisdk.UIMessage {
	return aisdk.UIMessage{
		ID:    id,
		Role:  aisdk.RoleUser,
		Parts: []aisdk.Part{aisdk.TextPart{Text: text}},
	}
}

func assistantMsg(id string, parts ...aisdk.Part) aisdk.UIMessage {
	return aisdk.UIMessage{ID: id, Role: aisdk.RoleAssistant, Parts: parts}
}

func TestCompactEstimateTokens(t *testing.T) {
	require.Equal(t, 0, EstimateTokens(nil))

	// 12 chars -> ceil(12/4) = 3
	msgs := []aisdk.UIMessage{userMsg("m1", "hello world!")}
	require.Equal(t, 3, EstimateTokens(msgs))

	// 非 text part 按 JSON 长度估算
	msgs = append(msgs, assistantMsg("m2",
		aisdk.TextPart{Text: "done"}, // 4 chars
		aisdk.ToolInvocationPart{
			ToolCallID: "c1",
			ToolName:   "read_file",
			State:      aisdk.ToolStateOutputAvailable,
		},
	))
	withTool := EstimateTokens(msgs)
	require.Greater(t, withTool, 4) // 工具 JSON 计入估算
}

func TestCompactNeedsCompaction(t *testing.T) {
	// 边界: 恰好等于阈值不触发，超过才触发
	cfg := CompactConfig{MaxTokens: 100}
	exact := []aisdk.UIMessage{userMsg("m1", strings.Repeat("a", 400))}
	over := []aisdk.UIMessage{userMsg("m1", strings.Repeat("a", 401))}
	require.False(t, NeedsCompaction(exact, cfg))
	require.True(t, NeedsCompaction(over, cfg))

	// 默认配置: ~100K token 阈值
	big := []aisdk.UIMessage{userMsg("m1", strings.Repeat("a", DefaultCompactConfig.MaxTokens*4+4))}
	require.True(t, NeedsCompaction(big))
	require.False(t, NeedsCompaction(exact))
}

func TestCompactBuildConversationText(t *testing.T) {
	msgs := []aisdk.UIMessage{
		userMsg("m1", "帮我修复 payments-api 的 500 错误"),
		assistantMsg("m2",
			aisdk.TextPart{Text: "先读一下代码"},
			aisdk.ToolInvocationPart{ToolCallID: "c1", ToolName: "read_file", State: aisdk.ToolStateOutputAvailable},
			aisdk.ToolInvocationPart{ToolCallID: "c2", ToolName: "edit_file", State: aisdk.ToolStateOutputAvailable},
		),
	}

	got := buildConversationText(msgs)
	require.Equal(t, strings.Join([]string{
		"Human: 帮我修复 payments-api 的 500 错误",
		"Assistant: 先读一下代码 [Tool: read_file], [Tool: edit_file]",
	}, "\n\n"), got)
}

func TestCompactMessages_TooFewMessages(t *testing.T) {
	// 消息数 <= PreserveRecent: 原样返回，不调用 LLM
	msgs := []aisdk.UIMessage{
		userMsg("m1", "hello"),
		assistantMsg("m2", aisdk.TextPart{Text: "hi"}),
	}
	out, err := CompactMessages(context.Background(), msgs, CompactConfig{PreserveRecent: 2})
	require.NoError(t, err)
	require.Equal(t, msgs, out)
}

func TestCompactMessages_Integration(t *testing.T) {
	initEnv(t)
	ctx := context.Background()

	// 模拟一段编码会话: 8 条消息，保留最近 2 条，前 6 条压缩为摘要
	messages := []aisdk.UIMessage{
		userMsg("m1", "帮我修复 payments-api 的 500 错误，相关代码在 internal/api/handler.go"),
		assistantMsg("m2",
			aisdk.TextPart{Text: "我来读一下这个文件"},
			aisdk.ToolInvocationPart{ToolCallID: "c1", ToolName: "read_file", State: aisdk.ToolStateOutputAvailable},
		),
		assistantMsg("m3", aisdk.TextPart{Text: "已定位: handler.go 第 42 行空指针，因为 order 参数没有做非空校验"}),
		userMsg("m4", "好，请加上校验并补充单元测试"),
		assistantMsg("m5", aisdk.TextPart{Text: "已在 handler.go 增加 order != nil 校验，并在 handler_test.go 添加 TestHandler_MissingOrder"}),
		userMsg("m6", "跑一下测试"),
		assistantMsg("m7", aisdk.TextPart{Text: "go test ./internal/api/ 全部通过 (12 tests)"}),
		userMsg("m8", "帮我把这些改动提交"),
	}

	cfg := CompactConfig{PreserveRecent: 2}
	out, err := CompactMessages(ctx, messages, cfg)
	require.NoError(t, err)

	// [摘要消息] + 最近 2 条原始消息
	require.Len(t, out, 3)

	compact := out[0]
	require.True(t, strings.HasPrefix(compact.ID, "compact-"), "compact id: %s", compact.ID)
	require.Equal(t, aisdk.RoleAssistant, compact.Role)
	require.Len(t, compact.Parts, 1)
	textPart, ok := compact.Parts[0].(aisdk.TextPart)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(textPart.Text, "[Context compressed]"), "summary: %s", textPart.Text)

	// 最近消息原样保留
	require.Equal(t, "m7", out[1].ID)
	require.Equal(t, "m8", out[2].ID)

	t.Logf("summary:\n%s", textPart.Text)
	testutil.PrintPretty(map[string]any{
		"before": len(messages),
		"after":  len(out),
		"tokens": EstimateTokens(out),
	})
}
