package client_test

// import (
// 	"context"
// 	"testing"
// 	"time"

// 	"github.com/openfoundry/agent/client"
// 	"github.com/openfoundry/agent/internal/chat"
// 	"github.com/openfoundry/lib/xlog"
// 	"github.com/stretchr/testify/require"
// )

// func realServer(t *testing.T) *chat.FoundryServer {
// 	xlog.Init()

// 	s, err := chat.CreateFoundryServer()
// 	require.NoError(t, err)
// 	// defer s.CloseDB()

// 	return s
// }

// func TestApi_ChatGetById(t *testing.T) {
// 	s := realServer(t)
// 	api := client.NewAPI(client.CreateClient(s.Srv))

// 	getFirstChatId := func(t *testing.T) string {
// 		chats, err := api.Chat.List(context.Background(), client.ConnectionQueryParams[client.ChatFilter, client.ChatOrderBy]{
// 			First: new(1),
// 			// Filter: &client.ChatFilter{
// 			// 	Title: &client.StringFilter{
// 			// 		Equals: "Test Chat",
// 			// 	},
// 			// },
// 		}, "id")
// 		require.NoError(t, err)
// 		require.NotNil(t, chats)
// 		require.NotEmpty(t, chats)
// 		// require.Len(t, chats, 1)
// 		return chats.Edges[0].Node.ID
// 	}

// 	t.Run("Create", func(t *testing.T) {
// 		// created, err := api.Chat.Create(context.Background(), client.Chat{
// 		// 	Title: "Test Chat",
// 		// })
// 		// require.NoError(t, err)
// 		// t.Logf("created chat: %+v", created.ID)

// 		// id := created.ID
// 		id := getFirstChatId(t)

// 		got, err := api.Chat.GetById(context.Background(), id, " id title")
// 		require.NoError(t, err)
// 		require.NotNil(t, got)
// 		t.Logf("got chat: %+v", got.ID)

// 		m, err := api.Message.Create(context.Background(), &client.Message{
// 			Role:  client.MessageRoleUser,
// 			Parts: "Test Message - " + time.Now().Format(time.RFC3339),
// 			Chat:  got,
// 		})
// 		require.NoError(t, err)
// 		t.Logf("created message: %+v", m.ID)

// 		r, err := api.Client.CreateLink(context.Background(), "InChat", m.ID, got.ID, nil)
// 		// require.NoError(t, err)
// 		t.Logf("created link: %+v", r)
// 	})

// 	t.Run("Update", func(t *testing.T) {
// 		id := getFirstChatId(t)
// 		got, err := api.Chat.GetById(context.Background(), id, " id title")

// 		require.NoError(t, err)
// 		require.NotNil(t, got)
// 		// t.Logf("got version: %d title: %s", got.Version, got.Title)

// 		// version := got.Version
// 		updated, err := api.Chat.Update(context.Background(), id, client.Chat{
// 			Title: "Test Chat Updated",
// 		}, nil)
// 		if err != nil {
// 			t.Fatalf("Update: %v", err)
// 		}
// 		t.Logf("updated chat: %+v", updated.ID)
// 		// t.Logf("version: %d %d %d", got.Version, updated.Version, version)
// 	})
// }
