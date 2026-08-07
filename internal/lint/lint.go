// Package lint checks that a lesson only uses vocabulary the reader has
// already met.
//
// This is an authoring aid, not a rule. Korean is agglutinative: 먹었어요 will
// never substring-match its dictionary form 먹다, so any cheap checker produces
// false positives. The matching is deliberately loose — see lexicon.Matcher,
// which this shares with the LLM output validator — and every result is a
// warning. Nothing in this package fails a build or blocks content.
//
// The honest summary: it catches a word you plainly forgot to teach, and it
// will also flag inflections it cannot reason about. Treat the output as a
// prompt to look, never as a verdict.
package lint

import (
	"fmt"
	"sort"

	"github.com/dominicgodfrey/korLearn/internal/lexicon"
	"github.com/dominicgodfrey/korLearn/internal/seed"
)

// Finding is one Korean token with no plausible source in an earlier lesson.
type Finding struct {
	Book     string
	LessonNo int
	Position int
	// Where names the field the token came from, e.g. `exercises[2].prompt`.
	Where string
	Token string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s lesson %d (position %d): %s uses %q, not taught at or before this position",
		f.Book, f.LessonNo, f.Position, f.Where, f.Token)
}

// Run checks lessons in position order and returns every finding.
//
// Lessons must already be sorted by position, which seed.LoadDir guarantees.
// A lesson's own vocabulary is added before its exercises and passages are
// checked, so a lesson may freely use the words it teaches.
func Run(lessons []seed.Lesson) []Finding {
	m := lexicon.NewMatcher()
	var findings []Finding

	for _, l := range lessons {
		for _, v := range l.Vocab {
			m.Add(v.Korean)
		}
		for _, extra := range l.AllowExtra {
			m.Add(extra)
		}
		// Grammar point titles are explanatory text naming the pattern being
		// taught, so they are a source of forms rather than a place to check.
		for _, g := range l.GrammarPoints {
			m.AddText(g.Title)
		}

		check := func(where, text string) {
			for _, tok := range m.Unknown(text) {
				findings = append(findings, Finding{
					Book: l.Book, LessonNo: l.LessonNo, Position: l.Position,
					Where: where, Token: tok,
				})
			}
		}

		for i, g := range l.GrammarPoints {
			check(fmt.Sprintf("grammarPoints[%d].example.korean", i), g.Example.Korean)
		}
		for i, e := range l.Exercises {
			check(fmt.Sprintf("exercises[%d].prompt", i), e.Prompt)
			for j, a := range e.Answer {
				check(fmt.Sprintf("exercises[%d].answer[%d]", i, j), a)
			}
		}
		for i, p := range l.Passages {
			check(fmt.Sprintf("passages[%d].korean", i), p.Korean)
			for j, q := range p.Questions {
				check(fmt.Sprintf("passages[%d].questions[%d].q", i, j), q.Q)
			}
		}
	}

	return dedupe(findings)
}

// dedupe collapses repeats of the same token within one lesson. A word used
// eight times in a passage is one thing to fix, not eight.
func dedupe(findings []Finding) []Finding {
	seen := map[string]bool{}
	out := []Finding{}
	for _, f := range findings {
		key := fmt.Sprintf("%s\x00%d\x00%s", f.Book, f.LessonNo, f.Token)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].Where < out[j].Where
	})
	return out
}
