# 日志场景

## 使用模式1

```go
var attrs = []any{
	slog.String("module", "email"),
}

func demo(ctx ctx.Context) {
	logger := FromContext(ctx).With(attrs...)
	logger.Info("demo")
}
```


