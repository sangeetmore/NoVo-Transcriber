package models

import (
	"sync"
	"sync/atomic"
)

// SessionStatus represents the lifecycle state of a capture session.
type SessionStatus string

const (
	StatusIdle      SessionStatus = "idle"
	StatusCapturing SessionStatus = "capturing"
	StatusStopped   SessionStatus = "stopped"
	StatusFailed    SessionStatus = "failed"
)

// SessionState holds all runtime state for a single NoVo capture session.
//
// Counter fields (EventsReceived, WindowsProcessed, etc.) are read and
// written exclusively through their Incr* methods using sync/atomic so that
// they may be updated from multiple goroutines without holding mu.
//
// All other fields must be accessed while holding mu (write lock for
// mutations, read lock for reads).
type SessionState struct {
	mu sync.RWMutex

	// Core identity
	SessionID        string        `json:"session_id"`
	Status           SessionStatus `json:"status"`
	CaptureSessionID string        `json:"capture_session_id"`

	// Sandbox fields — kept for wire-format compatibility; unused in Go stack.
	SandboxID     string `json:"sandbox_id"`
	SandboxStatus string `json:"sandbox_status"`

	// LiveKit fields — Go-stack transport layer.
	LiveKitRoomName         string `json:"livekit_room_name"`
	LiveKitParticipantToken string `json:"livekit_participant_token"`

	// Real-time stream IDs — kept for wire-format compatibility.
	DisplayRTStreamID string `json:"display_rt_stream_id"`
	AudioRTStreamID   string `json:"audio_rt_stream_id"`

	// WebSocket connection tracking.
	WSConnectionID string `json:"ws_connection_id"`

	// Model configuration in use for this session.
	VisualModelInUse string `json:"visual_model_in_use"`
	AudioModelInUse  string `json:"audio_model_in_use"`

	// Notion output tracking.
	NotionPageID    string `json:"notion_page_id"`
	NotionPageURL   string `json:"notion_page_url"`
	NotionPageTitle string `json:"notion_page_title"`

	// Atomic counters — access only via Incr* methods or atomic.*Int64.
	EventsReceived      int64 `json:"events_received"`
	WindowsProcessed    int64 `json:"windows_processed"`
	WindowsSkipped      int64 `json:"windows_skipped"`
	WindowsWritten      int64 `json:"windows_written"`
	ScreenshotsCaptured int64 `json:"screenshots_captured"`
	ScreenshotFailures  int64 `json:"screenshot_failures"`

	// Curator / classifier state — protected by mu.
	LastConceptTitle string `json:"last_concept_title"`
	LastSkipReason   string `json:"last_skip_reason"`
	ClassifierResult string `json:"classifier_result"`
	ClassifierTopic  string `json:"classifier_topic"`

	// Events designed for the current session — protected by mu.
	EventsDesigned []string `json:"events_designed"`
}

// IncrEventsReceived atomically increments the EventsReceived counter by 1.
func (s *SessionState) IncrEventsReceived() {
	atomic.AddInt64(&s.EventsReceived, 1)
}

// IncrWindowsProcessed atomically increments the WindowsProcessed counter by 1.
func (s *SessionState) IncrWindowsProcessed() {
	atomic.AddInt64(&s.WindowsProcessed, 1)
}

// IncrWindowsSkipped atomically increments the WindowsSkipped counter by 1.
func (s *SessionState) IncrWindowsSkipped() {
	atomic.AddInt64(&s.WindowsSkipped, 1)
}

// IncrWindowsWritten atomically increments the WindowsWritten counter by 1.
func (s *SessionState) IncrWindowsWritten() {
	atomic.AddInt64(&s.WindowsWritten, 1)
}

// IncrScreenshotsCaptured atomically increments the ScreenshotsCaptured
// counter by 1.
func (s *SessionState) IncrScreenshotsCaptured() {
	atomic.AddInt64(&s.ScreenshotsCaptured, 1)
}

// IncrScreenshotFailures atomically increments the ScreenshotFailures
// counter by 1.
func (s *SessionState) IncrScreenshotFailures() {
	atomic.AddInt64(&s.ScreenshotFailures, 1)
}

// Lock acquires the write lock for mutating non-atomic fields.
func (s *SessionState) Lock() { s.mu.Lock() }

// Unlock releases the write lock.
func (s *SessionState) Unlock() { s.mu.Unlock() }

// RLock acquires the read lock for reading non-atomic fields.
func (s *SessionState) RLock() { s.mu.RLock() }

// RUnlock releases the read lock.
func (s *SessionState) RUnlock() { s.mu.RUnlock() }
