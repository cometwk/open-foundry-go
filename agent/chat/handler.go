package chat

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net/http"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/labstack/echo/v5"
	"github.com/openfoundry/agent/client"
	"github.com/openfoundry/lib/testutil"
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

func NewChatHandler(agent aisdk.Agent, action *client.Action) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string            `json:"id"`
			Message  aisdk.UIMessage   `json:"message"`
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
		// if len(body.Messages) == 0 {
		// 	http.Error(w, "invalid messages", http.StatusBadRequest)
		// 	return
		// }

		ctx := r.Context()

		// check message role is valid
		// for _, message := range body.Messages {
		// 	switch message.Role {
		// 	case aisdk.RoleSystem, aisdk.RoleUser, aisdk.RoleAssistant:
		// 	default:
		// 		http.Error(w, "invalid messages", http.StatusBadRequest)
		// 		return
		// 	}
		// }

		switch body.Message.Role {
		case aisdk.RoleSystem, aisdk.RoleUser, aisdk.RoleAssistant:
		default:
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}

		// check chat
		// chat, err := action.Chat.GetById(r.Context(), body.ID, " id title messages { id role parts }")
		chat, err := action.Chat.GetById(r.Context(), body.ID, "id title")
		if err != nil {
			http.Error(w, "invalid chat", http.StatusBadRequest)
			return
		}

		var inputMessages []aisdk.UIMessage
		if chat != nil {
			// if (chat.userId !== session.user.id) {
			// 	return new ChatbotError("forbidden:chat").toResponse();
			// }

			// load history messages
			inputMessages, err = action.GetdMessagesByChatId(ctx, chat.ID)
			if err != nil {
				http.Error(w, "invalid chat", http.StatusBadRequest)
				return
			}
		} else {
			// no history, create new chat
			err := action.SaveChat(r.Context(), &client.Chat{
				ID:    body.ID,
				Title: "new chat-" + time.Now().Format("0102 15:04:05"),
			})
			if err != nil {
				http.Error(w, "invalid chat", http.StatusBadRequest)
				return
			}
		}

		// append new message
		inputMessages = append(inputMessages, body.Message)
		if body.Message.Role == aisdk.RoleUser {
			// 保存用户输入的消息
			err := action.SaveChatMessage(ctx, body.ID, body.Message)
			if err != nil {
				slog.Error("save chat message failed", "error", err)
				http.Error(w, "save chat message failed", http.StatusInternalServerError)
				return
			}
		}

		stream, err := aisdk.CreateAgentUIStream(
			ctx,
			agent,
			inputMessages,
			aisdk.WithUIMessageStreamReasoning(false),
			aisdk.OnUIMessageStreamFinish(func(args aisdk.UIMessageStreamOnFinishState) {
				slog.InfoContext(r.Context(), "resp=\n"+testutil.Pretty(args.ResponseMessage), "ai", "resp")
				slog.InfoContext(r.Context(), "array=\n"+testutil.Pretty(args.Messages), "ai", "resp")
				// 保存助手回复的消息
				err := action.SaveChatMessage(r.Context(), body.ID, args.ResponseMessage)
				if err != nil {
					slog.Error("save chat messages failed", "error", err)
					http.Error(w, "save chat messages failed", http.StatusInternalServerError)
					return
				}
			}),
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

func AttachChatHandler(e *echo.Echo, action *client.Action) error {
	agent, err := NewAgent(action.Model)
	if err != nil {
		return err
	}
	e.POST("/api/chat", echo.WrapHandler(NewChatHandler(agent, action)))
	return nil
}

func NewChatHandler0(agent aisdk.Agent, action *client.Action) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
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
