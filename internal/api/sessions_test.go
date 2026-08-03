package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/store"
)

func post(t *testing.T, srv *httptest.Server, path, body string, into any) int {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewBufferString(body))
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

// The whole Milestone 1 write path: open a session, record attempts against
// real vocab, close it, and get back a score.
func TestSessionFlow(t *testing.T) {
	srv, _ := newTestServer(t)

	var vocab []store.VocabItem
	get(t, srv, "/api/chapters/1/vocab", &vocab)
	if len(vocab) != 2 {
		t.Fatalf("fixture has %d vocab items, want 2", len(vocab))
	}

	var created struct {
		ID int64 `json:"id"`
	}
	if code := post(t, srv, "/api/sessions", `{"chapterId":1,"mode":"flashcards"}`, &created); code != http.StatusCreated {
		t.Fatalf("create session status = %d, want 201", code)
	}

	// First item missed then corrected, second right first time: 0.5.
	attempts := []string{
		`{"sessionId":%d,"itemType":"vocab","itemId":` + strconv.FormatInt(vocab[0].ID, 10) + `,"stage":"mc","correct":false,"originalCorrect":false,"userAnswer":"wrong","rubric":null,"elapsedMs":1200}`,
		`{"sessionId":%d,"itemType":"vocab","itemId":` + strconv.FormatInt(vocab[0].ID, 10) + `,"stage":"typed","correct":true,"originalCorrect":true,"userAnswer":"하나","rubric":null,"elapsedMs":900}`,
		`{"sessionId":%d,"itemType":"vocab","itemId":` + strconv.FormatInt(vocab[1].ID, 10) + `,"stage":"mc","correct":true,"originalCorrect":true,"userAnswer":"둘","rubric":null,"elapsedMs":700}`,
	}
	for i, tmpl := range attempts {
		body := fmt.Sprintf(tmpl, created.ID)
		if code := post(t, srv, "/api/attempts", body, nil); code != http.StatusCreated {
			t.Fatalf("attempt %d status = %d, want 201", i, code)
		}
	}

	var result store.SessionResult
	path := "/api/sessions/" + strconv.FormatInt(created.ID, 10) + "/end"
	if code := post(t, srv, path, "", &result); code != http.StatusOK {
		t.Fatalf("end session status = %d, want 200", code)
	}
	if result.Score == nil || *result.Score != 0.5 {
		t.Errorf("score = %v, want 0.5", result.Score)
	}
	if result.EndedAt == "" {
		t.Error("endedAt is empty")
	}

	// Ending again must not republish a different number.
	if code := post(t, srv, path, "", nil); code != http.StatusConflict {
		t.Errorf("second end status = %d, want 409", code)
	}
}

func TestAttemptValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	var created struct {
		ID int64 `json:"id"`
	}
	post(t, srv, "/api/sessions", `{"chapterId":1,"mode":"flashcards"}`, &created)
	id := strconv.FormatInt(created.ID, 10)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"bad item type", `{"sessionId":` + id + `,"itemType":"nonsense","itemId":1,"stage":"mc","correct":true,"originalCorrect":true,"userAnswer":"","rubric":null,"elapsedMs":0}`, http.StatusBadRequest},
		{"bad stage", `{"sessionId":` + id + `,"itemType":"vocab","itemId":1,"stage":"guess","correct":true,"originalCorrect":true,"userAnswer":"","rubric":null,"elapsedMs":0}`, http.StatusBadRequest},
		{"unknown item", `{"sessionId":` + id + `,"itemType":"vocab","itemId":9999,"stage":"mc","correct":true,"originalCorrect":true,"userAnswer":"","rubric":null,"elapsedMs":0}`, http.StatusNotFound},
		{"unknown session", `{"sessionId":9999,"itemType":"vocab","itemId":1,"stage":"mc","correct":true,"originalCorrect":true,"userAnswer":"","rubric":null,"elapsedMs":0}`, http.StatusNotFound},
		{"malformed json", `{"sessionId":`, http.StatusBadRequest},
		// A renamed client field must fail loudly rather than arrive as a zero
		// value and be recorded as a real answer.
		{"unknown field", `{"sessionId":` + id + `,"itemType":"vocab","itemId":1,"stage":"mc","correct":true,"originalCorrect":true,"typo":1}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := post(t, srv, "/api/attempts", tt.body, nil); code != tt.want {
				t.Errorf("status = %d, want %d", code, tt.want)
			}
		})
	}
}

func TestCreateSessionValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	if code := post(t, srv, "/api/sessions", `{"chapterId":1}`, nil); code != http.StatusBadRequest {
		t.Errorf("missing mode status = %d, want 400", code)
	}
	if code := post(t, srv, "/api/sessions", `{"chapterId":9999,"mode":"flashcards"}`, nil); code != http.StatusNotFound {
		t.Errorf("unknown chapter status = %d, want 404", code)
	}
	// Comprehensive quizzes belong to no chapter.
	if code := post(t, srv, "/api/sessions", `{"chapterId":null,"mode":"comprehensive"}`, nil); code != http.StatusCreated {
		t.Errorf("null chapter status = %d, want 201", code)
	}
}

func TestEndUnknownSession(t *testing.T) {
	srv, _ := newTestServer(t)
	if code := post(t, srv, "/api/sessions/9999/end", "", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}
