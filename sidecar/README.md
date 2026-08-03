# sidecar/

Korean text-to-speech, as a separate process. The Go binary talks to it over
HTTP and knows nothing about the engine behind it:

```
POST /synth   {"text": "안녕하세요", "voice": ""}  ->  audio/wav
GET  /health                                      ->  {"status": "ok", "loaded": bool}
```

Keeping it out of process is what lets the model live in a Python environment
with its own pinned dependencies while the app stays a single Go binary. It can
be started, stopped, or missing independently — the app answers 502 for audio
and keeps working.

## Why not Kokoro

`PLAN.md` chose Kokoro. Kokoro has **no Korean voices**: 54 voices across nine
languages, none Korean, and no Korean language code in its pipeline. Its
frontend library (`misaki`) ships Korean G2P, which is probably where the
confusion came from, but the model was never trained on it.

`facebook/mms-tts-kor` was tested as an alternative and works, but it
synthesizes from **uroman romanization** rather than Hangul, at 16kHz — so
Korean phonology is never applied and some pronunciations come out subtly
wrong. That is a bad trade in an app whose purpose is learning pronunciation.

MeloTTS was chosen instead: it has a real Korean frontend (`g2pkk`) that applies
받침 resolution and consonant assimilation (학생 → hakssaeng, 종로 → jongno), and
outputs 44.1kHz.

## Setup

Requires **Python 3.11**, not 3.12 or newer: MeloTTS pins `transformers==4.27.4`,
whose `tokenizers` dependency has no wheel past 3.11 and needs a Rust toolchain
to build from source.

```bash
uv venv sidecar/.venv --python 3.11
uv pip install --python sidecar/.venv/Scripts/python.exe torch --index-url https://download.pytorch.org/whl/cu128
uv pip install --python sidecar/.venv/Scripts/python.exe "git+https://github.com/myshell-ai/MeloTTS.git"
uv pip install --python sidecar/.venv/Scripts/python.exe mecab-ko mecab-ko-dic
sidecar/.venv/Scripts/python.exe -m unidic download
```

The CUDA index is `cu128` because the RTX 5060 Ti is Blackwell (sm_120); older
CUDA wheels will not run on it.

`unidic` is a 526MB Japanese dictionary that MeloTTS loads unconditionally, even
for Korean. It is not optional.

## Two Windows traps, already handled

**`eunjeon.py`** — g2pkk picks its morphological analyzer by platform:
`python-mecab-ko` on Unix, `eunjeon` on Windows. `eunjeon` has no wheels and
needs a full MSVC C++ toolchain to build. `mecab-ko` ships prebuilt Windows
wheels of the same MeCab but exposes only the raw SWIG `Tagger`, not the `.pos()`
method g2pkk calls, so `eunjeon.py` here bridges the two. It sits next to
`server.py` so that directory lands on `sys.path`, which is what makes g2pkk's
`find_spec("eunjeon")` succeed.

Without it, g2pkk still runs but silently skips its POS-dependent rules: the
의 particle (나의 → 나에), ㄹ-ending tensification, and nasal-stem tensification
(안다 → 안따). Common enough in beginner Korean to teach the wrong pronunciation,
which is why it is worth the shim.

**`os.chdir` at startup** — NLTK refuses to import any module whose file lives
under the current working directory. The venv sits at `sidecar/.venv`, inside the
repo, so running the server from the repo root makes every one of NLTK's own
dependencies look like a working-directory import and synthesis fails with a
confusing security error. `server.py` moves off the repo before importing
anything that pulls in NLTK. Nothing in it resolves paths relatively.

## Running

```bash
sidecar/.venv/Scripts/python.exe sidecar/server.py --port 8123
```

The model loads on the first request, not at startup, so a load failure shows up
as a readable HTTP error rather than a dead process. The first synthesis is
therefore slow; every one after is not.

Point the Go binary at it with `-tts-url` (default `http://localhost:8123`).
Results are cached to disk by the Go side, so each distinct string is only ever
synthesized once.
