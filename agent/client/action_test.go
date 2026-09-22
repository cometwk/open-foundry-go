package client_test

import (
	"context"
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
	action := client.NewAction(s.Srv)

	return action
}
func TestConvert(t *testing.T) {
	t.Run("convert: db -> ui", func(t *testing.T) {
		db := sampleMessage("123")
		testutil.PrintPretty(db)
		testutil.PrintPretty(client.ConvertToUIMessage(db))
	})
	t.Run("convert: ui -> db", func(t *testing.T) {
		ui := sampleUIMessage("abc", "ui-message sample")
		testutil.PrintPretty(ui)
		testutil.PrintPretty(client.ConvertToDBMessage(ui))
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
			UserId:     "1",
		}
		err := action.SaveChat(ctx, chat)
		require.NoError(t, err)
		t.Logf("chat: %+v", testutil.Pretty(chat))
	})

	t.Run("SaveChatMessage", func(t *testing.T) {
		message := sampleUIMessage("msg-"+suffix, "What is the weather in London?")
		err := action.SaveChatMessage(ctx, "1", message)
		require.NoError(t, err)
		t.Logf("message: %+v", testutil.Pretty(message))
	})

	t.Run("SaveChatMessages", func(t *testing.T) {
		messages := []aisdk.UIMessage{
			sampleUIMessage("msg-a-"+suffix, "Hello"),
			sampleUIMessage("msg-b-"+suffix, "18 C, partly cloudy."),
		}
		messages[1].Role = aisdk.RoleAssistant
		err := action.SaveChatMessages(ctx, "1", messages)
		require.NoError(t, err)
		t.Logf("messages: %+v", testutil.Pretty(messages))
	})

	t.Run("GenerateTitleFromUserMessage", func(t *testing.T) {
		message := sampleUIMessage("msg-title", "help me write an essay about space")
		title := action.GenerateTitleFromUserMessage(ctx, message)
		t.Logf("title: %s", title)
	})
}

func sampleUIMessage(id, text string) aisdk.UIMessage {
	return aisdk.UIMessage{
		ID:   id,
		Role: aisdk.RoleUser,
		Parts: []aisdk.Part{
			&aisdk.TextPart{Text: text},
		},
	}
}

func sampleMessage(id string) client.Message {
	return client.Message{
		ID:    id,
		Role:  aisdk.RoleUser,
		Parts: []byte(`[{"type":"text","text":"18 C, partly cloudy."}]`),
	}
}
