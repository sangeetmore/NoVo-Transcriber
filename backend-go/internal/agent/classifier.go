package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sangeetmore/novo-transcriber/backend-go/internal/adapters"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

// allowedContentTypes is the set of valid content_type values the classifier
// may return. Mirrors the Python validation set.
var allowedContentTypes = map[string]struct{}{
	"lecture":            {},
	"tutorial":           {},
	"coding_walkthrough": {},
	"explainer":          {},
	"demo":               {},
	"other":              {},
}

const classifierSystemPrompt = "You are the NoVo Transcriber classifier. Output JSON only."

// ClassifySession analyses the first EventWindow of a session and returns the
// content type and specific topic being taught. It is a direct port of the
// Python classify_session coroutine.
//
// On any error the function returns ("lecture", "", nil) — the same silent
// fallback as Python.
func ClassifySession(
	ctx context.Context,
	window models.EventWindow,
	llmClient *adapters.Client,
	cfg *config.Config,
) (string, string, error) {
	userPrompt := buildClassifierPrompt(window)

	raw, callErr := llmClient.CallLLMJSON(ctx, classifierSystemPrompt, userPrompt, cfg.ToolPlannerModel)
	if callErr != nil {
		return "lecture", "", nil
	}

	var result map[string]any
	if jsonErr := json.Unmarshal([]byte(raw), &result); jsonErr != nil {
		return "lecture", "", nil
	}

	ct := strings.TrimSpace(toString(result["content_type"]))
	if _, valid := allowedContentTypes[ct]; !valid {
		ct = "other"
	}
	t := strings.TrimSpace(toString(result["topic"]))

	return ct, t, nil
}

// buildClassifierPrompt constructs the user prompt from the window's text fields.
// It mirrors the f-string prompt template in Python classifier.py exactly.
func buildClassifierPrompt(window models.EventWindow) string {
	transcript := window.TranscriptText()
	if transcript == "" {
		transcript = "(none)"
	}
	visual := window.VisualText()
	if visual == "" {
		visual = "(none)"
	}

	return `
You receive the first educational video window. Identify:
1. The content type
2. The specific topic being taught

TRANSCRIPT:
` + transcript + `

VISUAL:
` + visual + `

Return strict JSON:
{
  "content_type": "lecture|tutorial|coding_walkthrough|explainer|demo|other",
  "topic": "2 to 6 words"
}

Rules:
- topic becomes the Notion page title, so make it specific and useful.
- Good: "Neural Networks Chapter 1", "Python Sets and Methods", "React useState Hook".
- Bad: "Programming Tutorial", "Math Video", "Educational Content", "Tech Tutorial".
- If unclear, return topic as an empty string.
- Do not include filler prefixes like "Tutorial:", "Lecture:", "Video about", or "Introduction to".
- Output JSON only.
`
}
