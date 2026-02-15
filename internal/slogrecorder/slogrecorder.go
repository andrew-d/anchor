// Package slogrecorder provides a test helper that captures slog records.
package slogrecorder

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// Record holds a captured slog record for test assertions.
type Record struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

// Handler is a [slog.Handler] that captures log records for testing.
type Handler struct {
	mu   sync.Mutex
	recs []Record
}

func (h *Handler) Enabled(context.Context, slog.Level) bool { return true }
func (h *Handler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *Handler) WithGroup(string) slog.Handler             { return h }

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	rec := Record{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make(map[string]string),
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	h.recs = append(h.recs, rec)
	h.mu.Unlock()
	return nil
}

// Records returns a snapshot of all captured records.
func (h *Handler) Records() []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.recs)
}

// Logger returns a new [slog.Logger] that writes to this handler.
func (h *Handler) Logger() *slog.Logger {
	return slog.New(h)
}
