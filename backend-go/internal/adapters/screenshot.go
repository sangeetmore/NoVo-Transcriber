package adapters

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
)

const frameDir = "extracted_frames"

// chooseTS returns the appropriate timestamp based on the hint.
// "start" → start, "end" → end, anything else → midpoint.
func chooseTS(start, end float64, hint string) float64 {
	switch hint {
	case "start":
		return start
	case "end":
		return end
	default:
		return (start + end) / 2.0
	}
}

// CaptureLocalScreenshot attempts to capture a desktop screenshot using the local OS
// tools (scrot/screencapture). Returns the path to the captured file.
func CaptureLocalScreenshot(
	ctx context.Context,
	cfg *config.Config,
	startTS, endTS float64,
	targetHint string,
) (string, error) {
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		return "", fmt.Errorf("create frame dir: %w", err)
	}
	outputPath := filepath.Join(frameDir, fmt.Sprintf("window_%d.jpg", time.Now().Unix()))

	if err := extractLocalScreenshot(outputPath); err != nil {
		return "", fmt.Errorf("local screenshot failed: %w", err)
	}
	
	log.Debug().Str("path", outputPath).Msg("Local screenshot captured")
	return outputPath, nil
}
