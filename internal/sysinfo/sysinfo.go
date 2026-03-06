package sysinfo

import (
	"strings"
)

// SystemInfo contains basic system information about the host.
type SystemInfo struct {
	Hostname string
	OS       string
	Arch     string
	Distro   string
}

// parseDistroFromOSRelease extracts the PRETTY_NAME field from /etc/os-release content.
// Returns "unknown" if the field is not found or content is empty.
func parseDistroFromOSRelease(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if after, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			val := after
			return strings.Trim(val, "\"")
		}
	}
	return "unknown"
}

// parseDistroFromSwVers extracts ProductName and ProductVersion from sw_vers output.
// Returns "unknown" if either field is missing.
func parseDistroFromSwVers(content string) string {
	var name, version string
	for line := range strings.SplitSeq(content, "\n") {
		if after, ok := strings.CutPrefix(line, "ProductName:"); ok {
			name = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "ProductVersion:"); ok {
			version = strings.TrimSpace(after)
		}
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	return "unknown"
}
