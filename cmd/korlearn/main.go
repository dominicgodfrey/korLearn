// Command korlearn serves the study app on localhost.
//
// Content is reloaded from disk into SQLite on every start, so editing a lesson
// file and restarting is the whole authoring loop. Deleting the database file
// costs nothing but attempt history.
package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/dominicgodfrey/korLearn/internal/api"
	"github.com/dominicgodfrey/korLearn/internal/seed"
	"github.com/dominicgodfrey/korLearn/internal/store"
	"github.com/dominicgodfrey/korLearn/internal/tts"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.addr, "addr", "localhost:8080", "listen address")
	flag.StringVar(&cfg.dbPath, "db", "korlearn.db", "path to the SQLite database")
	flag.StringVar(&cfg.contentPath, "content", "content", "directory of seed lesson files")
	flag.StringVar(&cfg.ttsCache, "tts-cache", "cache/tts", "directory for synthesized audio")
	flag.StringVar(&cfg.ttsURL, "tts-url", "http://localhost:8123", "base URL of the Python TTS sidecar")
	flag.StringVar(&cfg.ttsVoice, "tts-voice", "", "sidecar voice (empty uses the sidecar default)")
	flag.Parse()

	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

type config struct {
	addr        string
	dbPath      string
	contentPath string
	ttsCache    string
	ttsURL      string
	ttsVoice    string
}

func run(cfg config) error {
	db, err := store.Open(cfg.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	lessons, err := loadContent(context.Background(), db, cfg.contentPath)
	if err != nil {
		return err
	}
	log.Printf("loaded %d lessons from %s into %s", lessons, cfg.contentPath, cfg.dbPath)

	// The sidecar is a separate process and is not contacted until the first
	// audio request, so it can be started, stopped, or missing independently.
	audio := tts.New(cfg.ttsCache, cfg.ttsURL, cfg.ttsVoice)

	srv := api.New(db, audio)
	log.Printf("listening on http://%s", cfg.addr)
	return http.ListenAndServe(cfg.addr, srv.Routes())
}

// loadContent parses and loads every lesson file, returning how many it found.
//
// A missing content directory is not an error: it is the state of a fresh
// checkout, and refusing to start would make the API impossible to exercise
// before any lesson has been written.
func loadContent(ctx context.Context, db *store.DB, path string) (int, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		log.Printf("no content directory at %s; starting with an empty curriculum", path)
		return 0, nil
	}

	lessons, err := seed.LoadDir(os.DirFS(path))
	if err != nil {
		// Bad content is fatal. Starting anyway would serve a curriculum that
		// silently disagrees with the files on disk.
		return 0, err
	}
	if err := db.LoadLessons(ctx, lessons); err != nil {
		return 0, err
	}
	return len(lessons), nil
}
