package anchor

import (
	"log/slog"
	"sync"
	"time"
)

// Severity indicates how serious a [Problem] is.
type Severity int

const (
	Warning Severity = iota
	Error
)

// Problem is an active error or warning condition reported by a module.
type Problem struct {
	Severity Severity
	Module   string
	Key      string      // stable identifier within a module (e.g. "write:alice")
	Message  string      // static human-readable description
	Attrs    []slog.Attr // structured key-value details
	Since    time.Time   // when this problem first appeared (preserved across passes)
}

// ProblemReporter collects problems for a single module.
type ProblemReporter struct {
	module string
	store  *problemStore
	logger *slog.Logger
}

// Begin starts a new reconciliation pass, returning a [ProblemPass] that
// collects problems. Call [ProblemPass.Commit] on the pass when done.
func (r *ProblemReporter) Begin() *ProblemPass {
	return &ProblemPass{
		reporter: r,
	}
}

// ProblemPass collects problems during a single reconciliation pass.
type ProblemPass struct {
	reporter *ProblemReporter
	problems []Problem
}

// Error reports an error-severity problem and logs it via the module's logger.
func (ps *ProblemPass) Error(key, msg string, args ...any) {
	ps.reporter.logger.Error(msg, args...)
	ps.problems = append(ps.problems, Problem{
		Severity: Error,
		Module:   ps.reporter.module,
		Key:      key,
		Message:  msg,
		Attrs:    argsToAttrs(args),
	})
}

// Warn reports a warning-severity problem and logs it via the module's logger.
func (ps *ProblemPass) Warn(key, msg string, args ...any) {
	ps.reporter.logger.Warn(msg, args...)
	ps.problems = append(ps.problems, Problem{
		Severity: Warning,
		Module:   ps.reporter.module,
		Key:      key,
		Message:  msg,
		Attrs:    argsToAttrs(args),
	})
}

// Commit atomically replaces the module's problems with those reported in this
// pass. Problems with matching keys from the previous pass retain their
// original Since timestamp.
func (ps *ProblemPass) Commit() {
	ps.reporter.store.commit(ps.reporter.module, ps.problems)
}

// problemStore holds active problems across all modules.
type problemStore struct {
	mu       sync.Mutex
	problems map[string][]Problem // module name → active problems
	now      func() time.Time
}

func newProblemStore() *problemStore {
	return &problemStore{
		problems: make(map[string][]Problem),
	}
}

func (s *problemStore) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// commit replaces the module's active problems. Problems with keys that match
// the previous set retain their original Since timestamp.
func (s *problemStore) commit(module string, next []Problem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.problems[module]
	prevByKey := make(map[string]Problem, len(prev))
	for _, p := range prev {
		prevByKey[p.Key] = p
	}

	now := s.timeNow()
	for i := range next {
		if old, ok := prevByKey[next[i].Key]; ok {
			next[i].Since = old.Since
		} else {
			next[i].Since = now
		}
	}

	if len(next) == 0 {
		delete(s.problems, module)
	} else {
		s.problems[module] = next
	}
}

// all returns every active problem across all modules.
func (s *problemStore) all() []Problem {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []Problem
	for _, ps := range s.problems {
		result = append(result, ps...)
	}
	return result
}

// reporter creates a [ProblemReporter] for the given module.
func (s *problemStore) reporter(module string, logger *slog.Logger) *ProblemReporter {
	return &ProblemReporter{
		module: module,
		store:  s,
		logger: logger,
	}
}

// NewTestProblemReporter creates a [ProblemReporter] backed by a throw-away
// store. It is intended for use in tests outside the anchor package.
func NewTestProblemReporter(logger *slog.Logger) *ProblemReporter {
	return newProblemStore().reporter("test", logger)
}

// argsToAttrs converts slog-style alternating key-value args to []slog.Attr.
func argsToAttrs(args []any) []slog.Attr {
	var attrs []slog.Attr
	for i := 0; i < len(args); i++ {
		switch v := args[i].(type) {
		case slog.Attr:
			attrs = append(attrs, v)
		case string:
			if i+1 < len(args) {
				attrs = append(attrs, slog.Any(v, args[i+1]))
				i++
			}
		}
	}
	return attrs
}
