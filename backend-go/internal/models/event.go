package models

import "strings"

// Modality represents the type of content in a normalized event.
type Modality string

const (
	ModalityTranscript Modality = "transcript"
	ModalityVisual     Modality = "visual"
	ModalityAudio      Modality = "audio"
	ModalityAlert      Modality = "alert"
	ModalitySystem     Modality = "system"
)

// NormalizedEvent is a single piece of captured content from any source,
// normalized into a common envelope before being placed into an EventWindow.
type NormalizedEvent struct {
	TimestampStart float64  `json:"timestamp_start"`
	TimestampEnd   float64  `json:"timestamp_end"`
	Modality       Modality `json:"modality"`
	Text           string   `json:"text"`
	// Confidence defaults to 1.0 when not supplied by the source provider.
	Confidence float64 `json:"confidence"`
	// IsFinal indicates the event will not be superseded by a later update.
	// Defaults to true.
	IsFinal    bool   `json:"is_final"`
	RawChannel string `json:"raw_channel"`
}

// EventWindow is a time-bounded collection of NormalizedEvents that the
// curator evaluates as a single unit of potential knowledge.
type EventWindow struct {
	WindowID  int               `json:"window_id"`
	StartTime float64           `json:"start_time"`
	EndTime   float64           `json:"end_time"`
	Events    []NormalizedEvent `json:"events"`
}

// textForModality returns the joined text of all events whose modality
// matches m, separated by newlines.
func (w *EventWindow) textForModality(m Modality) string {
	var parts []string
	for _, e := range w.Events {
		if e.Modality == m {
			parts = append(parts, e.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// TranscriptText returns the concatenated text of all transcript-modality
// events in the window.
func (w *EventWindow) TranscriptText() string {
	return w.textForModality(ModalityTranscript)
}

// VisualText returns the concatenated text of all visual-modality events
// in the window.
func (w *EventWindow) VisualText() string {
	return w.textForModality(ModalityVisual)
}

// AudioText returns the concatenated text of all audio-modality events
// in the window.
func (w *EventWindow) AudioText() string {
	return w.textForModality(ModalityAudio)
}

// HasContent reports whether the window contains at least one event with
// non-empty text across transcript, visual, or audio modalities.
func (w *EventWindow) HasContent() bool {
	return w.TranscriptText() != "" || w.VisualText() != "" || w.AudioText() != ""
}
