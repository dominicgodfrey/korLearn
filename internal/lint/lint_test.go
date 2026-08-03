package lint

import (
	"strings"
	"testing"

	"github.com/dominicgodfrey/korLearn/internal/seed"
)

func lesson(position int, words []string, exercisePrompt string) seed.Lesson {
	l := seed.Lesson{
		SchemaVersion: seed.SchemaVersion,
		Book:          "Fixture Book",
		LessonNo:      position,
		Position:      position,
		Title:         "Fixture",
	}
	for _, w := range words {
		l.Vocab = append(l.Vocab, seed.Vocab{Korean: w, English: []string{w}, POS: "noun"})
	}
	if exercisePrompt != "" {
		l.Exercises = []seed.Exercise{
			{Kind: seed.KindFillBlank, Prompt: exercisePrompt, Answer: []string{"x"}},
		}
	}
	return l
}

func TestTokens(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"저는 학생___.", []string{"저는", "학생"}},
		{"hello 안녕", []string{"안녕"}},
		{"3개를 샀어요", []string{"개를", "샀어요"}},
		{"", nil},
		{"no hangul at all", nil},
	}
	for _, tt := range tests {
		got := tokens(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("tokens(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// The core case: a word never taught should be flagged.
func TestFlagsUntaughtWord(t *testing.T) {
	findings := Run([]seed.Lesson{
		lesson(1, []string{"학생"}, "학생과 선생님"),
	})
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(findings), findings)
	}
	if findings[0].Token != "선생님" {
		t.Errorf("flagged %q, want 선생님", findings[0].Token)
	}
}

// A lesson may use the words it teaches.
func TestOwnVocabularyIsAllowed(t *testing.T) {
	findings := Run([]seed.Lesson{
		lesson(1, []string{"학생", "선생님"}, "학생과 선생님"),
	})
	if len(findings) != 0 {
		t.Errorf("got %v, want none", findings)
	}
}

// Vocabulary from an earlier position carries forward.
func TestEarlierLessonsCarryForward(t *testing.T) {
	findings := Run([]seed.Lesson{
		lesson(1, []string{"학생"}, ""),
		lesson(2, []string{"선생님"}, "학생과 선생님"),
	})
	if len(findings) != 0 {
		t.Errorf("got %v, want none", findings)
	}
}

// A later lesson's vocabulary must not license an earlier lesson.
func TestLaterVocabularyDoesNotCountEarly(t *testing.T) {
	findings := Run([]seed.Lesson{
		lesson(1, []string{"학생"}, "학생과 선생님"),
		lesson(2, []string{"선생님"}, ""),
	})
	if len(findings) != 1 || findings[0].Position != 1 {
		t.Errorf("got %v, want one finding at position 1", findings)
	}
}

// The whole reason the matcher is stem-prefix rather than exact: teaching the
// dictionary form must cover its inflections.
func TestInflectionsMatchTheirStem(t *testing.T) {
	l := lesson(1, []string{"먹다"}, "")
	l.Exercises = []seed.Exercise{
		{Kind: seed.KindFillBlank, Prompt: "먹었어요", Answer: []string{"먹어요"}},
	}
	findings := Run([]seed.Lesson{l})
	if len(findings) != 0 {
		t.Errorf("got %v, want none — 먹다 should cover its inflections", findings)
	}
}

// allowExtra is the escape hatch for particles and endings that will never
// match a dictionary form.
func TestAllowExtraSuppresses(t *testing.T) {
	l := lesson(1, []string{"학생"}, "감사합니다")
	if got := Run([]seed.Lesson{l}); len(got) == 0 {
		t.Fatal("fixture no longer produces a finding; the test proves nothing")
	}

	l.AllowExtra = []string{"감사합니다"}
	if got := Run([]seed.Lesson{l}); len(got) != 0 {
		t.Errorf("got %v, want none after allowExtra", got)
	}
}

// A single syllable is a prefix of a huge share of Korean words; treating it as
// a licence would silence the check.
func TestSingleSyllableDoesNotLicenseEverything(t *testing.T) {
	findings := Run([]seed.Lesson{
		lesson(1, []string{"가"}, "가족과 친구"),
	})
	if len(findings) == 0 {
		t.Error("a one-syllable word licensed unrelated vocabulary")
	}
}

// One word used repeatedly is one thing to fix.
func TestRepeatsAreCollapsed(t *testing.T) {
	l := lesson(1, []string{"학생"}, "")
	l.Passages = []seed.Passage{{
		Korean:    "선생님 선생님 선생님",
		Questions: []seed.PassageQuestion{{Q: "선생님?", Reference: "x"}},
	}}
	findings := Run([]seed.Lesson{l})
	if len(findings) != 1 {
		t.Errorf("got %d findings, want 1: %v", len(findings), findings)
	}
}

// Non-Korean content must never be flagged.
func TestIgnoresNonKorean(t *testing.T) {
	findings := Run([]seed.Lesson{
		lesson(1, []string{"학생"}, "Fill in the blank: ___ (3 points)"),
	})
	if len(findings) != 0 {
		t.Errorf("got %v, want none", findings)
	}
}
