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

	var err error
	switch cmd.Type {
	case CmdSet:
		_, err = f.db.Exec(
			`INSERT INTO fsm_kv (kind, key, value) VALUES (?, ?, ?)
			 ON CONFLICT (kind, key) DO UPDATE SET value = excluded.value`,
			cmd.Kind, cmd.Key, string(cmd.Value),
		)
	case CmdDelete:
		_, err = f.db.Exec(
			`DELETE FROM fsm_kv WHERE kind = ? AND key = ?`,
			cmd.Kind, cmd.Key,
		)
	default:
		panic(fmt.Sprintf("unrecognized command type: %s", cmd.Type))
	}
	if err != nil {
		panic(fmt.Sprintf("failed to apply kv command: %v", err))
	}

	f.watches.signal(cmd.Kind)
	return nil
}

// snapshotData is the top-level structure serialized in a snapshot.
type snapshotData struct {
	KV []snapshotKV `json:"kv"`
}

type snapshotKV struct {
	Kind  string          `json:"kind"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// Snapshot returns a snapshot of the FSM state.
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	tx, err := f.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var data snapshotData

	rows, err := tx.Query(`SELECT kind, key, value FROM fsm_kv`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e snapshotKV
		var val string
		if err := rows.Scan(&e.Kind, &e.Key, &val); err != nil {
			return nil, err
		}
		e.Value = json.RawMessage(val)
		data.KV = append(data.KV, e)
	}
	if err := rows.Err(); err != nil {
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

	for _, e := range data.KV {
		if _, err := tx.Exec(`INSERT INTO fsm_kv (kind, key, value) VALUES (?, ?, ?)`,
			e.Kind, e.Key, string(e.Value)); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	f.watches.signalAll()
	return nil
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
