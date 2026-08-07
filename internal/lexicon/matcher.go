// Package lexicon owns the answer to "what has been taught by position N".
//
// Every module in the app is scoped to that answer — vocab lists, multiple
// choice distractors, matching decoys, the passages offered, the words the LLM
// is allowed to use. There is one implementation here rather than one per
// caller, because the failure mode of a second copy is silent: a module that
// filters slightly differently teaches lesson 12 during lesson 3 and nothing
// reports it.
//
// The package has two halves, and they are not equally reliable:
//
//   - Selecting rows by position (lexicon.go) is exact. SQL either filters or
//     it does not.
//   - Deciding whether a piece of *text* stays inside the lexicon (this file)
//     is a heuristic, and cannot be otherwise. Korean is agglutinative:
//     먹었어요 will never substring-match its dictionary form 먹다. The matcher
//     is deliberately loose, and its output is a suspicion, not a verdict.
package lexicon

import (
	"strings"
	"unicode"
)

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

// Tokens splits text into runs of Hangul, discarding everything else. Latin
// letters, digits, punctuation and the blank markers in fill-in prompts are all
// irrelevant to a vocabulary check.
func Tokens(text string) []string {
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

// Matcher accumulates every form the reader could recognize so far.
//
// Two sets, because the safe prefix length differs by source. A verb stem is a
// legitimate prefix at any length — 먹다 teaches 먹-, which has to cover
// 먹었어요. A whole taught word is not: one-syllable nouns and particles prefix
// an enormous share of Korean, so letting 가 license 가족 would silence the
// check entirely. Stems earn prefix matching; short words only match exactly.
type Matcher struct {
	exact    map[string]bool
	prefixes map[string]bool
}

func NewMatcher() *Matcher {
	return &Matcher{exact: map[string]bool{}, prefixes: map[string]bool{}}
}

// Add teaches the matcher one word.
func (m *Matcher) Add(word string) {
	word = strings.TrimSpace(word)
	if word == "" {
		return
	}
	m.exact[word] = true

	runes := []rune(word)
	if len(runes) >= 2 {
		m.prefixes[word] = true
	}
	// Dictionary-form verbs and adjectives also teach their stem.
	if len(runes) > 1 && runes[len(runes)-1] == '다' {
		stem := string(runes[:len(runes)-1])
		m.exact[stem] = true
		m.prefixes[stem] = true
	}
}

// AddText teaches every Hangul token in a piece of text. Used for sources that
// are prose rather than dictionary entries, such as a grammar point's title.
func (m *Matcher) AddText(text string) {
	for _, tok := range Tokens(text) {
		m.Add(tok)
	}
}

// Knows reports whether a token plausibly derives from something already
// taught: an exact match, a taught prefix (an inflection), or a token that is
// itself a prefix of a taught form (a contraction of something longer).
func (m *Matcher) Knows(token string) bool {
	if m.exact[token] {
		return true
	}
	for form := range m.prefixes {
		if strings.HasPrefix(token, form) {
			return true
		}
		if len([]rune(form)) >= 2 && strings.HasPrefix(form, token) {
			return true
		}
	}
	return false
}

// Unknown returns the tokens in text that Knows rejects, in order of first
// appearance and without repeats. This is what both the content lint and the
// LLM output validator call: a warning list for the author in one case, a
// retry-or-tag decision in the other.
func (m *Matcher) Unknown(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tok := range Tokens(text) {
		if m.Knows(tok) || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}
