// Package stats derives every number the dashboard shows from the attempts
// table and nothing else.
//
// Nothing here is stored or incrementally maintained. attempts is append-only,
// so each of these is a pure function of it — which means no counter can drift
// out of sync, and a metric can be redefined later by changing one query rather
// than by backfilling a table.
package stats

import (
	"context"
	"fmt"

	"github.com/dominicgodfrey/korLearn/internal/store"
)

type Stats struct {
	db *store.DB
}

func New(db *store.DB) *Stats {
	return &Stats{db: db}
}

// firstAttemptsCTE selects the first attempt at each item within each session.
//
// It is the shared basis of every accuracy number here, and it matches how a
// session's own score is computed, so the dashboard and the end-of-session
// screen can never disagree. Re-drilling an item within a session does not
// change any of these figures.
const firstAttemptsCTE = `
	WITH firsts AS (
		SELECT a.*
		FROM attempts a
		JOIN (
			SELECT session_id, item_type, item_id, MIN(id) AS id
			FROM attempts
			GROUP BY session_id, item_type, item_id
		) f ON a.id = f.id
	)`

// ChapterGrade is the headline per-chapter figure: how you have done over time,
// and the best you have ever done.
type ChapterGrade struct {
	ChapterID int64  `json:"chapterId"`
	Position  int    `json:"position"`
	Title     string `json:"title"`
	Sessions  int    `json:"sessions"`
	// AverageScore and BestScore are null until a session has been completed.
	AverageScore  *float64 `json:"averageScore"`
	BestScore     *float64 `json:"bestScore"`
	LastStudiedAt *string  `json:"lastStudiedAt"`
	DeadlineAt    *string  `json:"deadlineAt"`
}

// ChapterGrades returns one row per live chapter, including chapters never
// studied — a dashboard that hides them would hide exactly the ones needing
// attention.
//
// It averages the score already stored on each session rather than recomputing
// from attempts, so these figures agree with what each session reported at the
// time by construction.
func (s *Stats) ChapterGrades(ctx context.Context) ([]ChapterGrade, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.position, c.title, c.deadline_at,
		       COUNT(s.id),
		       AVG(s.score),
		       MAX(s.score),
		       MAX(s.ended_at)
		FROM chapters c
		LEFT JOIN sessions s
		       ON s.chapter_id = c.id AND s.ended_at IS NOT NULL AND s.score IS NOT NULL
		WHERE c.retired_at IS NULL
		GROUP BY c.id
		ORDER BY c.position`)
	if err != nil {
		return nil, fmt.Errorf("chapter grades: %w", err)
	}
	defer rows.Close()

	grades := []ChapterGrade{}
	for rows.Next() {
		var g ChapterGrade
		if err := rows.Scan(&g.ChapterID, &g.Position, &g.Title, &g.DeadlineAt,
			&g.Sessions, &g.AverageScore, &g.BestScore, &g.LastStudiedAt); err != nil {
			return nil, err
		}
		grades = append(grades, g)
	}
	return grades, rows.Err()
}

// TrendPoint is one completed session, for the per-chapter trend chart.
type TrendPoint struct {
	SessionID int64   `json:"sessionId"`
	Mode      string  `json:"mode"`
	EndedAt   string  `json:"endedAt"`
	Score     float64 `json:"score"`
}

// ChapterTrend returns completed sessions oldest first, so the chart reads
// left to right without the client having to sort.
func (s *Stats) ChapterTrend(ctx context.Context, chapterID int64) ([]TrendPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, mode, ended_at, score
		FROM sessions
		WHERE chapter_id = ? AND ended_at IS NOT NULL AND score IS NOT NULL
		ORDER BY ended_at`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("chapter trend: %w", err)
	}
	defer rows.Close()

	points := []TrendPoint{}
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.SessionID, &p.Mode, &p.EndedAt, &p.Score); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// WeakItem is one thing you keep getting wrong.
type WeakItem struct {
	ItemType string `json:"itemType"`
	ItemID   int64  `json:"itemId"`
	// Label is the Korean text or exercise prompt, so the UI needs no second
	// lookup to render the row.
	Label        string  `json:"label"`
	ChapterID    int64   `json:"chapterId"`
	Attempts     int     `json:"attempts"`
	Correct      int     `json:"correct"`
	Accuracy     float64 `json:"accuracy"`
	LastMissedAt *string `json:"lastMissedAt"`
}

