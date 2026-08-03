// Package lint checks that a lesson only uses vocabulary the reader has
// already met.
//
// This is an authoring aid, not a rule. Korean is agglutinative: 먹었어요 will
// never substring-match its dictionary form 먹다, so any cheap checker produces
// false positives. The matcher here is deliberately loose — stem-prefix, with a
// per-lesson allowExtra escape hatch — and every result is a warning. Nothing
// in this package fails a build or blocks content.
//
// The honest summary: it catches a word you plainly forgot to teach, and it
// will also flag inflections it cannot reason about. Treat the output as a
// prompt to look, never as a verdict.
package lint

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

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

// hangul reports whether r is Hangul: a composed syllable, or a standalone or
// conjoining jamo.
func hangul(r rune) bool {
	switch {
	case r >= 0xAC00 && r <= 0xD7A3: // syllables
		return true
	case r >= 0x1100 && r <= 0x11FF: // conjoining jamo
		return true
	case r >= 0x3130 && r <= 0x318F: // compatibility jamo
		return true
	}
	return false
}

// tokens splits text into runs of Hangul, discarding everything else. Latin
// letters, digits, punctuation and the blank markers in fill-in prompts are all
// irrelevant to a vocabulary check.
func tokens(text string) []string {
	var (
		out     []string
		current strings.Builder
	)
	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
		}
	}
	for _, r := range text {
		switch {
		case hangul(r):
			current.WriteRune(r)
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) ||
			unicode.IsDigit(r) || unicode.IsLetter(r):
			flush()
		default:
			flush()
		}
	}
	flush()
	return out
}

// lexicon accumulates every form the reader could recognize so far.
//
// Two sets, because the safe prefix length differs by source. A verb stem is a
// legitimate prefix at any length — 먹다 teaches 먹-, which has to cover
// 먹었어요. A whole taught word is not: one-syllable nouns and particles prefix
// an enormous share of Korean, so letting 가 license 가족 would silence the
// check entirely. Stems earn prefix matching; short words only match exactly.
type lexicon struct {
	exact    map[string]bool
	prefixes map[string]bool
}

func newLexicon() *lexicon {
	return &lexicon{exact: map[string]bool{}, prefixes: map[string]bool{}}
}

func (l *lexicon) add(word string) {
	word = strings.TrimSpace(word)
	if word == "" {
		return
	}
	l.exact[word] = true

	runes := []rune(word)
	if len(runes) >= 2 {
		l.prefixes[word] = true
	}
	// Dictionary-form verbs and adjectives also teach their stem.
	if len(runes) > 1 && runes[len(runes)-1] == '다' {
		stem := string(runes[:len(runes)-1])
		l.exact[stem] = true
		l.prefixes[stem] = true
	}
}

// knows reports whether a token plausibly derives from something already taught:
// an exact match, a taught prefix (an inflection), or a token that is itself a
// prefix of a taught form (a contraction of something longer).
func (l *lexicon) knows(token string) bool {
	if l.exact[token] {
		return true
	}
	for form := range l.prefixes {
		if strings.HasPrefix(token, form) {
			return true
		}
		if len([]rune(form)) >= 2 && strings.HasPrefix(form, token) {
			return true
		}
	}
	return false
}

// Run checks lessons in position order and returns every finding.
//
// Lessons must already be sorted by position, which seed.LoadDir guarantees.
// A lesson's own vocabulary is added before its exercises and passages are
// checked, so a lesson may freely use the words it teaches.
func Run(lessons []seed.Lesson) []Finding {
	lex := newLexicon()
	var findings []Finding

	for _, l := range lessons {
		for _, v := range l.Vocab {
			lex.add(v.Korean)
		}
		for _, extra := range l.AllowExtra {
			lex.add(extra)
		}
		// Grammar point titles are explanatory text naming the pattern being
		// taught, so they are a source of forms rather than a place to check.
		for _, g := range l.GrammarPoints {
			for _, tok := range tokens(g.Title) {
				lex.add(tok)
			}
		}

		check := func(where, text string) {
			for _, tok := range tokens(text) {
				if !lex.knows(tok) {
					findings = append(findings, Finding{
						Book: l.Book, LessonNo: l.LessonNo, Position: l.Position,
						Where: where, Token: tok,
					})
				}
			}
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
