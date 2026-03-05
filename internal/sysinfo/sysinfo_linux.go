//go:build linux

package sysinfo

import (
	"os"
	"runtime"
)

// GatherSystemInfo gathers system information on Linux.
func GatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	data, _ := os.ReadFile("/etc/os-release")
	return SystemInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   parseDistroFromOSRelease(string(data)),
	}
}
