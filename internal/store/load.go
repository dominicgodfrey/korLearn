package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dominicgodfrey/korLearn/internal/seed"
)

// LoadLessons makes the database match the seed files exactly, in one
// transaction, without disturbing attempt history.
//
// The strategy is retire-then-revive: everything is marked retired up front,
// and each row the seed files still contain is revived as it is upserted.
// Whatever is left retired was deleted from the content and stops appearing in
// study, while its rows — and the attempts pointing at them — survive. Deleting
// outright would either break foreign keys or silently erase the history of
// every word whose spelling was ever corrected.
func (db *DB) LoadLessons(ctx context.Context, lessons []seed.Lesson) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Positions are cleared to distinct negative values first. They are unique
	// across the whole table, so inserting a lesson mid-curriculum — which
	// shifts every position after it — would otherwise collide partway through
	// the reload against positions not yet reassigned.
	if _, err := tx.ExecContext(ctx,
		`UPDATE chapters SET retired_at = COALESCE(retired_at, ?), position = -id`, now); err != nil {
		return fmt.Errorf("retire chapters: %w", err)
	}

	for _, l := range lessons {
		if err := loadLesson(ctx, tx, l, now); err != nil {
			return fmt.Errorf("%s lesson %d: %w", l.Book, l.LessonNo, err)
		}
	}
	return tx.Commit()
}

func loadLesson(ctx context.Context, tx *sql.Tx, l seed.Lesson, now string) error {
	var chapterID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO chapters (book, lesson_no, position, title, retired_at)
		VALUES (?, ?, ?, ?, NULL)
		ON CONFLICT (book, lesson_no) DO UPDATE SET
			position   = excluded.position,
			title      = excluded.title,
			retired_at = NULL
		RETURNING id`,
		l.Book, l.LessonNo, l.Position, l.Title,
	).Scan(&chapterID)
	if err != nil {
		return fmt.Errorf("upsert chapter: %w", err)
	}

	// deadline_at is deliberately absent from that statement: it is set by the
	// user in the UI, not by content, and a reload must not clear it.

	for _, table := range []string{"vocab", "grammar_points", "exercises", "passages"} {
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+table+` SET retired_at = COALESCE(retired_at, ?) WHERE chapter_id = ?`,
			now, chapterID); err != nil {
			return fmt.Errorf("retire %s: %w", table, err)
		}
	}

	for _, v := range l.Vocab {
		english, err := json.Marshal(v.English)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO vocab (chapter_id, korean, english_json, pos, speech_level, irregular, notes, retired_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
			ON CONFLICT (chapter_id, korean) DO UPDATE SET
				english_json = excluded.english_json,
				pos          = excluded.pos,
				speech_level = excluded.speech_level,
				irregular    = excluded.irregular,
				notes        = excluded.notes,
				retired_at   = NULL`,
			chapterID, v.Korean, string(english), v.POS, v.SpeechLevel, v.Irregular, v.Notes); err != nil {
			return fmt.Errorf("upsert vocab %q: %w", v.Korean, err)
		}
	}

	// Authored grammar point ids are slugs; exercises reference them by slug and
	// need the database id.
	pointIDs := make(map[string]int64, len(l.GrammarPoints))
	for _, g := range l.GrammarPoints {
		var id int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO grammar_points (chapter_id, slug, title, explanation, retired_at)
			VALUES (?, ?, ?, ?, NULL)
			ON CONFLICT (chapter_id, slug) DO UPDATE SET
				title       = excluded.title,
				explanation = excluded.explanation,
				retired_at  = NULL
			RETURNING id`,
			chapterID, g.ID, g.Title, g.Explanation,
		).Scan(&id); err != nil {
			return fmt.Errorf("upsert grammar point %q: %w", g.ID, err)
		}
		pointIDs[g.ID] = id
	}

	for _, e := range l.Exercises {
		answer, err := json.Marshal(e.Answer)
		if err != nil {
			return err
		}
		var pointID any
		if e.GrammarPoint != "" {
			// seed validation guarantees the reference resolves.
			pointID = pointIDs[e.GrammarPoint]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO exercises (chapter_id, grammar_point_id, kind, prompt, payload_json, answer_json, generated, retired_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, NULL)
			ON CONFLICT (chapter_id, kind, prompt) DO UPDATE SET
				grammar_point_id = excluded.grammar_point_id,
				payload_json     = excluded.payload_json,
				answer_json      = excluded.answer_json,
				retired_at       = NULL`,
			chapterID, pointID, e.Kind, e.Prompt, string(e.Payload), string(answer)); err != nil {
			return fmt.Errorf("upsert exercise %q: %w", e.Prompt, err)
		}
	}

	for _, p := range l.Passages {
		questions, err := json.Marshal(p.Questions)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO passages (chapter_id, korean_text, questions_json, retired_at)
			VALUES (?, ?, ?, NULL)
			ON CONFLICT (chapter_id, korean_text) DO UPDATE SET
				questions_json = excluded.questions_json,
				retired_at     = NULL`,
			chapterID, p.Korean, string(questions)); err != nil {
			return fmt.Errorf("upsert passage: %w", err)
		}
	}

	return nil
}
