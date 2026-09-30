package agent

import (
	"context"
	"log"
	"sync"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/engine"
	"github.com/openfoundry/agent/engine/tools"
	"github.com/openfoundry/agent/internal/llm"
)

func NewAgentLoop(config AgentConfig) (*aisdk.ToolLoopAgent, error) {

	// model, err := llm.New(config.Model)
	// if err != nil {
	// 	return nil, err
	// }
	// agent := aisdk.NewToolLoopAgent(model,
	// 	aisdk.WithToolLoopAgentID("incident-assistant"),
	// 	aisdk.WithToolLoopAgentOptions(
	// 		aisdk.WithInstructions("Help operators investigate incidents."),
	// 		aisdk.WithTools(tools),
	// 		aisdk.WithStopWhen(aisdk.StepCountIs(8)),
	// 	),
	// )

	// result, err := agent.Generate(ctx,
	// 	aisdk.WithAgentPrompt("Investigate the payment API alert."),
	// )

	return nil, nil
}

// HandleMessage 处理一轮对话 — 对标 handleMessage()
//
// budgetTracker 可选: 跨请求持久化的 tracker (由调用方管理)，
// 未传时新建 (对应 TS 的 budgetTracker ?? createBudgetTracker())。
func HandleMessageUI(ctx context.Context, messages []aisdk.UIMessage, config AgentConfig, budgetTracker ...*engine.BudgetTracker) (*HandleMessageResult, error) {
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
			// // 对标 Claude Code: queryLoop 结束后更新 budget
			// u := state.TotalUsage
			// decision := engine.UpdateBudget(tracker,
			// 	int64(derefInt(u.InputTokens.Total)),
			// 	int64(derefInt(u.OutputTokens.Total)),
			// 	currentTurn()+1,
			// 	budget,
			// )
			// if decision.Action == engine.BudgetActionStop {
			// 	log.Printf("[agent] Budget stop: %s", decision.Reason)
			// }
			// stateMu.Lock()
			// appState.TurnCount++
			// stateMu.Unlock()

			// // ── Post-query: 自动记忆提取 (对标 handleStopHooks → extractMemories) ──
			// // 在后台异步运行, 不阻塞响应
			// if engine.EnvConfig.AgentMemory() {
			// 	go func(msgs []aisdk.UIMessage) {
			// 		// 请求 ctx 可能已随响应结束而取消，剥离取消信号保留值
			// 		bgCtx := context.WithoutCancel(ctx)
			// 		memories := engine.ExtractMemories(bgCtx, msgs, config.Cwd)
			// 		if len(memories) == 0 {
			// 			return
			// 		}
			// 		if n := engine.SaveExtractedMemories(memories, config.Cwd); n > 0 {
			// 			log.Printf("[agent] Extracted %d memories", n)
			// 		}
			// 	}(processedMessages)
			// }
		}),
	)

	// streamOpts := make([]aisdk.UIMessageStreamOption, 0)
	// streamOpts = append(streamOpts, aisdk.WithUIMessageStreamOriginalMessages(messages...))
	// uiStream := stream.ToUIMessageStream(streamOpts...)

	return &HandleMessageResult{
		Stream:        stream,
		BudgetTracker: tracker,
		WasCompacted:  wasCompacted,
		BudgetStatus:  engine.FormatBudgetStatus(tracker, currentTurn()),
	}, nil
}
