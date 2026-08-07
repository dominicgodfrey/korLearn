package stats

import (
	"context"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
	"github.com/dominicgodfrey/korLearn/internal/store"
)

type fixture struct {
	db      *store.DB
	stats   *Stats
	chapter int64
	vocab   []int64
	t       *testing.T
}

// newFixture loads one chapter of vocab plus a grammar point and an exercise.
func newFixture(t *testing.T, words ...string) *fixture {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	l := seed.Lesson{
		SchemaVersion: seed.SchemaVersion,
		Book:          "Fixture Book",
		LessonNo:      1,
		Position:      1,
		Title:         "Fixture",
		GrammarPoints: []seed.GrammarPoint{{ID: "fx", Title: "Fixture point"}},
		Exercises: []seed.Exercise{
			{Kind: seed.KindFillBlank, GrammarPoint: "fx", Prompt: "___", Answer: []string{"a"}},
		},
	}
	for _, w := range words {
		l.Vocab = append(l.Vocab, seed.Vocab{Korean: w, English: []string{w}, POS: "noun"})
	}
	if err := db.LoadLessons(context.Background(), []seed.Lesson{l}); err != nil {
		t.Fatal(err)
	}

	f := &fixture{db: db, stats: New(db), t: t}
	if err := db.QueryRow(`SELECT id FROM chapters`).Scan(&f.chapter); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM vocab ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.vocab = append(f.vocab, id)
	}
	return f
}

