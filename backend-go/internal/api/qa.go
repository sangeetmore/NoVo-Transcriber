package api

import (
	"encoding/json"
	"net/http"
)

// AskRequest is the JSON body for POST /api/qa/ask.
type AskRequest struct {
	Question string `json:"question"`
}

// handleAsk answers a natural-language question about the active session using
// the NoteItAgent. It returns 404 when no session is in progress.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Question == "" {
		jsonErr(w, http.StatusBadRequest, "question must not be empty")
		return
	}

	s.state.mu.Lock()
	ag := s.state.agent
	s.state.mu.Unlock()

	if ag == nil {
		jsonErr(w, http.StatusNotFound, "no active session")
		return
	}

	answer, err := ag.AnswerQuestion(r.Context(), req.Question)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "agent error: "+err.Error())
		return
	}

	jsonOK(w, map[string]any{"answer": answer})
}
