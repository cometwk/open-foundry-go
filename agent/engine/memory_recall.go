/**
 * 记忆召回 — 对标 Claude Code memdir/findRelevantMemories.ts
 *
 * Claude Code 流程:
 *   1. scanMemoryFiles() → 获取所有记忆 header
 *   2. formatMemoryManifest() → 格式化为摘要列表
 *   3. sideQuery(Sonnet) → 选出 ≤5 条最相关记忆
 *   4. 排除已注入的记忆
 *   5. 返回文件路径列表
 *
 * 我们的实现:
 *   - 用 flash (更快更便宜) 做选择
 *   - 基于当前用户消息 + memory manifest 判断相关性
 */
package engine

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/internal/llm"
)

// maxRecalled 最多召回的记忆数
const maxRecalled = 5

const recallSystemPrompt = `You select the most relevant memories for a user's current task.
Only include memories that are CERTAINLY helpful. Be selective.
Output ONLY the indices (comma-separated numbers), nothing else.
If none are relevant, output "NONE".`

const recallPromptTemplate = `User's message: %s

Available memories:
%s

Select up to %d most relevant (indices only):`

// RecallRelevantMemories 召回与当前查询相关的记忆
//
// 对标 findRelevantMemories():
//   - 输入: 用户最新消息 + 已注入的记忆文件名
//   - 输出: 最相关的 ≤5 个记忆文件内容
//
// LLM 失败时记日志并返回空 (与 TS 的 catch 行为一致)
func RecallRelevantMemories(ctx context.Context, userMessage, cwd string, alreadyInjected ...string) []MemoryFile {
	headers := ScanMemoryFiles(cwd)
	if len(headers) == 0 {
		return nil
	}

	// 过滤已注入的
	injected := make(map[string]struct{}, len(alreadyInjected))
	for _, name := range alreadyInjected {
		injected[name] = struct{}{}
	}
	candidates := make([]MemoryHeader, 0, len(headers))
	for _, h := range headers {
		if _, ok := injected[h.Filename]; !ok {
			candidates = append(candidates, h)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// 如果候选少于 maxRecalled，直接全部返回
	if len(candidates) <= maxRecalled {
		return readMemoryFiles(candidates)
	}

	// 用 LLM 选择最相关的记忆 (对标 sideQuery)
	manifests := make([]string, len(candidates))
	for i, h := range candidates {
		manifests[i] = fmt.Sprintf("%d: [%s] %s — %s", i, h.Type, h.Filename, h.Description)
	}

	result, err := aisdk.GenerateText(ctx, llm.NewFlashModel(),
		aisdk.WithSystem(recallSystemPrompt),
		aisdk.WithModelMessages(provider.UserText(fmt.Sprintf(recallPromptTemplate, userMessage, strings.Join(manifests, "\n"), maxRecalled))),
	)
	if err != nil {
		log.Printf("[memory-recall] Failed: %v", err)
		return nil
	}
	if strings.TrimSpace(result.Text) == "NONE" {
		return nil
	}

	// 解析索引，读取选中的记忆
	selected := make([]MemoryHeader, 0, maxRecalled)
	for _, idx := range parseRecallIndices(result.Text, len(candidates)) {
		selected = append(selected, candidates[idx])
	}
	return readMemoryFiles(selected)
}

// readMemoryFiles 读取记忆文件，跳过读取失败的
func readMemoryFiles(headers []MemoryHeader) []MemoryFile {
	var files []MemoryFile
	for _, h := range headers {
		if f := ReadMemoryFile(h.FilePath); f != nil {
			files = append(files, *f)
		}
	}
	return files
}

// parseRecallIndices 解析 LLM 输出的逗号分隔索引:
// 剔除非 [0-9,] 字符、过滤越界/非法值、最多 maxRecalled 个
func parseRecallIndices(text string, count int) []int {
	cleaned := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == ',' {
			return r
		}
		return -1
	}, text)

	var indices []int
	for _, part := range strings.Split(cleaned, ",") {
		if len(indices) >= maxRecalled {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 || n >= count {
			continue
		}
		indices = append(indices, n)
	}
	return indices
}

// FormatRecalledMemories 格式化召回的记忆为 system prompt 片段
func FormatRecalledMemories(memories []MemoryFile) string {
	if len(memories) == 0 {
		return ""
	}

	parts := make([]string, 0, len(memories))
	for _, m := range memories {
		parts = append(parts, fmt.Sprintf("### %s (%s)\n%s", m.Name, m.Type, m.Content))
	}
	return "\n## Recalled Memories\n" + strings.Join(parts, "\n\n")
}
