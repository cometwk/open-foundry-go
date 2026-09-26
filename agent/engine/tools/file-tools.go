/**
 * File Tools — 文件读写编辑
 *
 * 对标 Claude Code FileReadTool + FileEditTool + FileWriteTool:
 *   - FileRead: 读取文件内容 (带行号, 支持 offset/limit)
 *   - FileEdit: 字符串替换编辑 (old_string → new_string)
 *   - FileWrite: 创建/覆写文件
 */
package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/engine"
)

// resolvePath 相对路径基于 cwd 解析为绝对路径
func resolvePath(filePath, cwd string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(cwd, filePath)
}

// imageExts / binaryExts 图片/二进制文件扩展名
var imageExts = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".svg": {}, ".ico": {}, ".bmp": {},
}

var binaryExts = map[string]struct{}{
	".pdf": {}, ".zip": {}, ".tar": {}, ".gz": {}, ".woff": {}, ".woff2": {}, ".ttf": {}, ".eot": {}, ".mp3": {}, ".mp4": {},
}

// FileReadInput file_read 工具输入 (offset/limit 用指针区分 "未传" 与 0)
type FileReadInput struct {
	FilePath string `json:"file_path" jsonschema:"description=Absolute or relative file path"`
	Offset   *int   `json:"offset,omitempty" jsonschema:"minimum=0,description=Line number to start from (0-based)"`
	Limit    *int   `json:"limit,omitempty" jsonschema:"minimum=1,description=Max lines to read (default: 2000)"`
}

// CreateFileReadTool 创建文件读取工具 — 对标 createFileReadTool()
func CreateFileReadTool(tctx ToolContext) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[FileReadInput, any]{
		Name: "file_read",
		Description: "Read a file's contents. Returns text with line numbers, images as base64, or binary file info. " +
			"Use offset and limit for large text files.",
		Execute: func(ctx context.Context, input FileReadInput, _ aisdk.ToolExecutionOptions) (any, error) {
			resolved := resolvePath(input.FilePath, tctx.Cwd)
			ext := strings.ToLower(filepath.Ext(resolved))

			readErr := func(err error) (any, error) {
				return map[string]any{"error": fmt.Sprintf("Failed to read %s: %v", resolved, err)}, nil
			}

			info, err := os.Stat(resolved)
			if err != nil {
				return readErr(err)
			}

			// 图片: 返回 base64 (对标 CC 的 image 支持)
			if _, ok := imageExts[ext]; ok {
				raw, err := os.ReadFile(resolved)
				if err != nil {
					return readErr(err)
				}
				mime := "image/" + ext[1:] // 去掉前导 "."
				if ext == ".svg" {
					mime = "image/svg+xml"
				}
				return map[string]any{
					"type":    "image",
					"content": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw),
					"file":    resolved,
					"size":    info.Size(),
				}, nil
			}

			// 二进制: 返回文件信息
			if _, ok := binaryExts[ext]; ok {
				return map[string]any{
					"type":    "binary",
					"file":    resolved,
					"size":    info.Size(),
					"message": fmt.Sprintf("Binary file (%s), %.1fKB. Cannot display contents.", ext, float64(info.Size())/1024),
				}, nil
			}

			// 文本文件
			raw, err := os.ReadFile(resolved)
			if err != nil {
				return readErr(err)
			}
			lines := strings.Split(string(raw), "\n")
			totalLines := len(lines)

			if input.Offset != nil {
				off := *input.Offset
				if off >= len(lines) { // JS slice 越界时返回空数组
					lines = nil
				} else if off > 0 {
					lines = lines[off:]
				}
			}
			maxLines := 2000
			if input.Limit != nil {
				maxLines = *input.Limit
			}
			truncated := len(lines) > maxLines
			if truncated {
				lines = lines[:maxLines]
			}

			startLine := 1 // (offset ?? 0) + 1
			if input.Offset != nil {
				startLine = *input.Offset + 1
			}
			numbered := make([]string, len(lines))
			for i, line := range lines {
				numbered[i] = fmt.Sprintf("%d\t%s", startLine+i, line)
			}

			return map[string]any{
				"type":       "text",
				"content":    strings.Join(numbered, "\n"),
				"totalLines": totalLines,
				"file":       resolved,
				"truncated":  truncated,
			}, nil
		},
	})
}

