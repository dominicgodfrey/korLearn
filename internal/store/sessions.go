package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrSessionClosed = errors.New("session already ended")
)

// Item types an attempt can reference. attempts.item_id is polymorphic, so
// these are checked in Go rather than by a foreign key.
const (
	ItemVocab           = "vocab"
	ItemExercise        = "exercise"
	ItemPassageQuestion = "passage_question"
)

// Rungs of the mastery ladder, recorded so first-try accuracy can be read back
// per stage later without reprocessing anything.
const (
	StageFlip  = "flip"
	StageMC    = "mc"
	StageTyped = "typed"
)

var itemTables = map[string]string{
	ItemVocab:    "vocab",
	ItemExercise: "exercises",
	// Passage questions live inside passages.questions_json and have no table
	// of their own; item_id is the passage id.
	ItemPassageQuestion: "passages",
}

func ValidItemType(t string) bool {
	_, ok := itemTables[t]
	return ok
}

func ValidStage(s string) bool {
	return s == StageFlip || s == StageMC || s == StageTyped
}

// CreateSession opens a session. chapterID is nil for comprehensive quizzes
// that span the whole curriculum.
func (db *DB) CreateSession(ctx context.Context, chapterID *int64, mode string) (int64, error) {
	if chapterID != nil {
		ok, err := db.ChapterExists(ctx, *chapterID)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("chapter %d: %w", *chapterID, ErrNotFound)
		}
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO sessions (chapter_id, mode) VALUES (?, ?)`, chapterID, mode)
	if err != nil {
		return 0, fmt.Errorf("create session: %w", err)
	}
	return res.LastInsertId()
}

type Attempt struct {
	SessionID int64  `json:"sessionId"`
	ItemType  string `json:"itemType"`
	ItemID    int64  `json:"itemId"`
	Stage     string `json:"stage"`
	Correct   bool   `json:"correct"`
	// OriginalCorrect is the grader's verdict before any manual override.
	// Scores use Correct; keeping both is what makes overrides auditable.
	OriginalCorrect bool    `json:"originalCorrect"`
	UserAnswer      string  `json:"userAnswer"`
	Rubric          *string `json:"rubric"`
	ElapsedMS       int64   `json:"elapsedMs"`
}

// RecordAttempt appends one attempt. Nothing here ever updates a row: a
// correction is a new attempt, and the table is the source of every derived
// number in the app.
func (db *DB) RecordAttempt(ctx context.Context, a Attempt) (int64, error) {
	if !ValidItemType(a.ItemType) {
		return 0, fmt.Errorf("item type %q: %w", a.ItemType, ErrNotFound)
	}
	if !ValidStage(a.Stage) {
		return 0, fmt.Errorf("stage %q: %w", a.Stage, ErrNotFound)
	}

	// Appending to a finished session would invalidate the score already
	// stored on it, so it is refused rather than silently accepted.
	var ended *string
	switch err := db.QueryRowContext(ctx,
		`SELECT ended_at FROM sessions WHERE id = ?`, a.SessionID).Scan(&ended); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("session %d: %w", a.SessionID, ErrNotFound)
	case err != nil:
		return 0, err
	case ended != nil:
		return 0, fmt.Errorf("session %d: %w", a.SessionID, ErrSessionClosed)
	}

	// The referenced item must exist. attempts is append-only, so a bad
	// reference is permanent noise in the one table every metric reads.
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+itemTables[a.ItemType]+` WHERE id = ?`, a.ItemID).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("%s %d: %w", a.ItemType, a.ItemID, ErrNotFound)
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO attempts (session_id, item_type, item_id, stage, correct,
		                      original_correct, user_answer, rubric_json, elapsed_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.SessionID, a.ItemType, a.ItemID, a.Stage, a.Correct,
		a.OriginalCorrect, a.UserAnswer, a.Rubric, a.ElapsedMS)
	if err != nil {
		return 0, fmt.Errorf("record attempt: %w", err)
	}
	return res.LastInsertId()
}

// sessionScoreSQL is the single definition of what a score means, so no other
// query can drift from it.
//
// A session scores the share of distinct items answered correctly on the FIRST
// attempt. That measures what was known walking in, and stays comparable
// between sessions regardless of how much re-drilling happened afterward — in
// mastery mode you repeat an item until it is right, so counting every attempt
// would punish thoroughness and counting final state would always be ~100%.
//
// It reads correct rather than original_correct: a manual override is the
// user's final say, and original_correct remains for auditing how often it is
// used. AVG returns NULL for a session with no attempts, which is stored as
// NULL rather than zero — nothing attempted is not the same as nothing right.
const sessionScoreSQL = `
	WITH firsts AS (
		SELECT MIN(id) AS id
		FROM attempts
		WHERE session_id = ?
		GROUP BY item_type, item_id
	)
	SELECT AVG(a.correct) FROM attempts a JOIN firsts f ON a.id = f.id`

type SessionResult struct {
	ID      int64    `json:"id"`
	Score   *float64 `json:"score"`
	EndedAt string   `json:"endedAt"`
}

// EndSession closes a session and stores its score. Ending twice is an error:
// the second call would recompute a number the first one already published.
func (db *DB) EndSession(ctx context.Context, sessionID int64) (SessionResult, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return SessionResult{}, err
	}
	defer tx.Rollback()

	var ended *string
	switch err := tx.QueryRowContext(ctx,
		`SELECT ended_at FROM sessions WHERE id = ?`, sessionID).Scan(&ended); {
	case errors.Is(err, sql.ErrNoRows):
		return SessionResult{}, fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
	case err != nil:
		return SessionResult{}, err
	case ended != nil:
		return SessionResult{}, fmt.Errorf("session %d: %w", sessionID, ErrSessionClosed)
	}

	var score *float64
	if err := tx.QueryRowContext(ctx, sessionScoreSQL, sessionID).Scan(&score); err != nil {
		return SessionResult{}, fmt.Errorf("score session %d: %w", sessionID, err)
	}

	var endedAt string
	if err := tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET ended_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), score = ?, snapshot_json = NULL
		WHERE id = ?
		RETURNING ended_at`, score, sessionID).Scan(&endedAt); err != nil {
		return SessionResult{}, fmt.Errorf("end session %d: %w", sessionID, err)
	}

	if err := tx.Commit(); err != nil {
		return SessionResult{}, err
	}
	return SessionResult{ID: sessionID, Score: score, EndedAt: endedAt}, nil
}
