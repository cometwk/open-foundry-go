package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/obda"
)

func run(ctx context.Context, addr string) error {
	h, closeDB, err := openAPI()
	if err != nil {
		return err
	}
	defer closeDB()
	if addr == "" {
		addr = ":4000"
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{Addr: addr, Handler: h}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shCtx)
	}()

	slog.Info("listening", "addr", addr)
	err = httpSrv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func openAPI() (http.Handler, func(), error) {
	if conf == nil {
		return nil, nil, fmt.Errorf("config required")
	}
	b, err := bootstrap.Open(conf)
	if err != nil {
		slog.Error("open failed", "error", err)
		return nil, nil, err
	}
	if err := b.ApplySchema(); err != nil {
		_ = b.DB.Close()
		slog.Error("apply schema failed", "error", err)
		return nil, nil, err
	}
	compiled, err := compileMapping(b)
	if err != nil {
		_ = b.DB.Close()
		return nil, nil, err
	}
	e, err := engine.NewWithCompiled(b.SPI, b.Ontology, compiled)
	if err != nil {
		_ = b.DB.Close()
		slog.Error("engine.New failed", "error", err)
		return nil, nil, err
	}
	srv, err := api.New(e)
	if err != nil {
		_ = b.DB.Close()
		slog.Error("api.New failed", "error", err)
		return nil, nil, err
	}
	return srv.Handler(), func() { _ = b.DB.Close() }, nil
}

func compileMapping(b *bootstrap.Bootstrap) (*obda.Compiled, error) {
	if b == nil || len(b.Mappings) == 0 || b.Mappings[0].Doc == nil {
		return nil, fmt.Errorf("mapping required")
	}
	return obda.Compile(b.Schema, b.Mappings[0].Doc)
}
