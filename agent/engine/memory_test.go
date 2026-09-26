package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// setMtime 显式设置文件修改时间，控制扫描排序
func setMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestMemoryGetMemoryDirAndEnsure(t *testing.T) {
	cwd := t.TempDir()
	require.Equal(t, filepath.Join(cwd, ".agent", "memory"), GetMemoryDir(cwd))

	dir, err := EnsureMemoryDir(cwd)
	require.NoError(t, err)
	require.Equal(t, GetMemoryDir(cwd), dir)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestMemoryParseFrontmatter(t *testing.T) {
	// 标准 frontmatter
	meta, body := parseFrontmatter("---\nname: git-conventions\ndescription: How we commit: conventional style\ntype: feedback\n---\nbody here")
	require.Equal(t, map[string]string{
		"name":        "git-conventions",
		"description": "How we commit: conventional style", // 值中的冒号保留
		"type":        "feedback",
	}, meta)
	require.Equal(t, "body here", body)

	// 无 frontmatter: 原文即 body
	meta, body = parseFrontmatter("just text\nmore")
	require.Empty(t, meta)
	require.Equal(t, "just text\nmore", body)

	// 只有开始分隔符: 不匹配
	meta, body = parseFrontmatter("---\nname: x\nno end")
	require.Empty(t, meta)
	require.Equal(t, "---\nname: x\nno end", body)

	// 无冒号的行被忽略
	meta, _ = parseFrontmatter("---\nname: x\nignored line\n---\n")
	require.Equal(t, map[string]string{"name": "x"}, meta)
}

func TestMemoryWriteAndReadFile(t *testing.T) {
	cwd := t.TempDir()

	// 文件名不带 .md 自动补全
	path, err := WriteMemoryFile(cwd, "git-conventions", MemoryMeta{
		Name:        "git-conventions",
		Description: "团队 git 提交规范",
		Type:        MemoryTypeFeedback,
	}, "Use conventional commits.")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cwd, ".agent", "memory", "git-conventions.md"), path)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "---\nname: git-conventions\ndescription: 团队 git 提交规范\ntype: feedback\n---\n\nUse conventional commits.", string(raw))

	f := ReadMemoryFile(path)
	require.NotNil(t, f)
	require.Equal(t, "git-conventions.md", f.Filename)
	require.Equal(t, "git-conventions", f.Name)
	require.Equal(t, "团队 git 提交规范", f.Description)
	require.Equal(t, MemoryTypeFeedback, f.Type)
	// 写入格式是 frontmatter + "\n" + content，而 frontmatter 解析只吃掉一个换行，
	// 所以 body 以 \n 开头 (与 TS 正则 \n---\n? 行为一致)
	require.Equal(t, "\nUse conventional commits.", f.Content)

	// 不存在的文件返回 nil
	require.Nil(t, ReadMemoryFile(filepath.Join(cwd, "nope.md")))
}

func TestMemoryScanFiles(t *testing.T) {
	cwd := t.TempDir()

	write := func(name, content string, mtime time.Time) string {
		t.Helper()
		path, err := WriteMemoryFile(cwd, name, MemoryMeta{
			Name:        name,
			Description: "desc of " + name,
			Type:        MemoryTypeReference,
		}, content)
		require.NoError(t, err)
		setMtime(t, path, mtime)
		return path
	}

	base := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	write("old", "old content", base)
	write("new", "new content", base.Add(2*time.Hour))

	// MEMORY.md 和非 .md 文件应被跳过
	_, err := WriteMemoryFile(cwd, "idx-note", MemoryMeta{Name: "idx", Description: "", Type: MemoryTypeProject}, "x")
	require.NoError(t, err)
	require.NoError(t, os.Rename(filepath.Join(cwd, ".agent", "memory", "idx-note.md"), filepath.Join(cwd, ".agent", "memory", "MEMORY.md")))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".agent", "memory", "notes.txt"), []byte("skip me"), 0o644))

	headers := ScanMemoryFiles(cwd)
	require.Len(t, headers, 2)
	// 按修改时间倒序: new 在前
	require.Equal(t, "new.md", headers[0].Filename)
	require.Equal(t, "old.md", headers[1].Filename)
	require.Equal(t, MemoryTypeReference, headers[0].Type)
	require.Equal(t, "desc of new", headers[0].Description)
	// 文件系统返回本地时区的 time.Time，按时刻比较
	require.True(t, headers[0].ModTime.Equal(base.Add(2*time.Hour)))

	// 目录不存在返回空
	require.Empty(t, ScanMemoryFiles(t.TempDir()))
}

