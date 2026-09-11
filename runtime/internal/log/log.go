package log

import (
	"context"
	"log/slog"
)

// VictoriaLogs: _stream_fields=module,level,feature
const (
	MOD  string = "module"
	FEAT string = "feature"
	APP  string = "app"
)

type ctxKey string

const (
	reqIDKey  ctxKey = "reqid"
	loggerKey ctxKey = "logger"
)

// 1. Context 组装
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, reqIDKey, reqID)
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(reqIDKey).(string); ok {
		return v
	}
	return ""
}

// 2. Handler 拦截与自动注入
type ContextHandler struct {
	slog.Handler
}

func (h ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if reqID := RequestID(ctx); reqID != "" {
		r.AddAttrs(slog.String(string(reqIDKey), reqID))
	}
	return h.Handler.Handle(ctx, r)
}

func (h ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return ContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h ContextHandler) WithGroup(name string) slog.Handler {
	return ContextHandler{Handler: h.Handler.WithGroup(name)}
}

// 3. 初始化全局 Logger
func InitLogger(baseHandler slog.Handler) {
	slog.SetDefault(slog.New(ContextHandler{Handler: baseHandler}))
}
