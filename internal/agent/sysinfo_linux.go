//go:build linux

package agent

import (
	"os"
	"runtime"
)

// gatherSystemInfo gathers system information on Linux.
func gatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	data, _ := os.ReadFile("/etc/os-release")
	return SystemInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   parseDistroFromOSRelease(string(data)),
	}
}
