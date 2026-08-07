// Package seed parses the hand-authored lesson files under content/.
//
// Seed files are the only source of curriculum content: the SQLite database is
// derived from them on every startup and can be deleted at any time without
// losing anything but attempt history. Because the files are hand-authored,
// parsing is deliberately strict — an unknown field is far more likely to be a
// typo that would silently drop content than a deliberate extension.
package seed

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// SchemaVersion is the only seed schema this binary understands. Bump it in
// lockstep with a migration when the shape changes.
const SchemaVersion = 2

// Lesson is one seed file: a single lesson of a single textbook.
type Lesson struct {
	SchemaVersion int    `json:"schemaVersion"`
	Book          string `json:"book"`
	LessonNo      int    `json:"lesson"`
	// Position orders every lesson across every book in one global sequence.
	// Content is "unlocked" when its position is at or below the current one.
	Position int    `json:"position"`
	Title    string `json:"title"`
	// Intro is the English overview shown before anything is studied: what this
	// chapter is for and what you should be able to do afterwards. English on
	// purpose — it is the one place the goal matters more than the immersion.
	Intro string `json:"intro"`
	// AllowExtra lists tokens the lexicon lint should not flag for this lesson —
	// particles and inflections that will never match an earlier dictionary form.
	AllowExtra []string `json:"allowExtra,omitempty"`
	// CoverageExempt names vocabulary deliberately left out of the comprehension
	// coverage requirement. Every entry must be a Korean word this lesson
	// teaches, so an exemption cannot outlive the word it was written for.
	CoverageExempt []string       `json:"coverageExempt,omitempty"`
	Vocab          []Vocab        `json:"vocab"`
	GrammarPoints  []GrammarPoint `json:"grammarPoints,omitempty"`
	Exercises      []Exercise     `json:"exercises,omitempty"`
	Passages       []Passage      `json:"passages,omitempty"`
}

// Vocab is one dictionary entry. The grammatical metadata is carried from the
// first lesson onward so that Intermediate 1 does not force a schema migration.
type Vocab struct {
	Korean  string   `json:"korean"`
	English []string `json:"english"`
	// POS is a part of speech: noun, verb, adjective, adverb, particle, expression.
	POS string `json:"pos"`
	// SpeechLevel is deferential, polite, intimate, plain, or empty where the
	// distinction does not apply.
	SpeechLevel string `json:"speechLevel,omitempty"`
	// Irregular names the irregular conjugation class (ㅂ, ㄷ, ㅅ, 르, 으, ㄹ),
	// nil for regular entries.
	Irregular *string `json:"irregular"`
	Notes     string  `json:"notes,omitempty"`
}

// GrammarPoint is a named pattern that exercises can be attributed to, so the
// dashboard can aggregate weakness by grammar point rather than by item.
type GrammarPoint struct {
	// ID is referenced by Exercise.GrammarPoint and is unique within a lesson.
	ID          string `json:"id"`
	Title       string `json:"title"`
	Explanation string `json:"explanation"`
	// Example is the one worked example shown before the point is drilled.
	// Singular by design: the grammar module shows an example and then moves to
	// practice, and a list here is how a chapter turns into a textbook.
	Example Example `json:"example"`
}

// Example is a sentence and its translation.
type Example struct {
	Korean  string `json:"korean"`
	English string `json:"english"`
}

// Exercise kinds. An unknown kind is rejected at parse time rather than stored,
// because a kind the frontend cannot render would otherwise fail silently.
const (
	KindFillBlank = "fill_blank"
	KindMatching  = "matching"
)

