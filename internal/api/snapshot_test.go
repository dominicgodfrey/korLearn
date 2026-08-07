package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func put(t *testing.T, srv *httptest.Server, path, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func newSession(t *testing.T, srv *httptest.Server) int64 {
	t.Helper()
	var created struct {
		ID int64 `json:"id"`
	}
	if code := post(t, srv, "/api/sessions", `{"chapterId":1,"mode":"vocab_flip"}`, &created); code != http.StatusCreated {
		t.Fatalf("create session status = %d", code)
	}
	return created.ID
}

// The point of the feature: save state, reload, get the same state back.
func TestSnapshotRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	id := strconv.FormatInt(newSession(t, srv), 10)
	path := "/api/sessions/" + id + "/snapshot"

	const state = `{"queue":[3,1,2],"index":1,"stage":"mc","history":[{"item":3,"correct":false}]}`
	if code := put(t, srv, path, state); code != http.StatusNoContent {
		t.Fatalf("put status = %d, want 204", code)
	}

	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Compared as JSON, since the server stores it verbatim but need not
	// promise byte equality.
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("response is not JSON: %s", body)
	}
	json.Unmarshal([]byte(state), &want)
	if !jsonEqual(got, want) {
		t.Errorf("got %s, want %s", body, state)
	}
}

// Overwriting on every advance is the expected usage, not an edge case.
func TestSnapshotOverwrites(t *testing.T) {
	srv, _ := newTestServer(t)
	path := "/api/sessions/" + strconv.FormatInt(newSession(t, srv), 10) + "/snapshot"

	put(t, srv, path, `{"index":1}`)
	put(t, srv, path, `{"index":2}`)

	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"index":2`)) {
		t.Errorf("body = %s, want the second snapshot", body)
	}
}

// A session with nothing saved yet is not an error.
func TestSnapshotAbsentIsNull(t *testing.T) {
	srv, _ := newTestServer(t)
	path := "/api/sessions/" + strconv.FormatInt(newSession(t, srv), 10) + "/snapshot"

	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "null" {
		t.Errorf("status %d body %s, want 200 null", resp.StatusCode, body)
	}
}

// Ending a session clears its snapshot; there is nothing left to resume.
func TestSnapshotClearedAndRefusedAfterEnd(t *testing.T) {
	srv, _ := newTestServer(t)
	id := newSession(t, srv)
	path := "/api/sessions/" + strconv.FormatInt(id, 10) + "/snapshot"

	put(t, srv, path, `{"index":1}`)
	if code := post(t, srv, "/api/sessions/"+strconv.FormatInt(id, 10)+"/end", "", nil); code != http.StatusOK {
		t.Fatalf("end status = %d", code)
	}

	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "null" {
		t.Errorf("snapshot survived the session ending: %s", body)
	}

	if code := put(t, srv, path, `{"index":2}`); code != http.StatusConflict {
		t.Errorf("put after end status = %d, want 409", code)
	}
}

func TestSnapshotValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	id := strconv.FormatInt(newSession(t, srv), 10)

	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{"malformed json", "/api/sessions/" + id + "/snapshot", `{"index":`, http.StatusBadRequest},
		{"unknown session", "/api/sessions/9999/snapshot", `{"index":1}`, http.StatusNotFound},
		{"bad id", "/api/sessions/abc/snapshot", `{"index":1}`, http.StatusBadRequest},
		{"oversized", "/api/sessions/" + id + "/snapshot",
			`{"junk":"` + strings.Repeat("x", MaxSnapshotBytes) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := put(t, srv, tt.path, tt.body); code != tt.want {
				t.Errorf("status = %d, want %d", code, tt.want)
			}
		})
	}
}

func TestOpenSession(t *testing.T) {
	srv, _ := newTestServer(t)

	var none struct {
		SessionID *int64 `json:"sessionId"`
	}
	if code := get(t, srv, "/api/sessions/open", &none); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if none.SessionID != nil {
		t.Errorf("sessionId = %v, want null before any session", *none.SessionID)
	}

	id := newSession(t, srv)
	var open struct {
		SessionID *int64 `json:"sessionId"`
	}
	get(t, srv, "/api/sessions/open", &open)
	if open.SessionID == nil || *open.SessionID != id {
		t.Errorf("sessionId = %v, want %d", open.SessionID, id)
	}

	// Once ended there is nothing to resume.
	post(t, srv, "/api/sessions/"+strconv.FormatInt(id, 10)+"/end", "", nil)
	var closed struct {
		SessionID *int64 `json:"sessionId"`
	}
	get(t, srv, "/api/sessions/open", &closed)
	if closed.SessionID != nil {
		t.Errorf("sessionId = %v, want null after ending", *closed.SessionID)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
