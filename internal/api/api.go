// Package api serves the HTTP interface over the store.
//
// There is no authentication and no user table: this binds to localhost and
// serves exactly one person. Anything that would need auth is out of scope by
// construction, not forgotten.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/dominicgodfrey/korLearn/internal/lexicon"
	"github.com/dominicgodfrey/korLearn/internal/stats"
	"github.com/dominicgodfrey/korLearn/internal/store"
	"github.com/dominicgodfrey/korLearn/internal/tts"
)

type Server struct {
	db *store.DB
	// audio may be nil, in which case the tts endpoint reports 503 rather than
	// the binary refusing to start without a sidecar running.
	audio *tts.Cache
	stats *stats.Stats
}

func New(db *store.DB, audio *tts.Cache) *Server {
	return &Server{db: db, audio: audio, stats: stats.New(db)}
}

// Routes returns the mux. Patterns use the method-and-wildcard syntax added in
// Go 1.22, so no third-party router is needed.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/chapters", s.listChapters)
	mux.HandleFunc("GET /api/chapters/{id}", s.chapterDetail)
	mux.HandleFunc("GET /api/chapters/{id}/vocab", s.chapterVocab)
	mux.HandleFunc("GET /api/chapters/{id}/lexicon", s.chapterLexicon)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("POST /api/sessions/{id}/end", s.endSession)
	mux.HandleFunc("POST /api/attempts", s.createAttempt)
	mux.HandleFunc("GET /api/sessions/open", s.openSession)
	mux.HandleFunc("PUT /api/sessions/{id}/snapshot", s.putSnapshot)
	mux.HandleFunc("GET /api/sessions/{id}/snapshot", s.getSnapshot)
	mux.HandleFunc("GET /api/tts", s.speak)
	mux.HandleFunc("GET /api/stats/chapters", s.chapterGrades)
	mux.HandleFunc("GET /api/stats/weak", s.weakItems)
	mux.HandleFunc("GET /api/stats/grammar", s.weakGrammar)
	mux.HandleFunc("GET /api/stats/calendar", s.calendar)
	mux.HandleFunc("GET /api/chapters/{id}/trend", s.chapterTrend)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listChapters(w http.ResponseWriter, r *http.Request) {
	chapters, err := s.db.Chapters(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list chapters", err)
		return
	}
	writeJSON(w, http.StatusOK, chapters)
}

func (s *Server) chapterVocab(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chapter id must be a number", err)
		return
	}

	// An unknown chapter and a chapter with no vocab both return zero rows;
	// only one of them is a mistake worth reporting.
	exists, err := s.db.ChapterExists(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read chapter", err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "no such chapter", nil)
		return
	}

	vocab, err := s.db.ChapterVocab(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read vocab", err)
		return
	}
	writeJSON(w, http.StatusOK, vocab)
}

// chapterDetail backs the chapter page: the intro it opens with, what each
// module has to work with, and which modules have been finished.
func (s *Server) chapterDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chapter id must be a number", err)
		return
	}

	detail, err := s.db.ChapterByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, "could not read chapter", err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// chapterLexicon returns everything unlocked at this chapter's position: its
// own vocabulary and grammar plus everything from earlier chapters.
//
// This is what keeps a module honest. Multiple-choice distractors and matching
// decoys come from here rather than from the whole vocab table — a distractor
// from lesson 12 teaches lesson 12 — and it is the word list handed to the LLM
// for generated drills and conversation.
func (s *Server) chapterLexicon(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chapter id must be a number", err)
		return
	}

	set, ok, err := lexicon.LoadForChapter(r.Context(), s.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read lexicon", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no such chapter", nil)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so this can only be logged.
		log.Printf("write response: %v", err)
	}
}

// writeError sends a short message to the client and the underlying cause to
// the log. The client is the developer here, but the log is where a stack of
// SQL errors belongs.
func writeError(w http.ResponseWriter, status int, msg string, err error) {
	if err != nil {
		log.Printf("%d %s: %v", status, msg, err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
