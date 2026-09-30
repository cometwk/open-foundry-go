package sessions

import (
	"net/http"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/labstack/echo/v5"
	"github.com/openfoundry/agent/engine"
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
	sessions := engine.ListSessions(cwd)
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

	session := engine.LoadSession(cwd, id)
	if session == nil {
		return c.String(http.StatusNotFound, "session not found")
	}

	return c.JSON(http.StatusOK, session)
}

func post(c *echo.Context) error {
	type Input struct {
		SessionId string            `json:"sessionId"`
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
	id := input.SessionId
	if id == "" {
		id = util.UUIDv7()
	}
	session, err := engine.SaveSession(c.Request().Context(), cwd, input.Messages, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, Output{
		SessionId:    session.Metadata.ID,
		SystemPrompt: session.SystemPrompt,
	})
}
