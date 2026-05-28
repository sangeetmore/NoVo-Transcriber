package models

// VisualEvidence describes whether and how a screenshot should be captured
// alongside a curator decision.
type VisualEvidence struct {
	ShouldCapture bool `json:"should_capture"`
	Reason        string `json:"reason"`
	// TargetTimeHint controls which frame within the window is preferred.
	// Valid values: "start", "middle", "end". Defaults to "middle".
	TargetTimeHint string `json:"target_time_hint"`
}

// AudioSignal describes whether audio context was meaningful for the
// curator decision.
type AudioSignal struct {
	Used   bool   `json:"used"`
	Reason string `json:"reason"`
}

// SourceGrounding records which input modalities were used when reaching
// the curator decision.
type SourceGrounding struct {
	TranscriptUsed bool `json:"transcript_used"`
	VisualUsed     bool `json:"visual_used"`
	AudioUsed      bool `json:"audio_used"`
}

// CuratorDecision is the structured output produced by the AI curator for
// a single EventWindow. It determines whether the window's content should
// be written to Notion and carries the distilled knowledge payload.
type CuratorDecision struct {
	ShouldWrite bool   `json:"should_write"`
	SkipReason  string `json:"skip_reason"`
	ConceptTitle string `json:"concept_title"`
	Summary     string `json:"summary"`
	KeyPoints   []string `json:"key_points"`
	Takeaway    string   `json:"takeaway"`
	Confidence  float64  `json:"confidence"`

	VisualEvidence  VisualEvidence  `json:"visual_evidence"`
	AudioSignal     AudioSignal     `json:"audio_signal"`
	SourceGrounding SourceGrounding `json:"source_grounding"`

	// Window provenance — echoed from the originating EventWindow.
	WindowID    int     `json:"window_id"`
	WindowStart float64 `json:"window_start"`
	WindowEnd   float64 `json:"window_end"`
}

// NewCuratorDecision returns a CuratorDecision with its zero-safe defaults
// applied: ShouldWrite is true and VisualEvidence.TargetTimeHint is "middle".
func NewCuratorDecision() CuratorDecision {
	return CuratorDecision{
		ShouldWrite: true,
		VisualEvidence: VisualEvidence{
			TargetTimeHint: "middle",
		},
	}
}
