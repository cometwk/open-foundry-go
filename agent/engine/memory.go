/**
 * Memory System — 对标 Claude Code memdir/
 *
 * Claude Code 记忆架构:
 *   - 目录: ~/.claude/projects/{slug}/memory/
 *   - 索引: MEMORY.md (max 200 lines, 25KB)
 *   - 主题文件: {topic}.md (YAML frontmatter + markdown body)
 *   - 4 种类型: user, feedback, project, reference
 *   - 注入: MEMORY.md 内容注入 system prompt
 *   - 扫描: scanMemoryFiles() 解析 frontmatter, 200 newest first
 *
 * 简化版实现:
 *   - 同样的目录结构和文件格式
 *   - 读取 MEMORY.md + 扫描主题文件
 *   - 注入 system prompt (直接返回 system prompt string)
 *   - 提供读写 API 给 tools 使用
 */
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── 路径解析 ──

const (
	memoryDirName = "memory"
	memoryIndex   = "MEMORY.md"
	maxIndexLines = 200
	maxIndexBytes = 25_000
)

// GetMemoryDir 获取项目记忆目录 — 对标 getAutoMemPath()
func GetMemoryDir(cwd string) string {
	// 简化版: 直接在项目目录下 .agent/memory/
	return filepath.Join(cwd, ".agent", memoryDirName)
}

// EnsureMemoryDir 确保记忆目录存在 — 对标 ensureMemoryDirExists()
func EnsureMemoryDir(cwd string) (string, error) {
	dir := GetMemoryDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ── Memory 类型 ──

// MemoryType 记忆类型
type MemoryType string

const (
	MemoryTypeUser      MemoryType = "user"
	MemoryTypeFeedback  MemoryType = "feedback"
	MemoryTypeProject   MemoryType = "project"
	MemoryTypeReference MemoryType = "reference"
)

// parseMemoryType 空值默认 project (对应 TS 的 `?? "project"`)
func parseMemoryType(s string) MemoryType {
	if s == "" {
		return MemoryTypeProject
	}
	return MemoryType(s)
}

// MemoryHeader 记忆文件头 (frontmatter 元信息)
type MemoryHeader struct {
	Filename    string
	FilePath    string
	Name        string
	Description string
	Type        MemoryType
	ModTime     time.Time
}

// MemoryFile 记忆文件 (头 + 正文)
type MemoryFile struct {
	MemoryHeader
	Content string
}

// MemoryMeta 写入记忆文件时的元信息
type MemoryMeta struct {
	Name        string
	Description string
	Type        MemoryType
}

// ── Frontmatter 解析 ──

// parseFrontmatter 解析 YAML frontmatter，对应 TS 正则 /^---\n([\s\S]*?)\n---\n?([\s\S]*)$/
func parseFrontmatter(raw string) (meta map[string]string, body string) {
	meta = map[string]string{}
	const delim = "---\n"
	if !strings.HasPrefix(raw, delim) {
		return meta, raw
	}
	rest := raw[len(delim):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, raw
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 {
			key := strings.TrimSpace(line[:colonIdx])
			val := strings.TrimSpace(line[colonIdx+1:])
			meta[key] = val
		}
	}
	// 结束分隔符 "\n---" 后至多吃掉一个换行 (对应正则的 \n?)
	body = strings.TrimPrefix(rest[end+len("\n---"):], "\n")
	return meta, body
}

// buildFrontmatter 生成 YAML frontmatter (key 顺序固定，保证输出稳定)
func buildFrontmatter(meta map[string]string) string {
	keys := []string{"name", "description", "type"}
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		if v, ok := meta[k]; ok {
			lines = append(lines, k+": "+v)
		}
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n"
}

// firstLines 取前 n 行
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// ── 扫描 — 对标 scanMemoryFiles() ──

// ScanMemoryFiles 扫描记忆文件头，按修改时间倒序，最多 200 个；目录不存在返回空
func ScanMemoryFiles(cwd string) []MemoryHeader {
	dir := GetMemoryDir(cwd)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	headers := make([]MemoryHeader, 0, len(entries))
	for _, entry := range entries {
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".md") || filename == memoryIndex {
			continue
		}

		filePath := filepath.Join(dir, filename)
		info, err := entry.Info()
		if err != nil {
			continue // skip unreadable files
		}
		raw, err := os.ReadFile(filePath)
		if err != nil {
			continue // skip unreadable files
		}

		// 只读前 30 行 frontmatter (对标 Claude Code)
		meta, _ := parseFrontmatter(firstLines(string(raw), 30))

		name := strings.TrimSuffix(filename, ".md")
		if v, ok := meta["name"]; ok {
			name = v
		}
		headers = append(headers, MemoryHeader{
			Filename:    filename,
			FilePath:    filePath,
			Name:        name,
			Description: meta["description"],
			Type:        parseMemoryType(meta["type"]),
			ModTime:     info.ModTime(),
		})
	}

	// 按修改时间倒序，最多 200 个
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].ModTime.After(headers[j].ModTime)
	})
	if len(headers) > 200 {
		headers = headers[:200]
	}
	return headers
}

