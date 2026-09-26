/**
 * Tool Registry — 对标 Claude Code tools.ts (TS 的 index.ts)
 *
 * Round 4: + AgentTool + WebFetchTool
 * 解决循环依赖: agent-tool 接收 subTools 参数而非 import assembleTools
 *
 * 暂缺工具 (TS 源文件未提供): glob / grep (search-tools.ts)、
 * web_fetch (web-tool.ts)、web_search (web-search-tool.ts)。
 *
 * 本文件另含 engine ↔ tools 的依赖倒置接线 (原 wire.go):
 * TS 中 engine/agent.ts 直接 import 本模块 (模块循环在 TS 中允许)，
 * Go 不允许包循环，通过 engine.ToolsAssembler 钩子注入 (InstallAssembleTools)。
 */
package tools

import (
	"context"
	"sync"

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

// AssembleToolsForAgent 为 engine.HandleMessage 组装工具集:
// 内部构建 ToolContext (含 AppState 的 getState/setState 与技能加载)，
// 对标 agent.ts 中 toolCtx 的组装逻辑。
func AssembleToolsForAgent(ctx context.Context, cwd string, mode engine.PermissionMode, extra map[string]any) (aisdk.ToolSet, error) {
	// appState: TS 单线程无需锁，Go 中工具执行可能并发，加互斥保护
	var stateMu sync.Mutex
	appState := CreateInitialState(cwd)

	skills := engine.LoadAllSkills(cwd)
	tctx := ToolContext{
		Cwd:            cwd,
		Ctx:            ctx, // TS 的 AbortController 未对外暴露，直接用请求 ctx
		AllowWrite:     mode != engine.PermissionModePlan,
		AllowBash:      mode != engine.PermissionModePlan,
		PermissionMode: mode,
		GetState: func() AppState {
			stateMu.Lock()
			defer stateMu.Unlock()
			return appState
		},
		SetState: func(fn func(AppState) AppState) {
			stateMu.Lock()
			defer stateMu.Unlock()
			appState = fn(appState)
		},
		Extra:  ToolExtra(extra),
		Skills: skills,
	}
	return AssembleTools(tctx)
}

// InstallAssembleTools 把组装函数注入 engine.ToolsAssembler (程序启动时调用一次)
func InstallAssembleTools() {
	engine.ToolsAssembler = AssembleToolsForAgent
}
