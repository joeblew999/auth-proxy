## Why it masks the token

The mock logs a masked form of the bearer token plus a short hash, never the
token itself. The hash is stable, so a test can confirm *which* credential
arrived — the thing worth checking when switching providers — while the mock
stays safe to run even when pointed at a real key.

## Rules

- This file is written from the verbs by `mock-upstream skill`, never by hand;
  `dev build cmd/mock-upstream` rewrites it and `go test` fails when it is
  stale.
- A verb's usage is markdown in `usage.md` beside `main.go`, and the prose
  around it is `head.md` and `tail.md`. Keep `usage.md` to headings, `- ` list
  items with two-space continuations, paragraphs and inline code: that is the
  subset the terminal rendering reads. Write every `<placeholder>` in
  backticks, or a markdown renderer swallows it as an HTML tag.
