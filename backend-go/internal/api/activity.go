package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	hubBroadcastBuf = 256
	hubRingMax      = 200

	// WebSocket tuning
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
	wsMaxMsgSize = 4096
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ─── Hub ─────────────────────────────────────────────────────────────────────

// Hub manages WebSocket clients and broadcasts activity events to all of them.
// The ring buffer (max 200 entries) is replayed to every newly connected client
// so it receives recent history immediately.
type Hub struct {
	mu        sync.RWMutex
	clients   map[*hubClient]struct{}
	broadcast chan map[string]any
	log       []map[string]any // ring buffer, cap hubRingMax
}

// hubClient wraps a single WebSocket connection with a per-client send queue.
type hubClient struct {
	conn *websocket.Conn
	send chan map[string]any
}

// NewHub allocates and returns an idle Hub. Call Run to start processing.
func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*hubClient]struct{}),
		broadcast: make(chan map[string]any, hubBroadcastBuf),
		log:       make([]map[string]any, 0, hubRingMax),
	}
}

// Run processes the broadcast channel until ctx is cancelled.
// It must be started in its own goroutine.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for c := range h.clients {
				close(c.send)
				delete(h.clients, c)
			}
			h.mu.Unlock()
			return

		case evt, ok := <-h.broadcast:
			if !ok {
				return
			}
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- evt:
				default:
					// Slow client — drop the message rather than block the hub.
					log.Warn().Msg("activity hub: slow client, dropping event")
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Emit builds an agent_event map, appends it to the ring buffer and fans it
// out to all connected clients (non-blocking).
func (h *Hub) Emit(category, icon, label, detail string, metadata map[string]any) {
	evt := map[string]any{
		"type":      "agent_event",
		"timestamp": time.Now().Unix(),
		"category":  category,
		"icon":      icon,
		"label":     label,
		"detail":    detail,
		"metadata":  metadata,
	}

	h.mu.Lock()
	if len(h.log) >= hubRingMax {
		// Overwrite the oldest entry by shifting the slice.
		copy(h.log, h.log[1:])
		h.log[hubRingMax-1] = evt
	} else {
		h.log = append(h.log, evt)
	}
	snapshot := make([]map[string]any, len(h.log))
	copy(snapshot, h.log)
	h.mu.Unlock()
	_ = snapshot // snapshot held only to keep the ring consistent under the lock

	select {
	case h.broadcast <- evt:
	default:
		log.Warn().Msg("activity hub: broadcast channel full, dropping event")
	}
}

// ServeWS upgrades an HTTP connection to WebSocket, replays the ring buffer to
// the new client, then runs read/write pumps concurrently.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("activity hub: WebSocket upgrade failed")
		return
	}

	c := &hubClient{
		conn: conn,
		send: make(chan map[string]any, 64),
	}

	// Register client.
	h.mu.Lock()
	h.clients[c] = struct{}{}
	// Snapshot the current ring buffer so we can replay it without holding the lock.
	replay := make([]map[string]any, len(h.log))
	copy(replay, h.log)
	h.mu.Unlock()

	// Replay history to the new client.
	conn.SetWriteDeadline(time.Now().Add(wsWriteWait)) //nolint:errcheck
	for _, evt := range replay {
		if err := conn.WriteJSON(evt); err != nil {
			log.Error().Err(err).Msg("activity hub: replay write error")
			h.unregister(c)
			conn.Close()
			return
		}
	}

	go h.writePump(c)
	go h.readPump(c)
}

// unregister removes a client from the hub and closes its send channel.
func (h *Hub) unregister(c *hubClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

// readPump keeps the read loop alive for ping/pong handling. Browsers require
// the server to handle pong frames, otherwise the connection is closed.
func (h *Hub) readPump(c *hubClient) {
	defer func() {
		h.unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(wsMaxMsgSize)
	c.conn.SetReadDeadline(time.Now().Add(wsPongWait)) //nolint:errcheck
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(wsPongWait)) //nolint:errcheck
		return nil
	})

	for {
		// We don't use messages from the client but we must drain the read side.
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
			) {
				log.Warn().Err(err).Msg("activity hub: unexpected WebSocket close")
			}
			return
		}
	}
}

// writePump drains the client's send queue and sends ping frames on schedule.
func (h *Hub) writePump(c *hubClient) {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case evt, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)) //nolint:errcheck
			if !ok {
				// Hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
				return
			}
			if err := c.conn.WriteJSON(evt); err != nil {
				log.Warn().Err(err).Msg("activity hub: write error, closing client")
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)) //nolint:errcheck
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ─── Package-level singleton ──────────────────────────────────────────────────

var globalHub *Hub

// SetGlobalHub registers h as the package-level hub used by EmitActivity.
// Must be called before any call to EmitActivity.
func SetGlobalHub(h *Hub) {
	globalHub = h
}

// EmitActivity is a package-level convenience wrapper around the global Hub's
// Emit method. It silently no-ops when no global hub has been registered.
func EmitActivity(category, icon, label, detail string, metadata map[string]any) {
	if globalHub == nil {
		return
	}
	globalHub.Emit(category, icon, label, detail, metadata)
}
