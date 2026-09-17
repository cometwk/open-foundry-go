package testutil

import (
	"context"
	"fmt"
)

const RecordingLoggerKey = "recording_logger"

type Log interface {
	Logf(key string, step string, format string, args ...any)
	Log(key string, step string, args ...string)
	Steps(key string) [][]string
}

type recordingLogger struct {
	steps    [][]string
	stepsMap map[string][][]string
}

var _ Log = &recordingLogger{}

func (r *recordingLogger) Log(k, step string, args ...string) {
	args = append([]string{k, step}, args...)
	// str := strings.Join(args, " ")
	// logrus.Info(str)

	if k == "" {
		r.steps = append(r.steps, args)
	} else {
		r.stepsMap[k] = append(r.stepsMap[k], args)
	}
}

func (r *recordingLogger) Logf(k, step, format string, args ...any) {
	x := fmt.Sprintf(format, args...)
	r.Log(k, step, x)
}
func (r *recordingLogger) Steps(key string) [][]string {
	if key == "" {
		return r.steps
	}
	return r.stepsMap[key]
}

func NewRecordingLogger() Log {
	return &recordingLogger{
		steps:    nil,
		stepsMap: make(map[string][][]string),
	}

}

type contextKey string

var recordingLoggerKey = contextKey("recordingLogger")

func WithRecordingLogger(ctx context.Context, logger Log) context.Context {
	return context.WithValue(ctx, recordingLoggerKey, logger)
}

func RecordingLoggerFrom(ctx context.Context) Log {
	logger, ok := ctx.Value(recordingLoggerKey).(Log)
	if !ok {
		return nil
	}
	return logger
}
