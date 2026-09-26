package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/lib/env"
	"github.com/stretchr/testify/require"
)

// ── memory-extract ──

func TestExtractBuildTranscript(t *testing.T) {
	// 只取最近 10 条；短行 (<=10 字符) 被过滤；非 text part 不进文本
	msgs := make([]aisdk.UIMessage, 0, 12)
	for i := 0; i < 11; i++ {
		msgs = append(msgs, userMsg("m", "这是一个足够长的消息编号"+strings.Repeat("。", i+1)))
	}
	short := userMsg("short", "hi") // "Human: hi" 长度 <= 10，被过滤
	tool := assistantMsg("tool",
		aisdk.TextPart{Text: "调一下工具"},
		aisdk.ToolInvocationPart{ToolCallID: "c1", ToolName: "read_file", State: aisdk.ToolStateOutputAvailable},
	)
	msgs = append(msgs, short, tool)

	got := buildExtractTranscript(msgs)
	lines := strings.Split(got, "\n\n")
	// 13 条消息取最近 10 条 (8 长 + short + tool)，short 被过滤 -> 9 行
	require.Len(t, lines, 9)
	require.NotContains(t, got, "Human: hi")
	require.Equal(t, "Assistant: 调一下工具", lines[len(lines)-1])

	// 空消息
	require.Empty(t, buildExtractTranscript(nil))
}

func TestExtractParseExtractedMemories(t *testing.T) {
	// NONE / 无 FILENAME
	require.Nil(t, parseExtractedMemories("NONE"))
	require.Nil(t, parseExtractedMemories("some text without fields"))

	text := `---
FILENAME: user-prefers-go.md
NAME: User prefers Go
TYPE: user
DESCRIPTION: User is a Go developer
CONTENT:
User is a senior Go developer and prefers terse responses.
---
---
FILENAME: deploy-freeze.md
NAME: Deploy freeze
TYPE: project
DESCRIPTION: Deploy freeze until March 5
CONTENT:
Deploy freeze in effect until March 5.
---
---
FILENAME: broken.md
NAME: Missing fields
CONTENT:
missing type and description
---`

	memories := parseExtractedMemories(text)
	require.Len(t, memories, 2)

	require.Equal(t, "user-prefers-go.md", memories[0].Filename)
	require.Equal(t, "User prefers Go", memories[0].Name)
	require.Equal(t, MemoryTypeUser, memories[0].Type)
	require.Equal(t, "User is a senior Go developer and prefers terse responses.", memories[0].Content)

	require.Equal(t, "deploy-freeze.md", memories[1].Filename)
	require.Equal(t, MemoryTypeProject, memories[1].Type)
}

func TestExtractSaveExtractedMemories(t *testing.T) {
	cwd := t.TempDir()
	saved := SaveExtractedMemories([]ExtractedMemory{
		{
			Filename:    "user-prefers-go.md",
			Name:        "User prefers Go",
			Description: "User is a Go developer",
			Type:        MemoryTypeUser,
			Content:     "Senior Go developer.",
		},
		{
			Filename:    "deploy-freeze.md",
			Name:        "Deploy freeze",
			Description: "No deploys in March",
			Type:        MemoryTypeProject,
			Content:     "Deploy freeze until March 5.",
		},
	}, cwd)
	require.Equal(t, 2, saved)

	// 文件 + 索引都已写入
	f := ReadMemoryFile(filepath.Join(cwd, ".agent", "memory", "user-prefers-go.md"))
	require.NotNil(t, f)
	require.Equal(t, "User prefers Go", f.Name)

	index, ok := ReadMemoryIndex(cwd)
	require.True(t, ok)
	require.Equal(t,
		"- [User prefers Go](user-prefers-go.md) — User is a Go developer\n- [Deploy freeze](deploy-freeze.md) — No deploys in March\n",
		index)
}

func TestExtractMemories_TooShort(t *testing.T) {
	// 对话太短 (<100 字符) 直接返回，不调用 LLM
	msgs := []aisdk.UIMessage{userMsg("m1", "hi there")}
	require.Nil(t, ExtractMemories(context.Background(), msgs, t.TempDir()))
}

func TestExtractMemories_Integration(t *testing.T) {
	err := env.LoadEnv("")
	require.NoError(t, err)

	// 对话中包含明显的可记忆信息 (用户偏好)
	msgs := []aisdk.UIMessage{
		userMsg("m1", "以后的回复请全部使用中文，并且保持简洁，不要长篇大论解释。"),
		assistantMsg("m2", aisdk.TextPart{Text: "好的，我会用简洁的中文回复你。"}),
		userMsg("m3", "另外记住我们团队用 pnpm 管理依赖，不要用 npm，CI 里也是 pnpm。"),
		assistantMsg("m4", aisdk.TextPart{Text: "已了解，包管理器统一使用 pnpm。"}),
		userMsg("m5", "这个偏好我不用每次重复说吧？以后所有会话都记住它。"),
	}

	memories := ExtractMemories(context.Background(), msgs, t.TempDir())
	require.NotEmpty(t, memories, "应至少提取出一条记忆")
	for _, m := range memories {
		t.Logf("extracted: filename=%s type=%s name=%s", m.Filename, m.Type, m.Name)
		require.NotEmpty(t, m.Filename)
		require.NotEmpty(t, m.Name)
		require.NotEmpty(t, m.Description)
		require.NotEmpty(t, m.Content)
	}

	// 保存到磁盘
	cwd := t.TempDir()
	require.Equal(t, len(memories), SaveExtractedMemories(memories, cwd))
	index, ok := ReadMemoryIndex(cwd)
	require.True(t, ok)
	t.Logf("index:\n%s", index)
}

