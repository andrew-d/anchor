package anchor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// ChangeType identifies the kind of mutation that occurred.
type ChangeType int

const (
	ChangeSet ChangeType = iota
	ChangeDelete
)

// Event is emitted by the FSM on every Apply.
type Event struct {
	Change ChangeType
	Kind   string
	Key    string
	Value  json.RawMessage // nil for deletes

	raftIndex uint64
	ack       *acker
}

// Ack acknowledges the event, advancing the subscriber's cursor so that the
// event will not be redelivered. It is safe to call multiple times.
func (e *Event) Ack() {
	e.ack.ack()
}

// acker is a one-shot acknowledgment signal. It is allocated on the heap and
// shared by reference so that Event (a value type sent through channels) can
// be safely copied.
type acker struct {
	ch   chan struct{}
	once sync.Once
}

func newAcker() *acker {
	return &acker{ch: make(chan struct{})}
}

func (a *acker) ack()              { a.once.Do(func() { close(a.ch) }) }
func (a *acker) done() <-chan struct{} { return a.ch }

// Watcher receives events for a specific kind. It reads persisted events from
// SQLite one at a time, delivers each on the output channel, and waits for an
// Ack before advancing the cursor.
type Watcher struct {
	kind    string
	name    string
	hub     *watchHub
	out     chan Event
	signal  chan struct{}
	done    chan struct{}
	stopped chan struct{} // closed when drain exits
}

// Events returns the channel on which events are delivered.
func (w *Watcher) Events() <-chan Event {
	return w.out
}

// Stop stops the watcher and waits for the drain goroutine to exit. The
// events channel will be closed.
func (w *Watcher) Stop() {
	close(w.done)
	<-w.stopped
	w.hub.unsubscribe(w)
}

// drain reads events from the database and delivers them one at a time.
func (w *Watcher) drain() {
	defer close(w.stopped)
	defer close(w.out)

	// Read initial cursor position.
	var cursor int64
	err := w.hub.db.QueryRow(
		`SELECT pos FROM fsm_cursors WHERE name = ?`, w.name,
	).Scan(&cursor)
	if err != nil && err != sql.ErrNoRows {
		w.hub.logger.Error("failed to read watcher cursor", "watcher", w.name, "err", err)
		return
	}

	for {
		// Query next event after cursor.
		var e Event
		var change int
		var val string
		err := w.hub.db.QueryRow(
			`SELECT raft_index, change, key, value FROM fsm_events WHERE kind = ? AND raft_index > ? ORDER BY raft_index LIMIT 1`,
			w.kind, cursor,
		).Scan(&e.raftIndex, &change, &e.Key, &val)

		if err == sql.ErrNoRows {
			// No events available; wait for a signal or shutdown.
			select {
			case <-w.signal:
				continue
			case <-w.done:
				return
			}
		}
		if err != nil {
			w.hub.logger.Error("failed to query events", "watcher", w.name, "err", err)
			return
		}

		e.Change = ChangeType(change)
		e.Kind = w.kind
		if val != "" {
			e.Value = json.RawMessage(val)
		}
		e.ack = newAcker()

		// Deliver event (blocks until consumer takes it).
		select {
		case w.out <- e:
		case <-w.done:
			return
		}

		// Wait for ack. If done fires concurrently, still prefer ack so
		// the cursor advances for events the consumer already processed.
		select {
		case <-e.ack.done():
		case <-w.done:
			select {
			case <-e.ack.done():
			default:
				return
			}
		}

		// Advance cursor.
		cursor = int64(e.raftIndex)
		_, err = w.hub.db.Exec(
			`INSERT INTO fsm_cursors(name, pos) VALUES(?, ?) ON CONFLICT(name) DO UPDATE SET pos = excluded.pos`,
			w.name, cursor,
		)
		if err != nil {
			w.hub.logger.Error("failed to advance watcher cursor", "watcher", w.name, "err", err)
			return
		}
	}
}

// watchHub manages all watchers, keyed by kind.
type watchHub struct {
	mu       sync.Mutex
	watchers map[string][]*Watcher
	names    map[string]bool // active watcher names (must be unique)

	db     *sql.DB
	logger *slog.Logger
}

func newWatchHub(db *sql.DB, logger *slog.Logger) *watchHub {
	return &watchHub{
		watchers: make(map[string][]*Watcher),
		names:    make(map[string]bool),
		db:       db,
		logger:   logger,
	}
}

// subscribe creates a new watcher for the given kind with a unique subscriber
// name. It panics if a watcher with the same name already exists, since two
// watchers sharing a cursor row would corrupt each other's positions.
func (h *watchHub) subscribe(kind, name string) *Watcher {
	w := &Watcher{
		kind:    kind,
		name:    name,
		hub:     h,
		out:     make(chan Event),
		signal:  make(chan struct{}, 1),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	h.mu.Lock()
	if h.names[name] {
		h.mu.Unlock()
		panic(fmt.Sprintf("anchor: duplicate watcher name %q", name))
	}
	h.names[name] = true
	h.watchers[kind] = append(h.watchers[kind], w)
	h.mu.Unlock()
	go w.drain()
	return w
}

// unsubscribe removes a watcher from the hub, freeing its name for reuse.
func (h *watchHub) unsubscribe(w *Watcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.names, w.name)
	ws := h.watchers[w.kind]
	for i, ww := range ws {
		if ww == w {
			h.watchers[w.kind] = append(ws[:i], ws[i+1:]...)
			break
		}
	}
}

// signal wakes all watchers for the given kind to check for new events.
func (h *watchHub) signal(kind string) {
	h.mu.Lock()
	ws := h.watchers[kind]
	h.mu.Unlock()
	for _, w := range ws {
		select {
		case w.signal <- struct{}{}:
		default:
		}
	}
}

// TypedWatcher wraps a [Watcher] and deserializes event values into T.
type TypedWatcher[T any] struct {
	w  *Watcher
	ch chan TypedEvent[T]
}

// TypedEvent is an [Event] with the value already deserialized.
type TypedEvent[T any] struct {
	Change ChangeType
	Key    string
	Value  T     // zero value for deletes
	Err    error // non-nil if JSON deserialization failed

	ack *acker
}

// Ack acknowledges the typed event. It is safe to call multiple times.
func (e *TypedEvent[T]) Ack() {
	e.ack.ack()
}

func newTypedWatcher[T any](w *Watcher) *TypedWatcher[T] {
	tw := &TypedWatcher[T]{
		w:  w,
		ch: make(chan TypedEvent[T]),
	}
	go tw.convert()
	return tw
}

// Events returns the channel on which typed events are delivered.
func (tw *TypedWatcher[T]) Events() <-chan TypedEvent[T] {
	return tw.ch
}

// Stop stops the underlying watcher.
func (tw *TypedWatcher[T]) Stop() {
	tw.w.Stop()
}

func (tw *TypedWatcher[T]) convert() {
	defer close(tw.ch)
	for e := range tw.w.Events() {
		te := TypedEvent[T]{
			Change: e.Change,
			Key:    e.Key,
			ack:    e.ack,
		}
		if len(e.Value) > 0 {
			if err := json.Unmarshal(e.Value, &te.Value); err != nil {
				te.Err = err
			}
		}
		select {
		case tw.ch <- te:
		case <-tw.w.done:
			return
		}
	}
}
