package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/openfoundry/agent/foundry"
	"github.com/openfoundry/agent/route/chat"
	"github.com/openfoundry/agent/route/sessions"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/serve"
	"github.com/openfoundry/lib/xlog"
)

func main() {
	xlog.InitDebug()
	slog.Info("hello world", xlog.MOD, "main")
	e := serve.NewEcho()

	// 创建 Foundry Server
	s, err := foundry.CreateFoundryServer()
	if err != nil {
		slog.Error("create foundry server", "error", err)
		os.Exit(1)
	}
	defer s.CloseDB()

	err = foundry.AttachOpenFoundry(e, s.Srv)
	if err != nil {
		slog.Error("attach open foundry", "error", err)
		os.Exit(1)
	}

	// cc-agent 方式
	sessions.Attach(e)
	chat.Attach(e)

	httpSrv := &http.Server{Addr: ":" + env.String("PORT", "4000"), Handler: e}
	serve.ServeHTTP(context.Background(), httpSrv)
}
