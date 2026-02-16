package anchor

import "encoding/json"

// CommandType identifies the type of a Raft log command.
type CommandType string

const (
	CmdSet    CommandType = "set"
	CmdDelete CommandType = "delete"
)

// Command is the payload serialized into each Raft log entry.
type Command struct {
	Type  CommandType     `json:"type"`
	Kind  string          `json:"kind"`
	Scope string          `json:"scope,omitempty"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
}
