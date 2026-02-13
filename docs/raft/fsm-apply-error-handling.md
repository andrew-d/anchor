# Handling Errors in `FSM.Apply`

## The Problem

The `FSM.Apply` method returns `interface{}`, not `error`. Raft treats the return value as an opaque response — it has no knowledge of whether your FSM succeeded or failed.

When a caller uses `raft.Apply()`, the returned `ApplyFuture` exposes two methods:

- **`Error()`** — whether Raft committed the log (replication/leadership failures).
- **`Response()`** — whatever `FSM.Apply` returned, including any error you returned.

Raft commits the log entry to all nodes *before* `FSM.Apply` runs. If Apply fails on some nodes but not others, the cluster's FSM state diverges silently. This is the central danger.

## Key Distinction: Deterministic vs. Non-Deterministic Errors

- **Deterministic errors** (constraint violations, validation failures) produce the same result on every node. These are safe — all nodes agree the operation was a no-op.
- **Non-deterministic errors** (disk I/O, OOM, transient SQLite locks) may affect only some nodes. These cause divergence and must not be silently ignored.

## Recommended Patterns

### Validate before proposing

Reject invalid operations at the API layer, before calling `raft.Apply()`. This keeps most error cases out of the log entirely. There is a TOCTOU race between the check and apply, but it is acceptable for many applications.

### Treat deterministic errors as data

Return a result struct from Apply rather than a bare error. All nodes process the entry identically — the operation is simply a no-op.

```go
func (f *myFSM) Apply(log *raft.Log) interface{} {
    if err := f.db.Exec(cmd); err != nil {
        return &Result{Err: err} // all nodes agree
    }
    return &Result{Value: val}
}
```

### Panic on non-deterministic failures

If Apply fails for a reason other nodes might not reproduce, crash the node. On restart it restores from a snapshot and replays the log. This is the pattern used by Consul and Nomad. A crash is better than silent divergence.

```go
if err := f.db.Exec(cmd); err != nil {
    if isDeterministic(err) {
        return &Result{Err: err}
    }
    panic("non-deterministic FSM error: " + err.Error())
}
```

### Use transactions for atomicity (SQLite-specific)

Wrap each Apply in a database transaction. Deterministic failures (constraint violations) roll back and return an error result. Non-deterministic failures (commit errors) panic.

## Summary

| Error type | Behavior | Correct action |
|---|---|---|
| Deterministic (constraint violation) | Same on all nodes | Return as data in `Response()` |
| Non-deterministic (I/O, transient) | Varies across nodes | Panic; recover via snapshot + replay |
| Predictable/preventable | Catchable before commit | Validate before `raft.Apply()` |
