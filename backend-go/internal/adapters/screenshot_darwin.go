//go:build darwin

package adapters

import (
	"fmt"
	"os/exec"
)

// extractLocalScreenshot captures the screen using `screencapture` on macOS.
func extractLocalScreenshot(outputPath string) error {
	cmd := exec.Command("screencapture", "-x", outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("local screenshot failed (screencapture): %w", err)
	}
	return nil
}
