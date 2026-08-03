package seed

import (
	"os"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	data, err := os.ReadFile("testdata/valid/L02.json")
	if err != nil {
		t.Fatal(err)
	}
	l, err := Parse("L02.json", data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if l.Position != 2 || l.LessonNo != 2 {
		t.Errorf("position/lesson = %d/%d, want 2/2", l.Position, l.LessonNo)
	}
	if got := len(l.Vocab); got != 1 {
		t.Fatalf("len(vocab) = %d, want 1", got)
	}
	v := l.Vocab[0]
	if v.Irregular == nil || *v.Irregular != "ㅂ" {
		t.Errorf("irregular = %v, want ㅂ", v.Irregular)
	}
	if v.SpeechLevel != "polite" {
		t.Errorf("speechLevel = %q, want polite", v.SpeechLevel)
	}
	if len(l.AllowExtra) != 2 {
		t.Errorf("allowExtra = %v, want 2 entries", l.AllowExtra)
	}
	if len(l.Exercises) != 2 || l.Exercises[1].Kind != KindMatching {
		t.Errorf("exercises = %+v", l.Exercises)
	}
	if got := string(l.Exercises[1].Payload); !strings.Contains(got, "pairs") {
		t.Errorf("payload not preserved verbatim: %s", got)
	}
	if len(l.Passages) != 1 || len(l.Passages[0].Questions) != 1 {
		t.Errorf("passages = %+v", l.Passages)
	}
}

// A nil Irregular must survive as nil rather than becoming "": the distinction
// between "regular" and "unspecified" is what the conjugation drills key on.
func TestParseIrregularOmitted(t *testing.T) {
	data, err := os.ReadFile("testdata/valid/L01.json")
	if err != nil {
		t.Fatal(err)
	}
	l, err := Parse("L01.json", data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, v := range l.Vocab {
		if v.Irregular != nil {
			t.Errorf("%s: irregular = %q, want nil", v.Korean, *v.Irregular)
		}
	}
}

// Decomposed Hangul must be composed at parse time. 한 written as jamo compares
// unequal to 한 written as a syllable, which would produce duplicate vocab rows
// that no typed answer can ever match.
func TestParseNormalizesToNFC(t *testing.T) {
	const decomposed = "\u1112\u1161\u11AB" // ㅎ + ㅏ + ㄴ
	const composed = "\uD55C"               // 한

	src := `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
		"vocab":[{"korean":"` + decomposed + `","english":["one"],"pos":"noun","irregular":null}]}`

	l, err := Parse("nfc.json", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := l.Vocab[0].Korean; got != composed {
		t.Errorf("korean = %q (% x), want %q (% x)", got, got, composed, composed)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string // substring the error must mention
	}{
		{
			name: "malformed json",
			json: `{"schemaVersion":1,`,
			want: "unexpected EOF",
		},
		{
			name: "future schema version",
			json: `{"schemaVersion":2,"book":"b","lesson":1,"position":1,"title":"t"}`,
			want: "schemaVersion 2",
		},
		{
			// Note: encoding/json matches field names case-insensitively, so a
			// wrong-case key ("speechlevel") still binds correctly and is not
			// an error. Only a genuinely different name is caught.
			name: "misspelled field",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
				"vocab":[{"korean":"가","english":["a"],"pos":"noun","speach_level":"polite"}]}`,
			want: "speach_level",
		},
		{
			name: "missing position",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"title":"t"}`,
			want: "position must be >= 1",
		},
		{
			name: "duplicate vocab",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
				"vocab":[{"korean":"가","english":["a"],"pos":"noun"},
				         {"korean":"가","english":["b"],"pos":"noun"}]}`,
			want: "duplicates vocab[0]",
		},
		{
			name: "vocab without gloss",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
				"vocab":[{"korean":"가","english":[],"pos":"noun"}]}`,
			want: "english gloss",
		},
		{
			name: "unknown exercise kind",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
				"exercises":[{"kind":"dictation","prompt":"p","answer":["a"]}]}`,
			want: `unknown kind "dictation"`,
		},
		{
			name: "exercise references undeclared grammar point",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
				"exercises":[{"kind":"fill_blank","grammarPoint":"nope","prompt":"p","answer":["a"]}]}`,
			want: "not declared",
		},
		{
			name: "passage question without reference",
			json: `{"schemaVersion":1,"book":"b","lesson":1,"position":1,"title":"t",
				"passages":[{"korean":"k","questions":[{"q":"q","reference":""}]}]}`,
			want: "reference is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("x.json", []byte(tt.json))
			if err == nil {
				t.Fatal("Parse succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v\nwant substring %q", err, tt.want)
			}
		})
	}
}

// Every problem in a file should surface in one run, not one per fix-and-rerun.
func TestValidateReportsAllErrors(t *testing.T) {
	const src = `{"schemaVersion":1,"book":"","lesson":0,"position":0,"title":""}`
	_, err := Parse("x.json", []byte(src))
	if err == nil {
		t.Fatal("Parse succeeded, want error")
	}
	for _, want := range []string{"book is empty", "title is empty", "lesson must be", "position must be"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}

func TestLoadDir(t *testing.T) {
	lessons, err := LoadDir(os.DirFS("testdata/valid"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(lessons) != 2 {
		t.Fatalf("loaded %d lessons, want 2", len(lessons))
	}
	if lessons[0].Position != 1 || lessons[1].Position != 2 {
		t.Errorf("not sorted by position: %d, %d", lessons[0].Position, lessons[1].Position)
	}
}

func TestLoadDirRejectsDuplicatePosition(t *testing.T) {
	_, err := LoadDir(os.DirFS("testdata/dup_position"))
	if err == nil {
		t.Fatal("LoadDir succeeded, want error")
	}
	if !strings.Contains(err.Error(), "position 7") {
		t.Errorf("error = %v, want it to name position 7", err)
	}
}
