package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/adapters"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

// NoteItAgent is the core orchestrator.
type NoteItAgent struct {
	session *models.SessionState
	notion  *adapters.NotionWriter
	llm     *adapters.Client
	cfg     *config.Config
	emit    func(category, icon, label, detail string, metadata map[string]any)

	mu             sync.Mutex
	buffer         []models.NormalizedEvent
	windowStart    float64
	windowID       int
	classified     bool
	eventsDesigned bool
}

func NewNoteItAgent(
	session *models.SessionState,
	notion *adapters.NotionWriter,
	llm *adapters.Client,
	cfg *config.Config,
	emit func(category, icon, label, detail string, metadata map[string]any),
) *NoteItAgent {
	return &NoteItAgent{
		session:     session,
		notion:      notion,
		llm:         llm,
		cfg:         cfg,
		emit:        emit,
		windowStart: 0.0,
		windowID:    0,
	}
}

func (a *NoteItAgent) IngestRawEvent(raw map[string]any) {
	evt := NormalizeEvent(raw)
	if evt == nil {
		return
	}

	a.mu.Lock()
	a.buffer = append(a.buffer, *evt)
	a.mu.Unlock()

	a.session.IncrEventsReceived()
}

func (a *NoteItAgent) windowReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.buffer) == 0 {
		return false
	}

	// Calculate timespan
	minStart := a.buffer[0].TimestampStart
	maxEnd := a.buffer[0].TimestampEnd
	for _, e := range a.buffer {
		if e.TimestampStart < minStart {
			minStart = e.TimestampStart
		}
		if e.TimestampEnd > maxEnd {
			maxEnd = e.TimestampEnd
		}
	}

	span := maxEnd - a.windowStart
	threshold := a.cfg.CuratorInitialWindowSeconds
	if a.windowID > 0 {
		threshold = a.cfg.CuratorWindowSeconds
	}

	return span >= float64(threshold)
}

func (a *NoteItAgent) closeWindow() *models.EventWindow {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.buffer) == 0 {
		return nil
	}

	maxEnd := a.buffer[0].TimestampEnd
	for _, e := range a.buffer {
		if e.TimestampEnd > maxEnd {
			maxEnd = e.TimestampEnd
		}
	}

	w := &models.EventWindow{
		WindowID:  a.windowID,
		StartTime: a.windowStart,
		EndTime:   maxEnd,
		Events:    make([]models.NormalizedEvent, len(a.buffer)),
	}
	copy(w.Events, a.buffer)

	a.windowID++
	a.windowStart = maxEnd
	a.buffer = nil

	return w
}

func (a *NoteItAgent) ProcessIfReady(ctx context.Context) error {
	if !a.windowReady() {
		return nil
	}

	window := a.closeWindow()
	if window == nil {
		return nil
	}

	a.session.RLock()
	prevTitle := a.session.LastConceptTitle
	a.session.RUnlock()

	decision, err := CurateWindow(ctx, *window, prevTitle, a.llm, a.cfg)
	if err != nil {
		log.Warn().Err(err).Msg("curate window failed, using fallback")
		decision = deterministicFallback(*window)
	}

	a.session.IncrWindowsProcessed()

	if !decision.ShouldWrite {
		a.session.IncrWindowsSkipped()
		a.emit("agent", "⏭️", "Skipped segment", decision.SkipReason, nil)
		return nil
	}

	a.notion.WriteDecision(ctx, decision)
	a.session.IncrWindowsWritten()

	a.session.Lock()
	a.session.LastConceptTitle = decision.ConceptTitle
	a.session.Unlock()

	a.emit("agent", "📝", "Notes generated", decision.ConceptTitle, nil)

	if window.WindowID == 0 {
		a.notion.Flush(ctx) // flush immediately for the first window
	}

	// Screenshot capturing logic would go here
	// go a.attachScreenshot(context.Background(), decision)

	return nil
}

func (a *NoteItAgent) FlushOnStop(ctx context.Context) error {
	window := a.closeWindow()
	if window == nil || !window.HasContent() {
		return nil
	}

	a.session.RLock()
	prevTitle := a.session.LastConceptTitle
	a.session.RUnlock()

	decision, _ := CurateWindow(ctx, *window, prevTitle, a.llm, a.cfg)
	if decision.ShouldWrite {
		a.notion.WriteDecision(ctx, decision)
		a.session.IncrWindowsWritten()
		a.emit("agent", "📝", "Final notes generated", decision.ConceptTitle, nil)
	}
	return nil
}

func (a *NoteItAgent) AnswerQuestion(ctx context.Context, question string) (string, error) {
	return fmt.Sprintf("Q&A placeholder. Received: %s", question), nil
}
