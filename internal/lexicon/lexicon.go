package lexicon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Querier is the part of *sql.DB this package needs. Taking an interface keeps
// the dependency pointing one way — store knows nothing about lexicon — and
// lets the tests drive it with an ordinary database handle.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Word is one unlocked vocabulary entry.
type Word struct {
	ID      int64    `json:"id"`
	Korean  string   `json:"korean"`
	English []string `json:"english"`
	POS     string   `json:"pos"`
	// ChapterPosition is where it was taught, so a caller can tell "this
	// chapter's words" from "words carried forward" without a second query.
	ChapterPosition int `json:"chapterPosition"`
}

// Point is one unlocked grammar pattern.
type Point struct {
	ID              int64  `json:"id"`
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	ChapterPosition int    `json:"chapterPosition"`
}

// Set is everything taught at or before a position.
type Set struct {
	Position int     `json:"position"`
	Words    []Word  `json:"words"`
	Grammar  []Point `json:"grammar"`
}

// Load reads everything unlocked at or before position.
//
// Progression is linear by assumption: studying chapter N asserts that
// chapters 1..N-1 are known, so there is no per-chapter completion flag to
// consult and no way for the set to have holes in it.
func Load(ctx context.Context, q Querier, position int) (*Set, error) {
	set := &Set{Position: position, Words: []Word{}, Grammar: []Point{}}

	rows, err := q.QueryContext(ctx, `
		SELECT v.id, v.korean, v.english_json, v.pos, c.position
		FROM vocab v
		JOIN chapters c ON c.id = v.chapter_id
		WHERE c.position <= ? AND c.retired_at IS NULL AND v.retired_at IS NULL
		ORDER BY c.position, v.id`, position)
	if err != nil {
		return nil, fmt.Errorf("query unlocked vocab: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			w       Word
			english string
		)
		if err := rows.Scan(&w.ID, &w.Korean, &english, &w.POS, &w.ChapterPosition); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(english), &w.English); err != nil {
			return nil, fmt.Errorf("vocab %d: bad english_json: %w", w.ID, err)
		}
		set.Words = append(set.Words, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prows, err := q.QueryContext(ctx, `
		SELECT g.id, g.slug, g.title, c.position
		FROM grammar_points g
		JOIN chapters c ON c.id = g.chapter_id
		WHERE c.position <= ? AND c.retired_at IS NULL AND g.retired_at IS NULL
		ORDER BY c.position, g.id`, position)
	if err != nil {
		return nil, fmt.Errorf("query unlocked grammar: %w", err)
	}
	defer prows.Close()
	for prows.Next() {
		var p Point
		if err := prows.Scan(&p.ID, &p.Slug, &p.Title, &p.ChapterPosition); err != nil {
			return nil, err
		}
		set.Grammar = append(set.Grammar, p)
	}
	return set, prows.Err()
}

// LoadForChapter is Load for the chapter's own position. It reports whether the
// chapter exists, so a caller can answer 404 rather than returning the whole
// curriculum for a typo'd id — position 0 would otherwise read as "nothing
// unlocked", which is a plausible-looking wrong answer.
func LoadForChapter(ctx context.Context, q Querier, chapterID int64) (*Set, bool, error) {
	var position int
	err := q.QueryRowContext(ctx,
		`SELECT position FROM chapters WHERE id = ? AND retired_at IS NULL`, chapterID).Scan(&position)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("look up chapter %d: %w", chapterID, err)
	}
	set, err := Load(ctx, q, position)
	return set, true, err
}

// Matcher builds a text matcher from the set: every unlocked word, plus the
// grammar point titles, which name the patterns being taught and so are a
// source of legitimate forms rather than a place to check.
func (s *Set) Matcher() *Matcher {
	m := NewMatcher()
	for _, w := range s.Words {
		m.Add(w.Korean)
	}
	for _, p := range s.Grammar {
		m.AddText(p.Title)
	}
	return m
}
