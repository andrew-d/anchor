//go:build !linux && !darwin

package agent

import (
	"os"
	"runtime"
)

// gatherSystemInfo gathers system information on unsupported platforms.
// Returns basic info with "unknown" for Distro since no platform-specific method is available.
func gatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	return SystemInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   "unknown",
	}
}
