package lexicon_test

import (
	"context"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/lexicon"
	"github.com/dominicgodfrey/korLearn/internal/seed"
	"github.com/dominicgodfrey/korLearn/internal/store"
)

// three chapters, one word and one grammar point each, positions 1..3.
func fixture(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	words := []string{"하나", "둘", "셋"}
	var lessons []seed.Lesson
	for i, w := range words {
		pos := i + 1
		lessons = append(lessons, seed.Lesson{
			SchemaVersion: seed.SchemaVersion,
			Book:          "Fixture Book",
			LessonNo:      pos,
			Position:      pos,
			Title:         "Fixture",
			Intro:         "Fixture intro.",
			Vocab:         []seed.Vocab{{Korean: w, English: []string{w}, POS: "noun"}},
			GrammarPoints: []seed.GrammarPoint{{
				ID: "fx" + w, Title: "Point " + w,
				Example: seed.Example{Korean: w, English: "e"},
			}},
		})
	}
	if err := db.LoadLessons(context.Background(), lessons); err != nil {
		t.Fatalf("LoadLessons: %v", err)
	}
	return db
}

// The rule the whole app rests on: nothing above the current position is ever
// in the set. This is the test that fails if a future migration or query
// forgets the filter.
func TestLoadExcludesLaterChapters(t *testing.T) {
	db := fixture(t)

	set, err := lexicon.Load(context.Background(), db, 2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var got []string
	for _, w := range set.Words {
		got = append(got, w.Korean)
		if w.ChapterPosition > 2 {
			t.Errorf("%s came from position %d, above the requested 2", w.Korean, w.ChapterPosition)
		}
	}
	if len(got) != 2 || got[0] != "하나" || got[1] != "둘" {
		t.Errorf("words = %v, want [하나 둘] in position order", got)
	}
	if len(set.Grammar) != 2 {
		t.Errorf("grammar = %+v, want 2 points", set.Grammar)
	}
	for _, p := range set.Grammar {
		if p.ChapterPosition > 2 {
			t.Errorf("grammar %q came from position %d", p.Slug, p.ChapterPosition)
		}
	}
}

// Content deleted from a seed file is retired rather than removed, so that its
// attempt history survives. Retired rows must not come back through here.
func TestLoadSkipsRetiredContent(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()

	if _, err := db.Exec(
		`UPDATE vocab SET retired_at = '2026-01-01T00:00:00.000Z' WHERE korean = '하나'`); err != nil {
		t.Fatal(err)
	}

	set, err := lexicon.Load(ctx, db, 3)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range set.Words {
		if w.Korean == "하나" {
			t.Error("retired vocabulary is still unlocked")
		}
	}
}

func TestLoadForChapter(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()

	var id int64
	if err := db.QueryRow(`SELECT id FROM chapters WHERE position = 2`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	set, ok, err := lexicon.LoadForChapter(ctx, db, id)
	if err != nil || !ok {
		t.Fatalf("LoadForChapter: ok=%v err=%v", ok, err)
	}
	if set.Position != 2 || len(set.Words) != 2 {
		t.Errorf("set = position %d with %d words, want position 2 with 2", set.Position, len(set.Words))
	}

	// A missing chapter must be distinguishable from an empty one: position 0
	// would silently read as "nothing unlocked yet".
	if _, ok, err := lexicon.LoadForChapter(ctx, db, 9999); err != nil || ok {
		t.Errorf("unknown chapter: ok=%v err=%v, want false/nil", ok, err)
	}
}

// The matcher built from a set must accept the set's own words and reject what
// has not been taught — this is the join between the two halves of the package.
func TestSetMatcher(t *testing.T) {
	db := fixture(t)

	set, err := lexicon.Load(context.Background(), db, 1)
	if err != nil {
		t.Fatal(err)
	}
	m := set.Matcher()
	if !m.Knows("하나") {
		t.Error("matcher rejects a word from its own set")
	}
	if m.Knows("셋") {
		t.Error("matcher accepts a word from a later chapter")
	}
}
