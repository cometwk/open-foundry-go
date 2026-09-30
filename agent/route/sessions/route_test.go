package sessions

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/openfoundry/lib/serve"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sessionID = "sess-1"
	userText  = "帮我修复 payments-api 的 500 错误"

	saveBody = `{
		"sessionId": "sess-1",
		"messages": [
			{
				"id": "m1",
				"role": "user",
				"parts": [{"type": "text", "text": "帮我修复 payments-api 的 500 错误"}]
			}
		]
	}`

	overwriteBody = `{
		"sessionId": "sess-1",
		"messages": [
			{
				"id": "m1",
				"role": "user",
				"parts": [{"type": "text", "text": "帮我修复 payments-api 的 500 错误"}]
			},
			{
				"id": "m2",
				"role": "assistant",
				"parts": [{"type": "text", "text": "已定位问题"}]
			}
		]
	}`
)

func TestRoutes(t *testing.T) {
	cwd := t.TempDir()
	cwd = "/tmp/agent-test/"
	t.Setenv("BASE_DIR", cwd)
	// 关闭记忆召回，避免 SaveSession 打到 LLM；工具提示词保持默认开启。
	t.Setenv("AGENT_MEMORY", "")
	t.Setenv("AGENT_TOOLS", "")

	e := echo.New()
	e.Validator = serve.NewCustomValidator()
	Attach(e)

	t.Run("list", func(t *testing.T) {
		rec := testutil.Get(e, "/api/sessions", nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		// 目录不存在时 ListSessions 返回 nil，JSON 为 null。
		assert.Equal(t, "null", strings.TrimSpace(rec.Body.String()))
	})

	t.Run("get", func(t *testing.T) {
		rec := testutil.Get(e, "/api/sessions/missing", nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "session not found", rec.Body.String())
	})

	t.Run("post", func(t *testing.T) {
		rec := testutil.Post(e, "/api/sessions", `{"messages":[]}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		body, err := rec.BodyJson()
		assert.NoError(t, err)
		assert.Contains(t, body["message"], "SessionId")

		rec = testutil.Post(e, "/api/sessions", `{"sessionId":"sess-1"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		body, err = rec.BodyJson()
		assert.NoError(t, err)
		assert.Contains(t, body["message"], "Messages")

		rec = testutil.Post(e, "/api/sessions", `{`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		rec = testutil.Post(e, "/api/sessions", saveBody)
		assert.Equal(t, http.StatusOK, rec.Code)
		body, err = rec.BodyJson()
		assert.NoError(t, err)
		assert.Equal(t, sessionID, body["sessionId"])
		prompts, ok := body["systemPrompt"].([]any)
		assert.True(t, ok)
		assert.NotEmpty(t, prompts)
		assert.Contains(t, prompts[0], "## Tools")

		_, err = os.Stat(filepath.Join(cwd, ".agent", "sessions", sessionID+".json"))
		assert.NoError(t, err)
	})

	t.Run("get saved", func(t *testing.T) {
		rec := testutil.Get(e, "/api/sessions/"+sessionID, nil)
		assert.Equal(t, http.StatusOK, rec.Code)

		response, err := rec.BodyJson()
		assert.NoError(t, err)

		meta, ok := response["metadata"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, sessionID, meta["id"])
		assert.Equal(t, userText, meta["title"])
		assert.Equal(t, float64(1), meta["messageCount"])
		assert.Equal(t, cwd, meta["cwd"])

		messages, ok := response["messages"].([]any)
		assert.True(t, ok)
		assert.Len(t, messages, 1)
		msg, ok := messages[0].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "user", msg["role"])
		parts, ok := msg["parts"].([]any)
		assert.True(t, ok)
		require.NotEmpty(t, parts)
		part, ok := parts[0].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, "text", part["type"])
		assert.Equal(t, userText, part["text"])
	})

	t.Run("list saved", func(t *testing.T) {
		rec := testutil.Get(e, "/api/sessions", nil)
		assert.Equal(t, http.StatusOK, rec.Code)

		response, err := rec.BodyArrayJson()
		assert.NoError(t, err)
		assert.Len(t, response, 1)
		assert.Equal(t, sessionID, response[0]["id"])
		assert.Equal(t, userText, response[0]["title"])
		assert.Equal(t, float64(1), response[0]["messageCount"])
	})

	t.Run("post overwrite", func(t *testing.T) {
		rec := testutil.Post(e, "/api/sessions", overwriteBody)
		assert.Equal(t, http.StatusOK, rec.Code)
		body, err := rec.BodyJson()
		assert.NoError(t, err)
		assert.Equal(t, sessionID, body["sessionId"])

		entries, err := os.ReadDir(filepath.Join(cwd, ".agent", "sessions"))
		assert.NoError(t, err)
		assert.Len(t, entries, 1)

		rec = testutil.Get(e, "/api/sessions/"+sessionID, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		got, err := rec.BodyJson()
		assert.NoError(t, err)
		meta, ok := got["metadata"].(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, float64(2), meta["messageCount"])
		assert.Equal(t, userText, meta["title"])
	})

	t.Run("list order", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("BASE_DIR", dir)
		writeSession(t, dir, "old", "旧会话", "2026-09-25T10:00:00.000Z")
		writeSession(t, dir, "new", "最新会话", "2026-09-25T12:00:00.000Z")
		sessDir := filepath.Join(dir, ".agent", "sessions")
		require.NoError(t, os.WriteFile(filepath.Join(sessDir, "corrupt.json"), []byte("not json"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(sessDir, "notes.txt"), []byte("skip"), 0o644))

		rec := testutil.Get(e, "/api/sessions", nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		response, err := rec.BodyArrayJson()
		assert.NoError(t, err)
		require.Len(t, response, 2)
		assert.Equal(t, "new", response[0]["id"])
		assert.Equal(t, "old", response[1]["id"])
	})

	t.Run("post save error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		t.Setenv("BASE_DIR", dir)

		rec := testutil.Post(e, "/api/sessions", saveBody)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		body, err := rec.BodyJson()
		assert.NoError(t, err)
		assert.NotEmpty(t, body["message"])
	})
}

func writeSession(t *testing.T, cwd, id, title, updatedAt string) {
	t.Helper()
	dir := filepath.Join(cwd, ".agent", "sessions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	raw := `{
		"metadata": {
			"id": "` + id + `",
			"title": "` + title + `",
			"createdAt": "` + updatedAt + `",
			"updatedAt": "` + updatedAt + `",
			"messageCount": 1,
			"cwd": "` + cwd + `"
		},
		"messages": []
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".json"), []byte(raw), 0o644))
}
