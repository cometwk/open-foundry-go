package log

// import (
// 	"bytes"
// 	"context"
// 	"encoding/json"
// 	"io"
// 	"log/slog"
// 	"testing"

// 	"github.com/fatih/color"
// )

// func ctxWithCapture(t *testing.T) (context.Context, *bytes.Buffer) {
// 	t.Helper()
// 	var buf bytes.Buffer
// 	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
// 	ctx := WithLogger(context.Background(), slog.New(handler))
// 	return ctx, &buf
// }

// func setDefaultCapture(t *testing.T) *bytes.Buffer {
// 	t.Helper()
// 	var buf bytes.Buffer
// 	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
// 	return &buf
// }

// func parseLogAttrs(t *testing.T, buf *bytes.Buffer) map[string]any {
// 	t.Helper()
// 	var m map[string]any
// 	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
// 		t.Fatalf("unmarshal log: %v, raw=%q", err, buf.String())
// 	}
// 	return m
// }

// func TestFromContext_NilContext(t *testing.T) {
// 	logger := FromContext(nil)
// 	if logger == nil {
// 		t.Fatal("FromContext(nil) should return slog.Default(), not nil")
// 	}
// 	if logger != slog.Default() {
// 		t.Error("FromContext(nil) should return slog.Default()")
// 	}
// }

// func TestFromContext_EmptyContext(t *testing.T) {
// 	ctx := context.Background()
// 	logger := FromContext(ctx)
// 	if logger == nil {
// 		t.Fatal("FromContext(context.Background()) should return slog.Default(), not nil")
// 	}
// 	if logger != slog.Default() {
// 		t.Error("FromContext(context.Background()) should return slog.Default()")
// 	}
// }

// func TestFromContext_WithStoredLogger(t *testing.T) {
// 	ctx, buf := ctxWithCapture(t)
// 	customLogger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
// 		With(slog.String("custom", "value"))
// 	ctx = WithLogger(ctx, customLogger)
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["custom"] != "value" {
// 		t.Errorf("expected custom=value, got %v", attrs["custom"])
// 	}
// }

// func TestWithModule_NilContext(t *testing.T) {
// 	buf := setDefaultCapture(t)
// 	ctx := WithModule(nil, "test-module")
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["module"] != "test-module" {
// 		t.Errorf("expected module=test-module, got %v", attrs["module"])
// 	}
// }

// func TestWithModule_EmptyString(t *testing.T) {
// 	ctx, buf := ctxWithCapture(t)
// 	ctx = WithModule(ctx, "")
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["module"] != "default" {
// 		t.Errorf("empty module should fallback to 'default', got %v", attrs["module"])
// 	}
// }

// func TestWithFeature_EmptyString(t *testing.T) {
// 	ctx, buf := ctxWithCapture(t)
// 	ctx = WithFeature(ctx, "")
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["feature"] != "default" {
// 		t.Errorf("empty feature should fallback to 'default', got %v", attrs["feature"])
// 	}
// }

// func TestFieldOverride_LastWins(t *testing.T) {
// 	ctx, buf := ctxWithCapture(t)
// 	ctx = WithModule(ctx, "first")
// 	ctx = WithModule(ctx, "second")
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["module"] != "second" {
// 		t.Errorf("expected module=second (last wins), got %v", attrs["module"])
// 	}
// }

// func TestChain_WithModuleWithFeature(t *testing.T) {
// 	ctx, buf := ctxWithCapture(t)
// 	ctx = WithModule(ctx, "payment")
// 	ctx = WithFeature(ctx, "refund")
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["module"] != "payment" {
// 		t.Errorf("expected module=payment, got %v", attrs["module"])
// 	}
// 	if attrs["feature"] != "refund" {
// 		t.Errorf("expected feature=refund, got %v", attrs["feature"])
// 	}
// }

// func TestWithReqID(t *testing.T) {
// 	ctx, buf := ctxWithCapture(t)
// 	ctx = WithReqID(ctx, "req-123")
// 	FromContext(ctx).Info("test")

// 	attrs := parseLogAttrs(t, buf)
// 	if attrs["reqid"] != "req-123" {
// 		t.Errorf("expected reqid=req-123, got %v", attrs["reqid"])
// 	}
// }

// func TestGetReqID(t *testing.T) {
// 	ctx := context.Background()
// 	ctx = WithReqID(ctx, "req-456")
// 	reqID := GetReqID(ctx)
// 	if reqID != "req-456" {
// 		t.Errorf("expected req-456, got %s", reqID)
// 	}
// }

// func TestGetReqID_NilContext(t *testing.T) {
// 	reqID := GetReqID(nil)
// 	if reqID != "" {
// 		t.Errorf("GetReqID(nil) should return empty string, got %s", reqID)
// 	}
// }

// func TestLog1(t *testing.T) {
// 	color.NoColor = false
// 	InitDebug()

// 	log2 := slog.Default().With(slog.String("id", "123"))
// 	log2.Info("Benchmark log with caller info")
// 	log2.With(slog.String("feature", "feature")).Error("Warning log with caller info")
// 	log2.With(
// 		slog.String("error", "error"),
// 		slog.String("module", "ormx"),
// 		slog.String("reqid", "abc"),
// 		slog.String("id", "123"),
// 	).Error("Error log with caller info")
// }

// func TestLog2(t *testing.T) {
// 	InitDebugNoColor()
// 	slog.Info("Benchmark log without caller info")
// }

// func BenchmarkSlogWithSource(b *testing.B) {
// 	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
// 		Level:     slog.LevelInfo,
// 		AddSource: true,
// 	})
// 	logger := slog.New(handler)

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		logger.Info("Benchmark log with caller info")
// 	}
// }

// func BenchmarkSlogWithoutSource(b *testing.B) {
// 	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
// 		Level: slog.LevelInfo,
// 	})
// 	logger := slog.New(handler)

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		logger.Info("Benchmark log without caller info")
// 	}
// }
