package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const sampleSkill = `---
name: code-review
description: Structured code review for bugs and tests
whenToUse: User asks to review a PR or diff
allowedTools: read_file, git_diff ,  edit_file
---
You are a code reviewer. Check for:
- bugs
- missing tests
`

func TestSkillsParse(t *testing.T) {
	s := parseSkill(sampleSkill, "/tmp/code-review.md")
	require.NotNil(t, s)
	require.Equal(t, "code-review", s.Name)
	require.Equal(t, "Structured code review for bugs and tests", s.Description)
	require.Equal(t, "User asks to review a PR or diff", s.WhenToUse)
	require.Equal(t, []string{"read_file", "git_diff", "edit_file"}, s.AllowedTools)
	require.Equal(t, "You are a code reviewer. Check for:\n- bugs\n- missing tests", s.Prompt)
	require.Equal(t, "/tmp/code-review.md", s.FilePath)

	// snake_case when_to_use 备选；allowedTools 缺省为空
	s = parseSkill("---\nname: x\ndescription: d\nwhen_to_use: whenever\n---\nbody", "/tmp/x.md")
	require.NotNil(t, s)
	require.Equal(t, "whenever", s.WhenToUse)
	require.Empty(t, s.AllowedTools)

	// 缺 name -> nil
	require.Nil(t, parseSkill("---\ndescription: no name\n---\nbody", "/tmp/x.md"))
	// 无 frontmatter -> nil
	require.Nil(t, parseSkill("plain markdown, no frontmatter", "/tmp/x.md"))
}

func TestSkillsLoadFromDir(t *testing.T) {
	dir := t.TempDir()
	require.Nil(t, LoadSkillsFromDir(filepath.Join(dir, "missing")))

	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	write("review.md", sampleSkill)
	write("deploy.md", "---\nname: deploy\ndescription: Deploy the service\n---\nDeploy steps...")
	write("notes.txt", "---\nname: notes\n---\nnot markdown")    // 非 .md 跳过
	write("broken.md", "---\ndescription: missing name\n---\nx") // 缺 name 跳过

	skills := LoadSkillsFromDir(dir)
	require.Len(t, skills, 2)
	names := []string{skills[0].Name, skills[1].Name}
	require.ElementsMatch(t, []string{"code-review", "deploy"}, names)
	// FilePath 指向来源文件 (文件名与 skill name 可以不同)
	files := map[string]string{"code-review": "review.md", "deploy": "deploy.md"}
	for _, s := range skills {
		require.Equal(t, filepath.Join(dir, files[s.Name]), s.FilePath)
	}
}

func TestSkillsLoadAll(t *testing.T) {
	cwd := t.TempDir()
	require.Empty(t, LoadAllSkills(cwd)) // 目录不存在

	skillsDir := GetSkillsDir(cwd)
	require.Equal(t, filepath.Join(cwd, ".agent", "skills"), skillsDir)
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "review.md"), []byte(sampleSkill), 0o644))

	skills := LoadAllSkills(cwd)
	require.Len(t, skills, 1)
	require.Equal(t, "code-review", skills[0].Name)
}

func TestSkillsFormatForPrompt(t *testing.T) {
	require.Empty(t, FormatSkillsForPrompt(nil))

	skills := []Skill{
		{Name: "code-review", Description: "Review code", WhenToUse: "reviewing a PR"},
		{Name: "deploy", Description: "Deploy service"},
	}
	require.Equal(t,
		"\n## Available Skills\nThe user can invoke these skills with /<name>:\n"+
			"- **/code-review**: Review code — Use when: reviewing a PR\n"+
			"- **/deploy**: Deploy service",
		FormatSkillsForPrompt(skills))
}

func TestSkillsFindSkill(t *testing.T) {
	skills := []Skill{{Name: "code-review"}, {Name: "deploy"}}

	require.NotNil(t, FindSkill(skills, "code-review")) // 不带斜杠
	require.NotNil(t, FindSkill(skills, "/deploy"))     // 带前导斜杠
	require.Equal(t, "deploy", FindSkill(skills, "/deploy").Name)

	require.Nil(t, FindSkill(skills, "missing"))
	require.Nil(t, FindSkill(nil, "code-review"))
}
