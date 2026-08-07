import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { api } from "../api";
import { ErrorBox, Loading, Page } from "../components/Page";

export function ChaptersPage() {
  const { data, isPending, error } = useQuery({
    queryKey: ["chapters"],
    queryFn: api.chapters,
  });

  return (
    <Page>
      <h1 className="mb-1 text-2xl font-semibold tracking-tight">Chapters</h1>
      <p className="mb-6 text-sm text-stone-500">
        In curriculum order. Opening a chapter assumes you know everything before it.
      </p>

      {isPending && <Loading what="chapters" />}
      {error && <ErrorBox error={error} />}

      {data?.length === 0 && (
        <p className="text-sm text-stone-500">
          No chapters yet. Add a lesson file under <code>content/</code> and restart the
          binary.
        </p>
      )}

      <ul className="space-y-2">
        {data?.map((c) => (
          <li key={c.id}>
            <Link
              to={`/chapters/${c.id}`}
              className="flex items-baseline justify-between rounded-lg border border-stone-200 bg-white px-4 py-3 transition-colors hover:border-stone-400 dark:border-stone-800 dark:bg-stone-900 dark:hover:border-stone-600"
            >
              <span>
                <span className="mr-2 text-xs tabular-nums text-stone-400">
                  {String(c.position).padStart(2, "0")}
                </span>
                <span className="font-medium">{c.title}</span>
                <span className="ml-2 text-sm text-stone-500">
                  {c.book} · lesson {c.lessonNo}
                </span>
              </span>
              <span className="shrink-0 text-sm text-stone-500">{c.vocabCount} words</span>
            </Link>
          </li>
        ))}
      </ul>
    </Page>
  );
}
