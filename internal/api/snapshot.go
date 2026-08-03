package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

// MaxSnapshotBytes bounds stored client state. A flashcard session's reducer
// state is kilobytes; anything approaching this is a runaway loop appending to
// an array, and storing it would bloat the database silently.
const MaxSnapshotBytes = 1 << 20

// putSnapshot stores the client's session state verbatim.
//
// The body is validated as JSON but not decoded into a struct: the shape is the
// frontend reducer's business, and mirroring it here would create a second
// definition of session flow that could drift from the real one. Validity is
// still checked, because storing malformed JSON would only fail later on read,
// somewhere much harder to debug.
func (s *Server) putSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "session id must be a number", err)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxSnapshotBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body", err)
		return
	}
	if len(body) > MaxSnapshotBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "snapshot is too large", nil)
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "snapshot must be valid JSON", nil)
		return
	}

	if err := s.db.SaveSnapshot(r.Context(), id, string(body)); err != nil {
		writeStoreError(w, "could not save snapshot", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getSnapshot returns the stored state, or null when there is nothing to
// resume. Null rather than 404: the session exists, it simply has no snapshot,
// and making the client distinguish those two cases by status code invites
// treating a fresh session as an error.
func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "session id must be a number", err)
		return
	}

	snapshot, err := s.db.Snapshot(r.Context(), id)
	if err != nil {
		writeStoreError(w, "could not read snapshot", err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if snapshot == "" {
		w.Write([]byte("null"))
		return
	}
	// Already JSON, and already validated on the way in.
	w.Write([]byte(snapshot))
}

// openSession tells the client whether there is a session to offer resuming,
// so a reload does not silently strand one half-finished.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	id, err := s.db.OpenSession(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look for an open session", err)
		return
	}
	if id == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"sessionId": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": id})
}
