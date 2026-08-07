// Types mirror the Go structs in internal/store and internal/lexicon. They are
// hand-written rather than generated: the surface is small, and a generator is
// a build step to maintain for a handful of shapes.

/** One study module. Order matters — this is the order the flow chains them. */
export const STUDY_MODES = [
  "vocab_flip",
  "vocab_learn",
  "grammar",
  "comprehension",
  "conversation",
] as const;

export type StudyMode = (typeof STUDY_MODES)[number];

export interface Chapter {
  id: number;
  book: string;
  lessonNo: number;
  position: number;
  title: string;
  deadlineAt: string | null;
  vocabCount: number;
}

export interface ModuleProgress {
  mode: StudyMode;
  sessions: number;
  lastEndedAt: string | null;
  bestScore: number | null;
}

export interface ChapterCounts {
  vocab: number;
  grammar: number;
  exercises: number;
  passages: number;
}

export interface ChapterDetail extends Chapter {
  intro: string;
  counts: ChapterCounts;
  progress: ModuleProgress[];
}

export interface LexiconWord {
  id: number;
  korean: string;
  english: string[];
  pos: string;
  chapterPosition: number;
}

export interface LexiconPoint {
  id: number;
  slug: string;
  title: string;
  chapterPosition: number;
}

export interface Lexicon {
  position: number;
  words: LexiconWord[];
  grammar: LexiconPoint[];
}

/**
 * The server answers errors as `{error: string}`, so a failed request has a
 * message worth showing. Throwing the bare status instead would turn every
 * failure into "500" on screen.
 */
async function get<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: string } | null;
    throw new Error(body?.error ?? `${res.status} ${res.statusText}`);
  }
  return (await res.json()) as T;
}

export const api = {
  chapters: () => get<Chapter[]>("/api/chapters"),
  chapter: (id: number) => get<ChapterDetail>(`/api/chapters/${id}`),
  lexicon: (id: number) => get<Lexicon>(`/api/chapters/${id}/lexicon`),
};
