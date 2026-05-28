package api

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/sangeetmore/novo-transcriber/backend-go/internal/adapters"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/agent"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleStart creates a new session, wires up adapters, and begins capture.
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if s.state.session != nil && s.state.session.Status == models.StatusCapturing {
		jsonErr(w, http.StatusConflict, "session already active")
		return
	}

	sess := &models.SessionState{
		SessionID: uuid.New().String(),
		Status:    models.StatusCapturing,
	}
	s.state.session = sess
	s.state.consumerErr = ""

	ctx, cancel := context.WithCancel(context.Background())
	s.state.consumerCancel = cancel
	done := make(chan struct{})
	s.state.consumerDone = done

	sess.CaptureSessionID = "room-" + sess.SessionID

	// Create LiveKit client and generate token
	if s.state.livekit == nil {
		s.state.livekit = adapters.NewLiveKitClient(s.cfg)
	}
	
	if s.cfg.LiveKitAPIKey != "" {
		_ = s.state.livekit.CreateRoom(context.Background(), sess.CaptureSessionID)
		token, err := s.state.livekit.GenerateParticipantToken("capture-client", sess.CaptureSessionID, s.cfg.CaptureClientTokenTTL)
		if err == nil {
			sess.LiveKitParticipantToken = token
		}
	} else {
		// Mock token so frontend doesn't crash if no LiveKit is configured yet
		sess.LiveKitParticipantToken = "mock_token"
	}

	// Initialize core clients if not already done
	if s.state.notion == nil {
		s.state.notion = adapters.NewNotionWriter(s.cfg)
	}

	pageID, pageURL, err := s.state.notion.CreatePage(context.Background(), "Video Notes")
	if err != nil {
		log.Warn().Err(err).Msg("failed to create notion page")
	} else {
		sess.NotionPageID = pageID
		sess.NotionPageURL = pageURL
	}
	if s.state.deepgram == nil {
		s.state.deepgram = adapters.NewDeepgramClient(s.cfg)
	}

	llmClient := adapters.NewClient(s.cfg)
	s.state.agent = agent.NewNoteItAgent(sess, s.state.notion, llmClient, s.cfg, EmitActivity)

	go func() {
		defer close(done)
		log.Info().Str("session_id", sess.SessionID).Msg("session consumer started")

		// Start Notion flush loop
		s.state.notion.Start(ctx)
		defer s.state.notion.Stop(context.Background())

		// Start Deepgram WS
		if err := s.state.deepgram.Start(ctx); err != nil {
			log.Error().Err(err).Msg("failed to start deepgram")
			s.state.mu.Lock()
			s.state.consumerErr = err.Error()
			s.state.mu.Unlock()
			return
		}
		defer s.state.deepgram.Stop()

		transcriptChan := s.state.deepgram.TranscriptEvents()

		// 2.5s ticker for checking orchestrator window
		tick := time.NewTicker(2500 * time.Millisecond)
		defer tick.Stop()

		// 15s ticker for screenshots
		var shotTick *time.Ticker
		if s.cfg.ScreenshotLocalFallback {
			shotTick = time.NewTicker(15 * time.Second)
			defer shotTick.Stop()
		}

		for {
			select {
			case <-ctx.Done():
				// Flush remainder
				_ = s.state.agent.FlushOnStop(context.Background())
				log.Info().Str("session_id", sess.SessionID).Msg("session consumer stopped")
				return

			case ev, ok := <-transcriptChan:
				if ok {
					s.state.agent.IngestRawEvent(ev)
					
					// Emit transcript to frontend activity feed if it has text
					if data, ok := ev["data"].(map[string]any); ok {
						if text, ok := data["text"].(string); ok && text != "" {
							EmitActivity("transcript", "🗣️", "Transcript", text, nil)
						}
					}
				}

			case <-tick.C:
				_ = s.state.agent.ProcessIfReady(ctx)

			default:
				// If shotTick is enabled, process it
				if shotTick != nil {
					select {
					case <-shotTick.C:
						// Fire and forget local screenshot
						go func() {
							now := float64(time.Now().UnixMilli()) / 1000.0
							_, err := adapters.CaptureLocalScreenshot(ctx, s.cfg, now-1.0, now, "middle")
							if err == nil {
								// In a real flow, pass path to VisionClient
								EmitActivity("agent", "📸", "Captured screen", "Local Desktop", nil)
							}
						}()
					default:
					}
				}
			}
		}
	}()

	EmitActivity("system", "▶", "Session started", sess.SessionID, nil)

	jsonOK(w, map[string]any{
		"session_id":         sess.SessionID,
		"capture_session_id": sess.CaptureSessionID,
		"client_token":       sess.LiveKitParticipantToken,
		"sandbox_id":         sess.SandboxID,
		"notion_page_url":    sess.NotionPageURL,
	})
}

// handleStop tears down the active session cleanly.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.state.mu.Lock()

	if s.state.session == nil {
		s.state.mu.Unlock()
		jsonErr(w, http.StatusNotFound, "no active session")
		return
	}
	if s.state.session.Status != models.StatusCapturing {
		s.state.mu.Unlock()
		jsonErr(w, http.StatusConflict, "session is not capturing")
		return
	}

	cancel := s.state.consumerCancel
	done := s.state.consumerDone
	sess := s.state.session
	sess.Status = models.StatusStopped
	s.state.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	EmitActivity("system", "⏹", "Session stopped", sess.SessionID, nil)

	jsonOK(w, sess)
}

// handleStatus returns a snapshot of the current session state (or a no-session
// sentinel when idle).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.state.mu.Lock()
	sess := s.state.session
	consumerErr := s.state.consumerErr
	s.state.mu.Unlock()

	if sess == nil {
		jsonOK(w, map[string]any{
			"status":       string(models.StatusIdle),
			"consumer_err": "",
		})
		return
	}

	jsonOK(w, map[string]any{
		"session":      sess,
		"consumer_err": consumerErr,
	})
}

// handleAudio upgrades the connection and routes PCM data to Deepgram.
func (s *Server) handleAudio(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to upgrade audio ws")
		return
	}
	defer conn.Close()

	log.Info().Msg("Audio WebSocket connected from frontend")

	chunkCount := 0
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			log.Info().Msg("Audio WebSocket closed")
			break
		}

		// Debug log every single message from frontend for the first 5 messages
		if chunkCount < 5 {
			log.Debug().Int("messageType", mt).Int("bytes", len(msg)).Msg("Received WS message from frontend")
		}

		s.state.mu.Lock()
		dg := s.state.deepgram
		status := models.StatusIdle
		if s.state.session != nil {
			status = s.state.session.Status
		}
		s.state.mu.Unlock()

		if status == models.StatusCapturing && dg != nil && mt == websocket.BinaryMessage {
			chunkCount++
			if chunkCount%20 == 0 {
				log.Debug().Int("chunks", chunkCount).Int("bytes", len(msg)).Msg("Audio streaming to deepgram")
			}
			_ = dg.SendAudio(msg)
		}
	}
}
