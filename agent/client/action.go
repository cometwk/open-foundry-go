package client

import (
	"context"
	"fmt"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/provider"
	"github.com/openfoundry/agent/internal/llm"
	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/spi"
)

type Action struct {
	FlashModel provider.LanguageModel
	Model      provider.LanguageModel
	Client     *Client
	Account    *GraphQLResource[Account, AccountFilter, AccountOrderBy]
	Chat       *GraphQLResource[Chat, ChatFilter, ChatOrderBy]
	Message    *GraphQLResource[Message, MessageFilter, MessageOrderBy]
}

func NewAction(srv *api.Server) *Action {
	c := CreateClient(srv)
	a := NewAPI(c)
	return &Action{
		FlashModel: llm.NewFlashModel(),
		Model:      llm.NewModel(),
		Client:     c,
		Account:    a.Account,
		Chat:       a.Chat,
		Message:    a.Message,
	}
}

func (a *Action) GetChatById(ctx context.Context, chatId string) (*Chat, error) {
	return a.Chat.GetById(ctx, chatId, "id title")
}

func (a *Action) GetChatsByUserId(ctx context.Context, userId string, params ChatConnectionQueryParams) ([]Chat, error) {
	// TODO:
	// params.Filter = &ChatFilter{
	// 	OwnerId: &IDFilter{Eq: &userId},
	// }

	r, err := a.Chat.List(ctx, params, "id title")
	if err != nil {
		return nil, err
	}
	var chats []Chat
	for _, edge := range r.Edges {
		chats = append(chats, edge.Node)
	}
	return chats, nil
}

// GetMessagesByChatId returns all messages in the chat.
func (a *Action) GetdMessagesByChatId(ctx context.Context, chatId string) ([]aisdk.UIMessage, error) {
	r, err := a.Message.List(ctx, MessageConnectionQueryParams{
		Filter: &MessageFilter{
			ChatId: &IDFilter{Eq: &chatId},
		},
	}, "id role parts")
	if err != nil {
		return nil, err
	}
	var messages []aisdk.UIMessage
	for _, edge := range r.Edges {
		messages = append(messages, ConvertToUIMessage(edge.Node))
	}
	return messages, nil
}

func (a *Action) SaveChat(ctx context.Context, chat *Chat) error {
	p := a.Client.p
	rc := a.Client.rc

	props, err := toObjectProps(chat)
	if err != nil {
		return err
	}
	props[spi.FieldEngineObjectID] = chat.ID
	delete(props, "userId")
	delete(props, "id")
	obj, err := p.CreateObject(rc, "Chat", props)
	if err != nil {
		return err
	}
	if chat.ID != obj[spi.FieldID].(string) {
		return fmt.Errorf("chat ID mismatch: %s != %s", chat.ID, obj[spi.FieldID].(string))
	}
	return nil
}

func (a *Action) SaveChatMessages(ctx context.Context, chatId string, messages []aisdk.UIMessage) error {
	for _, message := range messages {
		err := a.SaveChatMessage(ctx, chatId, message)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Action) SaveChatMessage(ctx context.Context, chatId string, message aisdk.UIMessage) error {
	p := a.Client.p
	rc := a.Client.rc

	db := ConvertToDBMessage(message)
	_, err := p.CreateObject(rc, "Message", map[string]any{
		spi.FieldEngineObjectID: db.ID,
		"role":                  db.Role,
		"parts":                 db.Parts,
	})
	if err != nil {
		return err
	}
	_, err = p.CreateLink(rc, "InChat", message.ID, chatId, nil)
	if err != nil {
		return err
	}

	return nil
}

func (a *Action) GenerateTitleFromUserMessage(ctx context.Context, message aisdk.UIMessage) string {
	result, err := aisdk.GenerateText(ctx, a.FlashModel,
		aisdk.WithSystem(titlePrompt),
		aisdk.WithModelMessages(provider.Message{
			Role: provider.RoleUser,
			Content: []provider.ContentPart{
				{
					Type: provider.ContentPartTypeText,
					Text: GetTextFromMessage(message),
				},
			},
		}),
	)
	if err != nil {
		return ""
	}
	return result.Text
}
