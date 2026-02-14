package anchor

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"

	"github.com/hashicorp/raft"
)

//go:embed schema.sql
var fsmSchema string

// fsm implements raft.FSM using a type alias so that Apply/Snapshot/Restore
// are not exposed on the public App type.
type fsm App

// initTable creates the FSM tables if they do not exist.
func (f *fsm) initTable() error {
	_, err := f.db.Exec(fsmSchema)
	return err
}

// Apply applies a Raft log entry to the key-value store.
func (f *fsm) Apply(l *raft.Log) any {
	var cmd Command
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		panic(fmt.Sprintf("failed to unmarshal command: %v", err))
	}

	tx, err := f.db.Begin()
	if err != nil {
		panic(fmt.Sprintf("failed to begin transaction: %v", err))
	}
	defer tx.Rollback()

	var change ChangeType
	switch cmd.Type {
	case CmdSet:
		_, err = tx.Exec(
			`INSERT INTO fsm_kv (kind, key, value) VALUES (?, ?, ?)
			 ON CONFLICT (kind, key) DO UPDATE SET value = excluded.value`,
			cmd.Kind, cmd.Key, string(cmd.Value),
		)
		change = ChangeSet
	case CmdDelete:
		_, err = tx.Exec(
			`DELETE FROM fsm_kv WHERE kind = ? AND key = ?`,
			cmd.Kind, cmd.Key,
		)
		change = ChangeDelete
	default:
		panic(fmt.Sprintf("unrecognized command type: %s", cmd.Type))
	}
	if err != nil {
		panic(fmt.Sprintf("failed to apply kv command: %v", err))
	}

	// Insert event row atomically with the KV change.
	//
	// The RETURNING clause combined with ON CONFLICT DO NOTHING means:
	//   - New row inserted:  RETURNING produces one row, Scan succeeds.
	//   - Duplicate raft_index: DO NOTHING fires, RETURNING produces zero
	//     rows, and QueryRow().Scan() returns sql.ErrNoRows.
	//
	// This makes Apply idempotent on Raft log replay — duplicate entries
	// are silently skipped and watchers are only signaled for genuinely
	// new events.
	var value string
	if cmd.Type == CmdSet {
		value = string(cmd.Value)
	}
	var inserted bool
	var dummy int64
	err = tx.QueryRow(
		`INSERT INTO fsm_events(raft_index, kind, change, key, value) VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(raft_index) DO NOTHING RETURNING raft_index`,
		l.Index, cmd.Kind, int(change), cmd.Key, value,
	).Scan(&dummy)
	if err == sql.ErrNoRows {
		inserted = false
	} else if err != nil {
		panic(fmt.Sprintf("failed to insert event: %v", err))
	} else {
		inserted = true
	}

	if err := tx.Commit(); err != nil {
		panic(fmt.Sprintf("failed to commit transaction: %v", err))
	}

	if inserted {
		f.watches.signal(cmd.Kind)
	}
	return nil
}

// snapshotData is the top-level structure serialized in a snapshot. It
// includes all FSM tables so that events and cursor positions survive
// snapshot + log truncation.
type snapshotData struct {
	KV      []snapshotKV     `json:"kv"`
	Events  []snapshotEvent  `json:"events"`
	Cursors []snapshotCursor `json:"cursors"`
}

type snapshotKV struct {
	Kind  string          `json:"kind"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type snapshotEvent struct {
	RaftIndex int64  `json:"raft_index"`
	Kind      string `json:"kind"`
	Change    int    `json:"change"`
	Key       string `json:"key"`
	Value     string `json:"value"`
}

type snapshotCursor struct {
	Name string `json:"name"`
	Pos  int64  `json:"pos"`
}

// Snapshot returns a snapshot of the FSM state. All three tables are read
// inside a single transaction so the snapshot is self-consistent.
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	tx, err := f.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var data snapshotData

	// KV entries.
	kvRows, err := tx.Query(`SELECT kind, key, value FROM fsm_kv`)
	if err != nil {
		return nil, err
	}
	defer kvRows.Close()
	for kvRows.Next() {
		var e snapshotKV
		var val string
		if err := kvRows.Scan(&e.Kind, &e.Key, &val); err != nil {
			return nil, err
		}
		e.Value = json.RawMessage(val)
		data.KV = append(data.KV, e)
	}
	if err := kvRows.Err(); err != nil {
		return nil, err
	}

	// Events.
	eventRows, err := tx.Query(`SELECT raft_index, kind, change, key, value FROM fsm_events`)
	if err != nil {
		return nil, err
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var e snapshotEvent
		if err := eventRows.Scan(&e.RaftIndex, &e.Kind, &e.Change, &e.Key, &e.Value); err != nil {
			return nil, err
		}
		data.Events = append(data.Events, e)
	}
	if err := eventRows.Err(); err != nil {
		return nil, err
	}

	// Cursors.
	cursorRows, err := tx.Query(`SELECT name, pos FROM fsm_cursors`)
	if err != nil {
		return nil, err
	}
	defer cursorRows.Close()
	for cursorRows.Next() {
		var c snapshotCursor
		if err := cursorRows.Scan(&c.Name, &c.Pos); err != nil {
			return nil, err
		}
		data.Cursors = append(data.Cursors, c)
	}
	if err := cursorRows.Err(); err != nil {
		return nil, err
	}

	return &fsmSnapshot{data: data}, nil
}

// Restore replaces all FSM state from a snapshot. Raft guarantees this is
// only called during initialization when nothing else accesses the FSM.
func (f *fsm) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var data snapshotData
	if err := json.NewDecoder(rc).Decode(&data); err != nil {
		return err
	}

	tx, err := f.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM fsm_kv`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM fsm_events`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM fsm_cursors`); err != nil {
		return err
	}

	for _, e := range data.KV {
		if _, err := tx.Exec(`INSERT INTO fsm_kv (kind, key, value) VALUES (?, ?, ?)`,
			e.Kind, e.Key, string(e.Value)); err != nil {
			return err
		}
	}
	for _, e := range data.Events {
		if _, err := tx.Exec(`INSERT INTO fsm_events (raft_index, kind, change, key, value) VALUES (?, ?, ?, ?, ?)`,
			e.RaftIndex, e.Kind, e.Change, e.Key, e.Value); err != nil {
			return err
		}
	}
	for _, c := range data.Cursors {
		if _, err := tx.Exec(`INSERT INTO fsm_cursors (name, pos) VALUES (?, ?)`,
			c.Name, c.Pos); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// fsmGet reads a single value from the local FSM state.
func (f *fsm) fsmGet(kind, key string) (json.RawMessage, error) {
	var val string
	err := f.db.QueryRow(
		`SELECT value FROM fsm_kv WHERE kind = ? AND key = ?`,
		kind, key,
	).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(val), nil
}

// fsmList reads all entries for a given kind from the local FSM state.
func (f *fsm) fsmList(kind string) (map[string]json.RawMessage, error) {
	rows, err := f.db.Query(
		`SELECT key, value FROM fsm_kv WHERE kind = ?`,
		kind,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]json.RawMessage)
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		result[key] = json.RawMessage(val)
	}
	return result, rows.Err()
}

type fsmSnapshot struct {
	data snapshotData
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		b, err := json.Marshal(s.data)
		if err != nil {
			return err
		}
		if _, err := sink.Write(b); err != nil {
			return err
		}
		return sink.Close()
	}()
	if err != nil {
		sink.Cancel()
	}
	return err
}

func (s *fsmSnapshot) Release() {}
