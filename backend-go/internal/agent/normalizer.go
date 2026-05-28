package agent

import (
	"strings"

	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

// audioNoisePhrases lists substrings that indicate an audio_index LLM response
// is noise rather than meaningful content — identical to the Python AUDIO_NOISE tuple.
var audioNoisePhrases = []string{
	"cannot perform this analysis",
	"based on provided text",
	"i do not have access",
	"no access to the audio",
}

// compact collapses all internal whitespace in text to single spaces, strips
// leading/trailing space, then truncates to maxLen characters, appending "..."
// if truncation was necessary. Mirrors Python _compact.
func compact(text string, maxLen int) string {
	clean := strings.Join(strings.Fields(text), " ")
	if len(clean) <= maxLen {
		return clean
	}
	return clean[:maxLen-3] + "..."
}

// NormalizeEvent converts a raw pipeline event map into a typed NormalizedEvent.
// It returns nil for events that should be discarded (non-final transcripts,
// audio noise without a raw fallback, empty text, or unrecognised channels).
// This is a direct port of the Python normalize_event function.
func NormalizeEvent(raw map[string]any) *models.NormalizedEvent {
	channel := ""
	if v, ok := raw["channel"]; ok && v != nil {
		channel, _ = v.(string)
	}

	var data map[string]any
	if v, ok := raw["data"]; ok && v != nil {
		data, _ = v.(map[string]any)
	}
	if data == nil {
		data = map[string]any{}
	}

	tsStart := toFloat(data["start"])
	tsEnd := toFloat(data["end"])
	if tsEnd == 0 {
		tsEnd = tsStart
	}

	switch channel {
	case "transcript":
		if !toBool(data["is_final"]) {
			return nil
		}
		text := compact(toString(data["text"]), 260)
		if text == "" {
			return nil
		}
		return &models.NormalizedEvent{
			TimestampStart: tsStart,
			TimestampEnd:   tsEnd,
			Modality:       models.ModalityTranscript,
			Text:           text,
			IsFinal:        true,
			RawChannel:     channel,
		}

	case "scene_index", "visual_index":
		text := compact(toString(data["text"]), 380)
		if text == "" {
			return nil
		}
		return &models.NormalizedEvent{
			TimestampStart: tsStart,
			TimestampEnd:   tsEnd,
			Modality:       models.ModalityVisual,
			Text:           text,
			IsFinal:        true,
			RawChannel:     channel,
		}

	case "audio_index":
		text := compact(toString(data["text"]), 280)
		rawText := compact(toString(data["raw_text"]), 260)

		if text == "" {
			if rawText != "" {
				// Test-14 fallback: use audio raw transcript when transcript channel is sparse.
				return &models.NormalizedEvent{
					TimestampStart: tsStart,
					TimestampEnd:   tsEnd,
					Modality:       models.ModalityTranscript,
					Text:           rawText,
					IsFinal:        true,
					RawChannel:     "audio_index_raw_text",
				}
			}
			return nil
		}

		low := strings.ToLower(text)
		for _, phrase := range audioNoisePhrases {
			if strings.Contains(low, phrase) {
				if rawText != "" {
					return &models.NormalizedEvent{
						TimestampStart: tsStart,
						TimestampEnd:   tsEnd,
						Modality:       models.ModalityTranscript,
						Text:           rawText,
						IsFinal:        true,
						RawChannel:     "audio_index_raw_text",
					}
				}
				return nil
			}
		}

		return &models.NormalizedEvent{
			TimestampStart: tsStart,
			TimestampEnd:   tsEnd,
			Modality:       models.ModalityAudio,
			Text:           text,
			IsFinal:        true,
			RawChannel:     channel,
		}

	case "alert":
		label := compact(toString(data["label"]), 100)
		if label == "" {
			return nil
		}
		confidence := 1.0
		if v, ok := data["confidence"]; ok {
			confidence = toFloat(v)
		}
		return &models.NormalizedEvent{
			TimestampStart: tsStart,
			TimestampEnd:   tsEnd,
			Modality:       models.ModalityAlert,
			Text:           label,
			Confidence:     confidence,
			IsFinal:        true,
			RawChannel:     channel,
		}
	}

	return nil
}

// ── type-coercion helpers ─────────────────────────────────────────────────────

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func toFloat(v any) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func toBool(v any) bool {
	if v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}
