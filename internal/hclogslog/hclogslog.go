// Package hclogslog adapts a [*slog.Logger] to the [hclog.Logger] interface
// used by hashicorp/raft.
package hclogslog

import (
	"io"
	"log"
	"log/slog"

	"github.com/hashicorp/go-hclog"
)

// LevelTrace is the slog level used for hclog Trace messages.
// It sits below [slog.LevelDebug].
const LevelTrace = slog.LevelDebug - 4

// New returns an [hclog.Logger] that delegates to logger.
func New(logger *slog.Logger) hclog.Logger {
	return &adapter{logger: logger}
}

type adapter struct {
	logger *slog.Logger
	name   string
}

func hclogToSlog(level hclog.Level) slog.Level {
	switch level {
	case hclog.Trace:
		return LevelTrace
	case hclog.Debug:
		return slog.LevelDebug
	case hclog.Info:
		return slog.LevelInfo
	case hclog.Warn:
		return slog.LevelWarn
	case hclog.Error:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (a *adapter) Log(level hclog.Level, msg string, args ...any) {
	a.logger.Log(nil, hclogToSlog(level), msg, args...)
}

func (a *adapter) Trace(msg string, args ...any) {
	a.logger.Log(nil, LevelTrace, msg, args...)
}

func (a *adapter) Debug(msg string, args ...any) {
	a.logger.Debug(msg, args...)
}

func (a *adapter) Info(msg string, args ...any) {
	a.logger.Info(msg, args...)
}

func (a *adapter) Warn(msg string, args ...any) {
	a.logger.Warn(msg, args...)
}

func (a *adapter) Error(msg string, args ...any) {
	a.logger.Error(msg, args...)
}

func (a *adapter) IsTrace() bool { return a.logger.Enabled(nil, LevelTrace) }
func (a *adapter) IsDebug() bool { return a.logger.Enabled(nil, slog.LevelDebug) }
func (a *adapter) IsInfo() bool  { return a.logger.Enabled(nil, slog.LevelInfo) }
func (a *adapter) IsWarn() bool  { return a.logger.Enabled(nil, slog.LevelWarn) }
func (a *adapter) IsError() bool { return a.logger.Enabled(nil, slog.LevelError) }

func (a *adapter) ImpliedArgs() []any { return nil }

func (a *adapter) With(args ...any) hclog.Logger {
	return &adapter{
		logger: a.logger.With(args...),
		name:   a.name,
	}
}

func (a *adapter) Name() string { return a.name }

func (a *adapter) Named(name string) hclog.Logger {
	combined := name
	if a.name != "" {
		combined = a.name + "." + name
	}
	return &adapter{
		logger: a.logger.With("logger_name", combined),
		name:   combined,
	}
}

func (a *adapter) ResetNamed(name string) hclog.Logger {
	return &adapter{
		logger: a.logger.With("logger_name", name),
		name:   name,
	}
}

func (a *adapter) SetLevel(hclog.Level) {}

func (a *adapter) GetLevel() hclog.Level { return hclog.NoLevel }

func (a *adapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return slog.NewLogLogger(a.logger.Handler(), slog.LevelInfo)
}

func (a *adapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return a.StandardLogger(opts).Writer()
}
