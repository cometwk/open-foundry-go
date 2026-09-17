package orm

import (
	"fmt"
	"log/slog"

	pkglog "github.com/openfoundry/lib/xlog"
	"xorm.io/xorm/log"
	xormlog "xorm.io/xorm/log"
)

// 1. 采用 session.MustLogSQL() 代替 SKIP_LOG_SQL
// 2. WithReqID 采用 pkglog

// // WithReqID stores the request ID in context.
// // Delegates to log package for unified context key management.
// func WithReqID(ctx context.Context, reqID string) context.Context {
// 	return pkglog.WithReqID(ctx, reqID)
// }

// // GetReqID retrieves the request ID from context.
// // Delegates to log package for unified context key management.
// func GetReqID(ctx context.Context) string {
// 	return pkglog.GetReqID(ctx)
// }

// 将 xorm 的日志适配到 slog
type XormLogrus struct {
	logger   *slog.Logger
	levelVar *slog.LevelVar
}

var _ xormlog.ContextLogger = &XormLogrus{}

func NewXormLogrus(logger *slog.Logger) *XormLogrus {
	lv := &slog.LevelVar{}
	lv.Set(slog.LevelDebug)
	return &XormLogrus{
		logger:   logger,
		levelVar: lv,
	}
}

func (x *XormLogrus) BeforeSQL(context xormlog.LogContext) {
}

// only invoked when IsShowSQL is true
func (x *XormLogrus) AfterSQL(context xormlog.LogContext) {
	if !x.logger.Enabled(context.Ctx, slog.LevelDebug) {
		// 如果日志级别不是 DebugLevel，则不打印 SQL
		return
	}

	v := context.Ctx.Value(log.SessionShowSQLKey)
	if showSQL, ok := v.(bool); ok && showSQL == false {
		// 临时关闭 SQL 日志, 避免日志风暴
		return
	}

	reqid := pkglog.RequestID(context.Ctx)

	// 设置每个元素的最大长度
	const maxContentLength = 100

	// 截断每个元素的内容
	truncatedArgs := make([]any, len(context.Args))
	for i, arg := range context.Args {
		argStr := fmt.Sprintf("%v", arg)
		if len(argStr) > maxContentLength {
			truncatedArgs[i] = argStr[:maxContentLength] + "..." // 添加省略号表示截断
		} else {
			truncatedArgs[i] = argStr
		}
	}

	sessionID := context.Ctx.Value(log.SessionIDKey)
	// logger := pkglog.FromCtx(context.Ctx)
	// logger.Debug(fmt.Sprintf("[SQL %s] %s %v", sessionID, context.SQL, truncatedArgs),
	// 	"exec_time", context.ExecuteTime.Milliseconds(),
	// 	"reqid", reqid,
	// 	// "tx", sessionID,
	// )
	slog.DebugContext(context.Ctx, fmt.Sprintf("[SQL %s] %s %v", sessionID, context.SQL, truncatedArgs),
		slog.String("exec_time", context.ExecuteTime.String()),
		slog.String("reqid", reqid),
		// "tx", sessionID,
	)
}

// 实现 xorm 的 log.Logger 接口
func (x *XormLogrus) Debug(v ...any) {
	x.logger.Debug(fmt.Sprint(v...))
}

func (x *XormLogrus) Debugf(format string, v ...any) {
	x.logger.Debug(fmt.Sprintf(format, v...))
}

func (x *XormLogrus) Error(v ...any) {
	x.logger.Error(fmt.Sprint(v...))
}

func (x *XormLogrus) Errorf(format string, v ...any) {
	x.logger.Error(fmt.Sprintf(format, v...))
}

func (x *XormLogrus) Info(v ...any) {
	x.logger.Info(fmt.Sprint(v...))
}

func (x *XormLogrus) Infof(format string, v ...any) {
	x.logger.Info(fmt.Sprintf(format, v...))
}

func (x *XormLogrus) Warn(v ...any) {
	x.logger.Warn(fmt.Sprint(v...))
}

func (x *XormLogrus) Warnf(format string, v ...any) {
	x.logger.Warn(fmt.Sprintf(format, v...))
}

func (x *XormLogrus) Level() xormlog.LogLevel {
	switch x.levelVar.Level() {
	case slog.LevelDebug:
		return xormlog.LOG_DEBUG
	case slog.LevelInfo:
		return xormlog.LOG_INFO
	case slog.LevelWarn:
		return xormlog.LOG_WARNING
	case slog.LevelError:
		return xormlog.LOG_ERR
	default:
		return xormlog.LOG_OFF
	}
}

func (x *XormLogrus) SetLevel(l xormlog.LogLevel) {
	switch l {
	case xormlog.LOG_DEBUG:
		x.levelVar.Set(slog.LevelDebug)
	case xormlog.LOG_INFO:
		x.levelVar.Set(slog.LevelInfo)
	case xormlog.LOG_WARNING:
		x.levelVar.Set(slog.LevelWarn)
	case xormlog.LOG_ERR:
		x.levelVar.Set(slog.LevelError)
	case xormlog.LOG_OFF:
		x.levelVar.Set(slog.Level(100)) // effectively off
	}
}

func (x *XormLogrus) ShowSQL(show ...bool) {
}

func (x *XormLogrus) IsShowSQL() bool {
	return true
}
