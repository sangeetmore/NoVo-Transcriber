package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"
	"github.com/rs/zerolog/log"

	"github.com/sangeetmore/novo-transcriber/backend-go/internal/adapters"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/agent"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/config"
	"github.com/sangeetmore/novo-transcriber/backend-go/internal/models"
)

// ─── Server ───────────────────────────────────────────────────────────────────

// Server is the HTTP/WebSocket front-end for the NoVo backend. It owns the
// global Hub and the mutex-protected session state.
type Server struct {
	cfg   *config.Config
	hub   *Hub
	state *serverState
}

// serverState holds everything that belongs to the currently active session.
// All fields (except the atomic counters inside SessionState) must be accessed
// while holding mu.
type serverState struct {
	mu             sync.Mutex
	session        *models.SessionState
	agent          *agent.NoteItAgent
	consumerCancel context.CancelFunc
	consumerDone   chan struct{}
	notion         *adapters.NotionWriter
	livekit        *adapters.LiveKitClient
	deepgram       *adapters.DeepgramClient
	consumerErr    string
}

// NewServer allocates a Server and its global Hub. The Hub is registered as
// the package-level singleton via SetGlobalHub so that internal packages can
// call EmitActivity without a direct reference to the Server.
func NewServer(cfg *config.Config) *Server {
	h := NewHub()
	SetGlobalHub(h)

	return &Server{
		cfg:   cfg,
		hub:   h,
		state: &serverState{},
	}
}

// Router builds and returns the chi router with all routes and middleware
// attached. Call this once and pass the result to http.ListenAndServe.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// ── Middleware stack ───────────────────────────────────────────────────
	r.Use(zerologMiddleware)
	r.Use(middleware.Recoverer)

	// CORS: allow all origins (frontend dev + production domains handled at
	// the reverse-proxy layer when needed).
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
	})
	r.Use(corsHandler.Handler)

	// ── Routes ────────────────────────────────────────────────────────────
	r.Get("/health", s.handleHealth)

	r.Route("/api/session", func(r chi.Router) {
		r.Post("/start", s.handleStart)
		r.Post("/stop", s.handleStop)
		r.Get("/status", s.handleStatus)
	})

	r.Post("/api/qa/ask", s.handleAsk)

	r.Get("/ws/activity", s.hub.ServeWS)
	r.Get("/ws/audio", s.handleAudio)

	return r
}

// ─── Route handlers ───────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, map[string]string{"status": "ok"})
}

// handleStart is implemented in session.go.
// handleStop  is implemented in session.go.
// handleStatus is implemented in session.go.
// handleAudio is implemented in session.go.

// ─── Helpers ──────────────────────────────────────────────────────────────────

// jsonOK serialises v as JSON with status 200.
func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error().Err(err).Msg("api: failed to encode JSON response")
	}
}

// jsonErr writes a JSON error envelope with the given HTTP status code.
func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// zerologMiddleware logs every request using zerolog.
func zerologMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.Status()).
			Int("bytes", ww.BytesWritten()).
			Str("remote", r.RemoteAddr).
			Msg("http")
	})
}
