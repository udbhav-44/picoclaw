package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type SystemTool struct{}

func NewSystemTool() *SystemTool {
	return &SystemTool{}
}

func (t *SystemTool) Name() string {
	return "system_stats"
}

func (t *SystemTool) Description() string {
	return "Get current system statistics including OS/Arch, Process Memory, Disk usage, and Uptime/Load."
}

func (t *SystemTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *SystemTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	var sb strings.Builder

	// Host Info
	sb.WriteString(fmt.Sprintf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH))

	hostname, err := os.Hostname()
	if err == nil {
		sb.WriteString(fmt.Sprintf("Hostname: %s\n", hostname))
	}

	// Memory (Process level since system-level memory is platform specific)
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	allocGB := float64(memStats.Alloc) / 1024 / 1024 / 1024
	sysGB := float64(memStats.Sys) / 1024 / 1024 / 1024
	sb.WriteString(fmt.Sprintf("Process Memory: %.2f GB allocated / %.2f GB sys\n", allocGB, sysGB))

	// OS-specific commands for System-level Disk/Uptime
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// Disk Usage
		dfOut, err := exec.Command("df", "-h", "/").Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(dfOut)), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(lines[1])
				// usually: Filesystem Size Used Avail Capacity iused ifree %iused Mounted
				if len(fields) >= 5 {
					sb.WriteString(fmt.Sprintf("Disk (/): %s used / %s total (%s)\n", fields[2], fields[1], fields[4]))
				} else {
					sb.WriteString(fmt.Sprintf("Disk (/): %s\n", lines[1]))
				}
			}
		}

		// Uptime & Load Avg
		uptimeOut, err := exec.Command("uptime").Output()
		if err == nil {
			sb.WriteString(fmt.Sprintf("Uptime & Load: %s\n", strings.TrimSpace(string(uptimeOut))))
		}
	}

	return &ToolResult{
		ForLLM:  sb.String(),
		ForUser: "System stats retrieved.",
	}
}
