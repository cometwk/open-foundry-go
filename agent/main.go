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
	slog.Info("hello world", xlog.MOD, "main")
	e := serve.NewEcho()

	// 创建 Foundry Server
	s, err := chat.CreateFoundryServer()
	if err != nil {
		slog.Error("create foundry server", "error", err)
		os.Exit(1)
	}
	defer s.CloseDB()

	action := client.NewAction(s.Srv, s.SPI)

	err = chat.AttachOpenFoundry(e, s.Srv)
	if err != nil {
		slog.Error("attach open foundry", "error", err)
		os.Exit(1)
	}
	err = chat.AttachChatHandler(e, action)
	if err != nil {
		slog.Error("attach chat handler", "error", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{Addr: ":" + env.String("PORT", "4000"), Handler: e}
	serve.ServeHTTP(context.Background(), httpSrv)
}
