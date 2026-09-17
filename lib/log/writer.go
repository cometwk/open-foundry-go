package log

import (
	"log/slog"

	"gopkg.in/natefinch/lumberjack.v2"
)

func NewRotateFileHandler(filepath string) slog.Handler {
	// 日志文件
	rotate_writter := &lumberjack.Logger{
		Filename:   filepath,
		MaxSize:    20,   // 每个日志文件最大大小 (单位: MB)
		MaxBackups: 50,   // 保留旧日志文件的最大个数
		MaxAge:     365,  // 保留旧日志文件的最大天数 (天)
		Compress:   true, // 是否压缩/gzip 旧日志文件
		LocalTime:  true, // 使用本地时间命名切分后的文件（默认是 UTC）
	}

	return slog.NewJSONHandler(rotate_writter, opts)
}
