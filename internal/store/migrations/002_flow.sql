-- Schema v2: the columns the five-module chapter flow needs.
--
-- All three are content, rebuilt from seed files on every startup, so they are
-- added with a default rather than backfilled — the next load overwrites them.
-- Empty is not a legal value in the seed schema; it only exists in the window
-- between this migration and that load.

-- The English overview the chapter page opens with.
ALTER TABLE chapters ADD COLUMN intro TEXT NOT NULL DEFAULT '';

-- The single worked example the grammar module shows before drilling a point.
ALTER TABLE grammar_points ADD COLUMN example_korean  TEXT NOT NULL DEFAULT '';
ALTER TABLE grammar_points ADD COLUMN example_english TEXT NOT NULL DEFAULT '';
