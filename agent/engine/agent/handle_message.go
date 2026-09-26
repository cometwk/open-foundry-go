/**
 * Agent Engine — 对标 Claude Code QueryEngine + queryLoop
 *
 * Round 2 新增:
 *   - Auto Compact: 消息过长时自动压缩
 *   - Token Budget: 跟踪用量，超预算停止
 *   - Reactive Compact: prompt-too-long 错误恢复
 *
 * Claude Code 完整流程:
 *   QueryEngine.submitMessage(prompt)
 *     → fetchSystemPromptParts()
 *     → auto compact (if needed)
 *     → queryLoop() {
 *         callModel() → tool_use → execute → tool_result → loop
 *         catch prompt_too_long → reactive compact → retry streamText
 *         check token budget → continue or stop
 *       }
 *
 * grafana/ai-sdk 映射:
 *   queryLoop = StreamText({ tools, stopWhen })
 *   auto compact = 调用前检查 + GenerateText 压缩
 *   reactive compact = catch error → compact → retry StreamText
 *   token budget = OnFinish 回调 + usage tracking
 *
 * 注: TS 中 engine/agent.ts 与 ../tools 相互引用 (循环依赖，TS 模块系统允许)；
 * Go 不允许包循环，因此放在独立的 agent 子包，同时引用 engine 与 tools。
 */
package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/engine"
	"github.com/openfoundry/agent/engine/tools"
	"github.com/openfoundry/agent/internal/llm"
)

// AgentConfig agent 配置 (零值字段取默认)
type AgentConfig struct {
	Cwd string
	// MaxSteps 最大步数，默认 25
	MaxSteps int
	// PermissionMode 权限模式，默认 default
	PermissionMode engine.PermissionMode
	// Model 模型别名 (model.yaml)，默认 "default" (对标 MODELS.AGENT)
	Model string
	// Budget Token 预算配置，非零字段覆盖默认值 (对标 Partial<BudgetConfig>)
	Budget *engine.BudgetConfig
	// ThinkingBudget Extended thinking 预算 (对标 CC thinkingConfig)
	ThinkingBudget int
	// Extra 扩展上下文
	Extra map[string]any
}

// HandleMessageResult handleMessage 的返回
type HandleMessageResult struct {
	// Stream StreamText 的结果 (untyped to avoid tool set mismatch)；
	// 调用方可 ToUIMessageStream() 转 UI 消息流后 Pipe 到 HTTP 响应
	Stream *aisdk.StreamTextResult
	// BudgetTracker 更新后的 budget tracker (由调用方跨请求持久化)
	BudgetTracker *engine.BudgetTracker
	// WasCompacted 消息是否被压缩过
	WasCompacted bool
	// BudgetStatus budget 状态 (调试用)
	BudgetStatus string
}

// lastUserMessageText 提取最后一条用户消息的文本 (记忆召回用，
// 对标 agent.ts 内联的 reverse().find(role === "user") 逻辑)
func lastUserMessageText(messages []aisdk.UIMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == aisdk.RoleUser {
			texts := make([]string, 0, len(messages[i].Parts))
			for _, part := range messages[i].Parts {
				if tp, ok := part.(aisdk.TextPart); ok {
					texts = append(texts, tp.Text)
				}
			}
			return strings.Join(texts, " ")
		}
	}
	return ""
}

