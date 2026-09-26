/**
 * Skill 系统 — 对标 Claude Code skills/
 *
 * Claude Code Skill 架构:
 *   - Bundled: TypeScript objects + getPromptForCommand()
 *   - Disk: Markdown + frontmatter (name, description, whenToUse, tools)
 *   - 加载: bundledSkills.ts + loadSkillsDir.ts
 *   - 调用: /skillname → 注入 prompt + 限制 tools
 *
 * 我们的实现:
 *   - 统一 Markdown frontmatter 格式
 *   - 从 .agent/skills/ 目录加载
 *   - 注入 system prompt (skill 描述) + 按需加载完整 prompt
 */
package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// Skill 一个可调用的 skill (Markdown frontmatter + prompt 正文)
type Skill struct {
	Name         string
	Description  string
	WhenToUse    string
	AllowedTools []string // AllowedTools 允许的工具列表 (空 = 全部)
	Prompt       string   // Prompt 完整 prompt 内容
	FilePath     string   // FilePath 来源文件路径
}

// parseSkill 从 frontmatter markdown 解析 skill；
// 无 frontmatter 或缺 name 时返回 nil
// (frontmatter 解析复用 memory.go 的 parseFrontmatter，与 TS 正则语义一致)
func parseSkill(raw, filePath string) *Skill {
	meta, body := parseFrontmatter(raw)
	name := meta["name"]
	if name == "" {
		return nil
	}

	whenToUse := meta["whenToUse"]
	if whenToUse == "" {
		whenToUse = meta["when_to_use"] // snake_case 备选
	}

	allowedTools := []string{} // 空 = 全部
	if v := meta["allowedTools"]; v != "" {
		for _, part := range strings.Split(v, ",") {
			allowedTools = append(allowedTools, strings.TrimSpace(part))
		}
	}

	return &Skill{
		Name:         name,
		Description:  meta["description"],
		WhenToUse:    whenToUse,
		AllowedTools: allowedTools,
		Prompt:       strings.TrimSpace(body),
		FilePath:     filePath,
	}
}

// LoadSkillsFromDir 从目录加载所有 skills — 对标 loadSkillsDir()；
// 目录不存在或文件不可读/不合法时跳过
func LoadSkillsFromDir(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var skills []Skill
	for _, entry := range entries {
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".md") {
			continue
		}
		filePath := filepath.Join(dir, filename)
		raw, err := os.ReadFile(filePath)
		if err != nil {
			continue // skip unreadable
		}
		if skill := parseSkill(string(raw), filePath); skill != nil {
			skills = append(skills, *skill)
		}
	}
	return skills
}

// GetSkillsDir 获取项目 skills 目录
func GetSkillsDir(cwd string) string {
	return filepath.Join(cwd, ".agent", "skills")
}

// LoadAllSkills 加载项目 + 全局 skills
func LoadAllSkills(cwd string) []Skill {
	projectSkills := LoadSkillsFromDir(GetSkillsDir(cwd))
	// 未来可加载 ~/.agent/skills/ 全局 skills
	return projectSkills
}

// FormatSkillsForPrompt 格式化 skills 为 system prompt 片段
func FormatSkillsForPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	lines := make([]string, 0, len(skills))
	for _, s := range skills {
		line := "- **/" + s.Name + "**: " + s.Description
		if s.WhenToUse != "" {
			line += " — Use when: " + s.WhenToUse
		}
		lines = append(lines, line)
	}

	return "\n## Available Skills\nThe user can invoke these skills with /<name>:\n" + strings.Join(lines, "\n")
}

// FindSkill 根据名称查找 skill，支持带或不带前导 "/"
func FindSkill(skills []Skill, name string) *Skill {
	trimmed := strings.TrimPrefix(name, "/")
	for i := range skills {
		if skills[i].Name == name || skills[i].Name == trimmed {
			return &skills[i]
		}
	}
	return nil
}
