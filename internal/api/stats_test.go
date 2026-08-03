package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/stats"
)

// completeSession runs one session over both fixture vocab items, getting the
// first right and the second wrong, so the chapter scores 0.5.
func completeSession(t *testing.T, srv *httptest.Server) {
	t.Helper()

	var created struct {
		ID int64 `json:"id"`
	}
	if code := post(t, srv, "/api/sessions", `{"chapterId":1,"mode":"flashcards"}`, &created); code != http.StatusCreated {
		t.Fatalf("create session status = %d", code)
	}
	bodies := []string{
		`{"sessionId":%d,"itemType":"vocab","itemId":1,"stage":"mc","correct":true,"originalCorrect":true,"userAnswer":"","rubric":null,"elapsedMs":0}`,
		`{"sessionId":%d,"itemType":"vocab","itemId":2,"stage":"mc","correct":false,"originalCorrect":false,"userAnswer":"","rubric":null,"elapsedMs":0}`,
	}
	for _, b := range bodies {
		if code := post(t, srv, "/api/attempts", fmt.Sprintf(b, created.ID), nil); code != http.StatusCreated {
			t.Fatalf("attempt status = %d", code)
		}
	}
	end := "/api/sessions/" + strconv.FormatInt(created.ID, 10) + "/end"
	if code := post(t, srv, end, "", nil); code != http.StatusOK {
		t.Fatalf("end status = %d", code)
	}
}

func TestChapterGradesEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	completeSession(t, srv)

	var grades []stats.ChapterGrade
	if code := get(t, srv, "/api/stats/chapters", &grades); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(grades) != 1 {
		t.Fatalf("got %d grades, want 1", len(grades))
	}
	if grades[0].AverageScore == nil || *grades[0].AverageScore != 0.5 {
		t.Errorf("averageScore = %v, want 0.5", grades[0].AverageScore)
	}
	if grades[0].BestScore == nil || *grades[0].BestScore != 0.5 {
		t.Errorf("bestScore = %v, want 0.5", grades[0].BestScore)
	}
}

func TestChapterTrendEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	completeSession(t, srv)

	var points []stats.TrendPoint
	if code := get(t, srv, "/api/chapters/1/trend", &points); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(points) != 1 || points[0].Score != 0.5 {
		t.Errorf("points = %+v, want one point scoring 0.5", points)
	}

	if code := get(t, srv, "/api/chapters/9999/trend", nil); code != http.StatusNotFound {
		t.Errorf("unknown chapter status = %d, want 404", code)
	}
}

func TestWeakItemsEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	completeSession(t, srv)

	var items []stats.WeakItem
	if code := get(t, srv, "/api/stats/weak", &items); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Label != "둘" || items[0].Accuracy != 0 {
		t.Errorf("worst item = %+v, want 둘 at accuracy 0", items[0])
	}

	// A junk limit must not fail the dashboard.
	if code := get(t, srv, "/api/stats/weak?limit=abc", nil); code != http.StatusOK {
		t.Errorf("malformed limit status = %d, want 200", code)
	}
}

func TestCalendarEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	completeSession(t, srv)

	var days []stats.CalendarDay
	if code := get(t, srv, "/api/stats/calendar", &days); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(days) != 1 || days[0].Attempts != 2 {
		t.Fatalf("days = %+v, want one day with 2 attempts", days)
	}
	if days[0].Accuracy != 0.5 {
		t.Errorf("accuracy = %v, want 0.5", days[0].Accuracy)
	}
}

// Empty results must serialize as [] so the client can map over them.
func TestStatsEmptyCollectionsAreArrays(t *testing.T) {
	srv, _ := newTestServer(t)

	for _, path := range []string{"/api/stats/weak", "/api/stats/grammar", "/api/stats/calendar"} {
		t.Run(path, func(t *testing.T) {
			var raw json.RawMessage
			if code := get(t, srv, path, &raw); code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			if string(raw) != "[]" {
				t.Errorf("body = %s, want []", raw)
			}
		})
	}
}
