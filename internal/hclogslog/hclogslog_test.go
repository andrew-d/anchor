package hclogslog

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hashicorp/go-hclog"
)

type record struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

type captureHandler struct {
	records *[]record
	attrs   map[string]string
	level   slog.Level
}

func newCaptureHandler(level slog.Level) *captureHandler {
	return &captureHandler{
		records: &[]record{},
		attrs:   make(map[string]string),
		level:   level,
	}
}

func (h *captureHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := record{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make(map[string]string),
	}
	for k, v := range h.attrs {
		rec.Attrs[k] = v
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	*h.records = append(*h.records, rec)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := &captureHandler{
		records: h.records,
		attrs:   make(map[string]string),
		level:   h.level,
	}
	for k, v := range h.attrs {
		h2.attrs[k] = v
	}
	for _, a := range attrs {
		h2.attrs[a.Key] = a.Value.String()
	}
	return h2
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestLogLevels(t *testing.T) {
	tests := []struct {
		name      string
		logFunc   func(hclog.Logger)
		wantLevel slog.Level
	}{
		{"Trace", func(l hclog.Logger) { l.Trace("msg") }, LevelTrace},
		{"Debug", func(l hclog.Logger) { l.Debug("msg") }, slog.LevelDebug},
		{"Info", func(l hclog.Logger) { l.Info("msg") }, slog.LevelInfo},
		{"Warn", func(l hclog.Logger) { l.Warn("msg") }, slog.LevelWarn},
		{"Error", func(l hclog.Logger) { l.Error("msg") }, slog.LevelError},
		{"Log/Trace", func(l hclog.Logger) { l.Log(hclog.Trace, "msg") }, LevelTrace},
		{"Log/Warn", func(l hclog.Logger) { l.Log(hclog.Warn, "msg") }, slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newCaptureHandler(LevelTrace)
			logger := New(slog.New(h))

			tt.logFunc(logger)

			if len(*h.records) != 1 {
				t.Fatalf("got %d records, want 1", len(*h.records))
			}
			if (*h.records)[0].Level != tt.wantLevel {
				t.Errorf("level = %v, want %v", (*h.records)[0].Level, tt.wantLevel)
			}
			if (*h.records)[0].Message != "msg" {
				t.Errorf("message = %q, want %q", (*h.records)[0].Message, "msg")
			}
		})
	}
}

func TestIsLevel(t *testing.T) {
	h := newCaptureHandler(slog.LevelInfo)
	logger := New(slog.New(h))

	if logger.IsTrace() {
		t.Error("IsTrace should be false")
	}
	if logger.IsDebug() {
		t.Error("IsDebug should be false")
	}
	if !logger.IsInfo() {
		t.Error("IsInfo should be true")
	}
	if !logger.IsWarn() {
		t.Error("IsWarn should be true")
	}
	if !logger.IsError() {
		t.Error("IsError should be true")
	}
}

func TestWith(t *testing.T) {
	h := newCaptureHandler(LevelTrace)
	logger := New(slog.New(h))

	child := logger.With("key", "val")
	child.Info("hello")

	if len(*h.records) != 1 {
		t.Fatalf("got %d records, want 1", len(*h.records))
	}
	if (*h.records)[0].Attrs["key"] != "val" {
		t.Errorf("attr key = %q, want %q", (*h.records)[0].Attrs["key"], "val")
	}
}

func TestNamed(t *testing.T) {
	h := newCaptureHandler(LevelTrace)
	logger := New(slog.New(h))

	child := logger.Named("foo")
	if child.Name() != "foo" {
		t.Errorf("Name() = %q, want %q", child.Name(), "foo")
	}

	grandchild := child.Named("bar")
	if grandchild.Name() != "foo.bar" {
		t.Errorf("Name() = %q, want %q", grandchild.Name(), "foo.bar")
	}

	grandchild.Info("hello")
	if len(*h.records) != 1 {
		t.Fatalf("got %d records, want 1", len(*h.records))
	}
	if (*h.records)[0].Attrs["logger_name"] != "foo.bar" {
		t.Errorf("logger_name = %q, want %q", (*h.records)[0].Attrs["logger_name"], "foo.bar")
	}
}

func TestResetNamed(t *testing.T) {
	h := newCaptureHandler(LevelTrace)
	logger := New(slog.New(h))

	child := logger.Named("foo").ResetNamed("bar")
	if child.Name() != "bar" {
		t.Errorf("Name() = %q, want %q", child.Name(), "bar")
	}
}

func TestStandardWriter(t *testing.T) {
	h := newCaptureHandler(LevelTrace)
	logger := New(slog.New(h))

	w := logger.StandardWriter(nil)
	if w == nil {
		t.Fatal("StandardWriter returned nil")
	}
	if _, ok := w.(io.Writer); !ok {
		t.Fatal("StandardWriter does not implement io.Writer")
	}
}

func TestGetSetLevel(t *testing.T) {
	h := newCaptureHandler(LevelTrace)
	logger := New(slog.New(h))

	if logger.GetLevel() != hclog.NoLevel {
		t.Errorf("GetLevel() = %v, want NoLevel", logger.GetLevel())
	}
	// SetLevel is a no-op; just verify it doesn't panic.
	logger.SetLevel(hclog.Debug)
}
