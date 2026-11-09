package main

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// webviewGpuPolicy picks WebKitGTK's hardware-acceleration policy:
// MOXIE_GPU_POLICY=always|ondemand|never, default always (Wails' default).
// Measured on the library grid (2026-10-03): always and ondemand both left
// WebKitWebProcess at ~280 MB, while never (software compositing) rose to
// ~410 MB, so the override exists for drivers that render badly, not to
// save memory.
func webviewGpuPolicy() linux.WebviewGpuPolicy {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MOXIE_GPU_POLICY"))) {
	case "ondemand", "on-demand":
		return linux.WebviewGpuPolicyOnDemand
	case "never":
		return linux.WebviewGpuPolicyNever
	default:
		return linux.WebviewGpuPolicyAlways
	}
}
