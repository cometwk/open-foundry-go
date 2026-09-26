/**
 * Tool Registry — 对标 Claude Code tools.ts (TS 的 index.ts)
 *
 * Round 4: + AgentTool + WebFetchTool
 * 解决循环依赖: agent-tool 接收 subTools 参数而非 import assembleTools
 *
 * 暂缺工具 (TS 源文件未提供): glob / grep (search-tools.ts)、
 * web_fetch (web-tool.ts)、web_search (web-search-tool.ts)。
 */
package tools

import (
	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
)

// AssembleTools 组装全量工具集 — 对标 assembleToolPool()
//
// 设计: 先组装 base tools, 再将 base tools 传给 agent tool
// 这样子 agent 获得相同工具集 (不含 agent 自身, 防止无限嵌套)
func AssembleTools(ctx ToolContext) (aisdk.ToolSet, error) {
	tools := aisdk.ToolSet{}
	if engine.EnvConfig.AgentTools() {
		readOnlyTools := aisdk.ToolSet{}
		fileRead, err := CreateFileReadTool(ctx)
		if err != nil {
			return nil, err
		}
		readOnlyTools["file_read"] = fileRead
		// TODO: glob = CreateGlobTool(ctx); grep = CreateGrepTool(ctx) (search-tools.ts 未提供)

		if !ctx.AllowWrite && !ctx.AllowBash {
			return readOnlyTools, nil // 只读模式 (如 plan): 仅只读工具
		}

		// Base tools (子 agent 也会拥有这些)
		baseTools := aisdk.ToolSet{}
		for name, tool := range readOnlyTools {
			baseTools[name] = tool
		}
		bash, err := CreateBashTool(ctx)
		if err != nil {
			return nil, err
		}
		baseTools["bash"] = bash
		fileEdit, err := CreateFileEditTool(ctx)
		if err != nil {
			return nil, err
		}
		baseTools["file_edit"] = fileEdit
		fileWrite, err := CreateFileWriteTool(ctx)
		if err != nil {
			return nil, err
		}
		baseTools["file_write"] = fileWrite
		// TODO: web_fetch = CreateWebFetchTool(); web_search = CreateWebSearchTool() (源文件未提供)

		agentTool, err := CreateAgentTool(ctx.Cwd, baseTools)
		if err != nil {
			return nil, err
		}
		tools["agent"] = agentTool
		askUser, err := CreateAskUserTool()
		if err != nil {
			return nil, err
		}
		tools["ask_user"] = askUser
		skill, err := CreateSkillsTool(ctx)
		if err != nil {
			return nil, err
		}
		tools["read_skill"] = skill
	} else {
		askUser, err := CreateAskUserTool()
		if err != nil {
			return nil, err
		}
		tools["ask_user"] = askUser
		skill, err := CreateSkillsTool(ctx)
		if err != nil {
			return nil, err
		}
		tools["read_skill"] = skill
	}

	// 扩展工具: extra["createTools"] 存放 func(ToolExtra) aisdk.ToolSet
	if ctx.Extra != nil {
		if fn, ok := ctx.Extra["createTools"].(func(ToolExtra) aisdk.ToolSet); ok {
			for name, tool := range fn(ctx.Extra) {
				tools[name] = tool
			}
		}
	}
	return tools, nil
}
