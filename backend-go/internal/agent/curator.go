package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/adapters"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

// curatorSystemPrompt is the SYSTEM_PROMPT constant from Python curator.py,
// preserved verbatim.
const curatorSystemPrompt = `
You are the NoVo Transcriber curator.
Turn one educational window into structured study notes.

Rules:
- Use transcript as the main content backbone.
- Use visual/audio only to improve quality and screenshot decisions.
- Always produce a concise learner-friendly note if there is any useful signal.
- Never copy-paste raw transcript chunks as the final summary.
- Keep summary to 1-2 concise sentences.
- Keep 3-5 key points, short and non-redundant.
- Return strict JSON with fields:
  should_write, skip_reason, concept_title, summary, key_points, takeaway, confidence,
  visual_evidence{should_capture,reason,target_time_hint},
  audio_signal{used,reason},
  source_grounding{transcript_used,visual_used,audio_used}
`

// sentenceBoundaryRe matches one or more whitespace characters that follow a
// sentence-ending punctuation mark — used by clipSentences.
var sentenceBoundaryRe = regexp.MustCompile(`(?:[.!?])\s+`)

// clip collapses all internal whitespace in text to single spaces, strips
// leading/trailing space, then truncates to maxChars characters, appending
// "..." if truncation was necessary. Mirrors Python _clip.
func clip(text string, maxChars int) string {
	clean := strings.TrimSpace(sentenceBoundaryRe.ReplaceAllStringFunc(
		regexp.MustCompile(`\s+`).ReplaceAllString(text, " "),
		func(s string) string { return s },
	))
	// Use a simpler, direct approach matching Python exactly.
	clean = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(text, " "))
	if len(clean) <= maxChars {
		return clean
	}
	return strings.TrimRight(clean[:maxChars-3], " ") + "..."
}

// clipSentences splits text on sentence boundaries and returns the first
// maxSentences sentences joined by a space. Mirrors Python _clip_sentences.
func clipSentences(text string, maxSentences int) string {
	src := strings.TrimSpace(text)
	if src == "" {
		return ""
	}
	// Split keeping the delimiter attached to the preceding sentence.
	indices := sentenceBoundaryRe.FindAllStringIndex(src, -1)
	if len(indices) == 0 {
		return src
	}
	var parts []string
	prev := 0
	for _, loc := range indices {
		parts = append(parts, src[prev:loc[0]+1]) // include the punctuation
		prev = loc[1]
	}
	if prev < len(src) {
		parts = append(parts, src[prev:])
	}
	// Filter blank fragments.
	var sentences []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			sentences = append(sentences, t)
		}
	}
	if len(sentences) == 0 {
		return src
	}
	if len(sentences) > maxSentences {
		sentences = sentences[:maxSentences]
	}
	return strings.Join(sentences, " ")
}

// normalizeTitle clips value to 56 characters and returns "Untitled Concept"
// when the result is empty. Mirrors Python _normalize_title.
func normalizeTitle(value string) string {
	title := clip(value, 56)
	if title == "" {
		return "Untitled Concept"
	}
	return title
}

