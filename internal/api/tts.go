package api

import (
	"errors"
	"net/http"

	"github.com/dominicgodfrey/korLearn/internal/tts"
)

// speak serves synthesized audio for ?text=. The response is a plain wav file
// served from the cache directory, so the browser gets range requests and
// conditional GETs for free from net/http.
func (s *Server) speak(w http.ResponseWriter, r *http.Request) {
	if s.audio == nil {
		writeError(w, http.StatusServiceUnavailable, "audio is not configured", nil)
		return
	}

	text := r.URL.Query().Get("text")
	path, err := s.audio.Path(r.Context(), text)
	switch {
	case errors.Is(err, tts.ErrEmptyText):
		writeError(w, http.StatusBadRequest, "text is required", nil)
		return
	case errors.Is(err, tts.ErrTextTooLong):
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	case err != nil:
		// The sidecar is a separate process that may simply not be running.
		writeError(w, http.StatusBadGateway, "could not synthesize audio", err)
		return
	}

	// Synthesis is deterministic for a given voice and text, and the text is
	// the cache key, so this response can never go stale.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}