// derefInt 解引用，nil 返回 0 (对应 TS 的 ?? 0)
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// HandleMessage 处理一轮对话 — 对标 handleMessage()
//
// budgetTracker 可选: 跨请求持久化的 tracker (由调用方管理)，
// 未传时新建 (对应 TS 的 budgetTracker ?? createBudgetTracker())。
func HandleMessage(ctx context.Context, messages []aisdk.UIMessage, config AgentConfig, budgetTracker ...*engine.BudgetTracker) (*HandleMessageResult, error) {
	maxSteps := config.MaxSteps
	if maxSteps == 0 {
		maxSteps = 25
	}
	permissionMode := config.PermissionMode
	if permissionMode == "" {
		permissionMode = engine.PermissionModeDefault
	}
	budget := engine.DefaultBudget
	if config.Budget != nil { // { ...DEFAULT_BUDGET, ...config.budget }
		if config.Budget.MaxTotalTokens != 0 {
			budget.MaxTotalTokens = config.Budget.MaxTotalTokens
		}
		if config.Budget.MaxBudgetUsd != nil {
			budget.MaxBudgetUsd = config.Budget.MaxBudgetUsd
		}
		if config.Budget.MaxTurns != 0 {
			budget.MaxTurns = config.Budget.MaxTurns
		}
	}
	tracker := engine.CreateBudgetTracker()
	if len(budgetTracker) > 0 && budgetTracker[0] != nil {
		tracker = budgetTracker[0]
	}

	// appState (对标 AppState)；TS 单线程无需锁，
	// Go 中 OnFinish 回调与工具执行可能并发，加互斥保护
	var stateMu sync.Mutex
	appState := tools.CreateInitialState(config.Cwd)
	currentTurn := func() int {
		stateMu.Lock()
		defer stateMu.Unlock()
		return appState.TurnCount
	}

	// Tool context (对标 ToolUseContext)
	skills := engine.LoadAllSkills(config.Cwd)
	tctx := tools.ToolContext{
		Cwd:            config.Cwd,
		Ctx:            ctx, // TS 的 AbortController 未对外暴露，直接用请求 ctx
		AllowWrite:     permissionMode != engine.PermissionModePlan,
		AllowBash:      permissionMode != engine.PermissionModePlan,
		PermissionMode: permissionMode,
		GetState: func() tools.AppState {
			stateMu.Lock()
			defer stateMu.Unlock()
			return appState
		},
		SetState: func(fn func(tools.AppState) tools.AppState) {
			stateMu.Lock()
			defer stateMu.Unlock()
			appState = fn(appState)
		},
		Extra:  tools.ToolExtra(config.Extra),
		Skills: skills,
	}

	// ── Phase 0: Auto Compact (对标 autoCompact) ──
	// Claude Code: 在 queryLoop 开始前检查消息长度，超过阈值则压缩
	processedMessages := messages
	wasCompacted := false
	if engine.NeedsCompaction(messages) {
		log.Printf("[agent] Auto-compacting messages...")
		var err error
		processedMessages, err = engine.CompactMessages(ctx, messages)
		if err != nil {
			return nil, err
		}
		wasCompacted = true
	}

	// ── Phase 1: System prompt (含记忆召回) ──
	userMessage := lastUserMessageText(processedMessages)
	promptParts := engine.BuildSystemPrompt(ctx, config.Cwd, userMessage)
	sysMsgs := make([]aisdk.SystemModelMessage, len(promptParts))
	for i, p := range promptParts {
		sysMsgs[i] = aisdk.SystemModelMessage{Content: p}
	}

	// ── Phase 2: Tools ──
	toolSet, err := tools.AssembleTools(tctx)
	if err != nil {
		return nil, err
	}

	// ── Phase 3: Convert messages ──
	modelMessages, err := aisdk.ConvertToModelMessages(processedMessages)
	if err != nil {
		return nil, err
	}

	// ── Phase 4: Budget pre-check ──
	preCheck := engine.UpdateBudget(tracker, 0, 0, currentTurn(), budget)
	if preCheck.Action == engine.BudgetActionStop {
		// 预算已耗尽，返回通知消息而非调 LLM
		stream := aisdk.StreamText(ctx, llm.NewFlashModel(),
			aisdk.WithSystem("You are a helpful assistant."),
			aisdk.WithModelMessages(provider.UserText("Inform the user: "+preCheck.Reason+". Suggest they start a new session.")),
		)
		return &HandleMessageResult{
			Stream:        stream,
			BudgetTracker: tracker,
			WasCompacted:  wasCompacted,
			BudgetStatus:  preCheck.Reason,
		}, nil
	}

	// ── Phase 5: Stream (对标 queryLoop) ──
	model, err := llm.New(config.Model) // model ?? MODELS.AGENT
	if err != nil {
		return nil, err
	}

	stream := aisdk.StreamText(ctx, model,
		aisdk.WithSystemMessages(sysMsgs...),
		aisdk.WithModelMessages(modelMessages...),
		aisdk.WithTools(toolSet),
		aisdk.WithStopWhen(aisdk.StepCountIs(maxSteps)),
		// Extended thinking (对标 CC thinkingConfig):
		// TS 通过 anthropic providerOptions 透传，当前模型为 openai-compatible，暂不透传
		aisdk.OnFinish(func(state aisdk.OnFinishState) {
			// 对标 Claude Code: queryLoop 结束后更新 budget
			u := state.TotalUsage
			decision := engine.UpdateBudget(tracker,
				int64(derefInt(u.InputTokens.Total)),
				int64(derefInt(u.OutputTokens.Total)),
				currentTurn()+1,
				budget,
			)
			if decision.Action == engine.BudgetActionStop {
				log.Printf("[agent] Budget stop: %s", decision.Reason)
			}
			stateMu.Lock()
			appState.TurnCount++
			stateMu.Unlock()

			// ── Post-query: 自动记忆提取 (对标 handleStopHooks → extractMemories) ──
			// 在后台异步运行, 不阻塞响应
			if engine.EnvConfig.AgentMemory() {
				go func(msgs []aisdk.UIMessage) {
					// 请求 ctx 可能已随响应结束而取消，剥离取消信号保留值
					bgCtx := context.WithoutCancel(ctx)
					memories := engine.ExtractMemories(bgCtx, msgs, config.Cwd)
					if len(memories) == 0 {
						return
					}
					if n := engine.SaveExtractedMemories(memories, config.Cwd); n > 0 {
						log.Printf("[agent] Extracted %d memories", n)
					}
				}(processedMessages)
			}
		}),
	)

	return &HandleMessageResult{
		Stream:        stream,
		BudgetTracker: tracker,
		WasCompacted:  wasCompacted,
		BudgetStatus:  engine.FormatBudgetStatus(tracker, currentTurn()),
	}, nil
}

// 注: TS 末尾 re-export 的 BudgetTracker / createBudgetTracker / formatBudgetStatus
// 在 Go 中由调用方直接 import engine 包使用。
