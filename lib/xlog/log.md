### 使用模式

```go
func Demo(ctx ctx.Context) {
	ctx := log.WithRequestID(context.Background(), "123")
	logger := slog.With(slog.String(log.MOD, "module-name"))

	slog.InfoContext(ctx, "hello2", slog.String("test", "Request 级"))
	slog.InfoContext(ctx, "hello3", slog.String("test", "Event 级"))

	logger.Info("模块但没有ctx", slog.String("test", "Component 级"))
	logger.InfoContext(ctx, "模块但有ctx", slog.String("test", "Component 级"))
}
```

### 核心架构图

```text
               ┌────────────────────────────────────────────────────────┐
               │                    HTTP / Task Context                 │
               │  req_id / trace_id / tenant_id / user_id               │
               └──────────────────────────┬─────────────────────────────┘
                                          │
                                          ▼
┌───────────────────────────────────────────────────────────────────────────────┐
│                              slog.InfoContext()                               │
└─────────────────────────────────────────┬─────────────────────────────────────┘
                                          │
                                          ▼
┌───────────────────────────────────────────────────────────────────────────────┐
│                             ContextAwareHandler                               │
│                   ( 自动提取 Context 属性并注入 Log Record )                     │
└─────────────────────────────────────────┬─────────────────────────────────────┘
                                          │
                                          ▼
┌───────────────────────────────────────────────────────────────────────────────┐
│                             slog.JSONHandler                                  │
│                 Stdout -> Vector -> VictoriaLogs / ES                         │
└───────────────────────────────────────────────────────────────────────────────┘

```

---

### 字段三层分级模型

| 层级 | 覆盖范围 | 管理方式 | 典型字段 | 代码示例 |
| --- | --- | --- | --- | --- |
| **Request 级** | 单次请求/任务全局 | **Context + Custom Handler** 自动注入 | `req_id`, `trace_id`, `tenant_id` | `slog.InfoContext(ctx, ...)` |
| **Component 级** | 模块/服务生命周期 | **`logger.With()`** 实例化绑定 | `module`, `feature`, `component` | `var log = baseLog.With("module", "pay")` |
| **Event 级** | 单条日志事件 | **`InfoContext` 参数** 显式传入 | `order_id`, `amount`, `error` | `log.InfoContext(ctx, "paid", "amount", 100)` |

---

### 工程规范六金律

1. **Context 贯穿**：凡属于 Request/Task 生命周期的函数，首个参数必须为 `ctx context.Context`。
2. **Context 日志方法**：凡有 `ctx` 的地方，一律调用 `slog.InfoContext` / `ErrorContext`，禁止使用 `slog.Info`。
3. **职责分离**：Context 只负责传递 Identity（如 `req_id`），绝不直接存储 `*slog.Logger` 实例。
4. **轻量传递**：禁止在函数调用栈中逐层传递 `*slog.Logger`。
5. **并发隔离**：派生 Goroutine 时，若任务生命周期独立于原 HTTP 请求，需复制必要 Identity 并绑定至 `context.Background()`。
6. **类型安全**：日志 Key/Value 优先使用原生交替参数或 `slog.Attr`，避免传递裸 `map[string]any`。

