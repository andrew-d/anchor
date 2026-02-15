package anchor

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/raft"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var uiTemplates = template.Must(
	template.New("").Funcs(template.FuncMap{
		"timeAgo": timeAgo,
		"lower":   strings.ToLower,
	}).ParseFS(templateFS, "templates/*.html"),
)

func timeAgo(t time.Time) string {
	d := time.Since(t).Truncate(time.Second)
	if d < time.Second {
		return "just now"
	}
	return d.String() + " ago"
}

type uiData struct {
	NodeID       string
	DeploymentID string
	RaftState    string
	LeaderID     string
	LeaderAddr   string
	Nodes        []uiNode
	Modules      []uiModule
	Kinds        []string
	Problems     []uiProblem
	GeneratedAt  string
}

type uiNode struct {
	ID       string
	Address  string
	IsThis   bool
	IsLeader bool
	Suffrage string // "Voter" or "Nonvoter"
}

type uiModule struct {
	Name         string
	ProblemCount int
	HasProblems  bool
}

type uiProblem struct {
	Severity string
	Module   string
	Key      string
	Message  string
	Since    string
}

// apiEndpoint describes a single public API route for both mux registration
// and documentation rendering.
type apiEndpoint struct {
	Method      string // "GET", "PUT", "DELETE", "POST"
	Pattern     string // "/api/v1/{kind}", etc.
	Handler     func(*App, http.ResponseWriter, *http.Request)
	Description string // one-line description
	Example     string // example request/response block (pre-formatted text)
	Section     string // grouping label: "Configuration API", "Cluster API"
}

// apiEndpoints is the single source of truth for public API routes.
// Each entry drives both mux registration in startHTTP and the /docs page.
var apiEndpoints = []apiEndpoint{
	{
		Method:      "GET",
		Pattern:     "/api/v1/{kind}",
		Handler:     (*App).handleList,
		Description: "List all entries of a given kind.",
		Example: `GET /api/v1/sshkeys

200 OK
{"alice": {"public_key": "ssh-ed25519 AAAA..."}, "bob": {"public_key": "ssh-ed25519 BBBB..."}}`,
		Section: "Configuration API",
	},
	{
		Method:      "GET",
		Pattern:     "/api/v1/{kind}/{key}",
		Handler:     (*App).handleGet,
		Description: "Get a single entry. Returns 404 if the key does not exist.",
		Example: `GET /api/v1/sshkeys/alice

200 OK
{"public_key": "ssh-ed25519 AAAA..."}`,
		Section: "Configuration API",
	},
	{
		Method:      "PUT",
		Pattern:     "/api/v1/{kind}/{key}",
		Handler:     (*App).handleSet,
		Description: "Create or update an entry. Body must be valid JSON. If the kind has a registered type, the value is validated against it. Returns 204 No Content on success. Redirects to the leader (307) on followers.",
		Example: `PUT /api/v1/sshkeys/alice
Content-Type: application/json

{"public_key": "ssh-ed25519 AAAA..."}

204 No Content`,
		Section: "Configuration API",
	},
	{
		Method:      "DELETE",
		Pattern:     "/api/v1/{kind}/{key}",
		Handler:     (*App).handleDelete,
		Description: "Delete an entry. Returns 204 No Content on success. Redirects to the leader (307) on followers.",
		Example: `DELETE /api/v1/sshkeys/alice

204 No Content`,
		Section: "Configuration API",
	},
	{
		Method:      "GET",
		Pattern:     "/api/v1/status",
		Handler:     (*App).handleStatus,
		Description: "Returns cluster status including the current node, leader, and all known nodes.",
		Example: `GET /api/v1/status

200 OK
{
  "node_id": "node-1",
  "state": "Leader",
  "leader": {"id": "node-1", "address": "127.0.0.1:7000"},
  "nodes": [
    {"id": "node-1", "address": "127.0.0.1:7000", "suffrage": "voter"},
    {"id": "node-2", "address": "127.0.0.1:7001", "suffrage": "nonvoter"}
  ]
}`,
		Section: "Cluster API",
	},
}

// apiSection groups endpoints under a section heading for template rendering.
type apiSection struct {
	Name      string
	Endpoints []apiEndpoint
}

// groupEndpoints groups the flat apiEndpoints slice by Section, preserving
// the order in which sections first appear.
func groupEndpoints(endpoints []apiEndpoint) []apiSection {
	var sections []apiSection
	idx := make(map[string]int) // section name -> index in sections
	for _, ep := range endpoints {
		i, ok := idx[ep.Section]
		if !ok {
			i = len(sections)
			idx[ep.Section] = i
			sections = append(sections, apiSection{Name: ep.Section})
		}
		sections[i].Endpoints = append(sections[i].Endpoints, ep)
	}
	return sections
}

func (a *App) collectUIData() uiData {
	// Raft state.
	leaderAddr, leaderID := a.raft.LeaderWithID()
	raftState := a.raft.State().String()

	// Cluster nodes.
	var nodes []uiNode
	configFuture := a.raft.GetConfiguration()
	if configFuture.Error() == nil {
		for _, s := range configFuture.Configuration().Servers {
			suffrage := "Voter"
			if s.Suffrage == raft.Nonvoter {
				suffrage = "Nonvoter"
			}
			nodes = append(nodes, uiNode{
				ID:       string(s.ID),
				Address:  string(s.Address),
				IsThis:   string(s.ID) == a.config.NodeID,
				IsLeader: s.ID == leaderID,
				Suffrage: suffrage,
			})
		}
	}

	// Problems indexed by module for counting.
	problems := a.Problems()
	problemsByModule := make(map[string]int)
	for _, p := range problems {
		problemsByModule[p.Module]++
	}

	// Modules.
	modules := make([]uiModule, len(a.modules))
	for i, m := range a.modules {
		count := problemsByModule[m.Name()]
		modules[i] = uiModule{
			Name:         m.Name(),
			ProblemCount: count,
			HasProblems:  count > 0,
		}
	}

	// Kinds (sorted).
	kinds := make([]string, 0, len(a.kinds))
	for k := range a.kinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	// Problems for display.
	uiProblems := make([]uiProblem, len(problems))
	for i, p := range problems {
		uiProblems[i] = uiProblem{
			Severity: p.Severity.String(),
			Module:   p.Module,
			Key:      p.Key,
			Message:  p.Message,
			Since:    timeAgo(p.Since),
		}
	}

	return uiData{
		NodeID:       a.config.NodeID,
		DeploymentID: a.deploymentID,
		RaftState:    raftState,
		LeaderID:     string(leaderID),
		LeaderAddr:   string(leaderAddr),
		Nodes:        nodes,
		Modules:      modules,
		Kinds:        kinds,
		Problems:     uiProblems,
		GeneratedAt:  time.Now().Format(time.RFC3339),
	}
}

func (a *App) handleUI(w http.ResponseWriter, r *http.Request) {
	data := a.collectUIData()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := uiTemplates.ExecuteTemplate(w, "index.html", data); err != nil {
		a.logger.Error("failed to render dashboard", "err", err)
	}
}

func (a *App) handleDocs(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Sections []apiSection
	}{
		Sections: groupEndpoints(apiEndpoints),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := uiTemplates.ExecuteTemplate(w, "docs.html", data); err != nil {
		a.logger.Error("failed to render docs", "err", err)
	}
}

// staticSub returns the "static" subdirectory of the embedded static FS.
func staticSub() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
