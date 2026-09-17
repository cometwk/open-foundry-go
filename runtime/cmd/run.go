package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/internal/serve"
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

	r := serve.NewChiRouter()
	srv.Handler(r)

	httpSrv := &http.Server{Addr: addr, Handler: r}
	return serve.ServeHTTP(ctx, httpSrv)
}

func openAPI() (*api.Server, func(), error) {
	if conf == nil {
		return nil, nil, fmt.Errorf("config required")
	}
	b, err := bootstrap.Open(conf)
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
