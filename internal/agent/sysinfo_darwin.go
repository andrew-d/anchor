//go:build darwin

package agent

import (
	"os"
	"os/exec"
	"runtime"
)

// gatherSystemInfo gathers system information on macOS (darwin).
func gatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	out, _ := exec.Command("sw_vers").Output()
	return SystemInfo{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   parseDistroFromSwVers(string(out)),
	}
}