// Exercise is one authored drill. Payload carries kind-specific extras (the
// pairs of a matching exercise, for instance) and is stored opaquely.
type Exercise struct {
	Kind string `json:"kind"`
	// GrammarPoint optionally references a GrammarPoint.ID in the same lesson.
	GrammarPoint string          `json:"grammarPoint,omitempty"`
	Prompt       string          `json:"prompt"`
	Answer       []string        `json:"answer"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

// Passage is a reading comprehension text with free-response questions. Answers
// are graded by the LLM against Reference rather than matched exactly.
type Passage struct {
	Korean    string            `json:"korean"`
	Questions []PassageQuestion `json:"questions"`
}

type PassageQuestion struct {
	Q string `json:"q"`
	// Reference is a model answer given to the grader as context, never shown
	// to the user before they answer.
	Reference string `json:"reference"`
}

// Parse reads one seed file. name is used only in error messages.
//
// The input is normalized to Unicode NFC before decoding. Hangul has both
// composed and decomposed representations that look identical but compare
// unequal, and vocab is keyed on its Korean text — a decomposed file would
// insert duplicate rows that never match a user's typed answer.
func Parse(name string, data []byte) (Lesson, error) {
	var l Lesson
	dec := json.NewDecoder(strings.NewReader(string(norm.NFC.Bytes(data))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return Lesson{}, fmt.Errorf("%s: %w", name, err)
	}
	if err := l.validate(); err != nil {
		return Lesson{}, fmt.Errorf("%s: %w", name, err)
	}
	return l, nil
}

// validate reports every problem in a lesson at once. Hand-authoring is a
// write-check-fix loop, and surfacing one error per run makes it a slow one.
func (l Lesson) validate() error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format, a...))
	}

	if l.SchemaVersion != SchemaVersion {
		// Nothing below can be trusted if the shape is from another version.
		return fmt.Errorf("schemaVersion %d, want %d", l.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(l.Book) == "" {
		bad("book is empty")
	}
	if strings.TrimSpace(l.Title) == "" {
		bad("title is empty")
	}
	if strings.TrimSpace(l.Intro) == "" {
		// The chapter page leads with this. A blank one is a chapter that opens
		// on nothing and never says what it is for.
		bad("intro is empty")
	}
	if l.LessonNo < 1 {
		bad("lesson must be >= 1, got %d", l.LessonNo)
	}
	if l.Position < 1 {
		bad("position must be >= 1, got %d", l.Position)
	}

	seenKorean := make(map[string]int, len(l.Vocab))
	for i, v := range l.Vocab {
		switch {
		case strings.TrimSpace(v.Korean) == "":
			bad("vocab[%d]: korean is empty", i)
		case seenKorean[v.Korean] > 0:
			// Vocab is upserted on (chapter, korean); duplicates would collapse
			// into one row and quietly lose whichever definition loaded second.
			bad("vocab[%d]: %q duplicates vocab[%d]", i, v.Korean, seenKorean[v.Korean]-1)
		default:
			seenKorean[v.Korean] = i + 1
		}
		if len(v.English) == 0 {
			bad("vocab[%d] (%s): needs at least one english gloss", i, v.Korean)
		}
		if strings.TrimSpace(v.POS) == "" {
			bad("vocab[%d] (%s): pos is empty", i, v.Korean)
		}
	}

	points := make(map[string]bool, len(l.GrammarPoints))
	for i, g := range l.GrammarPoints {
		switch {
		case strings.TrimSpace(g.ID) == "":
			bad("grammarPoints[%d]: id is empty", i)
		case points[g.ID]:
			bad("grammarPoints[%d]: duplicate id %q", i, g.ID)
		default:
			points[g.ID] = true
		}
		if strings.TrimSpace(g.Title) == "" {
			bad("grammarPoints[%d] (%s): title is empty", i, g.ID)
		}
		if strings.TrimSpace(g.Example.Korean) == "" {
			bad("grammarPoints[%d] (%s): example.korean is empty", i, g.ID)
		}
		if strings.TrimSpace(g.Example.English) == "" {
			// Without a translation the example teaches nothing on its own, and
			// the grammar module shows it before any practice.
			bad("grammarPoints[%d] (%s): example.english is empty", i, g.ID)
		}
	}

	// An exemption for a word the lesson does not teach is dead config that will
	// silently stop exempting anything the moment the word is renamed.
	for i, word := range l.CoverageExempt {
		if seenKorean[word] == 0 {
			bad("coverageExempt[%d]: %q is not vocabulary in this lesson", i, word)
		}
	}

	// Exercises are stored keyed on (chapter, kind, prompt) and passages on
	// (chapter, korean); a duplicate would overwrite its twin on load rather
	// than appear twice, so it is rejected here where the file is visible.
	seenExercise := make(map[string]int, len(l.Exercises))
	for i, e := range l.Exercises {
		if e.Kind != KindFillBlank && e.Kind != KindMatching {
			bad("exercises[%d]: unknown kind %q", i, e.Kind)
		}
		key := e.Kind + "\x00" + e.Prompt
		if prev, dup := seenExercise[key]; dup {
			bad("exercises[%d]: same kind and prompt as exercises[%d]", i, prev)
		} else {
			seenExercise[key] = i
		}
		if strings.TrimSpace(e.Prompt) == "" {
			bad("exercises[%d]: prompt is empty", i)
		}
		if len(e.Answer) == 0 {
			bad("exercises[%d]: needs at least one accepted answer", i)
		}
		if e.GrammarPoint != "" && !points[e.GrammarPoint] {
			bad("exercises[%d]: grammarPoint %q is not declared in this lesson", i, e.GrammarPoint)
		}
	}

	seenPassage := make(map[string]int, len(l.Passages))
	for i, p := range l.Passages {
		if strings.TrimSpace(p.Korean) == "" {
			bad("passages[%d]: korean is empty", i)
		} else if prev, dup := seenPassage[p.Korean]; dup {
			bad("passages[%d]: same korean text as passages[%d]", i, prev)
		} else {
			seenPassage[p.Korean] = i
		}
		if len(p.Questions) == 0 {
			bad("passages[%d]: needs at least one question", i)
		}
		for j, q := range p.Questions {
			if strings.TrimSpace(q.Q) == "" {
				bad("passages[%d].questions[%d]: q is empty", i, j)
			}
			if strings.TrimSpace(q.Reference) == "" {
				// The grader has nothing to compare against without one.
				bad("passages[%d].questions[%d]: reference is empty", i, j)
			}
		}
	}

	return errors.Join(errs...)
}
