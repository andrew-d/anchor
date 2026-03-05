//go:build !linux && !darwin

package sysinfo

import (
	"os"
	"runtime"
)

// GatherSystemInfo gathers system information on unsupported platforms.
// Returns basic info with "unknown" for Distro since no platform-specific method is available.
func GatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	return SystemInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   "unknown",
	}
}
