package anchor

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"
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

func (a *App) collectUIData() uiData {
	// Raft state.
	leaderAddr, leaderID := a.raft.LeaderWithID()
	raftState := a.raft.State().String()

	// Cluster nodes.
	var nodes []uiNode
	configFuture := a.raft.GetConfiguration()
	if configFuture.Error() == nil {
		for _, s := range configFuture.Configuration().Servers {
			nodes = append(nodes, uiNode{
				ID:       string(s.ID),
				Address:  string(s.Address),
				IsThis:   string(s.ID) == a.config.NodeID,
				IsLeader: s.ID == leaderID,
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

// staticSub returns the "static" subdirectory of the embedded static FS.
func staticSub() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
