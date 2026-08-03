package api

import (
	"net/http"
	"strconv"
)

func (s *Server) chapterGrades(w http.ResponseWriter, r *http.Request) {
	grades, err := s.stats.ChapterGrades(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read chapter grades", err)
		return
	}
	writeJSON(w, http.StatusOK, grades)
}

func (s *Server) chapterTrend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chapter id must be a number", err)
		return
	}

	exists, err := s.db.ChapterExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read chapter", err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "no such chapter", nil)
		return
	}

	points, err := s.stats.ChapterTrend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read trend", err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) weakItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.stats.WeakItems(r.Context(), queryLimit(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not rank weak items", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) weakGrammar(w http.ResponseWriter, r *http.Request) {
	points, err := s.stats.WeakGrammarPoints(r.Context(), queryLimit(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not rank grammar points", err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	days, err := s.stats.Calendar(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build calendar", err)
		return
	}
	writeJSON(w, http.StatusOK, days)
}

// queryLimit reads ?limit=, falling back to the store's default. A malformed
// value is treated as absent rather than rejected: this only bounds a
// suggestion list, and failing the whole dashboard over it would be worse.
func queryLimit(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
