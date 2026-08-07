package lint

import (
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
)

// covered builds a lesson whose passage exercises everything it teaches.
func covered() seed.Lesson {
	return seed.Lesson{
		SchemaVersion: seed.SchemaVersion,
		Book:          "Fixture Book",
		LessonNo:      1,
		Position:      1,
		Title:         "Fixture",
		Intro:         "Fixture intro.",
		Vocab: []seed.Vocab{
			{Korean: "학생", English: []string{"student"}, POS: "noun"},
			{Korean: "먹다", English: []string{"to eat"}, POS: "verb"},
		},
		GrammarPoints: []seed.GrammarPoint{{
			ID: "g1", Title: "Fixture point", Explanation: "e",
			Example: seed.Example{Korean: "학생이에요.", English: "Is a student."},
		}},
		Passages: []seed.Passage{{
			Korean: "학생이 먹었어요.",
			Questions: []seed.PassageQuestion{{
				Q: "누구예요?", Reference: "학생이에요.", GrammarPoints: []string{"g1"},
			}},
		}},
	}
}

func TestCoverageAcceptsACompleteChapter(t *testing.T) {
	if gaps := Coverage([]seed.Lesson{covered()}); len(gaps) != 0 {
		t.Errorf("got %v, want none", gaps)
	}
}

// The case the rule exists for: a word taught in the vocab module and never
// met again.
func TestCoverageFlagsUnusedVocab(t *testing.T) {
	l := covered()
	l.Vocab = append(l.Vocab, seed.Vocab{Korean: "선생님", English: []string{"teacher"}, POS: "noun"})

	gaps := Coverage([]seed.Lesson{l})
	if len(gaps) != 1 || gaps[0].Item != "선생님" || gaps[0].Kind != "vocab" {
		t.Fatalf("gaps = %v, want one vocab gap for 선생님", gaps)
	}
}

// A dictionary-form verb will not appear in that form in a passage, so its
// inflections have to count.
func TestCoverageAcceptsInflectedUse(t *testing.T) {
	l := covered()
	// 먹다 is only ever present as 먹었어요.
	for _, g := range Coverage([]seed.Lesson{l}) {
		if g.Item == "먹다" {
			t.Error("an inflected use did not count as coverage")
		}
	}
}

// Short words are prefix-matched here even though lexicon.Matcher refuses to:
// a false miss would block a chapter that is genuinely complete.
func TestCoverageAcceptsParticleAttachedShortWords(t *testing.T) {
	l := covered()
	l.Vocab = append(l.Vocab, seed.Vocab{Korean: "물", English: []string{"water"}, POS: "noun"})
	l.Passages[0].Korean = "학생이 물을 먹었어요."

	if gaps := Coverage([]seed.Lesson{l}); len(gaps) != 0 {
		t.Errorf("got %v, want none — 물을 is a use of 물", gaps)
	}
}

func TestCoverageFlagsUnaskedGrammar(t *testing.T) {
	l := covered()
	l.Passages[0].Questions[0].GrammarPoints = nil

	gaps := Coverage([]seed.Lesson{l})
	if len(gaps) != 1 || gaps[0].Kind != "grammar" || gaps[0].Item != "g1" {
		t.Fatalf("gaps = %v, want one grammar gap for g1", gaps)
	}
}

func TestCoverageExemptSuppresses(t *testing.T) {
	l := covered()
	l.Vocab = append(l.Vocab, seed.Vocab{Korean: "선생님", English: []string{"teacher"}, POS: "noun"})
	if len(Coverage([]seed.Lesson{l})) == 0 {
		t.Fatal("fixture no longer produces a gap; the test proves nothing")
	}

	l.CoverageExempt = []string{"선생님"}
	if gaps := Coverage([]seed.Lesson{l}); len(gaps) != 0 {
		t.Errorf("got %v, want none after coverageExempt", gaps)
	}
}

// A chapter with content and no passages at all is the loudest version of the
// same failure, and must not pass by having nothing to check against.
func TestCoverageFlagsChapterWithoutPassages(t *testing.T) {
	l := covered()
	l.Passages = nil

	gaps := Coverage([]seed.Lesson{l})
	if len(gaps) != 3 {
		t.Errorf("got %d gaps, want 3 (two vocab, one grammar): %v", len(gaps), gaps)
	}
}

// Coverage is per chapter: a later chapter's passage cannot cover an earlier
// chapter's vocabulary, or the check would drift into meaninglessness as the
// curriculum grows.
func TestCoverageIsPerChapter(t *testing.T) {
	first := covered()
	first.Passages = nil

	second := covered()
	second.LessonNo, second.Position = 2, 2

	gaps := Coverage([]seed.Lesson{first, second})
	for _, g := range gaps {
		if g.Position != 1 {
			t.Errorf("gap at position %d: %v", g.Position, g)
		}
	}
	if len(gaps) == 0 {
		t.Error("the uncovered first chapter produced no gaps")
	}
}
