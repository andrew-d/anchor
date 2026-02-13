CREATE TABLE IF NOT EXISTS fsm_kv (
	kind  TEXT NOT NULL,
	key   TEXT NOT NULL,
	value TEXT NOT NULL,
	PRIMARY KEY (kind, key)
) STRICT;

CREATE TABLE IF NOT EXISTS fsm_events (
	raft_index INTEGER PRIMARY KEY,
	kind       TEXT    NOT NULL,
	change     INTEGER NOT NULL,
	key        TEXT    NOT NULL,
	value      TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX IF NOT EXISTS idx_fsm_events_kind ON fsm_events (kind, raft_index);

CREATE TABLE IF NOT EXISTS fsm_cursors (
	name TEXT PRIMARY KEY,
	pos  INTEGER NOT NULL
) STRICT;
