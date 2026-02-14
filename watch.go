package anchor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
)

// ChangeType identifies the kind of mutation that occurred.
type ChangeType int

const (
	ChangeSet ChangeType = iota
	ChangeDelete
)

// Event is emitted by the FSM on every Apply, with the value already
// deserialized into T.
type Event[T any] struct {
	Change ChangeType
	Key    string
	Value  T     // zero value for deletes
	Err    error // non-nil if JSON deserialization failed

	ack *acker
}

// Ack acknowledges the event, advancing the subscriber's cursor so that the
// event will not be redelivered. It is safe to call multiple times.
func (e *Event[T]) Ack() {
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

func (a *acker) ack()                  { a.once.Do(func() { close(a.ch) }) }
func (a *acker) done() <-chan struct{} { return a.ch }

// watchEntry is the non-generic subscription record tracked by watchHub.
type watchEntry struct {
	kind   string
	name   string
	signal chan struct{}
}

// Watcher receives events for a specific kind. It reads persisted events from
// SQLite one at a time, delivers each on the output channel, and waits for an
// Ack before advancing the cursor.
type Watcher[T any] struct {
	entry    *watchEntry
	hub      *watchHub
	out      chan Event[T]
	done     chan struct{}
	stopped  chan struct{} // closed when drain exits
	stopOnce sync.Once
}

func newWatcher[T any](hub *watchHub, entry *watchEntry) *Watcher[T] {
	w := &Watcher[T]{
		entry:   entry,
		hub:     hub,
		out:     make(chan Event[T]),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go w.drain()
	return w
}

// Events returns the channel on which events are delivered.
func (w *Watcher[T]) Events() <-chan Event[T] {
	return w.out
}

// Stop stops the watcher and waits for the drain goroutine to exit. The
// events channel will be closed.
func (w *Watcher[T]) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
		<-w.stopped
		w.hub.unsubscribe(w.entry)
	})
}

// drain reads events from the database, deserializes them, and delivers them
// one at a time.
func (w *Watcher[T]) drain() {
	defer close(w.stopped)
	defer close(w.out)

	// Read initial cursor position.
	var cursor int64
	err := w.hub.db.QueryRow(
		`SELECT pos FROM fsm_cursors WHERE name = ?`, w.entry.name,
	).Scan(&cursor)
	if err != nil && err != sql.ErrNoRows {
		w.hub.logger.Error("failed to read watcher cursor", "watcher", w.entry.name, "err", err)
		return
	}

	for {
		// Query next event after cursor.
		var raftIndex int64
		var change int
		var key, val string
		err := w.hub.db.QueryRow(
			`SELECT raft_index, change, key, value FROM fsm_events WHERE kind = ? AND raft_index > ? ORDER BY raft_index LIMIT 1`,
			w.entry.kind, cursor,
		).Scan(&raftIndex, &change, &key, &val)

		if err == sql.ErrNoRows {
			// No events available; wait for a signal or shutdown.
			select {
			case <-w.entry.signal:
				continue
			case <-w.done:
				return
			}
		}
		if err != nil {
			w.hub.logger.Error("failed to query events", "watcher", w.entry.name, "err", err)
			return
		}

		e := Event[T]{
			Change: ChangeType(change),
			Key:    key,
			ack:    newAcker(),
		}
		if val != "" {
			if err := json.Unmarshal([]byte(val), &e.Value); err != nil {
				e.Err = err
			}
		}

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
		cursor = raftIndex
		_, err = w.hub.db.Exec(
			`INSERT INTO fsm_cursors(name, pos) VALUES(?, ?) ON CONFLICT(name) DO UPDATE SET pos = excluded.pos`,
			w.entry.name, cursor,
		)
		if err != nil {
			w.hub.logger.Error("failed to advance watcher cursor", "watcher", w.entry.name, "err", err)
			return
		}

		// Compact events that all subscribers have processed.
		_, err = w.hub.db.Exec(
			`DELETE FROM fsm_events WHERE raft_index <= (SELECT MIN(pos) FROM fsm_cursors)`,
		)
		if err != nil {
			w.hub.logger.Error("failed to compact events", "watcher", w.entry.name, "err", err)
		}
	}
}

// watchHub manages all watchers, keyed by kind.
type watchHub struct {
	mu      sync.Mutex
	entries map[string][]*watchEntry
	names   map[string]bool // active watcher names (must be unique)

	db     *sql.DB
	logger *slog.Logger
}

func newWatchHub(db *sql.DB, logger *slog.Logger) *watchHub {
	return &watchHub{
		entries: make(map[string][]*watchEntry),
		names:   make(map[string]bool),
		db:      db,
		logger:  logger,
	}
}

// subscribe registers a new subscription for the given kind with a unique
// subscriber name. It panics if a watcher with the same name already exists,
// since two watchers sharing a cursor row would corrupt each other's positions.
func (h *watchHub) subscribe(kind, name string) *watchEntry {
	entry := &watchEntry{
		kind:   kind,
		name:   name,
		signal: make(chan struct{}, 1),
	}
	h.mu.Lock()
	if h.names[name] {
		h.mu.Unlock()
		panic(fmt.Sprintf("anchor: duplicate watcher name %q", name))
	}
	h.names[name] = true
	h.entries[kind] = append(h.entries[kind], entry)
	h.mu.Unlock()
	return entry
}

// unsubscribe removes a subscription from the hub, freeing its name for reuse.
func (h *watchHub) unsubscribe(entry *watchEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.names, entry.name)
	es := h.entries[entry.kind]
	for i, e := range es {
		if e == entry {
			h.entries[entry.kind] = append(es[:i], es[i+1:]...)
			break
		}
	}
}

// signal wakes all watchers for the given kind to check for new events.
func (h *watchHub) signal(kind string) {
	h.mu.Lock()
	es := slices.Clone(h.entries[kind])
	h.mu.Unlock()
	for _, e := range es {
		select {
		case e.signal <- struct{}{}:
		default:
		}
	}
}
