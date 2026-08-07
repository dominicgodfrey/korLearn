# testdata/content/

Fixture lessons for developing the UI. **Not curriculum** — the Korean here is
placeholder, the book is called "Fixture Book", and nothing in this directory
should ever be studied.

It exists so the frontend can be built and looked at before real lessons are
authored: three chapters, complete enough to exercise every part of the chapter
flow, and complete enough to pass `korlearn lint` including the coverage check.

```bash
go run ./cmd/korlearn -content testdata/content -db fixture.db
```

Real content lives in `content/`, one file per lesson, same schema. See
[content/README.md](../../content/README.md) for what the fields mean.
