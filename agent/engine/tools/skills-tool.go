// Read a skill's instructions from SKILL.md.
package tools

import (
	"context"
	"fmt"
	"log"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
)

// SkillsToolInput skills 工具输入
type SkillsToolInput struct {
	SkillID string `json:"skill_id" jsonschema:"description=Skill id or name"`
}

// CreateSkillsTool 创建 skills 读取工具 — 对标 createSkillsTool()。
//
// TS 的 tool() 无 name (由 ToolSet 的 key 决定，index.ts 中注册为 "read_skill")，
// Go 的 TypedToolDef 需要 Name，与之对齐。
// Execute 返回: 成功 -> skill 的完整 prompt (string)，未找到 -> {"error": ...}。
func CreateSkillsTool(tctx ToolContext) (aisdk.Tool, error) {
	if len(tctx.Skills) == 0 {
		log.Printf("[skills-tool] No skills found") // TS: console.error，不抛错
	}

	return aisdk.TypedTool(aisdk.TypedToolDef[SkillsToolInput, any]{
		Name:        "read_skill",
		Description: "Read a skill's instructions from SKILL.md.",
		Execute: func(ctx context.Context, input SkillsToolInput, _ aisdk.ToolExecutionOptions) (any, error) {
			skill := engine.FindSkill(tctx.Skills, input.SkillID)
			if skill == nil {
				return map[string]any{"error": fmt.Sprintf("Skill %s not found", input.SkillID)}, nil
			}
			return skill.Prompt, nil
		},
	})
}
