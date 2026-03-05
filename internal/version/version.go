package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// version is set via ldflags:
//
//	-X github.com/andrew-d/anchor/internal/version.version=0.1.0
var version string

// Long returns the full version string, e.g. "dev (commit abc123def, dirty)"
// or "0.1.0 (commit abc123def)".
func Long() string {
	v := version
	if v == "" {
		v = "dev"
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}

	var revision string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}

	if revision == "" {
		return v
	}

	var parts []string
	if len(revision) > 9 {
		revision = revision[:9]
	}
	parts = append(parts, "commit "+revision)
	if dirty {
		parts = append(parts, "dirty")
	}
	return fmt.Sprintf("%s (%s)", v, strings.Join(parts, ", "))
}
