package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
	"github.com/dominicgodfrey/korLearn/internal/store"
	"github.com/dominicgodfrey/korLearn/internal/tts"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.DB) {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	l := seed.Lesson{
		SchemaVersion: seed.SchemaVersion,
		Book:          "Fixture Book",
		LessonNo:      1,
		Position:      1,
		Title:         "Fixture",
		Vocab: []seed.Vocab{
			{Korean: "하나", English: []string{"one", "a single"}, POS: "noun"},
			{Korean: "둘", English: []string{"two"}, POS: "noun", SpeechLevel: "polite"},
		},
	}
	if err := db.LoadLessons(context.Background(), []seed.Lesson{l}); err != nil {
		t.Fatal(err)
	}

	// A stand-in for the Python sidecar, so the tts endpoint is exercised
	// without a model or a GPU.
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		w.Write([]byte("RIFF....WAVEfake"))
	}))
	t.Cleanup(sidecar.Close)

	srv := httptest.NewServer(New(db, tts.New(t.TempDir(), sidecar.URL, "kf_a")).Routes())
	t.Cleanup(srv.Close)
	return srv, db
}

func get(t *testing.T, srv *httptest.Server, path string, into any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func TestListChapters(t *testing.T) {
	srv, _ := newTestServer(t)

	var chapters []store.Chapter
	if code := get(t, srv, "/api/chapters", &chapters); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(chapters) != 1 {
		t.Fatalf("got %d chapters, want 1", len(chapters))
	}
	if chapters[0].VocabCount != 2 {
		t.Errorf("vocabCount = %d, want 2", chapters[0].VocabCount)
	}
	if chapters[0].DeadlineAt != nil {
		t.Errorf("deadlineAt = %v, want null", *chapters[0].DeadlineAt)
	}
}

func TestChapterVocab(t *testing.T) {
	srv, _ := newTestServer(t)

	var chapters []store.Chapter
	get(t, srv, "/api/chapters", &chapters)

	var vocab []store.VocabItem
	if code := get(t, srv, "/api/chapters/"+strconv.FormatInt(chapters[0].ID, 10)+"/vocab", &vocab); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(vocab) != 2 {
		t.Fatalf("got %d vocab items, want 2", len(vocab))
	}
	if got := vocab[0].English; len(got) != 2 || got[0] != "one" {
		t.Errorf("english = %v, want [one, a single]", got)
	}
	// Hangul must survive the round trip as composed syllables.
	if vocab[0].Korean != "하나" {
		t.Errorf("korean = %q", vocab[0].Korean)
	}
}

func TestChapterVocabNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	if code := get(t, srv, "/api/chapters/9999/vocab", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestChapterVocabBadID(t *testing.T) {
	srv, _ := newTestServer(t)
	if code := get(t, srv, "/api/chapters/abc/vocab", nil); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// An empty collection must serialize as [] rather than null: the frontend maps
// over it directly, and null would throw rather than render nothing.
func TestEmptyChaptersSerializeAsArray(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := httptest.NewServer(New(db, nil).Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/chapters")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Errorf("body = %s, want []", raw)
	}
}
