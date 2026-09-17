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

