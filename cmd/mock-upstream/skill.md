---
name: mock-upstream
description: Run auth-proxy's OpenAI-compatible mock upstream. Use when testing the proxy without a real provider or key: mise run mock:run serves it, and the verbs below are what the binary itself does.
---

# mock-upstream

A minimal OpenAI-compatible server for testing the proxy against something
other than a real provider, at zero cost. It is dependency-free and
deterministic, so a test can assert on what it returns.

Point the proxy at it with `UPSTREAM_BASE_URL=http://127.0.0.1:18080/v1` and
`UPSTREAM_API_KEY=mock-key`, or run `mise run proxy:run:mock`, which does
both. Run it through its tasks — `mise run mock:run` serves it,
`mise run mock:check` vets and tests it — never by hand.

## Verbs

<!-- verbs -->

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
