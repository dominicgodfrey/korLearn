package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func buildDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fetch(t *testing.T, srv *httptest.Server, path string) (int, string, http.Header) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func staticServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("/", StaticHandler(dir))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestStaticServesAssets(t *testing.T) {
	srv := staticServer(t, buildDir(t))

	code, body, hdr := fetch(t, srv, "/assets/app-abc123.js")
	if code != http.StatusOK || body != "console.log(1)" {
		t.Fatalf("status %d body %q", code, body)
	}
	// Hashed filenames change every build, so they are safe to cache hard.
	if cc := hdr.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q", cc)
	}
}

// Reloading on a client-side route must render the app, not 404.
func TestStaticFallsBackToIndex(t *testing.T) {
	srv := staticServer(t, buildDir(t))

	for _, path := range []string{"/", "/dashboard", "/chapters/3/study"} {
		code, body, hdr := fetch(t, srv, path)
		if code != http.StatusOK || body != "<html>app</html>" {
			t.Errorf("%s: status %d body %q, want the app shell", path, code, body)
		}
		// index.html must never be cached, or a deploy is invisible until a
		// hard refresh.
		if cc := hdr.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, cc)
		}
	}
}

// The frontend must never swallow an API route.
func TestStaticDoesNotShadowAPI(t *testing.T) {
	srv := staticServer(t, buildDir(t))

	code, body, _ := fetch(t, srv, "/api/health")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body == "<html>app</html>" {
		t.Error("the static handler served the app shell for an API route")
	}
}

// A fresh checkout has no build. That must not stop the API from working.
func TestStaticMissingBuildStillServesAPI(t *testing.T) {
	srv := staticServer(t, filepath.Join(t.TempDir(), "never-built"))

	if code, _, _ := fetch(t, srv, "/api/health"); code != http.StatusOK {
		t.Errorf("api status = %d, want 200", code)
	}
	code, body, _ := fetch(t, srv, "/")
	if code != http.StatusNotFound {
		t.Errorf("root status = %d, want 404", code)
	}
	if body == "" {
		t.Error("root returned no explanation of why there is nothing to serve")
	}
}
