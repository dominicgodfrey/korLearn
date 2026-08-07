package store

import (
	"context"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// lesson builds a minimal valid lesson with the given vocab words.
func lesson(position int, words ...string) seed.Lesson {
	l := seed.Lesson{
		SchemaVersion: seed.SchemaVersion,
		Book:          "Fixture Book",
		LessonNo:      position,
		Position:      position,
		Title:         "Fixture",
		Intro:         "Fixture intro.",
	}
	for _, w := range words {
		l.Vocab = append(l.Vocab, seed.Vocab{Korean: w, English: []string{w}, POS: "noun"})
	}
	return l
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := open(t)

	count := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// The count itself is not asserted: a fixed number here would have to be
	// bumped by every migration, which makes it a chore rather than a check.
	// What matters is that running migrate again applies nothing.
	before := count()
	if before == 0 {
		t.Fatal("no migrations applied on open")
	}
	if err := db.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if after := count(); after != before {
		t.Errorf("second migrate applied %d more migrations", after-before)
	}
}

func TestLoadLessons(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	l := lesson(1, "하나", "둘")
	l.GrammarPoints = []seed.GrammarPoint{{
		ID: "fx", Title: "Fixture point",
		Example: seed.Example{Korean: "하나예요.", English: "It is one."},
	}}
	l.Exercises = []seed.Exercise{{Kind: seed.KindFillBlank, GrammarPoint: "fx", Prompt: "___", Answer: []string{"a"}}}
	l.Passages = []seed.Passage{{Korean: "text", Questions: []seed.PassageQuestion{{Q: "q", Reference: "r"}}}}

	if err := db.LoadLessons(ctx, []seed.Lesson{l}); err != nil {
		t.Fatalf("LoadLessons: %v", err)
	}

	var vocabCount int
	if err := db.QueryRow(`SELECT count(*) FROM vocab WHERE retired_at IS NULL`).Scan(&vocabCount); err != nil {
		t.Fatal(err)
	}
	if vocabCount != 2 {
		t.Errorf("vocab count = %d, want 2", vocabCount)
	}

	// The exercise must resolve its authored slug to a real grammar_points row.
	var pointID, wantID int64
	if err := db.QueryRow(`SELECT grammar_point_id FROM exercises`).Scan(&pointID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM grammar_points WHERE slug = 'fx'`).Scan(&wantID); err != nil {
		t.Fatal(err)
	}
	if pointID != wantID {
		t.Errorf("exercise grammar_point_id = %d, want %d", pointID, wantID)
	}

	// Schema v2 content: the chapter page reads intro, the grammar module reads
	// the example. Both are written by the same load and easy to forget.
	var intro, exKO, exEN string
	if err := db.QueryRow(`SELECT intro FROM chapters`).Scan(&intro); err != nil {
		t.Fatal(err)
	}
	if intro != "Fixture intro." {
		t.Errorf("intro = %q, want the seeded text", intro)
	}
	if err := db.QueryRow(
		`SELECT example_korean, example_english FROM grammar_points WHERE slug = 'fx'`,
	).Scan(&exKO, &exEN); err != nil {
		t.Fatal(err)
	}
	if exKO != "하나예요." || exEN != "It is one." {
		t.Errorf("example = %q / %q, want the seeded pair", exKO, exEN)
	}
}

// The property the whole append-only history depends on: reloading content must
// update rows in place, never reinsert them under new ids.
func TestReloadPreservesRowIDs(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.LoadLessons(ctx, []seed.Lesson{lesson(1, "하나")}); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRow(`SELECT id FROM vocab WHERE korean = '하나'`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	// Same word, edited gloss, plus a new sibling.
	edited := lesson(1, "하나", "둘")
	edited.Vocab[0].English = []string{"one (native Korean)"}
	if err := db.LoadLessons(ctx, []seed.Lesson{edited}); err != nil {
		t.Fatal(err)
	}

	var after int64
	var english string
	if err := db.QueryRow(`SELECT id, english_json FROM vocab WHERE korean = '하나'`).Scan(&after, &english); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("vocab id changed on reload: %d -> %d", before, after)
	}
	if english != `["one (native Korean)"]` {
		t.Errorf("english_json = %s, want the edited gloss", english)
	}
}

// Content deleted from a seed file must stop appearing in study without taking
// its attempt history with it.
func TestRemovedContentIsRetiredNotDeleted(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.LoadLessons(ctx, []seed.Lesson{lesson(1, "하나", "둘")}); err != nil {
		t.Fatal(err)
	}
	var vocabID int64
	if err := db.QueryRow(`SELECT id FROM vocab WHERE korean = '둘'`).Scan(&vocabID); err != nil {
		t.Fatal(err)
	}

	res, err := db.Exec(`INSERT INTO sessions (mode) VALUES ('vocab_flip')`)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := res.LastInsertId()
	if _, err := db.Exec(`
		INSERT INTO attempts (session_id, item_type, item_id, stage, correct, original_correct)
		VALUES (?, 'vocab', ?, 'typed', 1, 1)`, sessionID, vocabID); err != nil {
		t.Fatal(err)
	}

	// 둘 is dropped from the lesson.
	if err := db.LoadLessons(ctx, []seed.Lesson{lesson(1, "하나")}); err != nil {
		t.Fatal(err)
	}

	var retired *string
	if err := db.QueryRow(`SELECT retired_at FROM vocab WHERE id = ?`, vocabID).Scan(&retired); err != nil {
		t.Fatalf("row was deleted, want retired: %v", err)
	}
	if retired == nil {
		t.Error("retired_at is NULL, want a timestamp")
	}

	var attempts int
	if err := db.QueryRow(`SELECT count(*) FROM attempts WHERE item_id = ?`, vocabID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Errorf("attempts for retired vocab = %d, want 1", attempts)
	}

	// And restoring the word revives the original row rather than making a new one.
	if err := db.LoadLessons(ctx, []seed.Lesson{lesson(1, "하나", "둘")}); err != nil {
		t.Fatal(err)
	}
	var revivedID int64
	if err := db.QueryRow(`SELECT id FROM vocab WHERE korean = '둘' AND retired_at IS NULL`).Scan(&revivedID); err != nil {
		t.Fatal(err)
	}
	if revivedID != vocabID {
		t.Errorf("revived id = %d, want %d", revivedID, vocabID)
	}
}

// Inserting a lesson mid-curriculum shifts every position after it. Positions
// are globally unique, so the reload must not collide partway through.
func TestReorderingPositions(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	a, b := lesson(1, "가"), lesson(2, "나")
	if err := db.LoadLessons(ctx, []seed.Lesson{a, b}); err != nil {
		t.Fatal(err)
	}

	// A new lesson takes position 1; the other two shift up.
	a.Position, b.Position = 2, 3
	c := lesson(1, "다")
	c.LessonNo = 3
	if err := db.LoadLessons(ctx, []seed.Lesson{c, a, b}); err != nil {
		t.Fatalf("reload after reordering: %v", err)
	}

	var pos int
	if err := db.QueryRow(`SELECT position FROM chapters WHERE lesson_no = 1`).Scan(&pos); err != nil {
		t.Fatal(err)
	}
	if pos != 2 {
		t.Errorf("lesson 1 position = %d, want 2", pos)
	}
}

// deadline_at is user data set in the UI, not content. A reload must not wipe it.
func TestReloadPreservesDeadline(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	if err := db.LoadLessons(ctx, []seed.Lesson{lesson(1, "가")}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE chapters SET deadline_at = '2026-09-01T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}
	if err := db.LoadLessons(ctx, []seed.Lesson{lesson(1, "가", "나")}); err != nil {
		t.Fatal(err)
	}

	var deadline *string
	if err := db.QueryRow(`SELECT deadline_at FROM chapters`).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if deadline == nil || *deadline != "2026-09-01T00:00:00.000Z" {
		t.Errorf("deadline_at = %v, want it preserved", deadline)
	}
}
