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

// Module is a user-defined component that registers types and reacts to
// configuration changes. Modules call [Register] in Init to create typed
// stores and set up watches.
type Module interface {
	Name() string
	Init(ctx context.Context, ic InitContext) error
}
