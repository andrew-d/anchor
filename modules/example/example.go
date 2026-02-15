// Package example provides an Anchor module that logs a message whenever the
// configured message changes. It keeps no state on disk; applying the same
// message twice produces no log output.
//
// The configuration kind is "example.Config" with entries keyed by an
// arbitrary name:
//
//	{
//	  "message": "Hello, world!"
//	}
package example

import (
	"context"
	"log/slog"

	"github.com/andrew-d/anchor"
)

// Config holds a single message entry.
type Config struct {
	Message string `json:"message"`
}

func (Config) Kind() string { return "example.Config" }

// Module logs message changes.
type Module struct {
	// applied, if non-nil, receives a value after each watch loop
	// iteration. Tests use this to know when the watcher has processed a
	// state delivery.
	applied chan struct{}

	logger *slog.Logger
	store  *anchor.TypedStore[Config]
}

func (m *Module) Name() string { return "example" }

func (m *Module) Init(_ context.Context, ic anchor.InitContext) error {
	m.logger = ic.Logger
	m.store = anchor.Register[Config](ic.App)

	ic.Go(func(ctx context.Context) {
		m.watchLoop(ctx)
	})
	return nil
}

func (m *Module) watchLoop(ctx context.Context) {
	w := anchor.WatchStore(m.store)
	defer w.Stop()

	// last tracks the most recently applied message for each key so we
	// only log when something actually changes.
	last := make(map[string]string)

	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-w.State():
			if !ok {
				return
			}

			for key, cfg := range state {
				prev, existed := last[key]
				if existed && prev == cfg.Message {
					continue
				}
				m.logger.Info("message changed", "key", key, "message", cfg.Message)
				last[key] = cfg.Message
			}

			// Log removals for keys that disappeared from state.
			for key := range last {
				if _, ok := state[key]; !ok {
					m.logger.Info("message removed", "key", key)
					delete(last, key)
				}
			}

			if m.applied != nil {
				m.applied <- struct{}{}
			}
		}
	}
}

// Verify interface compliance.
var _ anchor.Module = (*Module)(nil)
