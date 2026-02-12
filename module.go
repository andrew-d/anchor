package anchor

import (
	"context"
	"log/slog"
)

// InitContext is passed to [Module.Init] and provides access to the App and
// other potentially-useful options.
//
// All values are non-nil unless otherwise specified.
type InitContext struct {
	// App is the anchor application instance.
	App *App

	// Logger is a [slog.Logger] pre-configured with a "module" attribute
	// set to the module's name. Modules should store it and use it for all
	// logging.
	Logger *slog.Logger
}

// Go starts a background goroutine that is tracked by the App. During
// [App.Shutdown], the context passed to fn is canceled and Shutdown blocks
// until all goroutines started with Go have returned.
func (ic InitContext) Go(fn func(context.Context)) {
	ctx := ic.App.ctx
	ic.App.wg.Go(func() { fn(ctx) })
}

// Module is a user-defined component that registers types and reacts to
// configuration changes. Modules call [Register] in Init to create typed
// stores and set up watches.
type Module interface {
	Name() string
	Init(ctx context.Context, ic InitContext) error
}
