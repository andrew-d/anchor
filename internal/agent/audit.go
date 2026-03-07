package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// AuditEntry records the execution of a single module.
type AuditEntry struct {
	Timestamp  int64  `json:"timestamp"`
	Module     string `json:"module"`
	ScriptHash string `json:"script_hash"`
	Status     string `json:"status"`
}

// auditLog writes append-only JSON lines to a file.
type auditLog struct {
	mu   sync.Mutex
	path string
}

// newAuditLog creates an auditLog that writes to the given path.
func newAuditLog(path string) *auditLog {
	return &auditLog{path: path}
}

// log appends an audit entry for a module execution.
func (a *auditLog) log(moduleName, script, status string) {
	h := sha256.Sum256([]byte(script))
	entry := AuditEntry{
		Timestamp:  time.Now().Unix(),
		Module:     moduleName,
		ScriptHash: hex.EncodeToString(h[:]),
		Status:     status,
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	f.Write(line)
}
