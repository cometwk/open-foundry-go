package chat

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"

	// "github.com/openfoundry/agent/internal/api"
	"github.com/openfoundry/runtime/engine"
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

func NewChatHandler(agent aisdk.Agent, engine *engine.Engine) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string            `json:"id"`
			Messages []aisdk.UIMessage `json:"messages"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if len(body.Messages) == 0 {
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}
		for _, message := range body.Messages {
			switch message.Role {
			case aisdk.RoleSystem, aisdk.RoleUser, aisdk.RoleAssistant:
			default:
				http.Error(w, "invalid messages", http.StatusBadRequest)
				return
			}
		}

		// chat, err := api.GetChatById(r.Context(), engine, body.ID)
		// if err != nil {
		// 	http.Error(w, "invalid chat", http.StatusBadRequest)
		// 	return
		// }
		// if chat == nil {
		// 	chat, err = engine.CreateObject(spi.RequestContext{TenantID: "gold", ActorID: "test"}, "chat", map[string]any{
		// 		"id":       body.ID,
		// 		"messages": body.Messages,
		// 	})
		// 	http.Error(w, "invalid chat", http.StatusBadRequest)
		// 	return
		// } else {

		// }

		stream, err := aisdk.CreateAgentUIStream(
			r.Context(),
			agent,
			body.Messages,
			aisdk.WithUIMessageStreamReasoning(false),
		)
		if err != nil {
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}
		if err := aisdk.PipeAgentUIStreamToResponse(w, stream); err != nil {
			log.Printf("streaming agent response: %v", err)
		}
	})
}
