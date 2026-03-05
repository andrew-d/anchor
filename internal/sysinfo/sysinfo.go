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
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(val, "\"")
		}
	}
	return "unknown"
}

// parseDistroFromSwVers extracts ProductName and ProductVersion from sw_vers output.
// Returns "unknown" if either field is missing.
func parseDistroFromSwVers(content string) string {
	var name, version string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "ProductName:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "ProductName:"))
		}
		if strings.HasPrefix(line, "ProductVersion:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "ProductVersion:"))
		}
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	return "unknown"
}
