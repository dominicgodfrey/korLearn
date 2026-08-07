package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/lexicon"
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
		Intro:         "Fixture intro.",
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

// The lexicon endpoint is the app's one guarantee that a module never shows
// material from a chapter the user has not reached: it must carry earlier
// chapters forward and stop dead at the requested one.
func TestChapterLexicon(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	chapter := func(position int, word string) seed.Lesson {
		return seed.Lesson{
			SchemaVersion: seed.SchemaVersion,
			Book:          "Fixture Book",
			LessonNo:      position,
			Position:      position,
			Title:         "Fixture",
			Intro:         "Fixture intro.",
			Vocab:         []seed.Vocab{{Korean: word, English: []string{word}, POS: "noun"}},
			GrammarPoints: []seed.GrammarPoint{{
				ID: "fx" + word, Title: "Point " + word,
				Example: seed.Example{Korean: word, English: "e"},
			}},
		}
	}
	if err := db.LoadLessons(ctx, []seed.Lesson{
		chapter(1, "하나"), chapter(2, "둘"), chapter(3, "셋"),
	}); err != nil {
		t.Fatal(err)
	}

	var chapters []store.Chapter
	get(t, srv, "/api/chapters", &chapters)
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3", len(chapters))
	}

	var set lexicon.Set
	path := "/api/chapters/" + strconv.FormatInt(chapters[1].ID, 10) + "/lexicon"
	if code := get(t, srv, path, &set); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}

	if set.Position != 2 {
		t.Errorf("position = %d, want 2", set.Position)
	}
	for _, w := range set.Words {
		if w.Korean == "셋" {
			t.Error("a word from chapter 3 is unlocked at chapter 2")
		}
	}
	if len(set.Words) != 2 {
		t.Errorf("words = %+v, want 하나 and 둘", set.Words)
	}
	if len(set.Grammar) != 2 {
		t.Errorf("grammar = %+v, want 2 points", set.Grammar)
	}
}

func TestChapterLexiconNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	if code := get(t, srv, "/api/chapters/9999/lexicon", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
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
