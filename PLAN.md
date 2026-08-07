# korLearn — Korean study tool (Go + React/TS)

## Context

Self-study Korean using Integrated Korean (Beginner 1 & 2, then Intermediate 1). Existing tools
don't do what's needed: chapter-scoped drilling that respects textbook order, grammar practice
that grades real typed Korean, and speaking practice with feedback. `C:\Users\dsolg\Downloads\riplet`
already solves flashcard flow well (two-stage MC→typed mastery); korLearn is a greenfield rebuild
in TS with a Go backend, persistent history, and local AI for grading and conversation.

Outcome: a localhost app where you pick a lesson and walk a fixed path through it — what the
chapter is for, its vocabulary, its grammar, a passage that puts both to work, then a conversation
using only what you have been taught — with every stop on that path also reachable on its own, and
honest long-term progress behind it.

## Decisions locked during design

| Area | Decision |
|---|---|
| Content | Hand-authored seed files, one per lesson, committed to repo |
| Chapter order | Single global `position` integer across all books; "unlocked" = `position <= current` |
| Progression | Strictly linear. Selecting chapter N *asserts* chapters 1..N−1 are known — there is no per-chapter completion flag to keep in sync |
| Deployment | Single-user localhost. SQLite, no auth, no user table |
| Learning flow | Five modules in a fixed recommended order: intro → vocab → grammar → comprehension → conversation. The order is a suggestion the UI chains for you, not a lock |
| Navigation | Every module is directly launchable from the chapter page at any time. Nothing is gated behind finishing the one before it |
| Lexicon scope | Every module, in every chapter, draws only on vocab and grammar at `position <= current`. Enforced by a validator, not by trusting the prompt |
| Korean input | Flashcards: flip only (direction chosen by the user). Vocab learn: riplet's two-stage MC→typed. Matching: click. Fill-in-blank + passages + conversation: typed Hangul (OS IME) |
| Exercises | Authored core in seed files; LLM generates *extra* drills on demand |
| Session state | Client reducer (riplet's design, rewritten in TS). Server is an append-only event log |
| Review | User always picks what to study. Weakness scoring feeds a dashboard + an on-demand "trouble spots" playlist — never silently reorders a session |
| Free-text grading | LLM returns structured rubric JSON + corrected sentence. Feedback in English, corrections in Korean. Manual override retained |
| Grades | Per chapter: **average score across sessions over time** + **best score ever** |
| TTS | `GET /api/tts` with disk cache, MeloTTS sidecar (**not** Kokoro — see below) |
| STT | faster-whisper, push-to-talk turns, feedback after each turn |
| LLM | `exaone3.5:7.8b` (bilingual EN/KO, LG AI Research) via Ollama, behind a golden eval set |
| Frontend | Greenfield React + TS; riplet read as reference only |

## Learning flow

A chapter is five modules. The chapter page lists all five and can launch any of them directly;
finishing one offers "next" and walks you down the list. That is the only difference between the
guided path and free navigation — there is no unlock logic between modules, and no module is a
prerequisite for another.

```
Chapter page ──┬─ 0. Intro          (read, English)
               ├─ 1. Vocab          table → flashcards → learn
               ├─ 2. Grammar        per point: example → matching → fill-in-blank
               ├─ 3. Comprehension  passage → typed full-sentence answers
               └─ 4. Conversation   live back-and-forth
                     └── each module ends with → [next module] or → [chapter page]
```

**0. Intro.** A few paragraphs of English naming what the chapter is for — "introductions and
school vocabulary", "transportation and giving directions", "sino-Korean numbers and counters" —
and what you should be able to do by the end. English on purpose: this is the one place the goal
matters more than the immersion. Authored as `intro` in the seed file. Read-only, records no
attempts, is not scored.

**1. Vocab.** Three stages in one module:
- *Table.* Every word in the chapter as `English | Korean`, with a TTS button per row. Reference,
  not a drill — no attempts recorded.
- *Flashcards.* Flip only. Before starting you pick which side shows first (KO→EN or EN→KO); the
  choice applies to the whole deck. Self-graded, `stage = 'flip'`.
- *Learn.* riplet's two-stage mastery loop, unchanged: MC until a word is answerable, then typed;
  a failed typed answer drops the word back to MC; checkpoint every 10; 50-step undo; self-grade
  fallback with override. `stage = 'mc'` then `'typed'`.

**2. Grammar.** Iterates the chapter's grammar points. Each point shows its explanation and
**exactly one** worked example, then drills that point: matching first (click-to-pair, cheap
recognition), then fill-in-blank (typed Hangul, deterministic grading). One example is a
deliberate constraint — the seed schema carries a single `example` per point, so a chapter cannot
quietly turn into a grammar textbook.

**3. Comprehension.** A passage from the chapter plus free-response questions answered in full
Korean sentences, graded by the LLM on grammar and spelling as well as understanding. **Coverage
rule: every vocab word and every grammar point in the chapter must appear at least once across
the chapter's passages and their questions.** This is the chapter's integration test, and it is
checked mechanically — see *Coverage lint* below.

**4. Conversation.** Live turn-taking constrained to the same lexicon, seeded with the chapter's
topic and grammar points so the practice lands on what was just studied rather than drifting to
whatever the model finds easy.

### Sessions and progress

Each module run is one row in `sessions` with `mode` in
`vocab_flip | vocab_learn | grammar | comprehension | conversation`. The guided chain is client-side
routing between them, not a server-side workflow: there is no "flow" entity, no resumable
multi-module state, and a chapter's grade stays what it already is — an aggregate over `attempts`.

"Which modules have I done for this chapter" is a query, not a column:
`SELECT mode, max(ended_at) FROM sessions WHERE chapter_id = ? AND ended_at IS NOT NULL GROUP BY mode`.
The chapter page uses it for checkmarks and to decide which module "continue" points at. The intro
is exempt — reading it records nothing, so it is never marked done.

## Environment facts (verified on this machine)

- **RTX 5060 Ti, 16GB VRAM**; Ryzen 7 7700X; 31GB RAM. Enough to hold EXAONE (~5GB Q4) +
  MeloTTS (~1GB) + Whisper (~1–3GB) resident at once — Phase 3 won't thrash.
  The 5060 Ti is **Blackwell (sm_120)**: torch must come from the **cu128** index or newer.
  Verified working: `torch 2.11.0+cu128`, `torch.cuda.is_available() == True`.
- Ollama 0.30.10 installed (`qwen2.5vl:7b` present; `qwen3:8b` is gone).
  `exaone3.5:7.8b` still needs pulling — not required before Phase 2.
- **Go 1.26.5 installed** (2026-08-03, `winget install GoLang.Go`).
- Node v24.16.0 is installed, so Vite needs no extra setup.
- The sidecar venv must be **Python 3.11**, not 3.12: MeloTTS pins
  `transformers==4.27.4`, whose `tokenizers` has no wheel past 3.11 and needs Rust to
  build from source. `uv` fetches 3.11 on demand. Still do not fight 3.14.
- **ffmpeg is not installed** — faster-whisper needs it unless the browser records WAV directly.
- **C: is at 93% (66GB free)** — relevant when stacking CUDA wheels and models.

### TTS engine — corrected 2026-08-03

**Kokoro has no Korean voices.** Verified against the model repo: 54 voices across nine
languages (`af am bf bm ef em ff hf hm if im jf jm pf pm zf zm`), none Korean, and no Korean
code in `KPipeline.LANG_CODES`. Its frontend library `misaki` *does* ship Korean G2P
(`misaki.ko` imports fine), which is the likely source of the original mistake — but the
model was never trained on Korean. Piper remains ruled out for the same reason it always was.

`facebook/mms-tts-kor` was measured as a fallback and works (0.11s per word on the 5060 Ti),
but it synthesizes from **uroman romanization** rather than Hangul, at 16kHz. Korean
phonology is therefore never applied, which is the wrong trade for an app about pronunciation.

**MeloTTS** is the engine: a real Korean frontend (`g2pkk`) that resolves 받침 and consonant
assimilation (학생 → hakssaeng, 종로 → jongno), MIT licensed, 44.1kHz, runs locally.

The Go side is engine-agnostic — it speaks `POST /synth` to a sidecar and caches the wav —
so replacing the engine again costs one Python file.

## Architecture

```
korLearn/
├── cmd/korlearn/main.go        # single binary: serves API + built React assets
├── internal/
│   ├── seed/                   # seed file parsing, load-on-startup
│   ├── lint/                   # lexicon warnings + comprehension coverage check
│   ├── lexicon/                # the unlocked set: one owner, every caller agrees
│   ├── store/                  # SQLite (modernc.org/sqlite — pure Go, no cgo on Windows)
│   ├── api/                    # net/http handlers (Go 1.22 routing patterns)
│   ├── tts/                    # cache-through synth
│   ├── llm/                    # Ollama client, prompts, rubric parsing
│   └── stats/                  # SQL derivations: grades, weakness, calendar
├── content/
│   └── ik-beginner1/L01.json   # one file per lesson
├── sidecar/                    # Python 3.11 venv: MeloTTS + faster-whisper
├── web/                        # Vite + React + TS + React Router + TanStack Query + Tailwind
└── testdata/grading_golden.json
```

**Use `modernc.org/sqlite`, not `mattn/go-sqlite3`** — the latter needs cgo and a C toolchain,
which is a recurring Windows pain. Migrations are plain `.sql` files under `//go:embed`, applied
in order. No migration library.

### Data model

```
chapters   (id, book, lesson_no, position UNIQUE, title, intro, deadline_at NULL)
vocab      (id, chapter_id, korean, english_json, pos, speech_level, irregular, notes)
grammar_points (id, chapter_id, slug, title, explanation, example_korean, example_english)
exercises  (id, chapter_id, grammar_point_id NULL, kind, prompt, payload_json, answer_json)
passages   (id, chapter_id, korean_text, questions_json)
sessions   (id, chapter_id NULL, mode, started_at, ended_at NULL, score REAL NULL)
attempts   (id, session_id, item_type, item_id, stage, correct, original_correct,
            user_answer, rubric_json NULL, elapsed_ms, created_at)
```

`chapters.intro` and the two `grammar_points.example_*` columns arrive in migration `002` alongside
`seed.SchemaVersion = 2`. The parser uses `DisallowUnknownFields`, so adding them to content files
without bumping the version turns every lesson into a parse error — the two changes ship together.

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
  "schemaVersion": 2,
  "book": "Integrated Korean Beginner 1",
  "lesson": 1, "position": 1, "title": "인사 — Greetings",
  // English. What this chapter is about and what you can do after it.
  "intro": "Greeting people and introducing yourself...",
  "vocab": [{ "korean": "안녕하세요", "english": ["hello"], "pos": "expression",
              "speechLevel": "polite", "irregular": null, "notes": "" }],
  // Exactly one example per point — required, and rejected if a second is added.
  "grammarPoints": [{ "id": "g1-copula", "title": "N이에요/예요", "explanation": "...",
                      "example": { "korean": "저는 학생이에요.", "english": "I am a student." } }],
  "exercises": [{ "kind": "fill_blank", "grammarPoint": "g1-copula",
                  "prompt": "저는 학생___.", "answer": ["이에요"] }],
  "passages": [{ "korean": "...", "questions": [{ "q": "...", "reference": "..." }] }],
  // Vocab deliberately left out of the comprehension coverage requirement,
  // with the reason. Empty for most chapters; see Coverage lint.
  "coverageExempt": []
}
```

If hand-authoring long passages in JSON gets painful, swapping to YAML is a one-line parser change.

### Lexicon gating

**The rule: no module ever shows material from a chapter above the current position.** How
strictly that can be enforced depends on where the material comes from, and the three cases are
genuinely different. Conflating them is what would make this rule a lie.

**Item selection — hard, absolute.** Which vocab, exercises, passages and grammar points a module
can draw from is a `WHERE position <= ?` join, and there is no way around it. This covers the bulk
of the app, including two places that are easy to miss: **multiple-choice distractors** must be
drawn from the unlocked set (a distractor from lesson 12 teaches lesson 12), and the same goes for
matching-exercise decoy pairs. One `internal/lexicon` package owns the unlocked-set query so every
caller gets the same answer; nothing assembles this filter by hand.

**Authored text — lint, warning-level.** Whether a sentence *you wrote* smuggles in a later word is
a text check, and Korean is agglutinative: `먹었어요` will never substring-match `먹다`. A cheap
matcher produces constant false positives, so `korlearn lint` warns and never blocks, seed files
carry `allowExtra` for particles and inflections, and the matcher is stem-prefix based. This is the
one place the rule is aspirational rather than mechanical — it is also the one place a human read
the sentence before it shipped.

**LLM output — validated, not trusted.** Generated drills and conversation turns get the unlocked
lexicon in the system prompt, and then the output is checked against it anyway. A 7.8B model will
not obey a closed vocabulary just because it was asked. The generated-drill path can afford to be
strict: validate, retry twice with the offending tokens named, discard on the third failure —
nothing is lost, since authored content is always available. **Conversation cannot discard a turn**
without stalling the exchange, so it validates and *marks*: out-of-lexicon words are highlighted in
the transcript with a "not taught yet" tag and their gloss, which is more useful than a silent drop
and more honest than pretending it did not happen.

### Coverage lint

The comprehension rule — every vocab word and grammar point exercised at least once — is
mechanically checkable, so `korlearn lint` checks it and this one **fails**, unlike the lexicon
warning. Per chapter: each vocab entry must appear in some passage's text, question, or reference
answer, and each grammar point must be reachable from some passage question. Words listed in
`coverageExempt` are skipped, and the list is meant to stay short enough to read.

Two honest consequences to expect while authoring. A typical Integrated Korean lesson has ~30 vocab
entries, which is more than one natural passage can carry — chapters will need two or three
passages, and coverage is computed across the set, not per passage. And "grammar point covered" can
only mean the question was *designed* to elicit it; nothing can force the answer to use it. The
reference answer is what the check reads, and the grader's rubric is what actually notices when
your answer dodged the pattern.

## Phase 0 — Schema and shell (before any module UI)

The flow needs three things that touch every module, and retrofitting them later means editing
five screens instead of one place:

1. **Seed schema v2 + migration `002`**: `intro`, `grammarPoints[].example`, `coverageExempt`.
   Parse-time rules: `example` is required and singular, `intro` is required and non-empty.
2. **`internal/lexicon`**: unlocked vocab and grammar for a given position, plus the token checker
   the LLM validator and MC distractor picker both call. `GET /api/chapters/{id}/lexicon` exposes it.
3. **Chapter page**: intro text, five module tiles, per-module completion from the sessions query,
   and a "continue" button pointing at the first unfinished module. Direct launch from every tile.

## Phase 1 — Vocab

**Milestone 1 (do this first): vertical slice through every layer, one real lesson.**

Seed IK Beginner 1 Lesson 1 → Go loads it into SQLite on startup → chapter page renders the intro
and tiles → the vocab module runs table → flashcards → learn → audio button hits `/api/tts` and
caches to disk → each completed card POSTs an attempt → session ends with a score visible on the
dashboard.

This surfaces schema mistakes and MeloTTS sidecar problems in week one rather than week six. After
it works, lessons 2–16 are pure content authoring.

Then, in order:
1. Vocab table: `English | Korean` with per-row TTS. No attempts, no score — it is the reference view.
2. Flashcards: flip only, with a direction picker (KO→EN or EN→KO) shown before the deck starts.
   Self-graded, `stage = 'flip'`.
3. Learn: two-stage mastery (MC → typed, fail written → back to MC, checkpoint every 10, 50-step
   undo, self-grade fallback with override). Rewrite riplet's `session.js` / `distractors.js` /
   `shuffle.js` / `grading.js` in TS. **Distractors come from the unlocked lexicon only.**
   Answer normalization: Unicode **NFC**, trim, collapse whitespace, strip terminal punctuation.
4. Session resume: snapshot PUT on advance, restored on reload.
5. Module chaining: each module's end screen offers the next one in flow order plus a return to the
   chapter page. Client-side routing only.
6. Pre-warm script: synthesize every vocab word in a chapter into the TTS cache.
7. Dashboard: per-chapter average-over-time + best score, trend chart, weak-items table.
8. Calendar: contribution-style heatmap of practice days + per-day detail.
9. Optional per-chapter deadlines — **soft**: show on-track/behind on the dashboard and calendar,
   never block or penalize.
10. Comprehensive cross-chapter quiz mode (`chapter_id NULL` sessions).
11. "Trouble spots" playlist: generated from weakness ranking, launched only when you choose it.

## Phase 2 — Grammar and comprehension

1. Grammar module: iterate the chapter's grammar points; each shows explanation + its one example,
   then matching (click-to-pair, decoys from the unlocked lexicon), then fill-in-blank (typed
   Hangul, deterministic grading with normalization and multiple accepted answers).
2. Comprehension module: typed full-sentence Korean answers → `POST /api/grade` → Ollama →
   strict JSON: `{comprehension: bool, grammarIssues[], vocabIssues[], corrected: string, score: 0-3}`.
   Store the rubric on the attempt so the dashboard can aggregate error categories
   ("you keep missing 을/를"). Keep the manual override button.
3. **`testdata/grading_golden.json`**: ~30 hand-written Korean answers with expected verdicts,
   run by `go test`. This is what makes the model a config value instead of a guess, and it catches
   the real failure mode — malformed JSON — before it reaches the UI. Retry-with-repair on parse failure.
4. Coverage lint wired into `go test`, so an under-covered chapter fails before it ships.
5. LLM extra-drill generation from the unlocked lexicon, validated with retry, clearly labeled as
   generated and stored separately from authored content so replays stay deterministic.

## Phase 3 — Conversation

Text-first, because typed Korean is what the other four modules grade and it removes the entire
audio stack from the first working version: type a turn → EXAONE replies in Korean (system prompt
carries the unlocked lexicon + the chapter's grammar points and topic) → MeloTTS speaks the reply
via the same `/api/tts` cache. Per-turn feedback uses the Phase 2 rubric. Out-of-lexicon words in
the model's reply are tagged inline rather than dropped. Full transcript saved and replayable.

Push-to-talk lands after that and only changes the input side: hold key → record → release →
faster-whisper → same pipeline.

**Set expectations honestly: Whisper is not a pronunciation scorer.** It is trained to recover
intended words, so it will transcribe sloppy Korean as correct Korean. It tells you *what* you said,
not *how well*. Real pronunciation scoring needs forced alignment (MFA or similar) and is explicitly
out of scope. Record WAV in-browser via AudioWorklet to avoid needing ffmpeg for WebM/Opus decode.

No VAD, no barge-in, no echo cancellation — those are where projects like this stall.

## Verification

- `go test ./...` — seed parsing, answer normalization (NFC edge cases), stats SQL, grading golden set.
- `korlearn lint content/` — out-of-sequence vocabulary (warns) and comprehension coverage (fails).
- **Lexicon gating test, the one that matters most:** with the current chapter pinned to position 3,
  assert that vocab lists, MC distractors, matching decoys, exercise sets, passages and the
  conversation prompt lexicon contain nothing from position 4+. Table-driven over every selection
  query, so a new module that forgets the filter fails a test rather than teaching lesson 12 early.
- Milestone 1 end-to-end by hand: start the binary, open localhost, complete Lesson 1's quiz,
  confirm (a) audio plays and a `.wav` lands in the cache dir, (b) `attempts` rows exist in the
  SQLite file, (c) reloading mid-session resumes at the same card, (d) the dashboard score matches
  what the session reported, (e) every module is reachable directly from the chapter page and the
  end-of-module "next" lands on the right one.
- Phase 2: run the golden set against `exaone3.5:7.8b`, record the pass rate, then re-run after any
  prompt change — the number is the regression signal.
- Phase 3: measure round-trip latency per turn (release key → audio playing). If it exceeds ~4s,
  drop the Whisper model size before touching anything else.

## Setup prerequisites

```bash
winget install GoLang.Go
ollama pull exaone3.5:7.8b
```

Plus a Python **3.11** venv under `sidecar/` for MeloTTS and faster-whisper — **not** the
installed 3.14, and not 3.12 (see the environment notes above).
