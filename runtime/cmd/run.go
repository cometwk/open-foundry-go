package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/internal/serve"
	"github.com/openfoundry/runtime/mcp"
	"github.com/openfoundry/runtime/obda"
)

func run(ctx context.Context, addr string) error {
	srv, closeDB, err := openAPI()
	if err != nil {
		return err
	}
	defer closeDB()
	if addr == "" {
		addr = ":4000"
	}

	e := serve.NewEcho()

	// 安装 ODL API 路由
	srv.Handler(e.Group(""))

	// 安装 MCP 路由
	mcp, err := mcp.NewMCP(ctx)
	if err != nil {
		return err
	}
	e.POST("/mcp", echo.WrapHandler(mcp.Handler()))

	httpSrv := &http.Server{Addr: addr, Handler: e}
	return serve.ServeHTTP(ctx, httpSrv)
}

func openAPI() (*api.Server, func(), error) {
	if conf == nil {
		return nil, nil, fmt.Errorf("config required")
	}
	b, err := bootstrap.New(conf)
	if err != nil {
		slog.Error("open failed", "error", err)
		return nil, nil, err
	}
	if err := b.ApplySchema(); err != nil {
		_ = b.Close()
		slog.Error("apply schema failed", "error", err)
		return nil, nil, err
	}
	compiled, err := compileMapping(b)
	if err != nil {
		_ = b.Close()
		return nil, nil, err
	}
	e, err := engine.NewWithCompiled(b.SPI, b.Ontology, compiled)
	if err != nil {
		_ = b.Close()
		slog.Error("engine.New failed", "error", err)
		return nil, nil, err
	}
	srv, err := api.New(e)
	if err != nil {
		_ = b.Close()
		slog.Error("api.New failed", "error", err)
		return nil, nil, err
	}
	return srv, func() { _ = b.Close() }, nil
}

func compileMapping(b *bootstrap.Bootstrap) (*obda.Compiled, error) {
	if b == nil || len(b.Mappings) == 0 || b.Mappings[0].Doc == nil {
		return nil, fmt.Errorf("mapping required")
	}
	return obda.Compile(b.Schema, b.Mappings[0].Doc)
}
