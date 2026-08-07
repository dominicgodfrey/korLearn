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
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/dominicgodfrey/korLearn/internal/api"
	"github.com/dominicgodfrey/korLearn/internal/lint"
	"github.com/dominicgodfrey/korLearn/internal/seed"
	"github.com/dominicgodfrey/korLearn/internal/store"
	"github.com/dominicgodfrey/korLearn/internal/tts"
)

func main() {
	// `korlearn lint [dir]` reads content straight from disk and never touches
	// the database, so it is handled before any of the server flags.
	if len(os.Args) > 1 && os.Args[1] == "lint" {
		dir := "content"
		if len(os.Args) > 2 {
			dir = os.Args[2]
		}
		if err := runLint(dir); err != nil {
			log.Fatal(err)
		}
		return
	}

	var cfg config
	flag.StringVar(&cfg.addr, "addr", "localhost:8080", "listen address")
	flag.StringVar(&cfg.dbPath, "db", "korlearn.db", "path to the SQLite database")
	flag.StringVar(&cfg.contentPath, "content", "content", "directory of seed lesson files")
	flag.StringVar(&cfg.ttsCache, "tts-cache", "cache/tts", "directory for synthesized audio")
	flag.StringVar(&cfg.ttsURL, "tts-url", "http://localhost:8123", "base URL of the Python TTS sidecar")
	flag.StringVar(&cfg.ttsVoice, "tts-voice", "", "sidecar voice (empty uses the sidecar default)")
	flag.StringVar(&cfg.webDir, "web", "web/dist", "directory of built frontend assets")
	prewarm := flag.Bool("prewarm", false, "synthesize every vocab word into the audio cache, then exit")
	flag.Parse()

	cfg.prewarm = *prewarm
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
	webDir      string
	prewarm     bool
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

	if cfg.prewarm {
		return prewarmAudio(context.Background(), db, audio)
	}

	mux := api.New(db, audio).Routes()
	// Registered last and at the root, so every /api pattern above wins by
	// specificity and only unmatched paths reach the frontend.
	mux.Handle("/", api.StaticHandler(cfg.webDir))

	log.Printf("listening on http://%s", cfg.addr)
	return http.ListenAndServe(cfg.addr, mux)
}

// runLint reports vocabulary used before it is taught.
//
// It always exits 0, even with findings. The check cannot be precise about an
// agglutinative language — see the lint package — so failing on its output
// would block real content over guesses. It is a prompt to look, not a verdict.
func runLint(dir string) error {
	// os.DirFS defers opening, so a missing directory would otherwise surface
	// as a bare syscall error naming "." rather than the path asked for.
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("cannot read content directory %s: %w", dir, err)
	}

	lessons, err := seed.LoadDir(os.DirFS(dir))
	if err != nil {
		// Unparseable content is a genuine error, unlike a lint finding.
		return err
	}

	// Two checks with deliberately different weight. Vocabulary sequencing is a
	// heuristic over an agglutinative language and can only advise; coverage is
	// a set comparison and can be trusted to fail the run.
	findings := lint.Run(lessons)
	for _, f := range findings {
		fmt.Println(f)
	}
	gaps := lint.Coverage(lessons)
	if len(gaps) > 0 {
		fmt.Println()
		for _, g := range gaps {
			fmt.Println(g)
		}
	}

	fmt.Printf("\n%d lessons checked: %d to review, %d coverage gap(s)\n",
		len(lessons), len(findings), len(gaps))
	if len(gaps) > 0 {
		return fmt.Errorf("%d coverage gap(s): every word and grammar point must be exercised by its chapter's comprehension passages", len(gaps))
	}
	return nil
}

// prewarmAudio synthesizes every vocab word up front so that no card in a study
// session ever waits on the model. Cached words are skipped by the cache
// itself, which makes reruns cheap after adding a lesson.
//
// Failures are counted and reported rather than fatal: one word the sidecar
// chokes on should not abandon the other several hundred.
func prewarmAudio(ctx context.Context, db *store.DB, audio *tts.Cache) error {
	chapters, err := db.Chapters(ctx)
	if err != nil {
		return err
	}

	var done, failed int
	for _, c := range chapters {
		vocab, err := db.ChapterVocab(ctx, c.ID)
		if err != nil {
			return err
		}
		for _, v := range vocab {
			if _, err := audio.Path(ctx, v.Korean); err != nil {
				log.Printf("  %s: %v", v.Korean, err)
				failed++
				continue
			}
			done++
		}
		log.Printf("chapter %d (%s): %d words", c.Position, c.Title, len(vocab))
	}

	log.Printf("prewarm complete: %d synthesized or cached, %d failed", done, failed)
	if failed > 0 {
		return fmt.Errorf("%d words could not be synthesized", failed)
	}
	return nil
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
