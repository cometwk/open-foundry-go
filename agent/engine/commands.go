/**
 * 斜杠命令系统 — 对标 Claude Code commands.ts
 *
 * Round 11: 让 /cost 和 /resume 真正工作
 */
package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// CommandResult 命令执行结果 (Message/Data 为零值时对应 TS 的可选字段未设置)
type CommandResult struct {
	Handled bool
	Message string
	Action  string
	Data    map[string]any
}

// 命令 action 标识 (TS 是 "compact"|"clear"|"resume"|"cost" 联合类型，
// 其中 diff/mcp 是用 as 强转塞进去的，Go 直接用独立常量)
const (
	ActionCompact = "compact"
	ActionClear   = "clear"
	ActionResume  = "resume"
	ActionCost    = "cost"
	ActionDiff    = "diff"
	ActionMCP     = "mcp"
)

// CommandHandler 命令处理器 (TS 允许返回 Promise，此处全部为同步实现)
type CommandHandler func(args string) CommandResult

type commandEntry struct {
	description string
	handler     CommandHandler
}

// 包级注册表: 只在 init() 中写入，之后只读，无需加锁。
// commandOrder 记录注册顺序 (对标 JS Object.keys 的插入序)。
var (
	commands     = map[string]commandEntry{}
	commandOrder []string
)

func register(name, description string, handler CommandHandler) {
	if _, exists := commands[name]; !exists {
		commandOrder = append(commandOrder, name)
	}
	commands[name] = commandEntry{description: description, handler: handler}
}

// ── 内置命令 ──

func init() {
	register("help", "Show available commands", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Message: `**Available commands:**

- **/help** — Show this help
- **/compact** — Compress conversation history
- **/clear** — Clear conversation
- **/cost** — Show token usage and cost
- **/plan** — Read-only mode (safe exploration)
- **/auto** — Full mode (all tools, no confirmation)
- **/default** — Default mode (confirm dangerous ops)
- **/resume** — Resume a previous session

**Keyboard shortcuts:**
- **↑/↓** — Input history
- **Ctrl+C** — Stop streaming
- **Escape** — Clear input
- **/** — Command autocomplete`,
		}
	})

	register("compact", "Compress conversation history", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Action:  ActionCompact,
			Message: "⏳ Compacting conversation...",
		}
	})

	register("clear", "Clear conversation", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Action:  ActionClear,
			Message: "Conversation cleared.",
		}
	})

	register("cost", "Show token usage and cost", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Action:  ActionCost,
			// message 由调用方根据 agentStatus 填入
		}
	})

	register("resume", "Resume a previous session", func(args string) CommandResult {
		idx := -1
		if trimmed := strings.TrimSpace(args); trimmed != "" {
			if n, err := strconv.Atoi(trimmed); err == nil {
				idx = n // TS 的 parseInt 对非数字返回 NaN，这里按 -1 处理
			}
		}
		return CommandResult{
			Handled: true,
			Action:  ActionResume,
			Data:    map[string]any{"idx": idx},
		}
	})

	register("plan", "Enter plan mode (read-only tools)", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Message: "Switched to **plan mode** — only read-only tools (file_read, glob, grep).\nUse `/auto` to return to full mode.",
			Data:    map[string]any{"permissionMode": string(PermissionModePlan)},
		}
	})

	register("auto", "Enter auto mode (all tools, no confirmation)", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Message: "Switched to **auto mode** — all tools available, no confirmation needed.",
			Data:    map[string]any{"permissionMode": string(PermissionModeAuto)},
		}
	})

	register("default", "Enter default mode (confirm dangerous ops)", func(args string) CommandResult {
		return CommandResult{
			Handled: true,
			Message: "Switched to **default mode** — dangerous operations require confirmation.",
			Data:    map[string]any{"permissionMode": string(PermissionModeDefault)},
		}
	})

	register("diff", "Show all file changes in this session", func(args string) CommandResult {
		// 数据由调用方填入 (agentStatus 不含文件信息，用 action 标记)
		return CommandResult{Handled: true, Action: ActionDiff}
	})

	register("mcp", "Connect to an MCP server (usage: /mcp <url>)", func(args string) CommandResult {
		if args == "" {
			return CommandResult{
				Handled: true,
				Message: "Usage: `/mcp <sse-url>` — Connect to an MCP server\nExample: `/mcp http://localhost:3001/sse`",
			}
		}
		return CommandResult{
			Handled: true,
			Action:  ActionMCP,
			Data:    map[string]any{"mcpUrl": args},
			Message: fmt.Sprintf("Connecting to MCP server: `%s`...", args),
		}
	})
}

// ── 路由 ──

// IsCommand 判断输入是否为斜杠命令
func IsCommand(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "/")
}

// ExecuteCommand 解析并执行斜杠命令；非命令返回 {Handled: false}
func ExecuteCommand(input string) CommandResult {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return CommandResult{}
	}

	// 对标 trimmed.indexOf(" ", 1): 首个空格分隔命令名与参数
	spaceIdx := strings.Index(trimmed, " ")
	name := trimmed[1:]
	args := ""
	if spaceIdx != -1 {
		name = trimmed[1:spaceIdx]
		args = strings.TrimSpace(trimmed[spaceIdx+1:])
	}

	cmd, ok := commands[name]
	if !ok {
		return CommandResult{
			Handled: true,
			Message: fmt.Sprintf("Unknown command: `/%s`\nType `/help` for available commands.", name),
		}
	}
	return cmd.handler(args)
}

// GetCommandNames 返回已注册命令名 (按注册顺序，对标 Object.keys)
func GetCommandNames() []string {
	out := make([]string, len(commandOrder))
	copy(out, commandOrder)
	return out
}