// looksGenericPoint returns true when text starts with one of the known
// generic-sounding prefixes that the curator should discard. Mirrors Python
// _looks_generic_point.
func looksGenericPoint(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	badPrefixes := []string{
		"the instructor explains",
		"this section discusses",
		"the speaker talks about",
		"the content is about",
	}
	for _, prefix := range badPrefixes {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	return false
}

// deterministicFallback builds a CuratorDecision from the raw window text
// without calling any LLM. It is the last-resort path when all model calls
// fail. Mirrors Python _deterministic_fallback exactly.
func deterministicFallback(window models.EventWindow) models.CuratorDecision {
	transcriptLines := dedupeLines(window.TranscriptText())
	visualLines := dedupeLines(window.VisualText())
	audioLines := dedupeLines(window.AudioText())

	if len(transcriptLines) == 0 && len(visualLines) == 0 && len(audioLines) == 0 {
		return models.CuratorDecision{
			ShouldWrite: false,
			SkipReason:  "empty_window",
			Confidence:  0.0,
		}
	}

	seed := firstOf(transcriptLines, visualLines, audioLines)
	titleWords := strings.Fields(clip(seed, 80))
	if len(titleWords) > 8 {
		titleWords = titleWords[:8]
	}
	conceptTitle := normalizeTitle(strings.Join(titleWords, " "))

	summary := clip(
		fmt.Sprintf("This segment explains %s and why it matters in the current lesson.", strings.ToLower(conceptTitle)),
		220,
	)

	var keyPoints []string
	if len(transcriptLines) > 0 {
		keyPoints = append(keyPoints, clip(fmt.Sprintf("Core explanation focuses on %s.", strings.ToLower(conceptTitle)), 110))
		keyPoints = append(keyPoints, clip("The instructor gives an iterative or step-wise reasoning path.", 110))
	}
	if len(visualLines) > 0 {
		keyPoints = append(keyPoints, clip("Visual material reinforces the explanation with on-screen evidence.", 110))
	}
	if len(audioLines) > 0 && len(keyPoints) < 3 {
		keyPoints = append(keyPoints, clip("Audio cues indicate emphasis on practical understanding.", 110))
	}
	if len(keyPoints) < 3 {
		keyPoints = append(keyPoints, clip("Key details are presented as concise learning points in this window.", 110))
	}

	takeaway := clip(
		fmt.Sprintf("Takeaway: %s is presented as a practical concept to apply, not just memorize.", strings.ToLower(conceptTitle)),
		180,
	)

	var visualReason string
	if len(visualLines) > 0 {
		visualReason = clip(visualLines[0], 180)
	}
	var audioReason string
	if len(audioLines) > 0 {
		audioReason = clip(audioLines[0], 180)
	}

	return models.CuratorDecision{
		ShouldWrite:  true,
		SkipReason:   "",
		ConceptTitle: conceptTitle,
		Summary:      summary,
		KeyPoints:    keyPoints,
		Takeaway:     takeaway,
		Confidence:   0.52,
		VisualEvidence: models.VisualEvidence{
			ShouldCapture:  len(visualLines) > 0,
			Reason:         visualReason,
			TargetTimeHint: "middle",
		},
		AudioSignal: models.AudioSignal{
			Used:   len(audioLines) > 0,
			Reason: audioReason,
		},
		SourceGrounding: models.SourceGrounding{
			TranscriptUsed: len(transcriptLines) > 0,
			VisualUsed:     len(visualLines) > 0,
			AudioUsed:      len(audioLines) > 0,
		},
	}
}

// polishDecision applies product-mode post-processing to an LLM-produced
// decision: forces should_write, lifts low confidence, normalises the title,
// clips text fields, deduplicates key_points, and deterministically upgrades
// visual capture when visual text is present. Mirrors Python _polish_decision.
func polishDecision(decision *models.CuratorDecision, window models.EventWindow, previousTitle string) {
	decision.ShouldWrite = true
	decision.SkipReason = ""

	if decision.Confidence < 0.35 {
		decision.Confidence = 0.35
	}

	decision.ConceptTitle = normalizeTitle(decision.ConceptTitle)
	decision.Summary = clip(clipSentences(decision.Summary, 2), 320)
	decision.Takeaway = clip(clipSentences(decision.Takeaway, 1), 220)

	var cleaned []string
	seen := make(map[string]struct{})
	for _, point := range decision.KeyPoints {
		p := clip(point, 120)
		if p == "" || looksGenericPoint(p) {
			continue
		}
		key := strings.ToLower(p)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, p)
		if len(cleaned) >= 4 {
			break
		}
	}
	decision.KeyPoints = cleaned

	visualTextPresent := strings.TrimSpace(window.VisualText()) != ""
	if visualTextPresent && !decision.VisualEvidence.ShouldCapture {
		decision.VisualEvidence.ShouldCapture = true
		if decision.VisualEvidence.Reason == "" {
			decision.VisualEvidence.Reason = "Window contains relevant visual learning material."
		}
	}

	if decision.VisualEvidence.Reason != "" {
		decision.VisualEvidence.Reason = clip(clipSentences(decision.VisualEvidence.Reason, 1), 180)
	}
	if decision.AudioSignal.Reason != "" {
		decision.AudioSignal.Reason = clip(clipSentences(decision.AudioSignal.Reason, 1), 180)
	}

	if decision.Summary == "" && len(decision.KeyPoints) == 0 && decision.Takeaway == "" {
		decision.ShouldWrite = false
		decision.SkipReason = "empty_window"
	}
}

// buildPrompt constructs the user prompt for the curator LLM call from the
// window's text content and the previous section title. Mirrors Python
// _build_prompt exactly.
func buildPrompt(window models.EventWindow, previousTitle string) string {
	transcript := window.TranscriptText()
	if transcript == "" {
		transcript = "(none)"
	}
	visual := window.VisualText()
	if visual == "" {
		visual = "(none)"
	}
	audio := window.AudioText()
	if audio == "" {
		audio = "(none)"
	}
	prevTitle := previousTitle
	if prevTitle == "" {
		prevTitle = "(none)"
	}
	return fmt.Sprintf(`
WINDOW: %.0fs-%.0fs
PREVIOUS_TITLE: %s

TRANSCRIPT:
%s

VISUAL:
%s

AUDIO:
%s
`, window.StartTime, window.EndTime, prevTitle, transcript, visual, audio)
}

