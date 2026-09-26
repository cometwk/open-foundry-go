package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBudgetCreateTracker(t *testing.T) {
	before := time.Now().UnixMilli()
	tracker := CreateBudgetTracker()
	after := time.Now().UnixMilli()

	require.Equal(t, 0, tracker.ContinuationCount)
	require.Equal(t, int64(0), tracker.LastDeltaTokens)
	require.Equal(t, int64(0), tracker.TotalInputTokens)
	require.Equal(t, int64(0), tracker.TotalOutputTokens)
	require.GreaterOrEqual(t, tracker.StartedAt, before)
	require.LessOrEqual(t, tracker.StartedAt, after)
}

func TestBudgetEstimateCost(t *testing.T) {
	tracker := &BudgetTracker{TotalInputTokens: 1_000_000, TotalOutputTokens: 1_000_000}
	require.InDelta(t, 3.0+15.0, EstimateCost(tracker), 1e-9) // default 价格表

	tracker = &BudgetTracker{TotalInputTokens: 500_000, TotalOutputTokens: 500_000}
	require.InDelta(t, 1.5+7.5, EstimateCost(tracker), 1e-9)
}

func TestBudgetUpdateBudget(t *testing.T) {
	cfg := BudgetConfig{MaxTotalTokens: 1000, MaxTurns: 100} // 无 USD 限制 (nil)

	// 正常路径: continue + 状态消息
	tracker := CreateBudgetTracker()
	dec := UpdateBudget(tracker, 300, 200, 1, cfg)
	require.Equal(t, BudgetActionContinue, dec.Action)
	// cost = 300/1M*$3 + 200/1M*$15 = $0.0039 → %.3f = 0.004
	require.Equal(t, "[Budget: 50% used, ~$0.004, turn 1]", dec.Message)
	require.Equal(t, int64(300), tracker.TotalInputTokens)
	require.Equal(t, int64(200), tracker.TotalOutputTokens)
	require.Equal(t, int64(500), tracker.LastDeltaTokens) // delta tracking
	require.Equal(t, 1, tracker.ContinuationCount)

	// Check 1: >= 90% token 预算 → stop (500 + 200 + 200 = 900/1000)
	dec = UpdateBudget(tracker, 200, 200, 2, cfg)
	require.Equal(t, BudgetActionStop, dec.Action)
	require.Equal(t, "Token budget 90% exhausted (900 / 1,000)", dec.Reason)
	require.Equal(t, int64(400), tracker.LastDeltaTokens)
	require.Equal(t, 2, tracker.ContinuationCount)

	// Check 2: USD 预算 (1M input = $3 >= $2.99)
	usd := 2.99
	dec = UpdateBudget(CreateBudgetTracker(), 1_000_000, 0, 1, BudgetConfig{MaxTotalTokens: 100_000_000, MaxBudgetUsd: &usd, MaxTurns: 100})
	require.Equal(t, BudgetActionStop, dec.Action)
	require.Equal(t, "USD budget exhausted ($3.00 / $2.99)", dec.Reason)

	// Check 3: 轮次 (turnCount >= maxTurns)
	dec = UpdateBudget(CreateBudgetTracker(), 1, 1, 100, BudgetConfig{MaxTotalTokens: 1_000_000, MaxTurns: 100})
	require.Equal(t, BudgetActionStop, dec.Action)
	require.Equal(t, "Max turns reached (100 / 100)", dec.Reason)

	// 默认配置 (变参未传 → DefaultBudget: 1M / $5 / 100 turns)
	dec = UpdateBudget(CreateBudgetTracker(), 0, 0, 0)
	require.Equal(t, BudgetActionContinue, dec.Action)
	require.Contains(t, dec.Message, "[Budget: 0% used")
}

func TestBudgetFormatBudgetStatus(t *testing.T) {
	tracker := CreateBudgetTracker()
	tracker.TotalInputTokens = 1_500_000
	tracker.TotalOutputTokens = 500_000

	status := FormatBudgetStatus(tracker, 7)
	// cost = 1.5M*$3 + 0.5M*$15 = $12.000
	require.True(t, strings.HasPrefix(status, "Tokens: 2,000,000 | Cost: $12.000 | Turns: 7 | Time: "), status)
	require.True(t, strings.HasSuffix(status, "s"), status)
}

func TestBudgetToLocaleString(t *testing.T) {
	require.Equal(t, "0", toLocaleString(0))
	require.Equal(t, "999", toLocaleString(999))
	require.Equal(t, "1,000", toLocaleString(1000))
	require.Equal(t, "1,000,000", toLocaleString(1_000_000))
	require.Equal(t, "-1,234,567", toLocaleString(-1_234_567))
}
