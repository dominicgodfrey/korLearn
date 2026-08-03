package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/dominicgodfrey/korLearn/internal/store"
)

type createSessionRequest struct {
	// ChapterID is null for comprehensive quizzes spanning the curriculum.
	ChapterID *int64 `json:"chapterId"`
	Mode      string `json:"mode"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Mode == "" {
		writeError(w, http.StatusBadRequest, "mode is required", nil)
		return
	}

	id, err := s.db.CreateSession(r.Context(), req.ChapterID, req.Mode)
	if err != nil {
		writeStoreError(w, "could not create session", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) createAttempt(w http.ResponseWriter, r *http.Request) {
	var a store.Attempt
	if !decodeJSON(w, r, &a) {
		return
	}
	if !store.ValidItemType(a.ItemType) {
		writeError(w, http.StatusBadRequest, "itemType must be vocab, exercise, or passage_question", nil)
		return
	}
	if !store.ValidStage(a.Stage) {
		writeError(w, http.StatusBadRequest, "stage must be flip, mc, or typed", nil)
		return
	}

	id, err := s.db.RecordAttempt(r.Context(), a)
	if err != nil {
		writeStoreError(w, "could not record attempt", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "session id must be a number", err)
		return
	}

	result, err := s.db.EndSession(r.Context(), id)
	if err != nil {
		writeStoreError(w, "could not end session", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// decodeJSON reads a request body strictly, reporting whether it succeeded.
// Unknown fields are rejected: a renamed field on the client would otherwise
// arrive as a zero value and be recorded as a real answer.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), nil)
		return false
	}
	return true
}

// writeStoreError maps the store's sentinel errors onto status codes so each
// handler does not have to.
func writeStoreError(w http.ResponseWriter, msg string, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, store.ErrSessionClosed):
		// The session is finished and its score published; appending to it now
		// would silently invalidate that number.
		writeError(w, http.StatusConflict, err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, msg, err)
	}
}
