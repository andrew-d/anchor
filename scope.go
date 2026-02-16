package anchor

import (
	"fmt"
	"strings"
)

// Scope represents a targeting scope for configuration entries.
// The zero value is the global scope (matches all nodes).
type Scope struct {
	typ   string // "", "os", "node"
	value string // e.g. "linux", "node-1"
}

// ParseScope parses a scope string. Valid formats:
//   - "" (empty string) — global scope
//   - "os:<name>" — matches nodes with that OS
//   - "node:<id>" — matches a specific node
func ParseScope(s string) (Scope, error) {
	if s == "" {
		return Scope{}, nil
	}
	typ, value, ok := strings.Cut(s, ":")
	if !ok || value == "" {
		return Scope{}, fmt.Errorf("invalid scope %q: expected \"os:<name>\" or \"node:<id>\"", s)
	}
	switch typ {
	case "os", "node":
		return Scope{typ: typ, value: value}, nil
	default:
		return Scope{}, fmt.Errorf("invalid scope type %q: expected \"os\" or \"node\"", typ)
	}
}

// String returns the string representation of the scope.
// The global scope returns an empty string.
func (s Scope) String() string {
	if s.typ == "" {
		return ""
	}
	return s.typ + ":" + s.value
}

// IsGlobal reports whether this is the global (unscoped) scope.
func (s Scope) IsGlobal() bool {
	return s.typ == ""
}

// Priority returns the priority of this scope. Higher values win during
// resolution:
//   - global: 0
//   - os: 1
//   - node: 2
func (s Scope) Priority() int {
	switch s.typ {
	case "os":
		return 1
	case "node":
		return 2
	default:
		return 0
	}
}

// Matches reports whether this scope matches the given node info.
func (s Scope) Matches(info NodeInfo) bool {
	switch s.typ {
	case "":
		return true
	case "os":
		return s.value == info.OS
	case "node":
		return s.value == info.ID
	default:
		return false
	}
}

// NodeInfo describes the local node for scope matching.
type NodeInfo struct {
	ID string
	OS string
}
