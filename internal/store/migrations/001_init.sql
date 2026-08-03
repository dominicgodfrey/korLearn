-- Content tables (chapters, vocab, grammar_points, exercises, passages) are
-- derived from content/*.json and rebuilt on every startup. History tables
-- (sessions, attempts) are the only irreplaceable data in this file.
--
-- Every content table therefore carries a natural unique key so a reload
-- updates rows in place rather than reinserting them: attempts reference
-- content by rowid, and new ids on every boot would orphan the entire history.

CREATE TABLE chapters (
    id          INTEGER PRIMARY KEY,
    book        TEXT    NOT NULL,
    lesson_no   INTEGER NOT NULL,
    -- One global sequence across all books. Unlocked means position <= current.
    position    INTEGER NOT NULL UNIQUE,
    title       TEXT    NOT NULL,
    -- Soft, advisory only: shown as on-track/behind, never blocks or penalizes.
    deadline_at TEXT,
    retired_at  TEXT,
    -- Identity is (book, lesson_no), not position: reordering the curriculum
    -- must move a chapter, not create a second one and strand its history.
    UNIQUE (book, lesson_no)
);

CREATE TABLE vocab (
    id           INTEGER PRIMARY KEY,
    chapter_id   INTEGER NOT NULL REFERENCES chapters(id),
    korean       TEXT    NOT NULL,
    english_json TEXT    NOT NULL,
    pos          TEXT    NOT NULL,
    speech_level TEXT    NOT NULL DEFAULT '',
    irregular    TEXT,
    notes        TEXT    NOT NULL DEFAULT '',
    retired_at   TEXT,
    UNIQUE (chapter_id, korean)
);

CREATE TABLE grammar_points (
    id          INTEGER PRIMARY KEY,
    chapter_id  INTEGER NOT NULL REFERENCES chapters(id),
    -- The authored id from the seed file ("g1-copula"), stable across reloads.
    slug        TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    explanation TEXT    NOT NULL DEFAULT '',
    retired_at  TEXT,
    UNIQUE (chapter_id, slug)
);

CREATE TABLE exercises (
    id               INTEGER PRIMARY KEY,
    chapter_id       INTEGER NOT NULL REFERENCES chapters(id),
    grammar_point_id INTEGER REFERENCES grammar_points(id),
    kind             TEXT    NOT NULL,
    prompt           TEXT    NOT NULL,
    payload_json     TEXT    NOT NULL DEFAULT '',
    answer_json      TEXT    NOT NULL,
    -- Authored drills are stable; LLM-generated extras are stored with
    -- generated = 1 so replays of authored content stay deterministic.
    generated        INTEGER NOT NULL DEFAULT 0,
    retired_at       TEXT,
    UNIQUE (chapter_id, kind, prompt)
);

CREATE TABLE passages (
    id             INTEGER PRIMARY KEY,
    chapter_id     INTEGER NOT NULL REFERENCES chapters(id),
    korean_text    TEXT    NOT NULL,
    questions_json TEXT    NOT NULL,
    retired_at     TEXT,
    UNIQUE (chapter_id, korean_text)
);

-- chapter_id is nullable: comprehensive quizzes span the whole curriculum.
CREATE TABLE sessions (
    id         INTEGER PRIMARY KEY,
    chapter_id INTEGER REFERENCES chapters(id),
    mode       TEXT    NOT NULL,
    started_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ended_at   TEXT,
    score      REAL,
    -- Client-side session state, overwritten on advance so a reload resumes
    -- at the same card. Cleared when the session ends.
    snapshot_json TEXT
);

-- Append-only and immutable. Every derived number in the app — chapter grade,
-- best score, weakness ranking, the calendar — is SQL over this one table.
-- Nothing updates a row here; corrections are recorded as new rows.
CREATE TABLE attempts (
    id               INTEGER PRIMARY KEY,
    session_id       INTEGER NOT NULL REFERENCES sessions(id),
    -- Polymorphic by design, so it cannot be a foreign key: 'vocab',
    -- 'exercise', or 'passage_question'.
    item_type        TEXT    NOT NULL,
    item_id          INTEGER NOT NULL,
    -- Which rung of the mastery ladder: 'flip', 'mc', or 'typed'.
    stage            TEXT    NOT NULL,
    correct          INTEGER NOT NULL,
    -- What the grader said before any manual override. Keeping both is what
    -- makes it possible to audit how often the override is used.
    original_correct INTEGER NOT NULL,
    user_answer      TEXT    NOT NULL DEFAULT '',
    rubric_json      TEXT,
    elapsed_ms       INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_attempts_session ON attempts(session_id);
-- Weakness ranking scans by item; the calendar scans by day.
CREATE INDEX idx_attempts_item ON attempts(item_type, item_id);
CREATE INDEX idx_attempts_created ON attempts(created_at);
CREATE INDEX idx_sessions_chapter ON sessions(chapter_id);
