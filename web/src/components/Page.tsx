import type { ReactNode } from "react";
import { Link } from "react-router";

/** The shell every screen sits in: a header that gets you home, and a column. */
export function Page({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-stone-50 text-stone-900 dark:bg-stone-950 dark:text-stone-100">
      <header className="border-b border-stone-200 dark:border-stone-800">
        <div className="mx-auto flex max-w-3xl items-baseline gap-3 px-6 py-4">
          <Link to="/" className="text-lg font-semibold tracking-tight">
            korLearn
          </Link>
          <span className="text-sm text-stone-500">Korean study</span>
        </div>
      </header>
      <main className="mx-auto max-w-3xl px-6 py-8">{children}</main>
    </div>
  );
}

export function Loading({ what }: { what: string }) {
  return <p className="text-sm text-stone-500">Loading {what}…</p>;
}

/**
 * Errors show the server's message rather than a generic apology. This is a
 * single-user localhost tool and the user is the developer: "no such chapter"
 * is more useful than "something went wrong".
 */
export function ErrorBox({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error);
  return (
    <div className="rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-900 dark:border-red-900 dark:bg-red-950 dark:text-red-200">
      {message}
    </div>
  );
}