// ── 读取 MEMORY.md 索引 ──

// ReadMemoryIndex 读取 MEMORY.md 索引 (带截断)，不存在返回 false
func ReadMemoryIndex(cwd string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(GetMemoryDir(cwd), memoryIndex))
	if err != nil {
		return "", false
	}
	content := string(raw)

	// 截断: 先按行，再按字节 (对标 Claude Code)
	lines := strings.Split(content, "\n")
	if len(lines) > maxIndexLines {
		content = strings.Join(lines[:maxIndexLines], "\n")
	}
	if len(content) > maxIndexBytes {
		// 对标 content.slice(0, content.lastIndexOf("\n", MAX_INDEX_BYTES))
		cut := strings.LastIndexByte(content[:maxIndexBytes+1], '\n')
		if cut >= 0 {
			content = content[:cut]
		} else {
			content = content[:len(content)-1] // lastIndexOf 为 -1 时 slice(0,-1)
		}
	}
	return content, true
}

// ── 读取单个记忆文件 ──

// ReadMemoryFile 读取单个记忆文件，失败返回 nil
func ReadMemoryFile(filePath string) *MemoryFile {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil
	}
	meta, body := parseFrontmatter(string(raw))
	return &MemoryFile{
		MemoryHeader: MemoryHeader{
			Filename:    filepath.Base(filePath),
			FilePath:    filePath,
			Name:        meta["name"],
			Description: meta["description"],
			Type:        parseMemoryType(meta["type"]),
			ModTime:     info.ModTime(),
		},
		Content: body,
	}
}

// ── 写入记忆文件 ──

// WriteMemoryFile 写入记忆文件 (frontmatter + 正文)，返回文件路径
func WriteMemoryFile(cwd, filename string, meta MemoryMeta, content string) (string, error) {
	dir, err := EnsureMemoryDir(cwd)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(filename, ".md") {
		filename += ".md"
	}
	filePath := filepath.Join(dir, filename)
	frontmatter := buildFrontmatter(map[string]string{
		"name":        meta.Name,
		"description": meta.Description,
		"type":        string(meta.Type),
	})
	if err := os.WriteFile(filePath, []byte(frontmatter+"\n"+content), 0o644); err != nil {
		return "", err
	}
	return filePath, nil
}

// ── 更新 MEMORY.md 索引 ──

// UpdateMemoryIndex 追加一条索引 (已存在则跳过)
func UpdateMemoryIndex(cwd, entry string) error {
	dir, err := EnsureMemoryDir(cwd)
	if err != nil {
		return err
	}
	indexPath := filepath.Join(dir, memoryIndex)

	existing, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// 文件不存在时 existing 为空

	// 避免重复
	if strings.Contains(string(existing), entry) {
		return nil
	}

	var newContent string
	if len(existing) > 0 {
		newContent = strings.TrimRight(string(existing), " \t\r\n") + "\n" + entry + "\n" // trimEnd
	} else {
		newContent = entry + "\n"
	}
	return os.WriteFile(indexPath, []byte(newContent), 0o644)
}

// ── 构建记忆 system prompt 片段 — 对标 buildMemoryPrompt() ──

// BuildMemoryPromptPart 构建记忆 system prompt 片段 (system prompt string)，
// 未开启 AGENT_MEMORY 或无索引时返回 false
func BuildMemoryPromptPart(cwd string) (string, bool) {
	if !EnvConfig.AgentMemory() {
		return "", false
	}

	index, ok := ReadMemoryIndex(cwd)
	if !ok {
		return "", false
	}

	headers := ScanMemoryFiles(cwd)
	manifests := make([]string, 0, len(headers))
	for _, h := range headers {
		manifests = append(manifests, fmt.Sprintf("[%s] %s (%s): %s",
			h.Type, h.Filename, h.ModTime.UTC().Format("2006-01-02"), h.Description))
	}
	manifest := strings.Join(manifests, "\n")

	prompt := "## Memory\n\n" + index + "\n\n"
	if manifest != "" {
		prompt += "### Memory files available:\n" + manifest
	}
	prompt += "\n\nYou can read memory files with file_read and write new ones with file_write in the .agent/memory/ directory."
	return prompt, true
}
