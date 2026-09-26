/**
 * AgentTool — 子 Agent 生成
 *
 * 对标 Claude Code AgentTool 同步模式:
 *   1. 接收 prompt + description + model
 *   2. 组装子 agent 的 system prompt + tools
 *   3. 调用 GenerateText (同步等待)
 *   4. 返回子 agent 的输出
 *
 * 注意: 为避免循环依赖, 子 agent 的工具集通过参数传入
 */
package tools

import (
	"context"
	"fmt"
	"log"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/engine"
	"github.com/openfoundry/agent/internal/llm"
)

// AgentToolInput agent 工具输入
type AgentToolInput struct {
	Prompt      string `json:"prompt" jsonschema:"description=Detailed task description for the sub-agent"`
	Description string `json:"description" jsonschema:"description=Short 3-5 word summary"`
	Model       string `json:"model,omitempty" jsonschema:"enum=sonnet,enum=haiku,description=Model to use (default: sonnet)"`
}

// CreateAgentTool 创建子 agent 工具 — 对标 createAgentTool()
//
// TS 的 tool() 无 name (由 ToolSet 的 key 决定)，Go 固定为 "agent"。
// model: "haiku" → flash (MODELS.FAST)，其余 → default (MODELS.AGENT)。
func CreateAgentTool(cwd string, subTools aisdk.ToolSet) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[AgentToolInput, any]{
		Name: "agent",
		Description: "Launch a sub-agent to handle a complex, multi-step task. " +
			"The sub-agent has access to the same tools and works autonomously. " +
			"Use for tasks requiring multiple steps or deep exploration.",
		Execute: func(ctx context.Context, input AgentToolInput, _ aisdk.ToolExecutionOptions) (any, error) {
			log.Printf("[agent-tool] Spawning: %s", input.Description)

			// 组装子 agent 的 system prompt (与主 agent 一致)
			promptParts := engine.BuildSystemPrompt(ctx, cwd)
			sysMsgs := make([]aisdk.SystemModelMessage, len(promptParts))
			for i, p := range promptParts {
				sysMsgs[i] = aisdk.SystemModelMessage{Content: p}
			}

			model := llm.NewDefaultModel() // MODELS.AGENT
			if input.Model == "haiku" {
				model = llm.NewFlashModel() // MODELS.FAST
			}

			result, err := aisdk.GenerateText(ctx, model,
				aisdk.WithSystemMessages(sysMsgs...),
				aisdk.WithModelMessages(provider.UserText("You are a sub-agent. Complete this task thoroughly.\n\nTask: "+input.Prompt)),
				aisdk.WithTools(subTools),
				aisdk.WithStopWhen(aisdk.StepCountIs(15)),
			)
			if err != nil {
				return map[string]any{
					"status":      "error",
					"description": input.Description,
					"error":       err.Error(),
					"summary":     fmt.Sprintf("Sub-agent %q failed", input.Description),
				}, nil
			}

			steps := len(result.Steps)
			text := result.Text
			if text == "" {
				text = "(no text output)"
			}

			return map[string]any{
				"status":      "completed",
				"description": input.Description,
				"result":      text,
				"steps":       steps,
				"summary":     fmt.Sprintf("Sub-agent %q completed in %d steps", input.Description, steps),
			}, nil
		},
	})
}
