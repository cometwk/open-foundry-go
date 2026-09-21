package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/openfoundry/agent/client"
	"github.com/openfoundry/agent/internal/chat"
	"github.com/openfoundry/lib/xlog"
	"github.com/stretchr/testify/require"
)

func TestApi_ChatGetById(t *testing.T) {
	xlog.Init()

	srv, closeDB, err := chat.CreateFoundryServer()
	if err != nil {
		t.Fatalf("CreateFoundryServer: %v", err)
	}
	defer closeDB()

	api := client.NewAPIs(client.CreateClient(srv))

	t.Run("Create", func(t *testing.T) {
		// created, err := api.Chat.Create(context.Background(), client.Chat{
		// 	Title: "Test Chat",
		// })
		// require.NoError(t, err)
		// t.Logf("created chat: %+v", created.ID)

		// id := created.ID
		id := "01a0c2de-d8f6-75dc-9d15-9f594eedf2d1"
		got, err := api.Chat.GetById(context.Background(), id, " id title")
		require.NoError(t, err)
		require.NotNil(t, got)
		t.Logf("got chat: %+v", got.ID)

		m, err := api.Message.Create(context.Background(), client.Message{
			// ID:    id,
			Role:  client.MessageRoleUser,
			Parts: "Test Message - " + time.Now().Format(time.RFC3339),
			Chat:  got,
		})
		require.NoError(t, err)
		t.Logf("created message: %+v", m.ID)

		r, err := api.Client.CreateLink(context.Background(), "InChat", m.ID, got.ID, nil)
		// require.NoError(t, err)
		t.Logf("created link: %+v", r)
	})

	t.Run("Update", func(t *testing.T) {
		id := "01a0c2de-d8f6-75dc-9d15-9f594eedf2d1"
		got, err := api.Chat.GetById(context.Background(), id, " id title")

		require.NoError(t, err)
		require.NotNil(t, got)
		// t.Logf("got version: %d title: %s", got.Version, got.Title)

		// version := got.Version
		updated, err := api.Chat.Update(context.Background(), id, client.Chat{
			Title: "Test Chat Updated",
		}, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		t.Logf("updated chat: %+v", updated.ID)
		// t.Logf("version: %d %d %d", got.Version, updated.Version, version)
	})
}
