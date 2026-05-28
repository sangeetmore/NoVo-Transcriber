//go:build linux

package adapters

import (
	"fmt"
	"os/exec"
)

// extractLocalScreenshot captures the screen using `scrot` or `import` on Linux.
func extractLocalScreenshot(outputPath string) error {
	// Try scrot first
	cmd := exec.Command("scrot", "-z", outputPath)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fallback to ImageMagick import
	cmd = exec.Command("import", "-window", "root", outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("local screenshot failed (scrot and import): %w", err)
	}
	return nil
}