func TestMemoryScanFilesDefaults(t *testing.T) {
	cwd := t.TempDir()
	dir, err := EnsureMemoryDir(cwd)
	require.NoError(t, err)
	// 手写无 frontmatter 的文件: name 取文件名, type 默认 project
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plain.md"), []byte("# notes\n"), 0o644))

	headers := ScanMemoryFiles(cwd)
	require.Len(t, headers, 1)
	require.Equal(t, "plain", headers[0].Name)
	require.Equal(t, MemoryTypeProject, headers[0].Type)
	require.Empty(t, headers[0].Description)
}

func TestMemoryReadIndexTruncation(t *testing.T) {
	cwd := t.TempDir()

	// 不存在 -> false
	_, ok := ReadMemoryIndex(cwd)
	require.False(t, ok)

	dir, err := EnsureMemoryDir(cwd)
	require.NoError(t, err)
	indexPath := filepath.Join(dir, memoryIndex)

	// 按行截断: > 200 行只保留前 200 行
	lines := make([]string, 205)
	for i := range lines {
		lines[i] = "line-" + strings.Repeat("x", 10) + "-" + strconv.Itoa(i)
	}
	require.NoError(t, os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")), 0o644))
	content, ok := ReadMemoryIndex(cwd)
	require.True(t, ok)
	require.Equal(t, strings.Join(lines[:200], "\n"), content)

	// 按字节截断: 每行 200 字节 x 210 行，join 后行步长 201 (200 + 分隔符)
	// 行截断后 200 行 = 40199 字节 > 25KB，再按 25KB 内最后一个换行截断
	long := make([]string, 210)
	for i := range long {
		long[i] = strings.Repeat("x", 199) + strconv.Itoa(i%10) // 200 bytes
	}
	require.NoError(t, os.WriteFile(indexPath, []byte(strings.Join(long, "\n")), 0o644))
	content, ok = ReadMemoryIndex(cwd)
	require.True(t, ok)
	// 换行位于 i*201+200; 25000 内最后一个换行是 123*201+200 = 24923
	require.Len(t, content, 24923)
	require.Equal(t, 123, strings.Count(content, "\n"))
}

func TestMemoryUpdateIndex(t *testing.T) {
	cwd := t.TempDir()

	require.NoError(t, UpdateMemoryIndex(cwd, "- [deploy](deploy.md): 部署流程"))

	content, ok := ReadMemoryIndex(cwd)
	require.True(t, ok)
	require.Equal(t, "- [deploy](deploy.md): 部署流程\n", content)

	// 追加 + 去重
	require.NoError(t, UpdateMemoryIndex(cwd, "- [api](api.md): API 约定"))
	require.NoError(t, UpdateMemoryIndex(cwd, "- [deploy](deploy.md): 部署流程")) // 已存在，跳过

	content, _ = ReadMemoryIndex(cwd)
	require.Equal(t, "- [deploy](deploy.md): 部署流程\n- [api](api.md): API 约定\n", content)
}

func TestMemoryBuildPromptPart(t *testing.T) {
	cwd := t.TempDir()

	// 默认关闭 (AGENT_MEMORY != on)
	t.Setenv("AGENT_MEMORY", "")
	_, ok := BuildMemoryPromptPart(cwd)
	require.False(t, ok)

	// 开启但无索引 -> false
	t.Setenv("AGENT_MEMORY", "on")
	_, ok = BuildMemoryPromptPart(cwd)
	require.False(t, ok)

	// 写入索引 + 一个记忆文件
	require.NoError(t, UpdateMemoryIndex(cwd, "- [deploy](deploy.md): 部署流程"))
	path, err := WriteMemoryFile(cwd, "deploy", MemoryMeta{
		Name:        "deploy",
		Description: "如何部署服务",
		Type:        MemoryTypeProject,
	}, "staging 先行")
	require.NoError(t, err)
	setMtime(t, path, time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))

	prompt, ok := BuildMemoryPromptPart(cwd)
	require.True(t, ok)
	// index 内容自带尾换行，模板的 ${index}\n\n 会多出一个空行 (与 TS 行为一致)
	require.Equal(t, "## Memory\n\n"+
		"- [deploy](deploy.md): 部署流程\n"+
		"\n"+
		"\n"+
		"### Memory files available:\n"+
		"[project] deploy.md (2026-09-25): 如何部署服务\n"+
		"\n"+
		"You can read memory files with file_read and write new ones with file_write in the .agent/memory/ directory.",
		prompt)

	// 无主题文件时不输出 manifest 段
	cwd2 := t.TempDir()
	require.NoError(t, UpdateMemoryIndex(cwd2, "only index"))
	prompt, ok = BuildMemoryPromptPart(cwd2)
	require.True(t, ok)
	require.Equal(t, "## Memory\n\nonly index\n\n\n\n\nYou can read memory files with file_read and write new ones with file_write in the .agent/memory/ directory.", prompt)
}
