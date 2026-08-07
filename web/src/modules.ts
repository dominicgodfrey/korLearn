import type { ChapterCounts, StudyMode } from "./api";

/**
 * The five modules of a chapter, in the order the guided flow chains them.
 *
 * The order is a recommendation the chapter page walks you down, not a lock:
 * every module is launchable directly at any time, and finishing one is never
 * a prerequisite for another.
 */
export interface ModuleSpec {
  mode: StudyMode;
  /** Position in the flow, shown on the tile. */
  step: number;
  name: string;
  summary: string;
  /** What has to exist in the chapter for this module to have anything to do. */
  requires: (counts: ChapterCounts) => boolean;
  /** Why it is unavailable, when requires() fails. */
  missing: string;
}

export const MODULES: ModuleSpec[] = [
  {
    mode: "vocab_flip",
    step: 1,
    name: "Flashcards",
    summary: "Flip through the chapter's words. You choose which side shows first.",
    requires: (c) => c.vocab > 0,
    missing: "no vocabulary",
  },
  {
    mode: "vocab_learn",
    step: 2,
    name: "Learn vocabulary",
    summary: "Multiple choice until a word is answerable, then typed until it sticks.",
    requires: (c) => c.vocab > 0,
    missing: "no vocabulary",
  },
  {
    mode: "grammar",
    step: 3,
    name: "Grammar",
    summary: "One worked example per pattern, then matching, then fill-in-the-blank.",
    requires: (c) => c.grammar > 0 && c.exercises > 0,
    missing: "no grammar points or no exercises",
  },
  {
    mode: "comprehension",
    step: 4,
    name: "Comprehension",
    summary: "Read a passage and answer in full Korean sentences. Graded on grammar and spelling.",
    requires: (c) => c.passages > 0,
    missing: "no passages",
  },
  {
    mode: "conversation",
    step: 5,
    name: "Conversation",
    summary: "Practise back and forth, limited to what you have been taught.",
    requires: (c) => c.vocab > 0,
    missing: "no vocabulary",
  },
];
