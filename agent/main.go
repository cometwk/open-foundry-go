package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/openfoundry/agent/chat"
	"github.com/openfoundry/agent/client"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/serve"
	"github.com/openfoundry/lib/xlog"
)

func main() {
	xlog.InitDebug()
	e := serve.NewEcho()

	// 创建 Foundry Server
	s, err := chat.CreateFoundryServer()
	if err != nil {
		slog.Error("create foundry server", "error", err)
		os.Exit(1)
	}
	defer s.CloseDB()

	action := client.NewAction(s.Srv, s.SPI)

	chat.AttachOpenFoundry(e, s.Srv)
	chat.AttachChatHandler(e, action)

	httpSrv := &http.Server{Addr: ":" + env.String("PORT", "4000"), Handler: e}
	serve.ServeHTTP(context.Background(), httpSrv)
}
