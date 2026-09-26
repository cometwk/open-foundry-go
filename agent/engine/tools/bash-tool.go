/**
 * BashTool — Shell 命令执行
 *
 * 对标 Claude Code BashTool:
 *   - 执行 shell 命令
 *   - 超时控制
 *   - 输出截断
 *   - 安全检查 (危险命令拦截)
 */
package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
)

const (
	maxOutputChars   = 100_000
	defaultTimeoutMS = 120_000
	maxBufferBytes   = 10 * 1024 * 1024 // 10MB，对标 TS exec 的 maxBuffer
)

// blockedPatterns 永远阻止的命令
var blockedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`:\(\)\s*\{\s*:\|:\s*&\s*\}`), // fork bomb
	regexp.MustCompile(`mkfs\.`),
	regexp.MustCompile(`dd\s+if=`),
	regexp.MustCompile(`>\s*/dev/sd`),
}

// needsConfirmPatterns default 模式下需要确认的命令 (CC 的 classifier 对标)
var needsConfirmPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rm\s+-rf`),
	regexp.MustCompile(`git\s+push`),
	regexp.MustCompile(`git\s+reset\s+--hard`),
	regexp.MustCompile(`git\s+checkout\s+--`),
	regexp.MustCompile(`git\s+clean\s+-f`),
	regexp.MustCompile(`npm\s+publish`),
	regexp.MustCompile(`(?i)DROP\s+TABLE`),
	regexp.MustCompile(`(?i)DELETE\s+FROM`),
	regexp.MustCompile(`curl\s.*\|\s*(bash|sh)`),
}

// isBlocked 命中永久拦截时返回 "Permanently blocked: <pattern>"，否则空串
// (TS 输出 RegExp 的字符串形式 "/pattern/"，Go 输出 pattern 源串)
func isBlocked(command string) string {
	for _, pattern := range blockedPatterns {
		if pattern.MatchString(command) {
			return "Permanently blocked: " + pattern.String()
		}
	}
	return ""
}

// needsConfirmation 是否需要用户确认
func needsConfirmation(command string) bool {
	for _, p := range needsConfirmPatterns {
		if p.MatchString(command) {
			return true
		}
	}
	return false
}

// truncateOutput 截断输出: 超过上限时保留首尾各一半，中间标注截断字符数
func truncateOutput(output string) string {
	if len(output) <= maxOutputChars {
		return output
	}
	half := maxOutputChars / 2
	truncated := len(output) - maxOutputChars
	return output[:half] +
		fmt.Sprintf("\n\n... [truncated %d chars] ...\n\n", truncated) +
		output[len(output)-half:]
}

// truncateRunes 按字符 (rune) 截断，避免切断多字节字符 (engine 包有同名私有实现)
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// cappedBuffer 限制容量的输出缓冲，对标 TS exec 的 maxBuffer (10MB)。
// 差异: TS 超限会 kill 进程并报错，Go 简化为丢弃超限部分，避免 OOM。
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.max - b.buf.Len()
	if remaining <= 0 {
		return len(p), nil // 丢弃
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string { return b.buf.String() }

// BashToolInput bash 工具输入
type BashToolInput struct {
	Command string `json:"command" jsonschema:"description=The shell command to execute"`
	Timeout int64  `json:"timeout,omitempty" jsonschema:"maximum=600000,description=Timeout in ms (max 600000, default 120000)"`
}

// CreateBashTool 创建 shell 执行工具 — 对标 createBashTool()
func CreateBashTool(tctx ToolContext) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[BashToolInput, any]{
		Name: "bash",
		Description: "Execute a shell command and return its output. " +
			"Use for running scripts, git commands, builds, tests, etc. " +
			"Commands run in the project's working directory.",
		Execute: func(ctx context.Context, input BashToolInput, _ aisdk.ToolExecutionOptions) (any, error) {
			// 永远阻止的命令
			if blocked := isBlocked(input.Command); blocked != "" {
				return map[string]any{"error": blocked}, nil
			}

			if !tctx.AllowBash {
				return map[string]any{"error": "Bash execution is not allowed in current permission mode"}, nil
			}

			// default 模式: 危险命令返回确认请求 (对标 CC PermissionDialog)
			if tctx.PermissionMode == engine.PermissionModeDefault && needsConfirmation(input.Command) {
				return map[string]any{
					"operation": "needs_permission",
					"tool":      "bash",
					"command":   input.Command,
					"reason":    "This command may be destructive. Please confirm.",
					"summary":   "⚠️ Permission needed: `" + truncateRunes(input.Command, 60) + "`",
				}, nil
			}

			timeoutMS := input.Timeout
			if timeoutMS <= 0 {
				timeoutMS = defaultTimeoutMS // timeout ?? DEFAULT_TIMEOUT_MS
			}

			// 取消来源: ToolContext 的中止上下文 (对标 TS abortController.signal)
			base := tctx.Ctx
			if base == nil {
				base = ctx
			}
			runCtx, cancel := context.WithTimeout(base, time.Duration(timeoutMS)*time.Millisecond)
			defer cancel()

			// TS 的 exec 走 /bin/sh -c
			cmd := exec.CommandContext(runCtx, "sh", "-c", input.Command)
			cmd.Dir = tctx.Cwd
			stdout := &cappedBuffer{max: maxBufferBytes}
			stderr := &cappedBuffer{max: maxBufferBytes}
			cmd.Stdout = stdout
			cmd.Stderr = stderr

			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					// 对应 TS 的 error 事件 (spawn 失败等)
					return map[string]any{"error": err.Error()}, nil
				}
				// 非零退出码: 视为正常结束，走 close 分支
			}

			// 信号终止 (含超时 kill) 时 ExitCode 为 -1，对应 TS 的 code ?? 0
			exitCode := 0
			if cmd.ProcessState != nil {
				if c := cmd.ProcessState.ExitCode(); c >= 0 {
					exitCode = c
				}
			}

			return map[string]any{
				"stdout":   truncateOutput(stdout.String()),
				"stderr":   truncateOutput(stderr.String()),
				"exitCode": exitCode,
			}, nil
		},
	})
}
