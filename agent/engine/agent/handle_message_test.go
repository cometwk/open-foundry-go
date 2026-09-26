package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/require"
)

// forceEnv 屏蔽宿主环境变量，恢复 EnvConfig 默认状态 (tools/git on, memory off)，
// 并设置 BASE_DIR 指向仓库根 (llm 加载 model.yaml 依赖它；
// 不设置会让 llm 的 sync.Once 缓存加载错误，殃及后续集成测试)
func forceEnv(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs("../../..") // agent/engine/agent -> 仓库根
	require.NoError(t, err)
	t.Setenv("BASE_DIR", root)
	t.Setenv("AGENT_TOOLS", "")
	t.Setenv("AGENT_CONTEXT_GIT", "")
	t.Setenv("AGENT_MEMORY", "")
}

func TestAgentLastUserMessageText(t *testing.T) {
	msgs := []aisdk.UIMessage{
		{ID: "u1", Role: aisdk.RoleUser, Parts: []aisdk.Part{aisdk.TextPart{Text: "第一条"}}},
		{ID: "a1", Role: aisdk.RoleAssistant, Parts: []aisdk.Part{aisdk.TextPart{Text: "回复"}}},
		{ID: "u2", Role: aisdk.RoleUser, Parts: []aisdk.Part{
			aisdk.TextPart{Text: "怎么"},
			aisdk.ToolInvocationPart{ToolCallID: "c1", ToolName: "bash", State: aisdk.ToolStateOutputAvailable},
			aisdk.TextPart{Text: "部署"},
		}},
	}
	require.Equal(t, "怎么 部署", lastUserMessageText(msgs))
	require.Empty(t, lastUserMessageText([]aisdk.UIMessage{msgs[1]}))
	require.Empty(t, lastUserMessageText(nil))
}

// TestHandleMessageSetup 单元测试: 只验证 HandleMessage 的组装逻辑，
// 返回后立即 cancel 使后台流式调用中止 (StreamText 是急切启动的)。
func TestHandleMessageSetup(t *testing.T) {
	forceEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messages := []aisdk.UIMessage{
		{ID: "u1", Role: aisdk.RoleUser, Parts: []aisdk.Part{aisdk.TextPart{Text: "帮我看看这个项目"}}},
	}
	result, err := HandleMessage(ctx, messages, AgentConfig{Cwd: t.TempDir()})
	require.NoError(t, err)

	require.NotNil(t, result.Stream)
	require.NotNil(t, result.BudgetTracker)
	require.False(t, result.WasCompacted)
	require.Contains(t, result.BudgetStatus, "Tokens: 0 | Cost: $0.000 | Turns: 0")

	cancel() // 尽早终止后台 LLM 调用
}

// TestHandleMessageBudgetStop 预算耗尽: 不走主模型，返回通知流
func TestHandleMessageBudgetStop(t *testing.T) {
	forceEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker := engine.CreateBudgetTracker()
	tracker.TotalInputTokens = 900_000 // 默认预算 1M 的 90%

	messages := []aisdk.UIMessage{
		{ID: "u1", Role: aisdk.RoleUser, Parts: []aisdk.Part{aisdk.TextPart{Text: "hi"}}},
	}
	result, err := HandleMessage(ctx, messages, AgentConfig{Cwd: t.TempDir()}, tracker)
	require.NoError(t, err)
	require.NotNil(t, result.Stream)
	require.False(t, result.WasCompacted)
	require.Equal(t, "Token budget 90% exhausted (900,000 / 1,000,000)", result.BudgetStatus)

	cancel()
}

// TestHandleMessage_Integration 真实流式调用: 消费完整流并校验用量记账
func TestHandleMessage_Integration(t *testing.T) {
	require.NoError(t, env.LoadEnv(""))
	forceEnv(t)
	ctx := context.Background()

	messages := []aisdk.UIMessage{
		{ID: "u1", Role: aisdk.RoleUser, Parts: []aisdk.Part{aisdk.TextPart{Text: "2+2 等于几？只回答阿拉伯数字。"}}},
	}
	result, err := HandleMessage(ctx, messages, AgentConfig{Cwd: t.TempDir()})
	require.NoError(t, err)

	// 消费整个流 (对标 handler 中对 stream 的管道消费)
	for range result.Stream.FullStream() {
	}
	result.Stream.Wait()

	text := result.Stream.Text()
	require.NotEmpty(t, text)
	require.Contains(t, text, "4")

	// OnFinish 已执行: tracker 记账 + 状态更新
	// (ContinuationCount = 2: Phase 4 预检查一次 + OnFinish 一次，与 TS 行为一致)
	require.Greater(t, result.BudgetTracker.TotalInputTokens+result.BudgetTracker.TotalOutputTokens, int64(0))
	require.Equal(t, 2, result.BudgetTracker.ContinuationCount)

	testutil.PrintPretty(map[string]any{
		"text":         text,
		"budgetStatus": result.BudgetStatus,
		"turns":        1,
		"elapsed":      time.Since(time.UnixMilli(result.BudgetTracker.StartedAt)).String(),
	})
	// result.BudgetStatus 是返回时的快照 (OnFinish 尚未执行，与 TS 语义一致)；
	// 完成后用 tracker 重新格式化才能看到 Turns: 1
	require.Contains(t, result.BudgetStatus, "Turns: 0")
	require.Contains(t, engine.FormatBudgetStatus(result.BudgetTracker, 1), "Turns: 1")
}
