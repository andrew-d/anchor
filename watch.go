package anchor

import (
	"log/slog"
	"slices"
	"sync"
)

// watchEntry is the non-generic subscription record tracked by watchHub.
type watchEntry struct {
	kind   string
	signal chan struct{}
}

// Watcher delivers the current state for a kind whenever it changes. It calls
// readFn to read the latest state, delivers the result on the output channel,
// then waits for a signal before reading again. The first delivery happens
// immediately on startup.
type Watcher[T any] struct {
	readFn   func() (T, error)
	entry    *watchEntry
	hub      *watchHub
	out      chan T
	done     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

func newWatcher[T any](hub *watchHub, entry *watchEntry, readFn func() (T, error)) *Watcher[T] {
	w := &Watcher[T]{
		readFn:  readFn,
		entry:   entry,
		hub:     hub,
		out:     make(chan T),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go w.loop()
	return w
}

// State returns the channel on which state snapshots are delivered.
func (w *Watcher[T]) State() <-chan T {
	return w.out
}

// Stop stops the watcher and waits for the loop goroutine to exit. The state
// channel will be closed.
func (w *Watcher[T]) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
		<-w.stopped
		w.hub.unsubscribe(w.entry)
	})
}

// loop reads state and delivers it, then waits for a signal to re-read.
func (w *Watcher[T]) loop() {
	defer close(w.stopped)
	defer close(w.out)

	for {
		state, err := w.readFn()
		if err != nil {
			w.hub.logger.Error("failed to read watcher state", "kind", w.entry.kind, "err", err)
			return
		}

		select {
		case w.out <- state:
		case <-w.done:
			return
		}

		select {
		case <-w.entry.signal:
		case <-w.done:
			return
		}
	}
}

// watchHub manages all watchers, keyed by kind.
type watchHub struct {
	mu      sync.Mutex
	entries map[string][]*watchEntry

	logger *slog.Logger
}

func newWatchHub(logger *slog.Logger) *watchHub {
	return &watchHub{
		entries: make(map[string][]*watchEntry),
		logger:  logger,
	}
}

// subscribe registers a new subscription for the given kind.
func (h *watchHub) subscribe(kind string) *watchEntry {
	entry := &watchEntry{
		kind:   kind,
		signal: make(chan struct{}, 1),
	}
	h.mu.Lock()
	h.entries[kind] = append(h.entries[kind], entry)
	h.mu.Unlock()
	return entry
}

// unsubscribe removes a subscription from the hub.
func (h *watchHub) unsubscribe(entry *watchEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	es := h.entries[entry.kind]
	for i, e := range es {
		if e == entry {
			h.entries[entry.kind] = append(es[:i], es[i+1:]...)
			break
		}
	}
}

// signal wakes all watchers for the given kind to re-read state.
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

// signalAll wakes all watchers across all kinds to re-read state.
func (h *watchHub) signalAll() {
	h.mu.Lock()
	var all []*watchEntry
	for _, es := range h.entries {
		all = append(all, es...)
	}
	h.mu.Unlock()
	for _, e := range all {
		select {
		case e.signal <- struct{}{}:
		default:
		}
	}
}
