package anchor

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/andrew-d/anchor/internal/typekey"
	"github.com/hashicorp/raft"
)

const raftTimeout = 10 * time.Second

// Kinded is the interface that registered configuration types must implement.
// Kind returns a short, HTTP-safe name (e.g. "sshkeys.Config") used as the
// storage key in the database, commands, events, and HTTP URLs.
type Kinded interface {
	Kind() string
}

// kindInfo holds the metadata for a registered kind, used by the HTTP layer
// for validation.
type kindInfo struct {
	typeKey string         // full Go type path, for error messages
	newFn   func() any     // returns a pointer to a new zero value of the type
}

// Register registers a new configuration kind with the app and returns a
// [TypedStore] for type-safe access. It must be called during module Init,
// before the app starts.
//
// The kind string is derived from T's Kind() method. Register panics if the
// same Go type or the same kind string is registered twice.
func Register[T Kinded](app *App) *TypedStore[T] {
	var zero T
	kind := zero.Kind()
	tk := typekey.Of[T]()

	// Check for duplicate kind string from a different type.
	if existing, ok := app.kinds[kind]; ok {
		panic(fmt.Sprintf("anchor.Register: kind %q already registered by %s (new: %s)", kind, existing.typeKey, tk))
	}

	// Check for duplicate Go type registered under a different kind.
	for existingKind, info := range app.kinds {
		if info.typeKey == tk {
			panic(fmt.Sprintf("anchor.Register: type %s already registered as kind %q", tk, existingKind))
		}
	}

	app.kinds[kind] = kindInfo{
		typeKey: tk,
		newFn:   func() any { return new(T) },
	}
	return &TypedStore[T]{
		app:  app,
		kind: kind,
	}
}

// TypedStore provides type-safe access to a single configuration kind.
// Reads go directly to the local FSM state (eventual consistency).
// Writes are applied through Raft (leader only).
type TypedStore[T Kinded] struct {
	app  *App
	kind string
}

// Get returns the value for the given key from local FSM state.
// Returns the zero value of T and no error if the key does not exist.
func (s *TypedStore[T]) Get(key string) (T, error) {
	var zero T
	f := (*fsm)(s.app)
	raw, err := f.fsmGet(s.kind, key)
	if err != nil {
		return zero, err
	}
	if raw == nil {
		return zero, nil
	}
	var val T
	if err := json.Unmarshal(raw, &val); err != nil {
		return zero, err
	}
	return val, nil
}

// List returns all entries for this kind from local FSM state.
func (s *TypedStore[T]) List() (map[string]T, error) {
	f := (*fsm)(s.app)
	rawMap, err := f.fsmList(s.kind)
	if err != nil {
		return nil, err
	}
	result := make(map[string]T, len(rawMap))
	for k, raw := range rawMap {
		var val T
		if err := json.Unmarshal(raw, &val); err != nil {
			return nil, fmt.Errorf("unmarshal key %q: %w", k, err)
		}
		result[k] = val
	}
	return result, nil
}

// Set sets a value through Raft consensus. Only works on the leader.
func (s *TypedStore[T]) Set(key string, value T) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	cmd := Command{
		Type:  CmdSet,
		Kind:  s.kind,
		Key:   key,
		Value: data,
	}
	b, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	f := s.app.raft.Apply(b, raftTimeout)
	if err := f.Error(); err != nil {
		return err
	}
	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}
	return nil
}

// Delete deletes a key through Raft consensus. Only works on the leader.
func (s *TypedStore[T]) Delete(key string) error {
	cmd := Command{
		Type: CmdDelete,
		Kind: s.kind,
		Key:  key,
	}
	b, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	f := s.app.raft.Apply(b, raftTimeout)
	if err := f.Error(); err != nil {
		return err
	}
	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}
	return nil
}

// Watch returns a typed watcher that receives events for this kind.
func (s *TypedStore[T]) Watch() *TypedWatcher[T] {
	w := s.app.watches.subscribe(s.kind)
	return newTypedWatcher[T](w)
}

// applyCommand marshals and applies a command through Raft.
// Used by the HTTP layer for untyped operations.
func (a *App) applyCommand(cmd Command) error {
	b, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	f := a.raft.Apply(b, raftTimeout)
	if err := f.Error(); err != nil {
		return err
	}
	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}
	return nil
}

// Verify interface compliance.
var _ raft.FSM = (*fsm)(nil)
