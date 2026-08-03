package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// Retired content stays in the database to keep attempt history intact, so
// every read here filters it out. A query that forgets to would resurrect
// vocabulary the author deleted.

type Chapter struct {
	ID         int64   `json:"id"`
	Book       string  `json:"book"`
	LessonNo   int     `json:"lessonNo"`
	Position   int     `json:"position"`
	Title      string  `json:"title"`
	DeadlineAt *string `json:"deadlineAt"`
	VocabCount int     `json:"vocabCount"`
}

func (db *DB) Chapters(ctx context.Context) ([]Chapter, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.book, c.lesson_no, c.position, c.title, c.deadline_at,
		       (SELECT count(*) FROM vocab v WHERE v.chapter_id = c.id AND v.retired_at IS NULL)
		FROM chapters c
		WHERE c.retired_at IS NULL
		ORDER BY c.position`)
	if err != nil {
		return nil, fmt.Errorf("query chapters: %w", err)
	}
	defer rows.Close()

	chapters := []Chapter{}
	for rows.Next() {
		var c Chapter
		if err := rows.Scan(&c.ID, &c.Book, &c.LessonNo, &c.Position, &c.Title, &c.DeadlineAt, &c.VocabCount); err != nil {
			return nil, err
		}
		chapters = append(chapters, c)
	}
	return chapters, rows.Err()
}

type VocabItem struct {
	ID          int64    `json:"id"`
	Korean      string   `json:"korean"`
	English     []string `json:"english"`
	POS         string   `json:"pos"`
	SpeechLevel string   `json:"speechLevel"`
	Irregular   *string  `json:"irregular"`
	Notes       string   `json:"notes"`
}

func (db *DB) ChapterVocab(ctx context.Context, chapterID int64) ([]VocabItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, korean, english_json, pos, speech_level, irregular, notes
		FROM vocab
		WHERE chapter_id = ? AND retired_at IS NULL
		ORDER BY id`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("query vocab: %w", err)
	}
	defer rows.Close()

	items := []VocabItem{}
	for rows.Next() {
		var (
			v       VocabItem
			english string
		)
		if err := rows.Scan(&v.ID, &v.Korean, &english, &v.POS, &v.SpeechLevel, &v.Irregular, &v.Notes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(english), &v.English); err != nil {
			return nil, fmt.Errorf("vocab %d: bad english_json: %w", v.ID, err)
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// ChapterExists distinguishes an empty chapter from one that is not there,
// so the API can answer 404 instead of an empty list.
func (db *DB) ChapterExists(ctx context.Context, chapterID int64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM chapters WHERE id = ? AND retired_at IS NULL`, chapterID).Scan(&n)
	return n > 0, err
}
