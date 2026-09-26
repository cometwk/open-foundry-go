/**
 * AskUserQuestion Tool — 对标 Claude Code AskUserQuestionTool
 *
 * Claude Code 流程:
 *   1. LLM 调用 tool: questions[] (每个含 question + header + options + multiSelect)
 *   2. UI 渲染: 选项卡片 + 预览面板 + 文本输入
 *   3. 用户选择 → answers map 返回 LLM
 *   4. LLM 根据答案继续
 *
 * Web 实现:
 *   - tool execute 返回问题结构 (不阻塞)
 *   - 客户端识别 "ask_user" 工具结果，渲染交互卡片
 *   - 用户选择后作为新消息发送
 *   - 这比 CC 的阻塞式简单，但在 Web 上更自然
 */
package tools

import (
	"context"
	"fmt"

	aisdk "github.com/grafana/ai-sdk"
)

// AskUserOption 问题选项
type AskUserOption struct {
	Label       string `json:"label" jsonschema:"description=Display text for this option"`
	Description string `json:"description,omitempty" jsonschema:"description=Explanation of this option"`
}

// AskUserQuestion 单个问题
type AskUserQuestion struct {
	Question    string          `json:"question" jsonschema:"description=The question to ask the user"`
	Header      string          `json:"header,omitempty" jsonschema:"description=Short label (max 12 chars)"`
	Options     []AskUserOption `json:"options" jsonschema:"minItems=2,maxItems=4,description=Available choices"`
	MultiSelect bool            `json:"multiSelect" jsonschema:"default=false,description=Allow multiple selections"`
}

// AskUserToolInput ask_user 工具输入
type AskUserToolInput struct {
	Questions []AskUserQuestion `json:"questions" jsonschema:"minItems=1,maxItems=4,description=Questions to ask (1-4)"`
}

// CreateAskUserTool 创建用户提问工具 — 对标 createAskUserTool()
func CreateAskUserTool() (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[AskUserToolInput, any]{
		Name: "ask_user",
		Description: "Ask the user a question with predefined options. " +
			"Use when you need user input to make a decision. " +
			"Each question has 2-4 options. Users can also provide free text.",
		Execute: func(ctx context.Context, input AskUserToolInput, _ aisdk.ToolExecutionOptions) (any, error) {
			// 返回问题结构，由客户端渲染交互 UI
			// 客户端会识别 operation: "ask_user" 并特殊处理
			questions := make([]map[string]any, len(input.Questions))
			for i, q := range input.Questions {
				question := map[string]any{
					"question":    q.Question,
					"options":     q.Options,
					"multiSelect": q.MultiSelect,
				}
				if q.Header != "" { // TS 的 header 未设置时序列化时省略该键
					question["header"] = q.Header
				}
				questions[i] = question
			}

			return map[string]any{
				"operation": "ask_user",
				"questions": questions,
				"summary":   fmt.Sprintf("Asked %d question(s) — waiting for user response", len(input.Questions)),
			}, nil
		},
	})
}
