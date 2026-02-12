package anchor

import "context"

// Module is a user-defined component that registers types and reacts to
// configuration changes. Modules call [Register] in Init to create typed
// stores and set up watches.
type Module interface {
	Name() string
	Init(ctx context.Context, app *App) error
}
