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
	typeKey string     // full Go type path, for error messages
	newFn   func() any // returns a pointer to a new zero value of the type
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
//
// List and Get return the resolved view for this node: for each base key,
// only the highest-priority matching scope is returned.
type TypedStore[T Kinded] struct {
	app  *App
	kind string
}

// Get returns the resolved value for the given key from local FSM state.
// It collects all scoped entries for the key and returns the one with the
// highest priority that matches this node.
// Returns the zero value of T and no error if no matching entry exists.
func (s *TypedStore[T]) Get(key string) (T, error) {
	var zero T
	f := (*fsm)(s.app)
	entries, err := f.fsmGetAllScopes(s.kind, key)
	if err != nil {
		return zero, err
	}
	raw, err := resolveEntries(entries, s.app.nodeInfo)
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

// List returns all entries for this kind from local FSM state, resolved
// for this node. For each base key, only the highest-priority matching
// scope is returned.
func (s *TypedStore[T]) List() (map[string]T, error) {
	f := (*fsm)(s.app)
	allEntries, err := f.fsmListAll(s.kind)
	if err != nil {
		return nil, err
	}

	// Group by key.
	byKey := make(map[string][]fsmEntry)
	for _, e := range allEntries {
		byKey[e.Key] = append(byKey[e.Key], e)
	}

	result := make(map[string]T, len(byKey))
	for key, entries := range byKey {
		raw, err := resolveEntries(entries, s.app.nodeInfo)
		if err != nil {
			return nil, fmt.Errorf("resolve key %q: %w", key, err)
		}
		if raw == nil {
			continue
		}
		var val T
		if err := json.Unmarshal(raw, &val); err != nil {
			return nil, fmt.Errorf("unmarshal key %q: %w", key, err)
		}
		result[key] = val
	}
	return result, nil
}

// Set sets a value at global scope through Raft consensus. Only works on the leader.
func (s *TypedStore[T]) Set(key string, value T) error {
	return s.SetScoped("", key, value)
}

// SetScoped sets a value at the given scope through Raft consensus. Only works on the leader.
// An empty scope string means global scope.
func (s *TypedStore[T]) SetScoped(scope string, key string, value T) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	cmd := Command{
		Type:  CmdSet,
		Kind:  s.kind,
		Scope: scope,
		Key:   key,
		Value: data,
	}
	return s.app.applyCommand(cmd)
}

// Delete deletes a key at global scope through Raft consensus. Only works on the leader.
func (s *TypedStore[T]) Delete(key string) error {
	return s.DeleteScoped("", key)
}

// DeleteScoped deletes a key at the given scope through Raft consensus. Only works on the leader.
// An empty scope string means global scope.
func (s *TypedStore[T]) DeleteScoped(scope string, key string) error {
	cmd := Command{
		Type:  CmdDelete,
		Kind:  s.kind,
		Scope: scope,
		Key:   key,
	}
	return s.app.applyCommand(cmd)
}

// WatchStore returns a watcher that delivers the full state (all entries) for
// a kind whenever it changes. The first delivery happens immediately.
func WatchStore[T Kinded](store *TypedStore[T]) *Watcher[map[string]T] {
	entry := store.app.watches.subscribe(store.kind)
	return newWatcher(store.app.watches, entry, store.List)
}

// WatchKey returns a watcher that delivers the value for a single key whenever
// it changes. The first delivery happens immediately.
func WatchKey[T Kinded](store *TypedStore[T], key string) *Watcher[T] {
	entry := store.app.watches.subscribe(store.kind)
	return newWatcher(store.app.watches, entry, func() (T, error) {
		return store.Get(key)
	})
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

// resolveEntries picks the highest-priority entry that matches the node info.
// Returns nil if no entry matches.
func resolveEntries(entries []fsmEntry, info NodeInfo) (json.RawMessage, error) {
	var best *fsmEntry
	bestPriority := -1
	for i := range entries {
		scope, err := ParseScope(entries[i].Scope)
		if err != nil {
			return nil, fmt.Errorf("parse scope %q: %w", entries[i].Scope, err)
		}
		if !scope.Matches(info) {
			continue
		}
		p := scope.Priority()
		if p > bestPriority {
			bestPriority = p
			best = &entries[i]
		}
	}
	if best == nil {
		return nil, nil
	}
	return best.Value, nil
}

// Verify interface compliance.
var _ raft.FSM = (*fsm)(nil)
