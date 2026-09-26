/**
 * Token Budget — 对标 Claude Code query/tokenBudget.ts
 *
 * Claude Code 预算策略:
 *   - BudgetTracker 跟踪: continuationCount, lastDeltaTokens, startedAt
 *   - checkTokenBudget(): continue (注入 nudge) 或 stop
 *   - 阈值: 90% budget → stop; delta < 500 连续 3 次 → stop (收益递减)
 *   - USD 预算: getTotalCost() >= maxBudgetUsd → stop
 *
 * grafana/ai-sdk 映射:
 *   - StreamText result usage → { promptTokens, completionTokens }
 *   - 无内置预算管理，需手动追踪
 */
package engine

import (
	"fmt"
	"strconv"
	"time"
)

// BudgetTracker 用量跟踪 (跨请求持久化，由调用方管理)
type BudgetTracker struct {
	ContinuationCount int   // ContinuationCount 连续继续次数
	LastDeltaTokens   int64 // LastDeltaTokens 上一轮 delta tokens
	TotalInputTokens  int64 // TotalInputTokens 累计输入 tokens
	TotalOutputTokens int64 // TotalOutputTokens 累计输出 tokens
	StartedAt         int64 // StartedAt 会话开始时间 (UnixMilli)
}

// CreateBudgetTracker 创建 budget tracker
func CreateBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		ContinuationCount: 0,
		LastDeltaTokens:   0,
		TotalInputTokens:  0,
		TotalOutputTokens: 0,
		StartedAt:         time.Now().UnixMilli(),
	}
}

// BudgetConfig 预算配置
type BudgetConfig struct {
	/** MaxTotalTokens 最大总 token 数 (input + output) */
	MaxTotalTokens int64
	/** MaxBudgetUsd 最大 USD 花费 (nil = 不限) */
	MaxBudgetUsd *float64
	/** MaxTurns 最大轮次 */
	MaxTurns int
}

// DefaultBudget 默认预算
var DefaultBudget = BudgetConfig{
	MaxTotalTokens: 1_000_000, // 1M tokens per session
	MaxBudgetUsd:   float64Ptr(5.0),
	MaxTurns:       100,
}

// 预算判定动作 (TS 的 "continue" | "stop" 联合)
const (
	BudgetActionContinue = "continue"
	BudgetActionStop     = "stop"
)

// BudgetDecision 预算判定结果 (TS 是带可选字段的联合类型，Go 合并为一个结构)
type BudgetDecision struct {
	Action string
	/** Message continue 时的状态信息 */
	Message string
	/** Reason stop 时的原因 */
	Reason string
}

// price 每 1M token 价格 (USD)
type price struct{ Input, Output float64 }

// pricing 价格表 ($/1M tokens)
var pricing = map[string]price{
	"sonnet":  {Input: 3, Output: 15},
	"haiku":   {Input: 0.25, Output: 1.25},
	"default": {Input: 3, Output: 15},
}

// EstimateCost 估算 USD 花费
func EstimateCost(tracker *BudgetTracker) float64 {
	p := pricing["default"]
	return (float64(tracker.TotalInputTokens)/1_000_000)*p.Input +
		(float64(tracker.TotalOutputTokens)/1_000_000)*p.Output
}

// toLocaleString 千分位分组，对应 JS 的 Number.toLocaleString()
func toLocaleString(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := len(s) > 0 && s[0] == '-'
	if neg {
		s = s[1:]
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, s[i])
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// UpdateBudget 更新 tracker 并返回决策 — 对标 checkTokenBudget()。
// configs 可选，未传时用 DefaultBudget。
func UpdateBudget(tracker *BudgetTracker, promptTokens, completionTokens int64, turnCount int, configs ...BudgetConfig) BudgetDecision {
	config := DefaultBudget
	if len(configs) > 0 {
		config = configs[0]
	}

	// 更新累计
	prevTotal := tracker.TotalInputTokens + tracker.TotalOutputTokens
	tracker.TotalInputTokens += promptTokens
	tracker.TotalOutputTokens += completionTokens
	newTotal := tracker.TotalInputTokens + tracker.TotalOutputTokens

	// Delta tracking (对标 diminishing returns 检测)
	tracker.LastDeltaTokens = newTotal - prevTotal
	tracker.ContinuationCount++

	// Check 1: Token 总量
	utilization := float64(newTotal) / float64(config.MaxTotalTokens)
	if utilization >= 0.9 {
		return BudgetDecision{
			Action: BudgetActionStop,
			Reason: fmt.Sprintf("Token budget 90%% exhausted (%s / %s)",
				toLocaleString(newTotal), toLocaleString(config.MaxTotalTokens)),
		}
	}

	// Check 2: USD 预算
	if config.MaxBudgetUsd != nil {
		cost := EstimateCost(tracker)
		if cost >= *config.MaxBudgetUsd {
			return BudgetDecision{
				Action: BudgetActionStop,
				Reason: fmt.Sprintf("USD budget exhausted ($%.2f / $%g)", cost, *config.MaxBudgetUsd),
			}
		}
	}

	// Check 3: 轮次
	if turnCount >= config.MaxTurns {
		return BudgetDecision{
			Action: BudgetActionStop,
			Reason: fmt.Sprintf("Max turns reached (%d / %d)", turnCount, config.MaxTurns),
		}
	}

	// Continue with status
	pct := int(utilization*100 + 0.5) // Math.round
	return BudgetDecision{
		Action:  BudgetActionContinue,
		Message: fmt.Sprintf("[Budget: %d%% used, ~$%.3f, turn %d]", pct, EstimateCost(tracker), turnCount),
	}
}

// FormatBudgetStatus 格式化 budget 状态为字符串
func FormatBudgetStatus(tracker *BudgetTracker, turnCount int) string {
	total := tracker.TotalInputTokens + tracker.TotalOutputTokens
	cost := EstimateCost(tracker)
	duration := (time.Now().UnixMilli() - tracker.StartedAt) / 1000
	return fmt.Sprintf("Tokens: %s | Cost: $%.3f | Turns: %d | Time: %ds",
		toLocaleString(total), cost, turnCount, duration)
}

// float64Ptr 辅助取指针
func float64Ptr(f float64) *float64 { return &f }
