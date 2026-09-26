package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// initGitRepo 在临时目录创建一个 git 仓库 (分支 test-branch)，dirty 时留一个未跟踪文件
func initGitRepo(t *testing.T, dirty bool) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init")
	run("checkout", "-b", "test-branch")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	run("add", ".")
	run("commit", "-m", "init")
	if dirty {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n"), 0o644))
	}
	return dir
}

// forceDefaultEnv 恢复 EnvConfig 的默认状态 (tools/git on, memory off)，屏蔽宿主环境变量
func forceDefaultEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_TOOLS", "")
	t.Setenv("AGENT_CONTEXT_GIT", "")
	t.Setenv("AGENT_MEMORY", "")
}

func TestContextGetGitContext(t *testing.T) {
	forceDefaultEnv(t)

	// 非 git 目录 -> 空串 (cmd.Dir 已正确指向 cwd，而非进程目录)
	require.Empty(t, getGitContext(context.Background(), t.TempDir()))

	// 开关关闭 -> 空串
	t.Setenv("AGENT_CONTEXT_GIT", "off")
	require.Empty(t, getGitContext(context.Background(), initGitRepo(t, false)))
	t.Setenv("AGENT_CONTEXT_GIT", "")

	// 干净仓库
	require.Equal(t, "Git branch: test-branch\nWorking tree clean",
		getGitContext(context.Background(), initGitRepo(t, false)))

	// 脏工作区
	require.Equal(t, "Git branch: test-branch\nChanges:\n?? new.txt",
		getGitContext(context.Background(), initGitRepo(t, true)))
}

func TestContextReadAgentsMd(t *testing.T) {
	cwd := t.TempDir()
	require.Empty(t, readAgentsMd(cwd)) // 不存在

	content := "# Project Guide\nUse conventional commits."
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte(content), 0o644))
	require.Equal(t, content, readAgentsMd(cwd))

	// 超过 10000 字符截断 (按 rune，不切断多字节字符)
	long := strings.Repeat("中", 10_050)
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte(long), 0o644))
	require.Equal(t, strings.Repeat("中", 10_000), readAgentsMd(cwd))
}

func TestContextBuildSystemPrompt_Default(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()

	parts := BuildSystemPrompt(context.Background(), cwd)
	require.Len(t, parts, 1) // 只有核心指令，无 AGENTS.md / memory / skills

	prompt := parts[0]
	require.Contains(t, prompt, "You are an AI coding assistant")
	require.Contains(t, prompt, "## Tools")
	require.Contains(t, prompt, "- bash: run shell commands")
	require.Contains(t, prompt, "- Working directory: "+cwd)
	require.Contains(t, prompt, "- Platform: "+runtime.GOOS)
	require.Contains(t, prompt, "- Date: "+time.Now().UTC().Format("2006-01-02"))
	require.NotContains(t, prompt, "## Git Status") // tmpdir 非 git 仓库
	require.NotContains(t, prompt, "## Project Instructions")
	require.NotContains(t, prompt, "## Available Skills")
	require.NotContains(t, prompt, "## Recalled Memories")
}

func TestContextBuildSystemPrompt_AllParts(t *testing.T) {
	forceDefaultEnv(t)
	ctx := context.Background()

	// git 仓库 + AGENTS.md + skills 全部放在同一目录
	repo := initGitRepo(t, false)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Keep it simple."), 0o644))
	repoSkills := GetSkillsDir(repo)
	require.NoError(t, os.MkdirAll(repoSkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoSkills, "review.md"), []byte(sampleSkill), 0o644))

	// Part 1 + Part 2: tools prompt (含 git + skills) + AGENTS.md
	parts := BuildSystemPrompt(ctx, repo)
	require.Len(t, parts, 2)
	require.Contains(t, parts[0], "## Git Status\nGit branch: test-branch")
	require.Contains(t, parts[0], "## Available Skills\nThe user can invoke these skills with /<name>:")
	require.Contains(t, parts[0], "- **/code-review**: Structured code review for bugs and tests")
	require.Equal(t, "## Project Instructions (AGENTS.md)\nKeep it simple.", parts[1])

	// AGENT_TOOLS=off: Part 1 退化为纯 skills 片段 (普通目录，无 git 段)
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte("Keep it simple."), 0o644))
	skillsDir := GetSkillsDir(cwd)
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "review.md"), []byte(sampleSkill), 0o644))

	t.Setenv("AGENT_TOOLS", "off")
	parts = BuildSystemPrompt(ctx, cwd)
	require.Len(t, parts, 2)
	require.True(t, strings.HasPrefix(parts[0], "\n## Available Skills\n"),
		"parts[0] should be skills-only prompt, got: %.60s", parts[0])
	require.NotContains(t, parts[0], "## Tools")
}

// AGENT_MEMORY=on 时: memory part (cached) + 召回片段 (per-turn)。
// 记忆文件 <= 5 个时 recall 直接短路返回全部候选，不经过 LLM，纯文件系统可测。
func TestContextBuildSystemPrompt_Memory(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()
	ctx := context.Background()

	require.NoError(t, UpdateMemoryIndex(cwd, "- [deploy](deploy.md): 部署流程"))
	_, err := WriteMemoryFile(cwd, "deploy", MemoryMeta{
		Name: "deploy", Description: "如何部署", Type: MemoryTypeProject,
	}, "staging 先行")
	require.NoError(t, err)

	// 无 userMessage: 不召回
	t.Setenv("AGENT_MEMORY", "on")
	parts := BuildSystemPrompt(ctx, cwd)
	require.Len(t, parts, 2) // tools prompt + memory part
	require.Contains(t, parts[1], "## Memory")
	require.Contains(t, parts[1], "### Memory files available:")

	// 有 userMessage: 追加召回片段 (per-turn)
	parts = BuildSystemPrompt(ctx, cwd, "怎么部署到生产？")
	require.Len(t, parts, 3)
	require.Contains(t, parts[2], "\n## Recalled Memories\n")
	require.Contains(t, parts[2], "### deploy (project)")
}
