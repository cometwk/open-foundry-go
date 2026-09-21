package chat

import (
	"fmt"
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/bootstrap"
)

func attachOpenFoundry(echo *echo.Echo) error {
	srv, closeDB, err := CreateFoundryServer()
	if err != nil {
		return err
	}
	// defer closeDB()
	// if addr == "" {
	// 	addr = ":4000"
	// }
	_ = closeDB

	// e := serve.NewEcho()

	// 安装 ODL API 路由
	srv.Handler(echo.Group(""))

	// // 安装 MCP 路由
	// mcp, err := mcp.NewMCP(ctx)
	// if err != nil {
	// 	return err
	// }
	// e.POST("/mcp", echo.WrapHandler(mcp.Handler()))

	// httpSrv := &http.Server{Addr: addr, Handler: e}
	// return serve.ServeHTTP(ctx, httpSrv)
	return err
}

func CreateFoundryServer() (*api.Server, func(), error) {
	conf, err := bootstrap.LoadConfig("")
	if err != nil {
		return nil, nil, err
	}
	if conf == nil {
		return nil, nil, fmt.Errorf("config required")
	}
	b, err := bootstrap.New(conf)
	if err != nil {
		slog.Error("open failed", "error", err)
		return nil, nil, err
	}
	e, err := b.Open()
	if err != nil {
		_ = b.Close()
		slog.Error("open engine failed", "error", err)
		return nil, nil, err
	}
	srv, err := api.New(e)
	if err != nil {
		return nil, nil, err
	}
	return srv, func() { _ = b.Close() }, nil
}
