package anchor

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/hashicorp/raft"
)

func (a *App) startHTTP() error {
	mux := http.NewServeMux()

	// Public API routes driven by apiEndpoints (the single source of truth
	// shared with the /docs page).
	for _, ep := range apiEndpoints {
		h := ep.Handler
		mux.HandleFunc(ep.Method+" "+ep.Pattern, func(w http.ResponseWriter, r *http.Request) {
			h(a, w, r)
		})
	}

	// Internal routes (not in public docs).
	mux.HandleFunc("POST /internal/join", a.handleJoin)

	// Web UI.
	mux.HandleFunc("GET /{$}", a.handleUI)
	mux.HandleFunc("GET /docs", a.handleDocs)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub()))))

	a.httpServer = &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", a.config.HTTPAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", a.config.HTTPAddr, err)
	}
	a.httpAddr = ln.Addr().String()

	go func() {
		if err := a.httpServer.Serve(ln); err != http.ErrServerClosed {
			a.logger.Error("HTTP server error", "err", err)
			os.Exit(1)
		}
	}()
	return nil
}

// redirectToLeader sends a 307 Temporary Redirect to the leader's HTTP address.
// Returns true if a redirect was sent, false if this node is the leader.
func (a *App) redirectToLeader(w http.ResponseWriter, r *http.Request) bool {
	if a.raft.State() == raft.Leader {
		return false
	}

	leaderAddr, err := a.leaderHTTPAddr()
	if err != nil {
		http.Error(w, "no leader available", http.StatusServiceUnavailable)
		return true
	}

	target := fmt.Sprintf("http://%s%s", leaderAddr, r.URL.Path)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	return true
}

func (a *App) handleGet(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	key := r.PathValue("key")
	scope := r.URL.Query().Get("scope")

	f := (*fsm)(a)
	raw, err := f.fsmGetExact(kind, scope, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if raw == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}

func (a *App) handleList(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")

	f := (*fsm)(a)
	items, err := f.fsmListAll(kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (a *App) handleSet(w http.ResponseWriter, r *http.Request) {
	if a.redirectToLeader(w, r) {
		return
	}

	kind := r.PathValue("kind")
	key := r.PathValue("key")
	scope := r.URL.Query().Get("scope")

	if scope != "" {
		if _, err := ParseScope(scope); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Validate JSON if the kind is registered with a type.
	if info, ok := a.kinds[kind]; ok {
		v := info.newFn()
		if err := json.Unmarshal(body, v); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON for kind %q: %v", kind, err), http.StatusBadRequest)
			return
		}
	} else if !json.Valid(body) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	cmd := Command{
		Type:  CmdSet,
		Kind:  kind,
		Scope: scope,
		Key:   key,
		Value: body,
	}
	if err := a.applyCommand(cmd); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleDelete(w http.ResponseWriter, r *http.Request) {
	if a.redirectToLeader(w, r) {
		return
	}

	kind := r.PathValue("kind")
	key := r.PathValue("key")
	scope := r.URL.Query().Get("scope")

	if scope != "" {
		if _, err := ParseScope(scope); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	cmd := Command{
		Type:  CmdDelete,
		Kind:  kind,
		Scope: scope,
		Key:   key,
	}
	if err := a.applyCommand(cmd); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	leaderAddr, leaderID := a.raft.LeaderWithID()

	configFuture := a.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type nodeInfo struct {
		ID       string `json:"id"`
		Address  string `json:"address"`
		Suffrage string `json:"suffrage"`
	}

	servers := configFuture.Configuration().Servers
	nodes := make([]nodeInfo, 0, len(servers))
	for _, s := range servers {
		suffrage := "voter"
		if s.Suffrage == raft.Nonvoter {
			suffrage = "nonvoter"
		}
		nodes = append(nodes, nodeInfo{
			ID:       string(s.ID),
			Address:  string(s.Address),
			Suffrage: suffrage,
		})
	}

	status := struct {
		NodeID string     `json:"node_id"`
		State  string     `json:"state"`
		Leader nodeInfo   `json:"leader"`
		Nodes  []nodeInfo `json:"nodes"`
	}{
		NodeID: a.config.NodeID,
		State:  a.raft.State().String(),
		Leader: nodeInfo{
			ID:      string(leaderID),
			Address: string(leaderAddr),
		},
		Nodes: nodes,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (a *App) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID   string `json:"node_id"`
		RaftAddr string `json:"raft_addr"`
		Voter    *bool  `json:"voter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.NodeID == "" || req.RaftAddr == "" {
		http.Error(w, "node_id and raft_addr are required", http.StatusBadRequest)
		return
	}

	voter := req.Voter == nil || *req.Voter
	wantSuffrage := raft.Voter
	if !voter {
		wantSuffrage = raft.Nonvoter
	}

	// Deduplication: if a node with the same ID and address already exists, no-op.
	configFuture := a.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, srv := range configFuture.Configuration().Servers {
		if srv.ID == raft.ServerID(req.NodeID) || srv.Address == raft.ServerAddress(req.RaftAddr) {
			if srv.ID == raft.ServerID(req.NodeID) && srv.Address == raft.ServerAddress(req.RaftAddr) && srv.Suffrage == wantSuffrage {
				// Already a member with matching ID, address, and suffrage.
				a.logger.Info("node already member, ignoring join", "node_id", req.NodeID, "raft_addr", req.RaftAddr)
				w.WriteHeader(http.StatusOK)
				return
			}
			// ID or address conflicts, or suffrage changed; remove the old entry.
			future := a.raft.RemoveServer(srv.ID, 0, 0)
			if err := future.Error(); err != nil {
				http.Error(w, fmt.Sprintf("error removing existing node: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	var f raft.IndexFuture
	if voter {
		f = a.raft.AddVoter(raft.ServerID(req.NodeID), raft.ServerAddress(req.RaftAddr), 0, 0)
	} else {
		f = a.raft.AddNonvoter(raft.ServerID(req.NodeID), raft.ServerAddress(req.RaftAddr), 0, 0)
	}
	if err := f.Error(); err != nil {
		http.Error(w, fmt.Sprintf("error adding node: %v", err), http.StatusInternalServerError)
		return
	}

	a.logger.Info("node joined successfully", "node_id", req.NodeID, "raft_addr", req.RaftAddr, "voter", voter)
	w.WriteHeader(http.StatusOK)
}
