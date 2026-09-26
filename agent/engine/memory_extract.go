/**
 * 自动记忆提取 — 对标 Claude Code services/extractMemories/
 *
 * Claude Code 流程:
 *   1. 触发: queryLoop 结束 + 无 tool calls (最终回复)
 *   2. 方法: fork agent 分析对话 transcript
 *   3. 跳过: 如果 assistant 已手动写入 memory 目录
 *   4. 输出: 写入 .agent/memory/ (frontmatter + body)
 *
 * 我们的实现:
 *   - 用 GenerateText (flash) 分析最近消息
 *   - 提取值得记住的信息
 *   - 写入 memory 文件 + 更新索引
 */
package engine

import (
	"context"
	"fmt"
	"log"
	"strings"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/internal/llm"
)

// ExtractedMemory 提取出的一条记忆
type ExtractedMemory struct {
	Filename    string
	Name        string
	Description string
	Type        MemoryType
	Content     string
}

const extractSystemPrompt = `You analyze conversations to extract information worth remembering for future sessions.

Types of memory:
- user: User's role, preferences, expertise (e.g. "senior Go developer", "prefers terse responses")
- feedback: Corrections or confirmed approaches (e.g. "don't mock DB in tests", "single PR preferred")
- project: Non-obvious facts about the project (e.g. "auth rewrite driven by compliance", "deploy freeze March 5")
- reference: Pointers to external resources (e.g. "bugs tracked in Linear project INGEST")

Do NOT extract:
- Code patterns derivable from reading the code
- Git history information
- Debugging solutions (the fix is in the code)
- Ephemeral task details

Output format (one per memory, or "NONE" if nothing worth saving):
---
FILENAME: descriptive-slug.md
NAME: Short name
TYPE: user|feedback|project|reference
DESCRIPTION: One-line description for index
CONTENT:
The actual memory content
---`

// ExtractMemories 从对话中自动提取值得记忆的信息
//
// 对标 Claude Code 的 extractMemories():
//   - 分析最近 10 条消息
//   - 识别 user/feedback/project/reference 类型
//   - 跳过已在代码/git 中可推导的信息
//
// LLM 失败时记日志并返回空 (与 TS 的 catch 行为一致，不影响主流程)
func ExtractMemories(ctx context.Context, messages []aisdk.UIMessage, cwd string) []ExtractedMemory {
	_ = cwd // 与 TS 签名保持一致，实际写入在 SaveExtractedMemories

	transcript := buildExtractTranscript(messages)
	if len(transcript) < 100 {
		return nil // 对话太短，不提取
	}

	result, err := aisdk.GenerateText(ctx, llm.NewFlashModel(),
		aisdk.WithSystem(extractSystemPrompt),
		aisdk.WithModelMessages(provider.UserText("Analyze this conversation and extract memories worth saving:\n\n"+transcript)),
	)
	if err != nil {
		log.Printf("[memory-extract] Failed: %v", err)
		return nil
	}

	return parseExtractedMemories(result.Text)
}

// buildExtractTranscript 将最近 10 条消息转为 "Human: ..." 形式的 transcript，
// 过滤长度 <= 10 的行
func buildExtractTranscript(messages []aisdk.UIMessage) string {
	recent := messages
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}

	lines := make([]string, 0, len(recent))
	for _, m := range recent {
		role := "Assistant"
		if m.Role == aisdk.RoleUser {
			role = "Human"
		}
		texts := make([]string, 0, len(m.Parts))
		for _, part := range m.Parts {
			if tp, ok := part.(aisdk.TextPart); ok {
				texts = append(texts, tp.Text)
			}
		}
		line := role + ": " + strings.Join(texts, "\n")
		if len(line) > 10 {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n\n")
}

// parseExtractedMemories 解析 LLM 输出: "---" 分隔的块，每块含
// FILENAME:/NAME:/TYPE:/DESCRIPTION: 行字段和 CONTENT: 之后的正文，五项齐全才收录
func parseExtractedMemories(text string) []ExtractedMemory {
	if strings.TrimSpace(text) == "NONE" || !strings.Contains(text, "FILENAME:") {
		return nil
	}

	var memories []ExtractedMemory
	for _, block := range strings.Split(text, "---") {
		if !strings.Contains(block, "FILENAME:") {
			continue
		}
		mem := ExtractedMemory{
			Filename:    fieldLine(block, "FILENAME:"),
			Name:        fieldLine(block, "NAME:"),
			Type:        MemoryType(fieldLine(block, "TYPE:")),
			Description: fieldLine(block, "DESCRIPTION:"),
			Content:     strings.TrimSpace(fieldBody(block, "CONTENT:")),
		}
		if mem.Filename != "" && mem.Name != "" && mem.Type != "" && mem.Description != "" && mem.Content != "" {
			memories = append(memories, mem)
		}
	}
	return memories
}

// fieldLine 返回块中以 key 开头的那一行冒号后的值 (trim 后)；未找到返回空串
func fieldLine(block, key string) string {
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			return strings.TrimSpace(trimmed[len(key):])
		}
	}
	return ""
}

// fieldBody 返回块中以 key 开头的那一行之后的所有内容
func fieldBody(block, key string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return ""
}

// SaveExtractedMemories 保存提取的记忆到磁盘 — 对标 extractMemories 的写入逻辑，
// 返回成功保存的条数 (单条失败只记日志，继续处理)
func SaveExtractedMemories(memories []ExtractedMemory, cwd string) int {
	saved := 0
	for _, mem := range memories {
		_, err := WriteMemoryFile(cwd, mem.Filename, MemoryMeta{
			Name:        mem.Name,
			Description: mem.Description,
			Type:        mem.Type,
		}, mem.Content)
		if err == nil {
			err = UpdateMemoryIndex(cwd, fmt.Sprintf("- [%s](%s) — %s", mem.Name, mem.Filename, mem.Description))
		}
		if err != nil {
			log.Printf("[memory-extract] Failed to save %s: %v", mem.Filename, err)
			continue
		}
		saved++
	}
	return saved
}
