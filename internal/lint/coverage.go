package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dominicgodfrey/korLearn/internal/lexicon"
	"github.com/dominicgodfrey/korLearn/internal/seed"
)

// Gap is one thing a chapter teaches but never asks about.
//
// Comprehension is the chapter's integration test: a word introduced in the
// vocab module and never met again is a word that was shown, not learned. This
// check is why that rule is a rule rather than an intention.
type Gap struct {
	Book     string
	LessonNo int
	Position int
	// Kind is "vocab" or "grammar".
	Kind string
	Item string
}

func (g Gap) String() string {
	return fmt.Sprintf("%s lesson %d (position %d): %s %q is never used in a comprehension passage or question",
		g.Book, g.LessonNo, g.Position, g.Kind, g.Item)
}

// Coverage reports everything a lesson teaches that its passages never
// exercise. Unlike Run, these are errors: the check is exact enough to act on,
// and a chapter that fails it is incomplete rather than suspicious.
//
// Vocabulary counts as covered when it appears in a passage's text, a question,
// or a reference answer. Grammar counts as covered when a question names it in
// grammarPoints. That is authoring intent rather than proof — nothing can force
// an answer to use a pattern — but an unnamed point is a point nobody even
// tried to ask about, which is what this is for.
func Coverage(lessons []seed.Lesson) []Gap {
	var gaps []Gap

	for _, l := range lessons {
		// Everything Korean the reader is asked to read or write in this
		// chapter's comprehension module, as tokens.
		var seen []string
		askedPoints := map[string]bool{}
		for _, p := range l.Passages {
			seen = append(seen, lexicon.Tokens(p.Korean)...)
			for _, q := range p.Questions {
				seen = append(seen, lexicon.Tokens(q.Q)...)
				seen = append(seen, lexicon.Tokens(q.Reference)...)
				for _, id := range q.GrammarPoints {
					askedPoints[id] = true
				}
			}
		}

		exempt := make(map[string]bool, len(l.CoverageExempt))
		for _, w := range l.CoverageExempt {
			exempt[w] = true
		}

		for _, v := range l.Vocab {
			if exempt[v.Korean] || mentioned(v.Korean, seen) {
				continue
			}
			gaps = append(gaps, Gap{
				Book: l.Book, LessonNo: l.LessonNo, Position: l.Position,
				Kind: "vocab", Item: v.Korean,
			})
		}
		for _, g := range l.GrammarPoints {
			if askedPoints[g.ID] {
				continue
			}
			gaps = append(gaps, Gap{
				Book: l.Book, LessonNo: l.LessonNo, Position: l.Position,
				Kind: "grammar", Item: g.ID,
			})
		}
	}

	sort.SliceStable(gaps, func(i, j int) bool {
		if gaps[i].Position != gaps[j].Position {
			return gaps[i].Position < gaps[j].Position
		}
		return gaps[i].Kind < gaps[j].Kind
	})
	return gaps
}

// mentioned reports whether any token is a use of word.
//
// This is looser than lexicon.Matcher on purpose, and the asymmetry is the
// point: that matcher guards against over-matching, because a false match there
// hides a word taught too early. Here a false *miss* blocks a chapter that is
// actually complete — 물 appearing as 물을 is a use of 물 — so short words get
// prefix matching that Matcher deliberately withholds. Over-matching here costs
// a coverage claim that a human would have caught while reading the passage.
func mentioned(word string, tokens []string) bool {
	forms := []string{word}
	// A dictionary-form verb is almost never written in that form in a passage.
	if runes := []rune(word); len(runes) > 1 && runes[len(runes)-1] == '다' {
		forms = append(forms, string(runes[:len(runes)-1]))
	}
	for _, tok := range tokens {
		for _, f := range forms {
			if strings.HasPrefix(tok, f) {
				return true
			}
		}
	}
	return false
}