// session records a run of attempts against vocab and closes it, returning the
// stored score. results[i] is the verdict for the i'th listed item.
func (f *fixture) session(items []int64, results []bool) *float64 {
	f.t.Helper()
	ctx := context.Background()
	id, err := f.db.CreateSession(ctx, &f.chapter, "vocab_flip")
	if err != nil {
		f.t.Fatal(err)
	}
	for i, itemID := range items {
		if _, err := f.db.RecordAttempt(ctx, store.Attempt{
			SessionID: id, ItemType: store.ItemVocab, ItemID: itemID,
			Stage: store.StageMC, Correct: results[i], OriginalCorrect: results[i],
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	res, err := f.db.EndSession(ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return res.Score
}

func TestChapterGrades(t *testing.T) {
	f := newFixture(t, "하나", "둘")
	f.session(f.vocab, []bool{true, false}) // 0.5
	f.session(f.vocab, []bool{true, true})  // 1.0

	grades, err := f.stats.ChapterGrades(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(grades) != 1 {
		t.Fatalf("got %d grades, want 1", len(grades))
	}
	g := grades[0]
	if g.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", g.Sessions)
	}
	if g.AverageScore == nil || *g.AverageScore != 0.75 {
		t.Errorf("averageScore = %v, want 0.75", g.AverageScore)
	}
	if g.BestScore == nil || *g.BestScore != 1 {
		t.Errorf("bestScore = %v, want 1", g.BestScore)
	}
	if g.LastStudiedAt == nil {
		t.Error("lastStudiedAt is nil after two sessions")
	}
}

// A chapter never studied must still appear — those are the ones that need
// attention, and hiding them defeats the dashboard.
func TestChapterGradesIncludeUnstudied(t *testing.T) {
	f := newFixture(t, "하나")

	grades, err := f.stats.ChapterGrades(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(grades) != 1 {
		t.Fatalf("got %d grades, want 1", len(grades))
	}
	if grades[0].Sessions != 0 {
		t.Errorf("sessions = %d, want 0", grades[0].Sessions)
	}
	if grades[0].AverageScore != nil {
		t.Errorf("averageScore = %v, want null for an unstudied chapter", *grades[0].AverageScore)
	}
}

// An abandoned session has no score and must not drag the average down.
func TestUnfinishedSessionsAreExcluded(t *testing.T) {
	f := newFixture(t, "하나")
	f.session(f.vocab, []bool{true})

	ctx := context.Background()
	if _, err := f.db.CreateSession(ctx, &f.chapter, "vocab_flip"); err != nil {
		t.Fatal(err)
	}

	grades, err := f.stats.ChapterGrades(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if grades[0].Sessions != 1 {
		t.Errorf("sessions = %d, want 1 (the open one must not count)", grades[0].Sessions)
	}
	if grades[0].AverageScore == nil || *grades[0].AverageScore != 1 {
		t.Errorf("averageScore = %v, want 1", grades[0].AverageScore)
	}
}

// The dashboard average must equal what the sessions themselves reported.
func TestGradesAgreeWithSessionScores(t *testing.T) {
	f := newFixture(t, "하나", "둘")
	a := f.session(f.vocab, []bool{true, false})
	b := f.session(f.vocab, []bool{false, false})

	grades, err := f.stats.ChapterGrades(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := (*a + *b) / 2
	if got := *grades[0].AverageScore; got != want {
		t.Errorf("dashboard average = %v, sessions reported %v and %v (mean %v)", got, *a, *b, want)
	}
}

func TestChapterTrendIsChronological(t *testing.T) {
	f := newFixture(t, "하나")
	f.session(f.vocab, []bool{false})
	f.session(f.vocab, []bool{true})

	points, err := f.stats.ChapterTrend(context.Background(), f.chapter)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}
	if points[0].Score != 0 || points[1].Score != 1 {
		t.Errorf("scores = %v, %v; want 0 then 1", points[0].Score, points[1].Score)
	}
	if points[0].EndedAt > points[1].EndedAt {
		t.Error("trend is not oldest-first")
	}
}

func TestWeakItemsRankWorstFirst(t *testing.T) {
	f := newFixture(t, "하나", "둘")
	// 하나 right every time, 둘 wrong every time.
	f.session(f.vocab, []bool{true, false})
	f.session(f.vocab, []bool{true, false})

	items, err := f.stats.WeakItems(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Label != "둘" {
		t.Errorf("worst item = %q, want 둘", items[0].Label)
	}
	if items[0].Accuracy != 0 || items[1].Accuracy != 1 {
		t.Errorf("accuracies = %v, %v; want 0 then 1", items[0].Accuracy, items[1].Accuracy)
	}
	if items[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", items[0].Attempts)
	}
	if items[0].LastMissedAt == nil {
		t.Error("lastMissedAt is nil for an always-missed item")
	}
	if items[1].LastMissedAt != nil {
		t.Error("lastMissedAt is set for a never-missed item")
	}
}

// Re-drilling an item until it is right must not improve its weakness score,
// exactly as it does not improve the session score.
func TestWeakItemsUseFirstAttemptOnly(t *testing.T) {
	f := newFixture(t, "하나")
	ctx := context.Background()

	id, err := f.db.CreateSession(ctx, &f.chapter, "vocab_flip")
	if err != nil {
		t.Fatal(err)
	}
	for _, correct := range []bool{false, true, true, true} {
		if _, err := f.db.RecordAttempt(ctx, store.Attempt{
			SessionID: id, ItemType: store.ItemVocab, ItemID: f.vocab[0],
			Stage: store.StageMC, Correct: correct, OriginalCorrect: correct,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.EndSession(ctx, id); err != nil {
		t.Fatal(err)
	}

	items, err := f.stats.WeakItems(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Accuracy != 0 {
		t.Errorf("accuracy = %v, want 0 — only the first attempt counts", items[0].Accuracy)
	}
	if items[0].Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (one session, one first try)", items[0].Attempts)
	}
}

// Weakness advice you cannot act on is worse than none.
func TestWeakItemsExcludeRetiredContent(t *testing.T) {
	f := newFixture(t, "하나", "둘")
	f.session(f.vocab, []bool{true, false})

	// 둘 is dropped from the lesson.
	reduced := seed.Lesson{
		SchemaVersion: seed.SchemaVersion,
		Book:          "Fixture Book",
		LessonNo:      1,
		Position:      1,
		Title:         "Fixture",
		Vocab:         []seed.Vocab{{Korean: "하나", English: []string{"one"}, POS: "noun"}},
	}
	if err := f.db.LoadLessons(context.Background(), []seed.Lesson{reduced}); err != nil {
		t.Fatal(err)
	}

	items, err := f.stats.WeakItems(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Label == "둘" {
			t.Error("retired content appears in the weakness ranking")
		}
	}
}

func TestWeakGrammarPoints(t *testing.T) {
	f := newFixture(t, "하나")
	ctx := context.Background()

	var exerciseID int64
	if err := f.db.QueryRow(`SELECT id FROM exercises`).Scan(&exerciseID); err != nil {
		t.Fatal(err)
	}

	id, err := f.db.CreateSession(ctx, &f.chapter, "grammar")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.RecordAttempt(ctx, store.Attempt{
		SessionID: id, ItemType: store.ItemExercise, ItemID: exerciseID,
		Stage: store.StageTyped, Correct: false, OriginalCorrect: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.EndSession(ctx, id); err != nil {
		t.Fatal(err)
	}

	points, err := f.stats.WeakGrammarPoints(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Fatalf("got %d grammar points, want 1", len(points))
	}
	if points[0].Slug != "fx" || points[0].Accuracy != 0 {
		t.Errorf("got %+v, want slug fx with accuracy 0", points[0])
	}
}

func TestCalendar(t *testing.T) {
	f := newFixture(t, "하나", "둘")
	f.session(f.vocab, []bool{true, false})

	days, err := f.stats.Calendar(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("got %d days, want 1", len(days))
	}
	d := days[0]
	if d.Sessions != 1 || d.Attempts != 2 || d.Items != 2 {
		t.Errorf("got sessions=%d attempts=%d items=%d, want 1/2/2", d.Sessions, d.Attempts, d.Items)
	}
	if d.Accuracy != 0.5 {
		t.Errorf("accuracy = %v, want 0.5", d.Accuracy)
	}
}

// Heat reflects effort, so re-drills count as attempts; accuracy stays first-try
// so a day cannot disagree with the sessions that made it up.
func TestCalendarCountsEffortButScoresFirstTry(t *testing.T) {
	f := newFixture(t, "하나")
	ctx := context.Background()

	id, err := f.db.CreateSession(ctx, &f.chapter, "vocab_flip")
	if err != nil {
		t.Fatal(err)
	}
	for _, correct := range []bool{false, true, true} {
		if _, err := f.db.RecordAttempt(ctx, store.Attempt{
			SessionID: id, ItemType: store.ItemVocab, ItemID: f.vocab[0],
			Stage: store.StageMC, Correct: correct, OriginalCorrect: correct,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.EndSession(ctx, id); err != nil {
		t.Fatal(err)
	}

	days, err := f.stats.Calendar(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if days[0].Attempts != 3 {
		t.Errorf("attempts = %d, want 3 (all effort counts)", days[0].Attempts)
	}
	if days[0].Accuracy != 0 {
		t.Errorf("accuracy = %v, want 0 (first try only)", days[0].Accuracy)
	}
}

func TestCalendarRangeFilter(t *testing.T) {
	f := newFixture(t, "하나")
	f.session(f.vocab, []bool{true})

	days, err := f.stats.Calendar(context.Background(), "1999-01-01", "1999-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 0 {
		t.Errorf("got %d days in an empty range, want 0", len(days))
	}
}
