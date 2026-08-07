package lexicon

import (
	"strings"
	"testing"
)

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
		got := Tokens(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("Tokens(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// The whole reason the matcher is stem-prefix rather than exact: teaching the
// dictionary form must cover its inflections.
func TestKnowsInflections(t *testing.T) {
	m := NewMatcher()
	m.Add("먹다")
	for _, tok := range []string{"먹다", "먹", "먹었어요", "먹어요"} {
		if !m.Knows(tok) {
			t.Errorf("Knows(%q) = false, want true — 먹다 teaches its stem", tok)
		}
	}
}

// A single syllable is a prefix of a huge share of Korean words; treating it as
// a licence would silence every check built on this.
func TestSingleSyllableDoesNotLicenseEverything(t *testing.T) {
	m := NewMatcher()
	m.Add("가")
	if m.Knows("가족") {
		t.Error("one-syllable 가 licensed 가족")
	}
}

func TestUnknownReportsEachTokenOnce(t *testing.T) {
	m := NewMatcher()
	m.Add("학생")

	got := m.Unknown("학생과 선생님, 선생님")
	// 학생과 is an inflection of a taught word; 선생님 is not taught, twice.
	if len(got) != 1 || got[0] != "선생님" {
		t.Errorf("Unknown = %v, want [선생님]", got)
	}
	if len(m.Unknown("학생")) != 0 {
		t.Error("a taught word was reported unknown")
	}
}
