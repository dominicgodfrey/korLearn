# content/

One JSON file per lesson. Everything the app studies comes from here; the SQLite
database is derived from these files on every startup and holds no content of
its own.

Layout is by book, and file names are for humans only — ordering comes from the
`position` field, not the path:

```
content/
  ik-beginner1/L01.json
  ik-beginner2/L01.json
```

## Schema

```jsonc
{
  "schemaVersion": 2,
  "book": "Integrated Korean Beginner 1",
  "lesson": 1,
  "position": 1,                       // global order across ALL books, unique
  "title": "인사 — Greetings",
  "intro": "Greeting people and ...",  // English. Required. What this chapter is for.
  "allowExtra": ["은", "는"],           // tokens the lexicon lint should ignore
  "coverageExempt": [],                // vocab excused from the comprehension rule
  "vocab": [
    {
      "korean": "안녕하세요",
      "english": ["hello"],            // at least one gloss
      "pos": "expression",             // noun | verb | adjective | adverb | particle | expression
      "speechLevel": "polite",         // deferential | polite | intimate | plain | "" 
      "irregular": null,               // "ㅂ" | "ㄷ" | "ㅅ" | "르" | "으" | "ㄹ" | null
      "notes": ""
    }
  ],
  "grammarPoints": [
    {
      "id": "g1-copula",
      "title": "N이에요/예요",
      "explanation": "...",
      "example": {                     // exactly one, both halves required
        "korean": "저는 학생이에요.",
        "english": "I am a student."
      }
    }
  ],
  "exercises": [
    {
      "kind": "fill_blank",            // fill_blank | matching
      "grammarPoint": "g1-copula",     // must match a grammarPoints id in this file
      "prompt": "저는 학생___.",
      "answer": ["이에요"],             // every accepted answer
      "payload": {}                    // kind-specific extras, e.g. matching pairs
    }
  ],
  "passages": [
    {
      "korean": "...",
      "questions": [{ "q": "...", "reference": "..." }]
    }
  ]
}
```

## Rules the parser enforces

Parsing is strict, because these files are hand-written and a silently ignored
field means content that never appears in study:

- Unknown fields are rejected. (Caveat: JSON field matching is
  case-insensitive, so `speechlevel` still binds to `speechLevel`.)
- `position` must be unique across every file in every book.
- Vocab is unique per lesson by `korean`; exercises by `(kind, prompt)`;
  passages by `korean`. Duplicates would overwrite each other on load.
- `grammarPoint` on an exercise must name a `grammarPoints` id in the same file.
- `intro` is required and non-empty: it is what the chapter page opens with.
- Every grammar point needs exactly one `example`, with both `korean` and
  `english`. The field is an object, not a list — the grammar module shows one
  example and then starts drilling.
- Every `coverageExempt` entry must be a `korean` word this lesson teaches, so
  an exemption cannot outlive the word it was written for.
- Every passage question needs a `reference` answer — it is what the grader
  compares against, and it is never shown before answering.
- Text is normalized to Unicode NFC on load, so decomposed Hangul pasted from
  another tool is safe.

All problems in a file are reported at once, not one per run.

## Editing content

Restart the binary. Rows are matched on their natural keys and updated in place,
so ids — and the attempt history pointing at them — survive edits. Content
deleted from a file is marked retired rather than removed: it disappears from
study, but its history stays. Putting it back revives the original row.

Changing a word's `korean` text counts as deleting one entry and adding another,
since that field is its identity. Fix typos early.
