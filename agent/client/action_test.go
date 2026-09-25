package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/chat"
	"github.com/openfoundry/agent/client"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/require"
)

func realServer(t *testing.T) *chat.FoundryServer {
	t.Helper()
	s, err := chat.CreateFoundryServer()
	require.NoError(t, err)
	return s
}

func realAction(t *testing.T) *client.Action {
	t.Helper()
	s := realServer(t)
	action := client.NewAction(s.Srv, s.SPI)

	return action
}
func TestConvert(t *testing.T) {
	t.Run("convert: db -> ui", func(t *testing.T) {
		db := sampleMessage("123")
		ui := client.ConvertToUIMessage(db)
		require.Equal(t, "123", ui.ID)
		require.Equal(t, aisdk.RoleUser, ui.Role)
		require.Len(t, ui.Parts, 1)
		tp, ok := ui.Parts[0].(aisdk.TextPart)
		require.True(t, ok, "expected TextPart, got %T", ui.Parts[0])
		require.Equal(t, "18 C, partly cloudy.", tp.Text)
		testutil.PrintPretty(db)
		testutil.PrintPretty(ui)
	})
	t.Run("convert: ui -> db", func(t *testing.T) {
		ui := sampleUIMessage("abc", "ui-message sample")
		db := client.ConvertToDBMessage(ui)
		require.Equal(t, "abc", db.ID)
		require.Equal(t, aisdk.RoleUser, db.Role)
		require.JSONEq(t, `[{"type":"text","text":"ui-message sample"}]`, db.Parts)
	})
	t.Run("convert: round-trip", func(t *testing.T) {
		ui := sampleUIMessage("round", "hello")
		back := client.ConvertToUIMessage(client.ConvertToDBMessage(ui))
		require.Equal(t, ui.ID, back.ID)
		require.Equal(t, ui.Role, back.Role)
		require.Equal(t, "hello", client.GetTextFromMessage(back))
	})

	t.Run("static: json -> db", func(t *testing.T) {
		j := `{
  "id": "123",
  "role": "user",
  "parts": "[{\"type\":\"text\",\"text\":\"18 C, partly cloudy.\"}]",
  "attachments": "",
  "chatId": ""
}`
		var v client.Message
		err := json.Unmarshal([]byte(j), &v)
		require.NoError(t, err)
		testutil.PrintPretty(v)
	})

	t.Run("static: json -> ui", func(t *testing.T) {
		j := `{
  "id": "123",
  "role": "user",
  "parts": "[{\"type\":\"text\",\"text\":\"18 C, partly cloudy.\"}]",
  "attachments": "",
  "chatId": ""
}`
		var v client.Message
		err := json.Unmarshal([]byte(j), &v)
		require.NoError(t, err)

		ui := client.ConvertToUIMessage(v)
		testutil.PrintPretty(ui)

	})

	t.Run("static: db -> json", func(t *testing.T) {
		ui := aisdk.UIMessage{
			ID:   "123",
			Role: aisdk.RoleUser,
			Parts: []aisdk.Part{
				aisdk.TextPart{Text: "demo text"},
			},
		}
		_, err := json.Marshal(ui)
		require.NoError(t, err, "Parts 要求采用值模式")

		ui = aisdk.UIMessage{
			ID:   "123",
			Role: aisdk.RoleUser,
			Parts: []aisdk.Part{
				&aisdk.TextPart{Text: "demo text"},
			},
		}
		_, err = json.Marshal(ui)
		require.Error(t, err)
	})
}
func Test1(t *testing.T) {
	action := realAction(t)

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	realChatId := func(t *testing.T) string {
		chats, err := action.GetChatsByUserId(ctx, "1", client.ChatConnectionQueryParams{})
		require.NoError(t, err)
		require.Greater(t, len(chats), 0)
		return chats[0].ID
	}
	chatId := realChatId(t)

	t.Run("GetChatById", func(t *testing.T) {
		chat, err := action.GetChatById(ctx, chatId)
		require.NoError(t, err)
		t.Logf("chat: %+v", testutil.Pretty(chat))
	})

	t.Run("GetChatsByUserId", func(t *testing.T) {
		chats, err := action.GetChatsByUserId(ctx, "1", client.ChatConnectionQueryParams{})
		require.NoError(t, err)
		t.Logf("chats: %+v", testutil.Pretty(chats))
	})

	t.Run("GetdMessagesByChatId", func(t *testing.T) {
		messages, err := action.GetdMessagesByChatId(ctx, chatId)
		require.NoError(t, err)
		t.Logf("messages: %+v", testutil.Pretty(messages))
	})

	t.Run("SaveChat", func(t *testing.T) {
		chat := &client.Chat{
			ID:         "chat-" + suffix,
			Title:      "Weather",
			Visibility: client.VisibilityPrivate,
			// UserId:     "1",
		}
		err := action.SaveChat(ctx, chat)
		require.NoError(t, err)
		t.Logf("chat: %+v", testutil.Pretty(chat))
	})

	t.Run("SaveChatMessage", func(t *testing.T) {
		message := sampleUIMessage("msg-"+suffix, "What is the weather in London?")
		err := action.SaveChatMessage(ctx, chatId, message)
		require.NoError(t, err)
		t.Logf("message: %+v", testutil.Pretty(message))
	})

	t.Run("SaveChatMessages", func(t *testing.T) {
		messages := []aisdk.UIMessage{
			sampleUIMessage("msg-a-"+suffix, "Hello"),
			sampleUIMessage("msg-b-"+suffix, "18 C, partly cloudy."),
		}
		messages[1].Role = aisdk.RoleAssistant
		err := action.SaveChatMessages(ctx, chatId, messages)
		require.NoError(t, err)
		t.Logf("messages: %+v", testutil.Pretty(messages))
	})

	t.Run("GenerateTitleFromUserMessage", func(t *testing.T) {
		message := sampleUIMessage("msg-title", "help me write an essay about space")
		title, err := action.GenerateTitleFromUserMessage(ctx, message)
		require.NoError(t, err)
		require.NotEmpty(t, title)
		t.Logf("title: %s", title)
	})
}

func sampleUIMessage(id, text string) aisdk.UIMessage {
	return aisdk.UIMessage{
		ID:   id,
		Role: aisdk.RoleUser,
		Parts: []aisdk.Part{
			aisdk.TextPart{Text: text},
		},
	}
}

func sampleMessage(id string) client.Message {
	return client.Message{
		ID:    id,
		Role:  aisdk.RoleUser,
		Parts: `[{"type":"text","text":"18 C, partly cloudy."}]`,
	}
}