// WeakItems ranks live content by first-try accuracy, worst first.
//
// This feeds the dashboard table and the on-demand "trouble spots" playlist. It
// never reorders a session by itself: the plan is explicit that the user always
// chooses what to study, so this is only ever a suggestion.
//
// Retired content is excluded — it cannot be studied, so listing it as a
// weakness would be advice you cannot act on.
func (s *Stats) WeakItems(ctx context.Context, limit int) ([]WeakItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, firstAttemptsCTE+`
		SELECT f.item_type, f.item_id,
		       COALESCE(v.korean, e.prompt, p.korean_text, '') AS label,
		       COALESCE(v.chapter_id, e.chapter_id, p.chapter_id, 0) AS chapter_id,
		       COUNT(*)      AS attempts,
		       SUM(f.correct) AS correct,
		       MAX(CASE WHEN f.correct = 0 THEN f.created_at END) AS last_missed
		FROM firsts f
		LEFT JOIN vocab     v ON f.item_type = 'vocab'            AND v.id = f.item_id
		LEFT JOIN exercises e ON f.item_type = 'exercise'         AND e.id = f.item_id
		LEFT JOIN passages  p ON f.item_type = 'passage_question' AND p.id = f.item_id
		WHERE COALESCE(v.retired_at, e.retired_at, p.retired_at) IS NULL
		GROUP BY f.item_type, f.item_id
		ORDER BY (CAST(SUM(f.correct) AS REAL) / COUNT(*)) ASC, COUNT(*) DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("weak items: %w", err)
	}
	defer rows.Close()

	items := []WeakItem{}
	for rows.Next() {
		var w WeakItem
		if err := rows.Scan(&w.ItemType, &w.ItemID, &w.Label, &w.ChapterID,
			&w.Attempts, &w.Correct, &w.LastMissedAt); err != nil {
			return nil, err
		}
		if w.Attempts > 0 {
			w.Accuracy = float64(w.Correct) / float64(w.Attempts)
		}
		items = append(items, w)
	}
	return items, rows.Err()
}

// WeakGrammarPoint aggregates errors by grammar point, which is what turns a
// list of missed items into "you keep missing 을/를".
type WeakGrammarPoint struct {
	GrammarPointID int64   `json:"grammarPointId"`
	Slug           string  `json:"slug"`
	Title          string  `json:"title"`
	Attempts       int     `json:"attempts"`
	Correct        int     `json:"correct"`
	Accuracy       float64 `json:"accuracy"`
}

func (s *Stats) WeakGrammarPoints(ctx context.Context, limit int) ([]WeakGrammarPoint, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, firstAttemptsCTE+`
		SELECT g.id, g.slug, g.title, COUNT(*), SUM(f.correct)
		FROM firsts f
		JOIN exercises      e ON f.item_type = 'exercise' AND e.id = f.item_id
		JOIN grammar_points g ON g.id = e.grammar_point_id
		WHERE g.retired_at IS NULL
		GROUP BY g.id
		ORDER BY (CAST(SUM(f.correct) AS REAL) / COUNT(*)) ASC, COUNT(*) DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("weak grammar points: %w", err)
	}
	defer rows.Close()

	points := []WeakGrammarPoint{}
	for rows.Next() {
		var p WeakGrammarPoint
		if err := rows.Scan(&p.GrammarPointID, &p.Slug, &p.Title, &p.Attempts, &p.Correct); err != nil {
			return nil, err
		}
		if p.Attempts > 0 {
			p.Accuracy = float64(p.Correct) / float64(p.Attempts)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// CalendarDay is one cell of the contribution-style heatmap.
//
// Attempts counts everything done that day, including re-drills, because heat
// should reflect effort. Accuracy deliberately does not: it is first-try over
// Items, the same basis as every other figure here, so a day's accuracy cannot
// disagree with the sessions that made it up.
type CalendarDay struct {
	Day      string `json:"day"`
	Sessions int    `json:"sessions"`
	Attempts int    `json:"attempts"`
	// Items is the number of distinct items seen, which is what Accuracy is over.
	Items    int     `json:"items"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// Calendar aggregates practice by day. from and to are inclusive YYYY-MM-DD
// bounds; either may be empty for an open end.
//
// Days with no practice are absent rather than zero-filled: the range the UI
// wants to draw is a display concern, and filling here would mean guessing it.
func (s *Stats) Calendar(ctx context.Context, from, to string) ([]CalendarDay, error) {
	rows, err := s.db.QueryContext(ctx, firstAttemptsCTE+`,
		activity AS (
			SELECT date(created_at) AS day,
			       COUNT(DISTINCT session_id) AS sessions,
			       COUNT(*) AS attempts
			FROM attempts
			GROUP BY day
		),
		accuracy AS (
			SELECT date(created_at) AS day,
			       COUNT(*) AS items,
			       SUM(correct) AS correct
			FROM firsts
			GROUP BY day
		)
		SELECT a.day, a.sessions, a.attempts,
		       COALESCE(f.items, 0), COALESCE(f.correct, 0)
		FROM activity a
		LEFT JOIN accuracy f ON f.day = a.day
		WHERE (? = '' OR a.day >= ?)
		  AND (? = '' OR a.day <= ?)
		ORDER BY a.day`, from, from, to, to)
	if err != nil {
		return nil, fmt.Errorf("calendar: %w", err)
	}
	defer rows.Close()

	days := []CalendarDay{}
	for rows.Next() {
		var d CalendarDay
		if err := rows.Scan(&d.Day, &d.Sessions, &d.Attempts, &d.Items, &d.Correct); err != nil {
			return nil, err
		}
		if d.Items > 0 {
			d.Accuracy = float64(d.Correct) / float64(d.Items)
		}
		days = append(days, d)
	}
	return days, rows.Err()
}
