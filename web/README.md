# web/

Vite + React + TypeScript + Tailwind. The Go binary serves `web/dist` in
production and knows nothing about Vite.

## Running

Two processes in development. The binary owns the API; Vite owns the page and
proxies `/api` to it.

```bash
go run ./cmd/korlearn -content testdata/content -db fixture.db
```

```bash
npm --prefix web run dev
```

The proxy in `vite.config.ts` targets `localhost:8080`, the binary's default
`-addr`. Change both together if that port is taken.

To see what the binary actually ships, build first and skip Vite:

```bash
npm --prefix web run build
```

`testdata/content` holds fixture lessons — placeholder Korean, not curriculum —
so the UI can be developed before real content exists. Point `-content` at
`content/` for the real thing.

## Layout

```
src/
  api.ts            fetch helpers + types mirroring the Go structs
  modules.ts        the five study modules, in flow order
  components/       shared shell
  pages/            one file per route
```

`api.ts` types are hand-written rather than generated. The surface is small,
and a generator is a build step to maintain for a handful of shapes; if they
drift, `tsc` catches it at the call site.

## What exists

The chapter list and the chapter page. Every module tile reads "not built yet"
— the modules themselves are the next phase of work. The tile still shows
whether the chapter has the content that module needs, and whether it has ever
been completed, because both come from the API rather than from the module.
