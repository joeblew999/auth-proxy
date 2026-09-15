- Repo: dvcrn/grok-oauth-proxy

# Development instructions

## Rule zero: use mise

**Every workflow goes through a mise task.** Do not call `go`, `wrangler`,
`cloudflared`, or `fnox` directly — the tasks pin toolchain versions and inject
credentials consistently, and a bare command will not have them. Run `mise tasks`
for the live list; if something a dev needs is missing, add a task rather than
documenting a raw command.

## Go

| Task | Purpose |
|---|---|
| `mise run go_format` | gofmt |
| `mise run go_test` | unit tests |
| `mise run go_build` | native binary at `bin/grok-oauth-proxy` |
| `mise run go_build_worker` | Cloudflare Workers WASM build |

Run format, test, and build after Go changes.

## Cloudflare

| Task | Purpose |
|---|---|
| `mise run cf_deploy` / `cf_deploy_dry` | deploy, or validate the build without uploading |
| `mise run cf_tail` | stream live Worker logs |
| `mise run cf_kv_create` | create the `GROK_AUTH` KV namespace |
| `mise run cf_secret_admin_key` | set `ADMIN_API_KEY` on the Worker |
| `mise run cf_auth_start` / `cf_auth_status` / `cf_auth_wait` | xAI device-flow login |
| `mise run cf_verify_status` / `cf_verify_models` / `cf_verify_chat` | verify a deployment |

Credentials come from **fnox**, not `wrangler login` state, and are never committed.
The Cloudflare tasks wrap `fnox exec --`, so they do not prompt and leave nothing on
disk. `wrangler secret put` reads the value from **stdin**, so pipe it — its hidden
TTY prompt fails inside the mise/fnox layers (see `cf_secret_admin_key_sync`).

## Upstream provider

The upstream is configurable, so the proxy is **not** xAI-only:

- `UPSTREAM_BASE_URL` — upstream OpenAI-compatible base URL. Defaults to
  `https://api.x.ai/v1`, so leaving it unset preserves xAI behaviour exactly.
- `UPSTREAM_API_KEY` — static bearer token. When set, the OAuth device flow is
  bypassed entirely and no refresh is ever attempted. Leave unset to keep OAuth.

Do not reintroduce a hardcoded upstream host: always resolve through
`upstreamBaseURL()` in `upstream.go`. Anything xAI-specific (extra model aliases,
OAuth endpoints) must stay behind `isXAIUpstream()` or the OAuth-only code paths.

Because these are read from the environment at call time, tests that exercise the
OAuth path must pin them with `t.Setenv` to stay hermetic.

With `UPSTREAM_API_KEY` set there is no OAuth flow at all, so the OAuth-only admin
routes (`/admin/auth/start`, `/admin/auth/status`, `/admin/tokens`) return **409**
rather than failing obscurely, and `/admin/status` reports `authMode` alongside
`configured`.

## Local provider-agnostic testing — free, no xAI account

| Task | Purpose |
|---|---|
| `mise run mock_upstream` | local OpenAI-compatible mock on `127.0.0.1:18080` |
| `mise run proxy_local_mock` | proxy in static-key mode pointed at that mock |

This path is verified and exercises routing, streaming, and the admin middleware
without touching xAI. Prefer it over spending anything to test a change.

## Rules

- Keep OAuth tokens and `ADMIN_API_KEY` out of source, logs, and commits.
- Use the repository's npm lockfile and npm for changes under `npm/`.
- Preserve the local proxy's loopback-only browser login while keeping Workers authentication routes behind the admin middleware.
- Workers provider and OAuth traffic must use the `GROK_EGRESS` VPC binding **when one is configured**. The binding is optional: with no binding the Worker falls back to direct egress, which is verified to reach `api.x.ai`.

