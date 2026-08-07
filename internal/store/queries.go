package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// ModuleProgress is one module's history within a chapter.
//
// There is no completion column anywhere: "done" is a finished session in that
// mode, derived on read. A flag would be a second source of truth that could
// disagree with the attempts it was supposed to summarize.
type ModuleProgress struct {
	Mode string `json:"mode"`
	// Sessions counts finished runs. Zero means never completed, which is what
	// the chapter page draws as an unchecked tile.
	Sessions    int      `json:"sessions"`
	LastEndedAt *string  `json:"lastEndedAt"`
	BestScore   *float64 `json:"bestScore"`
}

// ChapterProgress reports every module of a chapter, including the ones never
// run. Absent modes are returned as zeroes rather than omitted: the chapter
// page renders one tile per module either way, and making the caller reconcile
// a partial list is how a new module silently stops appearing.
func (db *DB) ChapterProgress(ctx context.Context, chapterID int64) ([]ModuleProgress, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT mode, count(*), max(ended_at), max(score)
		FROM sessions
		WHERE chapter_id = ? AND ended_at IS NOT NULL
		GROUP BY mode`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("query chapter progress: %w", err)
	}
	defer rows.Close()

	byMode := map[string]ModuleProgress{}
	for rows.Next() {
		var p ModuleProgress
		if err := rows.Scan(&p.Mode, &p.Sessions, &p.LastEndedAt, &p.BestScore); err != nil {
			return nil, err
		}
		byMode[p.Mode] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	progress := make([]ModuleProgress, 0, len(StudyModes))
	for _, mode := range StudyModes {
		p, ok := byMode[mode]
		if !ok {
			p = ModuleProgress{Mode: mode}
		}
		progress = append(progress, p)
	}
	return progress, nil
}

// ChapterDetail is everything the chapter page needs in one request: the
// heading, the English intro, how much of each module there is to do, and how
// much of it has been done.
type ChapterDetail struct {
	Chapter
	Intro    string           `json:"intro"`
	Counts   ChapterCounts    `json:"counts"`
	Progress []ModuleProgress `json:"progress"`
}

// ChapterCounts is what each module has to work with. A zero here is why a
// tile renders as unavailable rather than opening onto an empty screen.
type ChapterCounts struct {
	Vocab     int `json:"vocab"`
	Grammar   int `json:"grammar"`
	Exercises int `json:"exercises"`
	Passages  int `json:"passages"`
}

// ChapterByID returns the detail for one chapter, or ErrNotFound.
func (db *DB) ChapterByID(ctx context.Context, chapterID int64) (ChapterDetail, error) {
	var d ChapterDetail
	err := db.QueryRowContext(ctx, `
		SELECT c.id, c.book, c.lesson_no, c.position, c.title, c.intro, c.deadline_at,
		       (SELECT count(*) FROM vocab          x WHERE x.chapter_id = c.id AND x.retired_at IS NULL),
		       (SELECT count(*) FROM grammar_points x WHERE x.chapter_id = c.id AND x.retired_at IS NULL),
		       (SELECT count(*) FROM exercises      x WHERE x.chapter_id = c.id AND x.retired_at IS NULL),
		       (SELECT count(*) FROM passages       x WHERE x.chapter_id = c.id AND x.retired_at IS NULL)
		FROM chapters c
		WHERE c.id = ? AND c.retired_at IS NULL`, chapterID,
	).Scan(&d.ID, &d.Book, &d.LessonNo, &d.Position, &d.Title, &d.Intro, &d.DeadlineAt,
		&d.Counts.Vocab, &d.Counts.Grammar, &d.Counts.Exercises, &d.Counts.Passages)
	if errors.Is(err, sql.ErrNoRows) {
		return ChapterDetail{}, fmt.Errorf("chapter %d: %w", chapterID, ErrNotFound)
	}
	if err != nil {
		return ChapterDetail{}, fmt.Errorf("query chapter %d: %w", chapterID, err)
	}
	d.VocabCount = d.Counts.Vocab

	d.Progress, err = db.ChapterProgress(ctx, chapterID)
	if err != nil {
		return ChapterDetail{}, err
	}
	return d, nil
}

// ChapterExists distinguishes an empty chapter from one that is not there,
// so the API can answer 404 instead of an empty list.
func (db *DB) ChapterExists(ctx context.Context, chapterID int64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM chapters WHERE id = ? AND retired_at IS NULL`, chapterID).Scan(&n)
	return n > 0, err
}
