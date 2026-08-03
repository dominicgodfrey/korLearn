package api

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// StaticHandler serves the built frontend from dir, falling back to index.html
// so client-side routes survive a page reload.
//
// PLAN.md calls for embedding the assets in the binary. That has to wait:
// //go:embed fails to compile when the directory does not exist, so embedding
// now would mean nobody can build the project until a frontend is built first.
// Serving from disk keeps `go build` working on a fresh checkout, and switching
// to embed later touches only this file.
//
// A missing directory is not an error — it is the state of the project before
// the frontend exists — but it is logged once, since a blank page with no
// explanation is a bad way to discover you forgot to run the build.
func StaticHandler(dir string) http.Handler {
	if dir == "" {
		return notBuilt()
	}
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		log.Printf("no frontend at %s; serving the API only (run the frontend build to populate it)", dir)
		return notBuilt()
	}

	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A real file wins. Anything else is a client-side route and must
		// receive index.html, or reloading on /dashboard would 404.
		if name := path.Clean(r.URL.Path); name != "/" {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
				// Hashed asset filenames change on every build, so they are
				// safe to cache hard; index.html must never be.
				if strings.HasPrefix(name, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}

func notBuilt() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound,
			"the frontend has not been built yet; the API is available under /api", nil)
	})
}
