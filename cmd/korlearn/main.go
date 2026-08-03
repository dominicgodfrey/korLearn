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
)

func main() {
	var (
		addr        = flag.String("addr", "localhost:8080", "listen address")
		dbPath      = flag.String("db", "korlearn.db", "path to the SQLite database")
		contentPath = flag.String("content", "content", "directory of seed lesson files")
	)
	flag.Parse()

	if err := run(*addr, *dbPath, *contentPath); err != nil {
		log.Fatal(err)
	}
}

func run(addr, dbPath, contentPath string) error {
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	lessons, err := loadContent(context.Background(), db, contentPath)
	if err != nil {
		return err
	}
	log.Printf("loaded %d lessons from %s into %s", lessons, contentPath, dbPath)

	srv := api.New(db)
	log.Printf("listening on http://%s", addr)
	return http.ListenAndServe(addr, srv.Routes())
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