// FileEditInput file_edit 工具输入
type FileEditInput struct {
	FilePath   string `json:"file_path" jsonschema:"description=File to edit"`
	OldString  string `json:"old_string" jsonschema:"description=Exact string to find and replace"`
	NewString  string `json:"new_string" jsonschema:"description=Replacement string"`
	ReplaceAll bool   `json:"replace_all" jsonschema:"description=Replace all occurrences"`
}

// CreateFileEditTool 创建字符串替换编辑工具 — 对标 createFileEditTool()
func CreateFileEditTool(tctx ToolContext) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[FileEditInput, any]{
		Name: "file_edit",
		Description: "Edit a file by replacing an exact string with a new string. " +
			"The old_string must match exactly (including whitespace). " +
			"Use replace_all to replace all occurrences.",
		Execute: func(ctx context.Context, input FileEditInput, _ aisdk.ToolExecutionOptions) (any, error) {
			if !tctx.AllowWrite {
				return map[string]any{"error": "Write operations not allowed in current permission mode"}, nil
			}

			resolved := resolvePath(input.FilePath, tctx.Cwd)
			editErr := func(err error) (any, error) {
				return map[string]any{"error": fmt.Sprintf("Failed to edit %s: %v", resolved, err)}, nil
			}

			raw, err := os.ReadFile(resolved)
			if err != nil {
				return editErr(err)
			}
			content := string(raw)

			if !strings.Contains(content, input.OldString) {
				return map[string]any{
					"error": fmt.Sprintf("old_string not found in %s. Make sure it matches exactly.", input.FilePath),
				}, nil
			}

			// TS 的 content.replace 只替换第一处; replaceAll 全部替换
			newContent := ""
			count := 1
			if input.ReplaceAll {
				newContent = strings.ReplaceAll(content, input.OldString, input.NewString)
				count = strings.Count(content, input.OldString)
			} else {
				newContent = strings.Replace(content, input.OldString, input.NewString, 1)
			}

			if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
				return editErr(err)
			}
			engine.TrackFileChange(input.FilePath, engine.FileActionEdit)

			return map[string]any{
				"success":      true,
				"file":         resolved,
				"replacements": count,
				"summary":      fmt.Sprintf("Edited %s: %d replacement(s)", input.FilePath, count),
			}, nil
		},
	})
}

// FileWriteInput file_write 工具输入
type FileWriteInput struct {
	FilePath string `json:"file_path" jsonschema:"description=File path to write"`
	Content  string `json:"content" jsonschema:"description=File content"`
}

// CreateFileWriteTool 创建文件写入工具 — 对标 createFileWriteTool()
func CreateFileWriteTool(tctx ToolContext) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[FileWriteInput, any]{
		Name:        "file_write",
		Description: "Create a new file or overwrite an existing file with the given content.",
		Execute: func(ctx context.Context, input FileWriteInput, _ aisdk.ToolExecutionOptions) (any, error) {
			if !tctx.AllowWrite {
				return map[string]any{"error": "Write operations not allowed in current permission mode"}, nil
			}

			resolved := resolvePath(input.FilePath, tctx.Cwd)

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
				return map[string]any{"error": fmt.Sprintf("Failed to write %s: %v", resolved, err)}, nil
			}
			if err := os.WriteFile(resolved, []byte(input.Content), 0o644); err != nil {
				return map[string]any{"error": fmt.Sprintf("Failed to write %s: %v", resolved, err)}, nil
			}
			engine.TrackFileChange(input.FilePath, engine.FileActionWrite)

			return map[string]any{
				"success": true,
				"file":    resolved,
				// TS 的 content.split("\n").length
				"summary": fmt.Sprintf("Wrote %d lines to %s", strings.Count(input.Content, "\n")+1, input.FilePath),
			}, nil
		},
	})
}
