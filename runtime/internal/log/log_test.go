package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/internal/log"
)

func TestLog1(t *testing.T) {
	log.Init()

	ctx := log.WithRequestID(context.Background(), "123")
	logger := slog.With(slog.String(log.MOD, "module-name"))

	slog.InfoContext(ctx, "hello2", slog.String("test", "Request 级"))
	slog.InfoContext(ctx, "hello3", slog.String("test", "Event 级"))

	logger.Info("模块但没有ctx", slog.String("test", "Component 级"))
	logger.InfoContext(ctx, "模块但有ctx", slog.String("test", "Component 级"))
}

func TestContextHandler_WithPreservesReqID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(log.ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)}).
		With(slog.String(log.MOD, "module-name"))

	ctx := log.WithRequestID(context.Background(), "123")
	logger.InfoContext(ctx, "模块但有ctx", slog.String("test", "Component 级"))

	out := buf.String()
	if !strings.Contains(out, `"reqid":"123"`) {
		t.Fatalf("expected reqid after slog.With, got: %s", out)
	}
	if !strings.Contains(out, `"module":"module-name"`) {
		t.Fatalf("expected module after slog.With, got: %s", out)
	}
}