// CurateWindow is the main entry point for the curator pipeline step. It
// attempts to produce a CuratorDecision for the given EventWindow by calling
// the LLM with a model fallback chain, then applies polishDecision. When all
// LLM paths fail it returns deterministicFallback. This is a direct port of
// Python curate_window.
func CurateWindow(
	ctx context.Context,
	window models.EventWindow,
	previousTitle string,
	llmClient *adapters.Client,
	cfg *config.Config,
) (models.CuratorDecision, error) {
	prompt := buildPrompt(window, previousTitle)
	models_ := curatorModels(cfg)

	var llmErrors []string

	for _, model := range models_ {
		// ── Structured output path ────────────────────────────────────────
		var decision models.CuratorDecision
		if err := llmClient.CallStructuredOutput(ctx, curatorSystemPrompt, prompt, model, &decision); err != nil {
			llmErrors = append(llmErrors, fmt.Sprintf("%s structured: %v", model, err))
		} else {
			decision.WindowID = window.WindowID
			decision.WindowStart = window.StartTime
			decision.WindowEnd = window.EndTime
			polishDecision(&decision, window, previousTitle)
			if decision.ShouldWrite {
				log.Debug().
					Str("model", model).
					Str("path", "structured").
					Int("window_id", window.WindowID).
					Msg("curator decision produced")
				return decision, nil
			}
			break
		}

		// ── JSON fallback path ────────────────────────────────────────────
		raw, jsonErr := llmClient.CallLLMJSON(ctx, curatorSystemPrompt, prompt, model)
		if jsonErr != nil {
			llmErrors = append(llmErrors, fmt.Sprintf("%s json: %v", model, jsonErr))
			continue
		}
		var fallbackDecision models.CuratorDecision
		if unmarshalErr := json.Unmarshal([]byte(raw), &fallbackDecision); unmarshalErr != nil {
			llmErrors = append(llmErrors, fmt.Sprintf("%s json: unmarshal: %v", model, unmarshalErr))
			continue
		}
		fallbackDecision.WindowID = window.WindowID
		fallbackDecision.WindowStart = window.StartTime
		fallbackDecision.WindowEnd = window.EndTime
		polishDecision(&fallbackDecision, window, previousTitle)
		if fallbackDecision.ShouldWrite {
			log.Debug().
				Str("model", model).
				Str("path", "json").
				Int("window_id", window.WindowID).
				Msg("curator decision produced")
			return fallbackDecision, nil
		}
		break
	}

	// ── Deterministic fallback ────────────────────────────────────────────
	if len(llmErrors) > 0 {
		log.Warn().
			Strs("errors", llmErrors).
			Int("window_id", window.WindowID).
			Msg("curator falling back to deterministic output")
	}
	fb := deterministicFallback(window)
	fb.WindowID = window.WindowID
	fb.WindowStart = window.StartTime
	fb.WindowEnd = window.EndTime
	if !fb.ShouldWrite && len(llmErrors) > 0 {
		errStr := strings.Join(llmErrors, " | ")
		if len(errStr) > 220 {
			errStr = errStr[:220]
		}
		fb.Summary = "Curator unavailable: " + errStr
	}
	return fb, nil
}

// curatorModels returns the ordered model list for the fallback chain,
// de-duplicated and filtered to non-empty strings. Mirrors Python
// _curator_models (sans the use_sandbox branch which does not apply to Go).
func curatorModels(cfg *config.Config) []string {
	candidates := []string{cfg.CuratorModel, cfg.CuratorFallback}
	seen := make(map[string]struct{})
	var out []string
	for _, m := range candidates {
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// ── line helpers ──────────────────────────────────────────────────────────────

// dedupeLines splits a newline-delimited string into trimmed, non-empty lines
// with order-preserving de-duplication. Mirrors the Python list(dict.fromkeys(...))
// pattern used in _deterministic_fallback.
func dedupeLines(text string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// firstOf returns the first element of the first non-empty slice.
func firstOf(slices ...[]string) string {
	for _, s := range slices {
		if len(s) > 0 {
			return s[0]
		}
	}
	return ""
}
