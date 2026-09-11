package log

import (
	"io"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/openfoundry/runtime/internal/log/xfmt"
)

func jsonLogReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		return a
	case slog.LevelKey:
		level, ok := a.Value.Any().(slog.Level)
		if !ok {
			return a
		}
		return slog.String(a.Key, strings.ToLower(level.String()))
	case slog.MessageKey:
		return slog.String("message", a.Value.String())
	case slog.SourceKey:
		source, ok := a.Value.Any().(*slog.Source)
		if !ok {
			return a
		}
		file := path.Base(source.File) + ":" + strconv.Itoa(source.Line)
		return slog.String("file", file)
	default:
		return a
	}
}

// Init 初始化日志 黑白输出, 输出格式 h = json, text, discard
func Init(h ...string) {
	initlog(true, h...)
}

// InitDebug 初始化日志, 彩色输出,  输出格式 h = json, text, discard
func InitDebug(h ...string) {
	initlog(false, h...)
}

// 创建一个可变级别的 LevelVar（方便后续随时动态修改级别）
var programLevel = new(slog.LevelVar)

func SetLevel(level slog.Level) {
	programLevel.Set(level)
}

func GetLevel() slog.Level {
	return programLevel.Level().Level()
}

func initlog(c bool, t ...string) {
	color.NoColor = c
	handler := NewDebugHandler(t...)
	InitLogger(handler)
}

var opts = &slog.HandlerOptions{
	Level:       programLevel,
	AddSource:   true,
	ReplaceAttr: jsonLogReplaceAttr,
}

func NewDebugHandler(t ...string) slog.Handler {
	programLevel.Set(slog.LevelDebug)

	h := ""
	if len(t) > 0 {
		h = t[0]
	}
	var handler slog.Handler
	switch h {
	case "discard":
		handler = slog.NewTextHandler(io.Discard, nil)
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(xfmt.NewFmtMainPrinter(os.Stderr), opts)
	}
	return handler
}
