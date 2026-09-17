package log

import (
	"context"
	"log/slog"
)

type ctxKey string

const (
	reqIDKey   ctxKey = "reqid"
	moduleKey  ctxKey = "module"
	featureKey ctxKey = "feature"
	appKey     ctxKey = "app"
	loggerKey  ctxKey = "logger"
)

// VictoriaLogs: _stream_fields=module,level,feature
const (
	FReqID   = "reqid"   // http request id
	FModule  = "module"  // http request module
	FFeature = "feature" // http request feature
)

type field struct {
	Key  ctxKey
	Name string
}

var contextFields = []field{
	{reqIDKey, FReqID},
	{moduleKey, FModule},
	{featureKey, FFeature},
}

// WithLogger 将 logger 添加到 context 中
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, loggerKey, logger)
}

// FromCtx 新建一个 logger 对象，从 context 中获取数据并添加到 logger 中
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}

	logger := slog.Default()
	if ctx == nil {
		return logger
	}

	attrs := make([]any, 0, len(contextFields))

	for _, f := range contextFields {
		if v, ok := ctx.Value(f.Key).(string); ok && v != "" {
			attrs = append(attrs, slog.String(f.Name, v))
		}
	}

	if len(attrs) == 0 {
		return logger
	}

	return logger.With(attrs...)
}

func WithReqID(ctx context.Context, reqID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, reqIDKey, reqID)
}

func GetReqID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(reqIDKey).(string)
	return v
}

func WithModule(ctx context.Context, module string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, moduleKey, module)
}

func GetModule(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(moduleKey).(string)
	return v
}

func WithFeature(ctx context.Context, feature string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, featureKey, feature)
}

func GetFeature(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(featureKey).(string)
	return v
}

func WithApp(ctx context.Context, app string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, appKey, app)
}

func GetApp(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(appKey).(string)
	return v
}
