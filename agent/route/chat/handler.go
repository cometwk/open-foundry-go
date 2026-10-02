package chat

import (
	"context"
	"log/slog"
	"net/http"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/labstack/echo/v5"
	"github.com/openfoundry/agent/engine"
	"github.com/openfoundry/agent/engine/agent"
	"github.com/openfoundry/agent/engine/session"
	"github.com/openfoundry/agent/route"
	"github.com/openfoundry/lib/testutil"
	"github.com/openfoundry/lib/util"
)

type weatherInput struct {
	City string `json:"city" jsonschema:"description=City to look up the weather for"`
}

type weatherOutput struct {
	City       string `json:"city"`
	Celsius    int    `json:"celsius"`
	Conditions string `json:"conditions"`
}

func getWeather(_ context.Context, input weatherInput, _ aisdk.ToolExecutionOptions) (weatherOutput, error) {
	return weatherOutput{
		City:       input.City,
		Celsius:    18,
		Conditions: "partly cloudy",
	}, nil
}

func NewAgent(model provider.LanguageModel) (*aisdk.ToolLoopAgent, error) {
	weather, err := aisdk.TypedTool(aisdk.TypedToolDef[weatherInput, weatherOutput]{
		Name:        "get_weather",
		Description: "Return deterministic sample weather data for a city.",
		Execute:     getWeather,
	})
	if err != nil {
		return nil, err
	}

	return aisdk.NewToolLoopAgent(model,
		aisdk.WithToolLoopAgentID("weather-assistant"),
		aisdk.WithToolLoopAgentOptions(
			aisdk.WithInstructions("You are a helpful assistant. Use the weather tool when the answer depends on current weather."),
			aisdk.WithTools(aisdk.ToolSet{"get_weather": weather}),
			aisdk.WithStopWhen(aisdk.StepCountIs(5)),
		),
	), nil
}

func Attach(e *echo.Echo) {
	h := &handler{
		store: session.NewFromEnv(),
	}
	e.POST("/api/chat", h.chat)
}

var sessionBudgets = NewSafeMap[string, *engine.BudgetTracker]()

type handler struct {
	store session.Store
}

func (h *handler) chat(c *echo.Context) error {
	type Input struct {
		ID      string          `json:"id" validate:"required"`
		Message aisdk.UIMessage `json:"message" validate:"required"`
	}

	input := &Input{}
	if err := util.BindAndValidate(c, input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	cwd := route.GetAgentCwd(c)

	initMessages, err := h.store.LoadSession(cwd, input.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var messages []aisdk.UIMessage

	if initMessages != nil {
		messages = append(messages, initMessages.Messages...)
	}
	messages = append(messages, input.Message)

	ctx := c.Request().Context()
	id := input.ID

	// 获取或创建 budget tracker (对标 Claude Code 的 QueryEngine 跨轮状态)
	tracker, ok := sessionBudgets.Get(id)
	if !ok {
		tracker = engine.CreateBudgetTracker()
		sessionBudgets.Set(id, tracker)
	}
	// 调用 agent engine (内含 auto compact + budget check)
	result, err := agent.HandleMessage(c.Request().Context(), messages, agent.AgentConfig{
		Cwd: cwd,
	}, tracker)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	w := c.Response()
	// 附加 budget 状态到 response headers
	if result.BudgetStatus != "" {
		w.Header().Set("X-Budget-Status", result.BudgetStatus)
	}
	if result.WasCompacted {
		w.Header().Set("X-Was-Compacted", "true")
	}

	uiStream := result.Stream.ToUIMessageStream(
		// OriginalMessages = 本轮新传入的消息；args.Messages = 本轮新增 + assistant 回复，
		// 正好作为 SaveSession 增量追加的批次
		// aisdk.WithUIMessageStreamOriginalMessages(messages...),
		// aisdk.WithUIMessageStreamReasoning(true),
		// aisdk.WithUIMessageStreamSources(true),
		aisdk.OnUIMessageStreamError(func(err error) string {
			return "The model request failed."
		}),
		aisdk.OnUIMessageStreamFinish(func(args aisdk.UIMessageStreamOnFinishState) {
			slog.InfoContext(ctx, "resp=\n"+testutil.Pretty(args.ResponseMessage), "ai", "resp")
			slog.InfoContext(ctx, "array=\n"+testutil.Pretty(args.Messages), "ai", "resp")

			// 增量追加本轮消息 (user 新消息 + assistant 回复)
			slog.InfoContext(ctx, "save session", "id-debug", id, "cwd-debug", cwd)
			_, err := h.store.SaveSession(ctx, cwd, id, args.Messages)
			if err != nil {
				slog.Error("save chat messages failed", "error", err)
				http.Error(w, "save chat messages failed", http.StatusInternalServerError)
				return
			}
		}),
	)

	if err := aisdk.PipeUIMessageStreamToResponse(w, uiStream); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return nil
}
