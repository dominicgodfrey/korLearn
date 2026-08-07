import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router";

import { api, type ChapterDetail, type ModuleProgress } from "../api";
import { ErrorBox, Loading, Page } from "../components/Page";
import { MODULES, type ModuleSpec } from "../modules";

export function ChapterPage() {
  const { id } = useParams();
  const chapterID = Number(id);

  const chapter = useQuery({
    queryKey: ["chapter", chapterID],
    queryFn: () => api.chapter(chapterID),
    enabled: Number.isFinite(chapterID),
  });

  const lexicon = useQuery({
    queryKey: ["lexicon", chapterID],
    queryFn: () => api.lexicon(chapterID),
    enabled: Number.isFinite(chapterID),
  });

  if (chapter.isPending) return <Page><Loading what="chapter" /></Page>;
  if (chapter.error) return <Page><ErrorBox error={chapter.error} /></Page>;

  const c = chapter.data;
  const byMode = new Map(c.progress.map((p) => [p.mode, p]));
  const next = MODULES.find(
    (m) => m.requires(c.counts) && (byMode.get(m.mode)?.sessions ?? 0) === 0,
  );

  // Words carried forward from earlier chapters. Shown because it is the one
  // rule the whole app is built around, and a number on screen is how you
  // notice when it is wrong.
  const carried = lexicon.data
    ? lexicon.data.words.filter((w) => w.chapterPosition < c.position).length
    : null;

  return (
    <Page>
      <p className="text-sm text-stone-500">
        {c.book} · lesson {c.lessonNo}
      </p>
      <h1 className="mt-1 text-2xl font-semibold tracking-tight">{c.title}</h1>

      <p className="mt-4 leading-relaxed text-stone-700 dark:text-stone-300">{c.intro}</p>

      <div className="mt-4 flex flex-wrap gap-x-4 gap-y-1 text-sm text-stone-500">
        <span>{count(c.counts.vocab, "word")}</span>
        <span>{count(c.counts.grammar, "grammar point")}</span>
        <span>{count(c.counts.exercises, "exercise")}</span>
        <span>{count(c.counts.passages, "passage")}</span>
        {carried !== null && <span>{count(carried, "word")} carried forward</span>}
      </div>

      <div className="mt-8 mb-3 flex items-baseline justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-stone-500">
          Modules
        </h2>
        {next && (
          <p className="text-sm text-stone-500">
            Suggested next: <span className="text-stone-700 dark:text-stone-300">{next.name}</span>
          </p>
        )}
      </div>

      <ul className="space-y-2">
        {MODULES.map((m) => (
          <ModuleTile
            key={m.mode}
            spec={m}
            chapter={c}
            progress={byMode.get(m.mode)}
            suggested={m.mode === next?.mode}
          />
        ))}
      </ul>

      <p className="mt-6 text-sm text-stone-500">
        The order above is the recommended path, not a lock — any module can be started at
        any time.
      </p>
    </Page>
  );
}

/** "1 passage" / "2 passages". Every noun on this page pluralises with -s. */
function count(n: number, noun: string) {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

function ModuleTile({
  spec,
  chapter,
  progress,
  suggested,
}: {
  spec: ModuleSpec;
  chapter: ChapterDetail;
  progress: ModuleProgress | undefined;
  suggested: boolean;
}) {
  const available = spec.requires(chapter.counts);
  const done = (progress?.sessions ?? 0) > 0;

  return (
    <li
      className={[
        "rounded-lg border px-4 py-3",
        suggested
          ? "border-stone-400 bg-white dark:border-stone-600 dark:bg-stone-900"
          : "border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900",
        available ? "" : "opacity-60",
      ].join(" ")}
    >
      <div className="flex items-baseline gap-2">
        <span className="text-xs tabular-nums text-stone-400">{spec.step}</span>
        <span className="font-medium">{spec.name}</span>
        {done && (
          <span className="text-xs text-stone-500">
            done{progress?.bestScore != null && ` · best ${Math.round(progress.bestScore * 100)}%`}
          </span>
        )}
        <span className="ml-auto shrink-0 text-xs text-stone-400">
          {available ? "not built yet" : spec.missing}
        </span>
      </div>
      <p className="mt-1 text-sm text-stone-500">{spec.summary}</p>
    </li>
  );
}
