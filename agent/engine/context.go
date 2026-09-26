/**
 * Context Assembly — 对标 Claude Code context.ts
 *
 * Round 5: + 记忆召回 + Skills 注入
 *
 * 系统提示词组装顺序 (对标 Claude Code):
 *   1. 核心指令 + 环境 + Git (cached)
 *   2. AGENTS.md 项目指令 (cached)
 *   3. Memory 索引 + manifest (cached)
 *   4. 召回的相关记忆 (per-turn, 不 cached)
 *   5. Skills 列表 (cached)
 */
package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// getGitContext 获取 git 状态信息；未开启、非 git 仓库或超时 (5s) 返回空串
func getGitContext(ctx context.Context, cwd string) string {
	if !EnvConfig.AgentContextGit() {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 对标 TS 的单条 shell 命令 && 串联，失败 (非 git 仓库等) 即返回空
	cmd := exec.CommandContext(ctx, "sh", "-c",
		"git rev-parse --is-inside-work-tree && git branch --show-current && git status --short")
	cmd.Dir = cwd // 对标 TS exec 的 { cwd } 选项
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] != "true" {
		return ""
	}
	branch := "unknown"
	if len(lines) > 1 && lines[1] != "" {
		branch = lines[1]
	}
	status := ""
	if len(lines) > 2 {
		status = strings.Join(lines[2:], "\n")
	}

	if status != "" {
		return "Git branch: " + branch + "\nChanges:\n" + status
	}
	return "Git branch: " + branch + "\nWorking tree clean"
}

// readAgentsMd 读取 AGENTS.md (TS 里叫 readClaudeMd)，截断前 10000 字符；不存在返回空串
func readAgentsMd(cwd string) string {
	raw, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err != nil {
		return ""
	}
	return truncateRunes(string(raw), 10_000)
}

// truncateRunes 按字符 (rune) 截断，避免像 TS 的 UTF-16 slice 那样切断多字节字符
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

const agentPromptTemplate = `You are an AI coding assistant that helps users with software engineering tasks.
You have access to tools for reading files, editing files, searching code, running shell commands, fetching URLs, searching the web, and spawning sub-agents.

## Tools
- bash: run shell commands (git, builds, tests, scripts)
- file_read: read file contents with line numbers
- file_edit: edit files via string replacement
- file_write: create or overwrite files
- glob: find files by pattern
- grep: search file contents with regex
- web_fetch: fetch URL content (docs, APIs)
- web_search: search the web for information
- agent: spawn a sub-agent for complex multi-step subtasks
- ask_user: ask the user a question with predefined options

## Rules
- Read files before editing them to understand existing code
- Use the appropriate tool for each task (grep for searching, glob for finding files, etc.)
- When editing files, preserve existing patterns and style
- Run tests after making changes when possible
- Be concise in explanations, focus on the task
- If a task requires multiple steps, work through them systematically

## Environment
- Working directory: %s
- Platform: %s
- Date: %s
`

// BuildSystemPrompt 构建完整系统提示词 — 对标 fetchSystemPromptParts()，
// 返回按序组装的 system prompt 片段列表 (SystemModelMessage 即 string)。
//
// userMessage 为可选参数: 当前用户消息，用于记忆召回。
func BuildSystemPrompt(ctx context.Context, cwd string, userMessage ...string) []string {
	msg := ""
	if len(userMessage) > 0 {
		msg = userMessage[0]
	}

	// 加载所有上下文源 (TS 用 Promise.all 并行，这里串行依次加载)
	gitContext := getGitContext(ctx, cwd)
	agentsMd := readAgentsMd(cwd)
	memoryPart, hasMemory := BuildMemoryPromptPart(cwd)
	skills := LoadAllSkills(cwd)

	// 记忆召回 (依赖 userMessage, 不能并行)
	var recalledPart string
	if EnvConfig.AgentMemory() && msg != "" {
		recalled := RecallRelevantMemories(ctx, msg, cwd)
		if len(recalled) > 0 {
			recalledPart = FormatRecalledMemories(recalled)
		}
	}

	var messages []string

	// Part 1: 核心指令 + 环境 (cached)
	skillsPrompt := FormatSkillsForPrompt(skills)
	if EnvConfig.AgentTools() {
		// Platform 对应 process.platform; Date 对应 toISOString().split('T')[0]
		prompt := fmt.Sprintf(agentPromptTemplate, cwd, runtime.GOOS, time.Now().UTC().Format("2006-01-02"))
		if gitContext != "" {
			prompt += "\n## Git Status\n" + gitContext
		}
		prompt += skillsPrompt
		messages = append(messages, prompt)
	} else {
		messages = append(messages, skillsPrompt)
	}

	// Part 2: AGENTS.md
	if agentsMd != "" {
		messages = append(messages, "## Project Instructions (AGENTS.md)\n"+agentsMd)
	}

	// Part 3: Memory index + manifest (cached)
	if hasMemory {
		messages = append(messages, memoryPart)
	}

	// Part 4: Recalled memories (per-turn, NOT cached)
	if recalledPart != "" {
		messages = append(messages, recalledPart)
	}

	return messages
}
