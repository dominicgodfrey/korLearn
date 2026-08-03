package store

import (
	"context"
	"errors"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
)

// fixture loads a one-chapter lesson and returns the chapter id and its vocab
// ids in order.
func fixture(t *testing.T, db *DB, words ...string) (int64, []int64) {
	t.Helper()
	if err := db.LoadLessons(context.Background(), []seed.Lesson{lesson(1, words...)}); err != nil {
		t.Fatal(err)
	}

	var chapterID int64
	if err := db.QueryRow(`SELECT id FROM chapters`).Scan(&chapterID); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`SELECT id FROM vocab ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return chapterID, ids
}

func attempt(sessionID, itemID int64, stage string, correct bool) Attempt {
	return Attempt{
		SessionID:       sessionID,
		ItemType:        ItemVocab,
		ItemID:          itemID,
		Stage:           stage,
		Correct:         correct,
		OriginalCorrect: correct,
	}
}

// The defining property: a session scores what was known on the first try, so
// re-drilling an item until it is right must not raise the score.
func TestScoreUsesFirstAttemptPerItem(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	chapterID, ids := fixture(t, db, "하나", "둘")

	sessionID, err := db.CreateSession(ctx, &chapterID, "flashcards")
	if err != nil {
		t.Fatal(err)
	}

	// First item: missed, then drilled until correct. Second: correct at once.
	for _, a := range []Attempt{
		attempt(sessionID, ids[0], StageMC, false),
		attempt(sessionID, ids[0], StageMC, true),
		attempt(sessionID, ids[0], StageTyped, true),
		attempt(sessionID, ids[1], StageMC, true),
	} {
		if _, err := db.RecordAttempt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	res, err := db.EndSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score == nil || *res.Score != 0.5 {
		t.Errorf("score = %v, want 0.5 (one of two items right first try)", res.Score)
	}
}

// A manual override is the user's final say, so the score follows correct while
// original_correct survives for auditing.
func TestScoreUsesOverriddenVerdict(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	chapterID, ids := fixture(t, db, "하나")

	sessionID, err := db.CreateSession(ctx, &chapterID, "flashcards")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordAttempt(ctx, Attempt{
		SessionID: sessionID, ItemType: ItemVocab, ItemID: ids[0], Stage: StageTyped,
		Correct: true, OriginalCorrect: false, UserAnswer: "close enough",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := db.EndSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score == nil || *res.Score != 1 {
		t.Errorf("score = %v, want 1", res.Score)
	}

	var original bool
	if err := db.QueryRow(`SELECT original_correct FROM attempts`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if original {
		t.Error("original_correct was overwritten; the override is no longer auditable")
	}
}

// Nothing attempted is not the same as nothing right.
func TestScoreIsNullWithoutAttempts(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	chapterID, _ := fixture(t, db, "하나")

	sessionID, err := db.CreateSession(ctx, &chapterID, "flashcards")
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.EndSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != nil {
		t.Errorf("score = %v, want null", *res.Score)
	}
}

func TestEndSessionTwiceIsRefused(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	chapterID, _ := fixture(t, db, "하나")

	sessionID, err := db.CreateSession(ctx, &chapterID, "flashcards")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EndSession(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EndSession(ctx, sessionID); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("second end returned %v, want ErrSessionClosed", err)
	}
}

// Appending to a finished session would invalidate the score already stored.
func TestAttemptAfterEndIsRefused(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	chapterID, ids := fixture(t, db, "하나")

	sessionID, err := db.CreateSession(ctx, &chapterID, "flashcards")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EndSession(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordAttempt(ctx, attempt(sessionID, ids[0], StageMC, true)); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("RecordAttempt returned %v, want ErrSessionClosed", err)
	}
}

// attempts is append-only, so a dangling reference is permanent noise.
func TestAttemptRejectsUnknownReferences(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	chapterID, ids := fixture(t, db, "하나")

	sessionID, err := db.CreateSession(ctx, &chapterID, "flashcards")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		a    Attempt
	}{
		{"unknown item", attempt(sessionID, 9999, StageMC, true)},
		{"unknown session", attempt(9999, ids[0], StageMC, true)},
		{"bad item type", Attempt{SessionID: sessionID, ItemType: "nonsense", ItemID: ids[0], Stage: StageMC}},
		{"bad stage", Attempt{SessionID: sessionID, ItemType: ItemVocab, ItemID: ids[0], Stage: "guess"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.RecordAttempt(ctx, tt.a); !errors.Is(err, ErrNotFound) {
				t.Errorf("RecordAttempt returned %v, want ErrNotFound", err)
			}
		})
	}
}

// Comprehensive quizzes span the curriculum and belong to no chapter.
func TestCreateSessionWithoutChapter(t *testing.T) {
	db := open(t)
	if _, err := db.CreateSession(context.Background(), nil, "comprehensive"); err != nil {
		t.Errorf("CreateSession(nil): %v", err)
	}
}

func TestCreateSessionUnknownChapter(t *testing.T) {
	db := open(t)
	missing := int64(9999)
	if _, err := db.CreateSession(context.Background(), &missing, "flashcards"); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateSession returned %v, want ErrNotFound", err)
	}
}
