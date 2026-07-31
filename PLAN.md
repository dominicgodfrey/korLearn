# korLearn — Korean study tool (Go + React/TS)

## Context

Self-study Korean using Integrated Korean (Beginner 1 & 2, then Intermediate 1). Existing tools
don't do what's needed: chapter-scoped drilling that respects textbook order, grammar practice
that grades real typed Korean, and speaking practice with feedback. `C:\Users\dsolg\Downloads\riplet`
already solves flashcard flow well (two-stage MC→typed mastery); korLearn is a greenfield rebuild
in TS with a Go backend, persistent history, and local AI for grading and conversation.

Outcome: a localhost app where you pick a lesson, drill vocab with audio, do grammar exercises
graded by a local LLM, hold spoken conversations constrained to vocab you've actually learned,
and see honest long-term progress.

## Decisions locked during design

| Area | Decision |
|---|---|
| Content | Hand-authored seed files, one per lesson, committed to repo |
| Chapter order | Single global `position` integer across all books; "unlocked" = `position <= current` |
| Deployment | Single-user localhost. SQLite, no auth, no user table |
| Korean input | Flashcards: flip mode **and** riplet's two-stage MC→typed. Matching: click. Fill-in-blank + passages: typed Hangul (OS IME) |
| Exercises | Authored core in seed files; LLM generates *extra* drills on demand |
| Session state | Client reducer (riplet's design, rewritten in TS). Server is an append-only event log |
| Review | User always picks what to study. Weakness scoring feeds a dashboard + an on-demand "trouble spots" playlist — never silently reorders a session |
| Free-text grading | LLM returns structured rubric JSON + corrected sentence. Feedback in English, corrections in Korean. Manual override retained |
| Grades | Per chapter: **average score across sessions over time** + **best score ever** |
| TTS | `GET /api/tts` with disk cache, Kokoro sidecar |
| STT | faster-whisper, push-to-talk turns, feedback after each turn |
| LLM | `exaone3.5:7.8b` (bilingual EN/KO, LG AI Research) via Ollama, behind a golden eval set |
| Frontend | Greenfield React + TS; riplet read as reference only |

## Environment facts (verified on this machine)

- **RTX 5060 Ti, 16GB VRAM**; Ryzen 7 7700X; 31GB RAM. Enough to hold EXAONE (~5GB Q4) +
  Kokoro (~2GB) + Whisper (~1–3GB) resident at once — Phase 3 won't thrash.
- Ollama 0.30.10 installed (`qwen3:8b`, `qwen2.5vl:7b` present). `exaone3.5:7.8b` needs pulling.
- **Go is not installed** — first setup step.
- **Only Python 3.14 is installed.** torch / kokoro / faster-whisper do not ship 3.14 wheels.
  The sidecar needs its own **Python 3.11 or 3.12 venv**. Do not fight 3.14.
- **ffmpeg is not installed** — faster-whisper needs it unless the browser records WAV directly.
- Piper was ruled out: **no Korean voice exists** for it. Kokoro supports Korean natively.

## Architecture

```
korLearn/
├── cmd/korlearn/main.go        # single binary: serves API + built React assets
├── internal/
│   ├── seed/                   # seed file parsing, load-on-startup, lexicon lint
│   ├── store/                  # SQLite (modernc.org/sqlite — pure Go, no cgo on Windows)
│   ├── api/                    # net/http handlers (Go 1.22 routing patterns)
│   ├── tts/                    # cache-through synth
│   ├── llm/                    # Ollama client, prompts, rubric parsing
│   └── stats/                  # SQL derivations: grades, weakness, calendar
├── content/
│   └── ik-beginner1/L01.json   # one file per lesson
├── sidecar/                    # Python 3.11 venv: Kokoro TTS + faster-whisper
├── web/                        # Vite + React + TS + React Router + TanStack Query + Tailwind
└── testdata/grading_golden.json
```

**Use `modernc.org/sqlite`, not `mattn/go-sqlite3`** — the latter needs cgo and a C toolchain,
which is a recurring Windows pain. Migrations are plain `.sql` files under `//go:embed`, applied
in order. No migration library.

### Data model

```
chapters   (id, book, lesson_no, position UNIQUE, title, deadline_at NULL)
vocab      (id, chapter_id, korean, english_json, pos, speech_level, irregular, notes)
grammar_points (id, chapter_id, title, explanation)
exercises  (id, chapter_id, grammar_point_id NULL, kind, prompt, payload_json, answer_json)
passages   (id, chapter_id, korean_text, questions_json)
sessions   (id, chapter_id NULL, mode, started_at, ended_at NULL, score REAL NULL)
attempts   (id, session_id, item_type, item_id, stage, correct, original_correct,
            user_answer, rubric_json NULL, elapsed_ms, created_at)
```

`attempts` is **append-only and immutable**. Every derived number — chapter grade, best score,
weakness ranking, the calendar, "what did I struggle with" — is SQL over this one table. Nothing
else needs to stay in sync, and it means a real SRS scheduler can be retrofitted later from full
history if you ever want one.

`chapter_id NULL` on sessions covers cross-chapter comprehensive quizzes.

### Seed file shape

JSON (zero deps, same as riplet). One file per lesson. Carries POS / speech level / irregular
flags **from day one** so Intermediate 1 doesn't force a schema migration later.

```jsonc
{
  "schemaVersion": 1,
  "book": "Integrated Korean Beginner 1",
  "lesson": 1, "position": 1, "title": "인사 — Greetings",
  "vocab": [{ "korean": "안녕하세요", "english": ["hello"], "pos": "expression",
              "speechLevel": "polite", "irregular": null, "notes": "" }],
  "grammarPoints": [{ "id": "g1-copula", "title": "N이에요/예요", "explanation": "..." }],
  "exercises": [{ "kind": "fill_blank", "grammarPoint": "g1-copula",
                  "prompt": "저는 학생___.", "answer": ["이에요"] }],
  "passages": [{ "korean": "...", "questions": [{ "q": "...", "reference": "..." }] }]
}
```

If hand-authoring long passages in JSON gets painful, swapping to YAML is a one-line parser change.

### Lexicon gating — honest caveat

The "only prior vocab" rule ships as a **lint, not a hard runtime constraint**:
`korlearn lint` scans seed content and flags Korean tokens absent from chapters at lower positions.

Korean is agglutinative — `먹었어요` will not substring-match `먹다`. A naive checker produces
constant false positives. Therefore: the lint **warns, never blocks**, each seed file may carry an
`allowExtra` list for particles/inflections, and the initial matcher is stem-prefix based. Treat it
as an authoring aid. For the *LLM* paths (extra drills, conversation) the unlocked lexicon is
injected into the prompt and the output is lint-checked, with out-of-vocabulary words flagged in
the UI rather than rejected — a 7.8B model cannot be trusted to obey a closed vocabulary, and
pretending otherwise would silently drop good content.

## Phase 1 — Vocab

**Milestone 1 (do this first): vertical slice through every layer, one real lesson.**

Seed IK Beginner 1 Lesson 1 → Go loads it into SQLite on startup → React renders flip flashcards
and the two-stage quiz → audio button hits `/api/tts` and caches to disk → each completed card
POSTs an attempt → session ends with a score visible on the dashboard.

This surfaces schema mistakes and Kokoro sidecar problems in week one rather than week six. After
it works, lessons 2–16 are pure content authoring.

Then, in order:
1. Flashcards: flip mode (KO→EN and EN→KO) + two-stage mastery mode (MC → typed, fail written →
   back to MC, checkpoint every 10, 50-step undo, self-grade fallback with override).
   Rewrite riplet's `session.js` / `distractors.js` / `shuffle.js` / `grading.js` in TS.
   Answer normalization: Unicode **NFC**, trim, collapse whitespace, strip terminal punctuation.
2. Session resume: snapshot PUT on advance, restored on reload.
3. Pre-warm script: synthesize every vocab word in a chapter into the TTS cache.
4. Dashboard: per-chapter average-over-time + best score, trend chart, weak-items table.
5. Calendar: contribution-style heatmap of practice days + per-day detail.
6. Optional per-chapter deadlines — **soft**: show on-track/behind on the dashboard and calendar,
   never block or penalize.
7. Comprehensive cross-chapter quiz mode (`chapter_id NULL` sessions).
8. "Trouble spots" playlist: generated from weakness ranking, launched only when you choose it.

## Phase 2 — Grammar

1. Matching (click-to-pair), fill-in-blank (typed Hangul, deterministic grading with normalization
   and multiple accepted answers).
2. Passage comprehension: typed full-sentence Korean answers → `POST /api/grade` → Ollama →
   strict JSON: `{comprehension: bool, grammarIssues[], vocabIssues[], corrected: string, score: 0-3}`.
   Store the rubric on the attempt so the dashboard can aggregate error categories
   ("you keep missing 을/를"). Keep the manual override button.
3. **`testdata/grading_golden.json`**: ~30 hand-written Korean answers with expected verdicts,
   run by `go test`. This is what makes the model a config value instead of a guess, and it catches
   the real failure mode — malformed JSON — before it reaches the UI. Retry-with-repair on parse failure.
4. LLM extra-drill generation from the unlocked lexicon, lint-checked, clearly labeled as generated
   and stored separately from authored content so replays stay deterministic.

## Phase 3 — Conversation

Push-to-talk: hold key → record → release → faster-whisper → EXAONE replies in Korean (system prompt
carries the unlocked lexicon + current grammar points) → Kokoro speaks it via the same `/api/tts`
cache. Per-turn feedback uses the Phase 2 rubric. Full transcript saved and replayable.

**Set expectations honestly: Whisper is not a pronunciation scorer.** It is trained to recover
intended words, so it will transcribe sloppy Korean as correct Korean. It tells you *what* you said,
not *how well*. Real pronunciation scoring needs forced alignment (MFA or similar) and is explicitly
out of scope. Record WAV in-browser via AudioWorklet to avoid needing ffmpeg for WebM/Opus decode.

No VAD, no barge-in, no echo cancellation — those are where projects like this stall.

## Verification

- `go test ./...` — seed parsing, answer normalization (NFC edge cases), stats SQL, grading golden set.
- `korlearn lint content/` — reports out-of-sequence vocabulary per chapter.
- Milestone 1 end-to-end by hand: start the binary, open localhost, complete Lesson 1's quiz,
  confirm (a) audio plays and a `.wav` lands in the cache dir, (b) `attempts` rows exist in the
  SQLite file, (c) reloading mid-session resumes at the same card, (d) the dashboard score matches
  what the session reported.
- Phase 2: run the golden set against `exaone3.5:7.8b`, record the pass rate, then re-run after any
  prompt change — the number is the regression signal.
- Phase 3: measure round-trip latency per turn (release key → audio playing). If it exceeds ~4s,
  drop the Whisper model size before touching anything else.

## Setup prerequisites

```bash
winget install GoLang.Go
ollama pull exaone3.5:7.8b
```

Plus a Python **3.11/3.12** venv under `sidecar/` for Kokoro and faster-whisper — **not** the
installed 3.14.
