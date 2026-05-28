package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
)

// DeepgramClient wraps the Deepgram streaming client using raw websockets.
type DeepgramClient struct {
	cfg       *config.Config
	events    chan map[string]any
	done      chan struct{}
	mu        sync.Mutex
	connected bool
	conn      *websocket.Conn
}

// NewDeepgramClient creates a new Deepgram STT client.
func NewDeepgramClient(cfg *config.Config) *DeepgramClient {
	return &DeepgramClient{
		cfg:    cfg,
		events: make(chan map[string]any, 256),
		done:   make(chan struct{}),
	}
}

// TranscriptEvents returns a read-only channel emitting transcript events.
func (d *DeepgramClient) TranscriptEvents() <-chan map[string]any {
	return d.events
}

// Start initiates the WebSocket connection to Deepgram.
func (d *DeepgramClient) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cfg.DeepgramAPIKey == "" {
		log.Warn().Msg("DeepgramAPIKey is empty, STT disabled")
		return nil
	}

	url := "wss://api.deepgram.com/v1/listen?model=nova-2&language=en&smart_format=true&interim_results=true"
	
	header := http.Header{}
	header.Set("Authorization", "Token "+d.cfg.DeepgramAPIKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("failed to connect to Deepgram WS: %w", err)
	}

	d.conn = conn
	d.connected = true

	// Read loop
	go func() {
		defer d.Stop()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Error().Err(err).Msg("Deepgram WS read error")
				}
				return
			}

			var msg map[string]any
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Warn().Err(err).Str("raw", string(message)).Msg("deepgram non-json message")
				continue
			}
			
			msgType, _ := msg["type"].(string)
			if msgType == "Error" || msgType == "Warning" {
				log.Warn().RawJSON("payload", message).Msg("deepgram error/warning")
			} else if msgType != "Results" {
				log.Debug().Str("type", msgType).Msg("deepgram event")
			}

			// Parse Deepgram response format
			channel, ok := msg["channel"].(map[string]any)
			if !ok {
				continue
			}
			alts, ok := channel["alternatives"].([]any)
			if !ok || len(alts) == 0 {
				continue
			}
			alt, ok := alts[0].(map[string]any)
			if !ok {
				continue
			}
			transcript, _ := alt["transcript"].(string)
			if transcript == "" {
				continue
			}
			
			start, _ := msg["start"].(float64)
			duration, _ := msg["duration"].(float64)
			isFinal, _ := msg["is_final"].(bool)

			event := map[string]any{
				"channel": "transcript",
				"data": map[string]any{
					"text":     transcript,
					"start":    start,
					"end":      start + duration,
					"is_final": isFinal,
				},
			}

			select {
			case d.events <- event:
			default:
				log.Warn().Msg("Deepgram event channel full, dropping event")
			}
		}
	}()

	log.Info().Msg("Deepgram WebSocket connected")
	return nil
}

// SendAudio sends raw PCM audio bytes to Deepgram.
func (d *DeepgramClient) SendAudio(data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.connected || d.conn == nil {
		return fmt.Errorf("Deepgram client not connected")
	}

	if err := d.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to send audio to Deepgram: %w", err)
	}
	return nil
}

// Stop cleanly closes the Deepgram connection.
func (d *DeepgramClient) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.connected && d.conn != nil {
		// Attempt clean close
		msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		d.conn.WriteMessage(websocket.CloseMessage, msg)
		d.conn.Close()
		d.connected = false
	}
	select {
	case <-d.done:
	default:
		close(d.done)
		close(d.events)
	}
	return nil
}
