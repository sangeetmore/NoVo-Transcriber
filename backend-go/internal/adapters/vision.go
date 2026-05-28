package adapters

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
)

// VisionClient handles extracting frames and sending them to the VLM.
type VisionClient struct {
	cfg    *config.Config
	llm    *Client
	events chan map[string]any
	done   chan struct{}
}

// NewVisionClient creates a VisionClient.
func NewVisionClient(cfg *config.Config, llm *Client) *VisionClient {
	return &VisionClient{
		cfg:    cfg,
		llm:    llm,
		events: make(chan map[string]any, 64),
		done:   make(chan struct{}),
	}
}

// VisualEvents returns a read-only channel emitting visual_index events.
func (v *VisionClient) VisualEvents() <-chan map[string]any {
	return v.events
}

// StartFrameExtractionLoop simulates an event loop that extracts frames periodically.
// In a full LiveKit setup, this would consume frames directly from the video track.
// For now, this is a placeholder loop that would be driven by the consumer.
func (v *VisionClient) StartFrameExtractionLoop(ctx context.Context) {
	// Not fully implemented: requires LiveKit video track subscription
}

// AnalyzeLocalFrame analyzes a local image file and emits a visual_index event.
func (v *VisionClient) AnalyzeLocalFrame(ctx context.Context, imagePath string, timestamp float64) error {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("read image: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(b)
	
	// Use the first prompt configured
	prompts := v.cfg.VisualPrompts()
	prompt := "Describe learning-relevant visual content."
	if len(prompts) > 0 {
		prompt = prompts[0]
	}

	model := v.cfg.VisionModel
	if v.cfg.UseOllamaForVision() {
		model = v.cfg.OllamaVisionModel
	}

	desc, err := v.llm.CallVision(ctx, "You are a visual analysis AI.", b64, "image/jpeg", prompt, model)
	if err != nil {
		return fmt.Errorf("vision API call failed: %w", err)
	}

	if strings.TrimSpace(desc) == "" {
		return nil
	}

	v.events <- map[string]any{
		"channel": "visual_index",
		"data": map[string]any{
			"text":  desc,
			"start": timestamp,
			"end":   timestamp + 1.0,
		},
	}
	return nil
}

// ExtractFrameFromStream extracts a frame using ffmpeg.
func ExtractFrameFromStream(streamURL string, offset float64, outputPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		"ffmpeg", "-y", "-loglevel", "error",
		"-i", streamURL,
		"-ss", fmt.Sprintf("%.3f", offset),
		"-frames:v", "1", "-q:v", "2",
		outputPath,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg error: %w (output: %s)", err, out)
	}
	return nil
}

// Close closes the vision client.
func (v *VisionClient) Close() {
	close(v.done)
	close(v.events)
}
