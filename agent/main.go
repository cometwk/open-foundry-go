package main

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/openfoundry/agent/internal/chat"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/serve"
	"github.com/openfoundry/lib/xlog"
)

func attachAgent(e *echo.Echo) error {
	model := chat.NewModel()
	agent, err := chat.NewAgent(model)
	if err != nil {
		return err
	}
	e.POST("/api/chat", echo.WrapHandler(chat.NewChatHandler(agent, nil)))
	return nil
}

func main() {
	// model := NewModel()
	// agent, err := newAgent(model)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// mux := http.NewServeMux()
	// mux.Handle("POST /api/chat", newChatHandler(agent))

	// server := http.Server{
	// 	Addr:              ":4000",
	// 	Handler:           mux,
	// 	ReadHeaderTimeout: 5 * time.Second,
	// }

	// log.Println("listening on http://localhost:4000")
	// log.Fatal(server.ListenAndServe())

	///

	xlog.InitDebug()

	e := serve.NewEcho()

	chat.AttachOpenFoundry(e)
	attachAgent(e)

	httpSrv := &http.Server{Addr: ":" + env.String("PORT", "4000"), Handler: e}
	serve.ServeHTTP(context.Background(), httpSrv)
}