// ── memory-recall ──

func TestRecallParseIndices(t *testing.T) {
	// 正常逗号分隔 + 空白
	require.Equal(t, []int{0, 2}, parseRecallIndices("0, 2", 7))
	// 混入空白与文字只保留数字和逗号 (对应 TS 的 replace(/[^0-9,]/g, ''))
	require.Equal(t, []int{1, 3}, parseRecallIndices("Indices: 1, 3", 7))
	// " and " 会被整体剔除拼接成 "13"，越界后被过滤为空
	require.Empty(t, parseRecallIndices("indices: 1 and 3", 7))
	// 越界过滤；"-1" 剔除负号后变 "1"，在范围内会被保留 (与 TS 行为一致)
	require.Equal(t, []int{2, 1}, parseRecallIndices("2,7,9,-1", 5))
	// 最多 5 个
	require.Equal(t, []int{0, 1, 2, 3, 4}, parseRecallIndices("0,1,2,3,4,5,6", 7))
	// 全部非法
	require.Empty(t, parseRecallIndices("no digits here", 5))
}

func TestRecallFormatRecalledMemories(t *testing.T) {
	require.Empty(t, FormatRecalledMemories(nil))

	files := []MemoryFile{
		{MemoryHeader: MemoryHeader{Name: "Deploy freeze", Type: MemoryTypeProject}, Content: "Freeze until Mar 5."},
		{MemoryHeader: MemoryHeader{Name: "User prefers Go", Type: MemoryTypeUser}, Content: "Senior Go dev."},
	}
	require.Equal(t,
		"\n## Recalled Memories\n### Deploy freeze (project)\nFreeze until Mar 5.\n\n### User prefers Go (user)\nSenior Go dev.",
		FormatRecalledMemories(files))
}

func TestRecallShortPaths(t *testing.T) {
	// 无记忆目录 -> 空
	require.Empty(t, RecallRelevantMemories(context.Background(), "query", t.TempDir()))

	// 写 3 个记忆 (<= maxRecalled，不经过 LLM 直接全部返回)
	cwd := t.TempDir()
	for _, name := range []string{"go-style", "deploy", "pnpm"} {
		_, err := WriteMemoryFile(cwd, name, MemoryMeta{
			Name:        name,
			Description: "about " + name,
			Type:        MemoryTypeProject,
		}, "content of "+name)
		require.NoError(t, err)
	}

	files := RecallRelevantMemories(context.Background(), "how to deploy?", cwd)
	require.Len(t, files, 3)
	names := []string{files[0].Name, files[1].Name, files[2].Name}
	require.ElementsMatch(t, []string{"go-style", "deploy", "pnpm"}, names)
	for _, f := range files {
		// WriteMemoryFile 写入格式使读回的 body 带前导换行 (与 TS 一致)
		require.Equal(t, "\ncontent of "+f.Name, f.Content)
	}

	// 全部已注入 -> 空
	files = RecallRelevantMemories(context.Background(), "how to deploy?", cwd,
		"go-style.md", "deploy.md", "pnpm.md")
	require.Empty(t, files)
}

func TestRecallRelevantMemories_Integration(t *testing.T) {
	err := env.LoadEnv("")
	require.NoError(t, err)

	// 7 个候选 (> maxRecalled) 强制走 LLM 选择
	cwd := t.TempDir()
	topics := map[string]string{
		"deploy-flow":   "如何部署服务到 staging 与生产",
		"go-style":      "Go 代码风格与错误处理约定",
		"pnpm":          "包管理器统一使用 pnpm",
		"api-auth":      "API 鉴权使用内部 token 服务",
		"meeting-notes": "每周例会纪要存放位置",
		"oncall":        "值班排班与升级流程",
		"db-migrations": "数据库迁移走 golang-migrate",
	}
	for name, desc := range topics {
		_, err := WriteMemoryFile(cwd, name, MemoryMeta{
			Name:        name,
			Description: desc,
			Type:        MemoryTypeProject,
		}, "关于 "+desc+" 的详细内容")
		require.NoError(t, err)
	}

	files := RecallRelevantMemories(context.Background(), "我要发布新版本，怎么部署到生产环境？", cwd)
	require.NotEmpty(t, files, "应召回至少一条相关记忆")
	require.LessOrEqual(t, len(files), maxRecalled)
	for _, f := range files {
		t.Logf("recalled: %s (%s)", f.Name, f.Description)
		require.Contains(t, topics, f.Name)
	}

	// 召回结果可格式化为 prompt 片段
	prompt := FormatRecalledMemories(files)
	require.True(t, strings.HasPrefix(prompt, "\n## Recalled Memories\n"))
	require.Contains(t, prompt, "### deploy-flow (project)")

	// 已注入的记忆被排除
	files = RecallRelevantMemories(context.Background(), "怎么部署？", cwd, "deploy-flow.md", "go-style.md", "pnpm.md", "api-auth.md", "meeting-notes.md", "oncall.md", "db-migrations.md")
	require.Empty(t, files)
}
