package tts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeSidecar counts synthesis calls so tests can prove the cache is used.
func fakeSidecar(t *testing.T, audio []byte, status int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != http.StatusOK {
			http.Error(w, "synthesis failed", status)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(audio)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestSynthesizesOnceThenServesFromDisk(t *testing.T) {
	srv, calls := fakeSidecar(t, []byte("RIFF....WAVEfake"), http.StatusOK)
	c := New(t.TempDir(), srv.URL, "kf_a")
	ctx := context.Background()

	path, err := c.Path(ctx, "안녕하세요")
	if err != nil {
		t.Fatalf("first Path: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
	if string(got) != "RIFF....WAVEfake" {
		t.Errorf("cached bytes = %q", got)
	}

	// Second request for the same text must not reach the sidecar.
	again, err := c.Path(ctx, "안녕하세요")
	if err != nil {
		t.Fatalf("second Path: %v", err)
	}
	if again != path {
		t.Errorf("path changed between calls: %s vs %s", path, again)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("sidecar called %d times, want 1", n)
	}
}

// Changing the voice must not serve audio in the old one.
func TestVoiceIsPartOfTheCacheKey(t *testing.T) {
	srv, calls := fakeSidecar(t, []byte("RIFFfake"), http.StatusOK)
	dir := t.TempDir()
	ctx := context.Background()

	a, err := New(dir, srv.URL, "voice-one").Path(ctx, "하나")
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(dir, srv.URL, "voice-two").Path(ctx, "하나")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("both voices resolved to the same file")
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("sidecar called %d times, want 2", n)
	}
}

func TestDistinctTextsGetDistinctFiles(t *testing.T) {
	srv, _ := fakeSidecar(t, []byte("RIFFfake"), http.StatusOK)
	c := New(t.TempDir(), srv.URL, "kf_a")
	ctx := context.Background()

	a, err := c.Path(ctx, "하나")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Path(ctx, "둘")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("different texts collided on one file")
	}
}

func TestRejectsEmptyAndOverlongText(t *testing.T) {
	srv, calls := fakeSidecar(t, []byte("RIFFfake"), http.StatusOK)
	c := New(t.TempDir(), srv.URL, "kf_a")
	ctx := context.Background()

	if _, err := c.Path(ctx, ""); !errors.Is(err, ErrEmptyText) {
		t.Errorf("empty text returned %v, want ErrEmptyText", err)
	}
	if _, err := c.Path(ctx, strings.Repeat("가", MaxTextLen)); err == nil {
		t.Error("overlong text was accepted")
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("sidecar called %d times for invalid input, want 0", n)
	}
}

// A failed synthesis must leave nothing behind: a cached empty or partial wav
// would never be retried and would play as silence forever.
func TestFailureCachesNothing(t *testing.T) {
	dir := t.TempDir()

	srv, _ := fakeSidecar(t, nil, http.StatusInternalServerError)
	if _, err := New(dir, srv.URL, "kf_a").Path(context.Background(), "하나"); err == nil {
		t.Fatal("Path succeeded despite a sidecar error")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("cache dir holds %d files after a failure, want 0", len(entries))
	}
}

func TestEmptyAudioIsRejected(t *testing.T) {
	srv, _ := fakeSidecar(t, []byte{}, http.StatusOK)
	if _, err := New(t.TempDir(), srv.URL, "kf_a").Path(context.Background(), "하나"); err == nil {
		t.Error("empty audio was accepted and cached")
	}
}

func TestUnreachableSidecar(t *testing.T) {
	// Port 1 is reserved and nothing listens there.
	c := New(t.TempDir(), "http://localhost:1", "kf_a")
	_, err := c.Path(context.Background(), "하나")
	if err == nil {
		t.Fatal("Path succeeded with no sidecar running")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %v, want it to name the unreachable sidecar", err)
	}
}
