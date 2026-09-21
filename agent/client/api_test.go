package client

import (
	"context"
	"testing"

	"github.com/openfoundry/agent/internal/chat"
	"github.com/openfoundry/lib/xlog"
)

func TestApi_ChatGetById(t *testing.T) {
	xlog.InitDebug()

	srv, closeDB, err := chat.CreateFoundryServer()
	if err != nil {
		t.Fatalf("CreateFoundryServer: %v", err)
	}
	defer closeDB()

	api := NewAPI(CreateClient(srv))
	got, err := api.Chat.GetById(context.Background(), "missing-chat-id")
	if err != nil {
		t.Fatalf("GetById: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}
