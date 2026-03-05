//go:build darwin

package sysinfo

import (
	"os"
	"os/exec"
	"runtime"
)

// GatherSystemInfo gathers system information on macOS (darwin).
func GatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	out, _ := exec.Command("sw_vers").Output()
	return SystemInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   parseDistroFromSwVers(string(out)),
	}
}
