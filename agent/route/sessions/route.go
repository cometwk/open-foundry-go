package sessions

import (
	"net/http"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/labstack/echo/v5"
	"github.com/openfoundry/agent/engine/session"
	"github.com/openfoundry/agent/route"
	"github.com/openfoundry/lib/util"
)

func Attach(e *echo.Echo) {
	e.GET("/api/sessions", list)
	e.GET("/api/sessions/:id", get)
	e.POST("/api/sessions", post)
}

func list(c *echo.Context) error {
	cwd := route.GetAgentCwd(c)
	sessions, err := session.NewFromEnv().ListSessions(cwd)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, sessions)
}

func get(c *echo.Context) error {
	type Input struct {
		ID string `param:"id"`
	}

	input := &Input{}
	if err := util.BindAndValidate(c, input); err != nil {
		return util.BadRequest(c, err)
	}
	cwd := route.GetAgentCwd(c)
	id := input.ID

	sess, err := session.NewFromEnv().LoadSession(cwd, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if sess == nil {
		return c.String(http.StatusNotFound, "session not found")
	}

	return c.JSON(http.StatusOK, sess)
}

func post(c *echo.Context) error {
	type Input struct {
		SessionId string            `json:"sessionId" validate:"required"`
		Messages  []aisdk.UIMessage `json:"messages" validate:"required"`
	}

	type Output struct {
		SessionId    string   `json:"sessionId"`
		SystemPrompt []string `json:"systemPrompt"` // 用于调试 (运行时由 agent 每轮重新构建)
	}

	input := &Input{}
	if err := util.BindAndValidate(c, input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	cwd := route.GetAgentCwd(c)
	// 增量追加语义: 新建会话传初始消息，续传只传新增消息
	saved, err := session.NewFromEnv().SaveSession(c.Request().Context(), cwd, input.SessionId, input.Messages)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, Output{
		SessionId:    saved.Metadata.ID,
		SystemPrompt: saved.SystemPrompt,
	})
}
