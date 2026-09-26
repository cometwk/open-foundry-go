// Package engine 提供上下文压缩 (Context Compaction) — 对标 Claude Code services/compact/
//
// Claude Code 压缩策略:
//  1. 触发条件: 消息 token 接近模型上下文窗口 (~80%)
//  2. 压缩方式: 用 LLM 生成结构化摘要替换旧消息
//  3. 保留: 最近 N 条消息不压缩
//  4. 摘要结构: 主要请求 + 技术概念 + 文件变更 + 错误 + 下一步
//
// grafana/ai-sdk 映射:
//   - 无内置压缩，需手动实现
//   - 用 aisdk.GenerateText 生成摘要
//   - 在 handleMessage 前检查并压缩
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/internal/llm"
)

// CompactConfig 压缩配置
type CompactConfig struct {
	MaxTokens      int // MaxTokens 触发压缩的 token 阈值
	PreserveRecent int // PreserveRecent 保留的最近消息数
	TargetTokens   int // TargetTokens 压缩后的目标 token 数
}

// DefaultCompactConfig 默认压缩配置
var DefaultCompactConfig = CompactConfig{
	MaxTokens:      100_000, // ~80% of 128K context
	PreserveRecent: 6,       // 保留最近 6 条消息
	TargetTokens:   50_000,  // 压缩后目标 50K
}

// configOrDefault 取可选配置，未传时用默认值 (对应 TS 的默认参数)
func configOrDefault(configs []CompactConfig) CompactConfig {
	if len(configs) > 0 {
		return configs[0]
	}
	return DefaultCompactConfig
}

// EstimateTokens 粗略估算消息 token 数 (4 chars ≈ 1 token)
func EstimateTokens(messages []aisdk.UIMessage) int {
	chars := 0
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if tp, ok := part.(aisdk.TextPart); ok {
				chars += len(tp.Text)
			} else {
				// tool calls, etc. — 估算 JSON 长度
				if raw, err := json.Marshal(part); err == nil {
					chars += len(raw)
				}
			}
		}
	}
	return int(math.Ceil(float64(chars) / 4))
}

// NeedsCompaction 判断是否需要压缩
func NeedsCompaction(messages []aisdk.UIMessage, config ...CompactConfig) bool {
	cfg := configOrDefault(config)
	return EstimateTokens(messages) > cfg.MaxTokens
}

const compactSystemPrompt = `You are a conversation summarizer. Create a concise structured summary.
Do NOT use any tools. Only output text.`

const compactPromptTemplate = `Summarize this conversation into a structured summary that preserves all important context for continuing the work:

%s

Format your summary as:
## Summary of prior conversation
- **Primary request**: What the user originally asked for
- **Key decisions**: Important choices made
- **Files modified**: List of files changed and how
- **Current state**: Where things stand now
- **Next steps**: What remains to be done

Be concise but preserve technical details (file paths, function names, error messages).`

// CompactMessages 压缩消息 — 对标 Claude Code 的 autoCompact
//
// 流程:
//  1. 将旧消息 (排除最近 N 条) 提取为文本
//  2. 用 LLM 生成结构化摘要
//  3. 返回 [摘要消息] + [最近 N 条原始消息]
func CompactMessages(ctx context.Context, messages []aisdk.UIMessage, config ...CompactConfig) ([]aisdk.UIMessage, error) {
	cfg := configOrDefault(config)
	if len(messages) <= cfg.PreserveRecent {
		return messages, nil // 消息太少，不压缩
	}

	splitAt := len(messages) - cfg.PreserveRecent
	oldMessages := messages[:splitAt]
	recentMessages := messages[splitAt:]

	// 提取旧消息文本
	conversationText := buildConversationText(oldMessages)

	// 对标 Claude Code compact prompt: 结构化摘要
	result, err := aisdk.GenerateText(ctx, llm.NewFlashModel(), // flash 快速压缩
		aisdk.WithSystem(compactSystemPrompt),
		aisdk.WithModelMessages(provider.UserText(fmt.Sprintf(compactPromptTemplate, conversationText))),
	)
	if err != nil {
		return nil, err
	}

	// 构建压缩后的消息列表
	compactMessage := aisdk.UIMessage{
		ID:   fmt.Sprintf("compact-%d", time.Now().UnixMilli()),
		Role: aisdk.RoleAssistant,
		Parts: []aisdk.Part{
			aisdk.TextPart{Text: "[Context compressed]\n\n" + result.Text},
		},
	}

	return append([]aisdk.UIMessage{compactMessage}, recentMessages...), nil
}

// buildConversationText 将消息列表提取为 "Human: .../Assistant: ..." 形式的对话文本
func buildConversationText(messages []aisdk.UIMessage) string {
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		role := "Assistant"
		if msg.Role == aisdk.RoleUser {
			role = "Human"
		}

		var texts []string
		var toolSummaries []string
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case aisdk.TextPart:
				texts = append(texts, p.Text)
			case aisdk.ToolInvocationPart: // 工具调用摘要
				toolSummaries = append(toolSummaries, toolSummary(p.ToolName, part))
			case aisdk.DynamicToolUIPart: // 工具调用摘要
				toolSummaries = append(toolSummaries, toolSummary(p.ToolName, part))
			}
		}

		line := role + ": " + strings.Join(texts, "\n")
		if len(toolSummaries) > 0 {
			line += " " + strings.Join(toolSummaries, ", ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}

// toolSummary 生成 "[Tool: name]" 摘要，名称缺失时退回 part 类型
func toolSummary(name string, part aisdk.Part) string {
	if name == "" {
		name = part.PartType()
	}
	return "[Tool: " + name + "]"
}
