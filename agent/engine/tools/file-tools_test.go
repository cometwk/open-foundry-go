package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/agent/engine"
	"github.com/stretchr/testify/require"
)

// testCwd 返回已写入 marker.txt 的临时工作目录
func testCwd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("hello-cwd"), 0o644))
	return dir
}

func writeToolContext(t *testing.T, allowWrite bool) ToolContext {
	t.Helper()
	tctx := testToolContext()
	tctx.Cwd = t.TempDir()
	tctx.PermissionMode = engine.PermissionModeDefault
	tctx.AllowWrite = allowWrite
	return tctx
}

func TestFileResolvePath(t *testing.T) {
	require.Equal(t, "/abs/path/a.go", resolvePath("/abs/path/a.go", "/work"))
	require.Equal(t, filepath.Join("/work", "rel/a.go"), resolvePath("rel/a.go", "/work"))
}

func TestFileReadTool(t *testing.T) {
	tctx := writeToolContext(t, false)
	cwd := tctx.Cwd
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "text.txt"), []byte("l1\nl2\nl3\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "pic.png"), []byte{0x89, 0x50, 0x4E, 0x47}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "icon.svg"), []byte("<svg/>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "data.zip"), []byte("PK\x03\x04xxxxx"), 0o644))

	tool, err := CreateFileReadTool(tctx)
	require.NoError(t, err)

	read := func(input string) map[string]any {
		t.Helper()
		return execTool(t, tool, input)
	}

	// 文本: 默认全量 + 行号 (末尾空行计入 totalLines)
	res := read(`{"file_path": "text.txt"}`)
	require.Equal(t, "text", res["type"])
	require.Equal(t, "1\tl1\n2\tl2\n3\tl3\n4\t", res["content"])
	require.EqualValues(t, 4, res["totalLines"])
	require.Equal(t, filepath.Join(cwd, "text.txt"), res["file"])
	require.Equal(t, false, res["truncated"])

	// offset + limit + truncated
	res = read(`{"file_path": "text.txt", "offset": 1, "limit": 2}`)
	require.Equal(t, "2\tl2\n3\tl3", res["content"])
	require.EqualValues(t, 4, res["totalLines"])
	require.Equal(t, true, res["truncated"]) // offset 后剩 3 行 > limit 2

	// 越界 offset → 空
	res = read(`{"file_path": "text.txt", "offset": 100}`)
	require.Equal(t, "", res["content"])
	require.EqualValues(t, 4, res["totalLines"])

	// 图片: base64 data URI
	res = read(`{"file_path": "pic.png"}`)
	require.Equal(t, "image", res["type"])
	require.True(t, strings.HasPrefix(res["content"].(string), "data:image/png;base64,"))
	require.EqualValues(t, 4, res["size"])

	// svg 特殊 mime
	res = read(`{"file_path": "icon.svg"}`)
	require.True(t, strings.HasPrefix(res["content"].(string), "data:image/svg+xml;base64,"))

	// 二进制: 文件信息
	res = read(`{"file_path": "data.zip"}`)
	require.Equal(t, "binary", res["type"])
	require.Contains(t, res["message"], "KB. Cannot display contents.")

	// 不存在
	res = read(`{"file_path": "missing.txt"}`)
	require.Contains(t, res["error"], "Failed to read")
}

func TestFileEditTool(t *testing.T) {
	// 不允许写
	tool, err := CreateFileEditTool(writeToolContext(t, false))
	require.NoError(t, err)
	res := execTool(t, tool, `{"file_path": "a.txt", "old_string": "x", "new_string": "y"}`)
	require.Equal(t, "Write operations not allowed in current permission mode", res["error"])

	tctx := writeToolContext(t, true)
	path := filepath.Join(tctx.Cwd, "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("aXbXc"), 0o644))

	tool, err = CreateFileEditTool(tctx)
	require.NoError(t, err)

	// old_string 不存在
	res = execTool(t, tool, `{"file_path": "a.txt", "old_string": "NOPE", "new_string": "y"}`)
	require.Equal(t, "old_string not found in a.txt. Make sure it matches exactly.", res["error"])

	engine.ClearFileChanges()
	t.Cleanup(engine.ClearFileChanges)

	// 默认只替换第一处 (对标 TS String.replace)
	res = execTool(t, tool, `{"file_path": "a.txt", "old_string": "X", "new_string": "Y"}`)
	require.Equal(t, true, res["success"])
	require.EqualValues(t, 1, res["replacements"])
	require.Equal(t, "Edited a.txt: 1 replacement(s)", res["summary"])
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "aYbXc", string(raw))

	// replace_all: 全部替换并统计次数
	require.NoError(t, os.WriteFile(path, []byte("aYbYc"), 0o644))
	res = execTool(t, tool, `{"file_path": "a.txt", "old_string": "Y", "new_string": "Z", "replace_all": true}`)
	require.EqualValues(t, 2, res["replacements"])
	require.Equal(t, "Edited a.txt: 2 replacement(s)", res["summary"])
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "aZbZc", string(raw))

	// 追踪: 记录的是原始 file_path
	changes := engine.GetFileChanges()
	require.NotEmpty(t, changes)
	for _, c := range changes {
		require.Equal(t, "a.txt", c.Path)
		require.Equal(t, engine.FileActionEdit, c.Action)
	}

	// 编辑不存在的文件
	res = execTool(t, tool, `{"file_path": "missing.txt", "old_string": "x", "new_string": "y"}`)
	require.Contains(t, res["error"], "Failed to edit")
}

func TestFileWriteTool(t *testing.T) {
	// 不允许写
	tool, err := CreateFileWriteTool(writeToolContext(t, false))
	require.NoError(t, err)
	res := execTool(t, tool, `{"file_path": "a.txt", "content": "x"}`)
	require.Equal(t, "Write operations not allowed in current permission mode", res["error"])

	tctx := writeToolContext(t, true)
	tool, err = CreateFileWriteTool(tctx)
	require.NoError(t, err)

	engine.ClearFileChanges()
	t.Cleanup(engine.ClearFileChanges)

	// 自动创建父目录
	res = execTool(t, tool, `{"file_path": "sub/dir/f.txt", "content": "a\nb\nc"}`)
	require.Equal(t, true, res["success"])
	require.Equal(t, filepath.Join(tctx.Cwd, "sub", "dir", "f.txt"), res["file"])
	require.Equal(t, "Wrote 3 lines to sub/dir/f.txt", res["summary"])

	raw, err := os.ReadFile(filepath.Join(tctx.Cwd, "sub", "dir", "f.txt"))
	require.NoError(t, err)
	require.Equal(t, "a\nb\nc", string(raw))

	// 追踪 write
	changes := engine.GetFileChanges()
	require.Len(t, changes, 1)
	require.Equal(t, "sub/dir/f.txt", changes[0].Path)
	require.Equal(t, engine.FileActionWrite, changes[0].Action)
}
