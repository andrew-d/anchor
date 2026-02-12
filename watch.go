package anchor

import (
	"encoding/json"
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
}

// Watcher receives events for a specific kind. It uses an internal unbounded
// queue so the FSM's Apply never blocks on slow consumers.
type Watcher struct {
	kind   string
	out    chan Event
	signal chan struct{}
	done   chan struct{}

	mu  sync.Mutex
	buf []Event
}

func newWatcher(kind string) *Watcher {
	w := &Watcher{
		kind:   kind,
		out:    make(chan Event),
		signal: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	go w.drain()
	return w
}

// Events returns the channel on which events are delivered.
func (w *Watcher) Events() <-chan Event {
	return w.out
}

// Stop stops the watcher. The events channel will be closed.
func (w *Watcher) Stop() {
	close(w.done)
}

// send enqueues an event for delivery (called by the FSM, never blocks).
func (w *Watcher) send(e Event) {
	w.mu.Lock()
	w.buf = append(w.buf, e)
	w.mu.Unlock()

	// Non-blocking signal to the drain goroutine.
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

// drain moves events from the buffer to the output channel.
func (w *Watcher) drain() {
	defer close(w.out)
	for {
		w.mu.Lock()
		buf := w.buf
		w.buf = nil
		w.mu.Unlock()

		for _, e := range buf {
			select {
			case w.out <- e:
			case <-w.done:
				return
			}
		}

		if len(buf) > 0 {
			// We delivered events; check for more without blocking.
			continue
		}

		// No events in buffer; wait for a signal or shutdown.
		select {
		case <-w.signal:
		case <-w.done:
			return
		}
	}
}

// watchHub manages all watchers, keyed by kind.
type watchHub struct {
	mu       sync.Mutex
	watchers map[string][]*Watcher
}

func newWatchHub() *watchHub {
	return &watchHub{
		watchers: make(map[string][]*Watcher),
	}
}

func (h *watchHub) subscribe(kind string) *Watcher {
	w := newWatcher(kind)
	h.mu.Lock()
	h.watchers[kind] = append(h.watchers[kind], w)
	h.mu.Unlock()
	return w
}

func (h *watchHub) notify(e Event) {
	h.mu.Lock()
	ws := h.watchers[e.Kind]
	h.mu.Unlock()
	for _, w := range ws {
		w.send(e)
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
		}
		if len(e.Value) > 0 {
			if err := json.Unmarshal(e.Value, &te.Value); err != nil {
				te.Err = err
			}
		}
		tw.ch <- te
	}
}
