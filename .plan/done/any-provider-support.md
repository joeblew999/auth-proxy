# Plan: Provider-Agnostic Upstream

| | |
|---|---|
| **Status** | COMPLETE, with two defects found and fixed in the §9 audit — §5.1–5.5 implemented and tested, §6 step 3 closed by §6.2, §8 all met. Nothing outstanding |
| **Created** | 2026-09-15 |
| **Repo** | `joeblew999/grok-oauth-proxy` (fork of `dvcrn/grok-oauth-proxy`) |
| **Goal** | Point the proxy at any OpenAI-compatible provider without forking auth code |
| **Blocked by** | Nothing. The xAI entitlement issue that once looked external is now bypassed by the static-key path, and the deployment plan is complete — `.plan/done/cloudflare-workers-deployment.md` |

---

## 1. Objective

Today the proxy is hardwired to xAI in a handful of constants, so trying a different
provider means editing Go source. The goal is to make the upstream configurable at
deploy time, so switching providers is a config change rather than a code change.

Motivation was concrete: the xAI account was entitlement-blocked
(`personal-team-blocked:spending-limit`) and validating the proxy appeared to cost
$30/month via SuperGrok. A provider-agnostic upstream allowed $0 validation against a
local model — and the static-key mode it introduced also turned out to be the fix for
the entitlement block itself.

---

## 2. What Is Already Provider-Agnostic

Verified by reading the source, not inferred.

| Layer | File | Finding |
|---|---|---|
| Request routing | `proxy_helpers.go` | `workerUpstreamURL` builds `apiURL + "/" + path`. Strips a leading `/v1/`, drops the `key` query param, forwards everything else verbatim |
| Upstream auth | `proxy_helpers.go` | `copyRequestHeaders` strips client `Authorization` / `X-Api-Key`, so the proxy substitutes its own stored token. Client credential (`ADMIN_API_KEY`) and upstream credential are already cleanly separated |
| Token injection | `admin_handlers.go` | `POST /admin/tokens` accepts `{accessToken, refreshToken, expiresAt}` (expiresAt in Unix **ms**) |
| Token refresh | `auth_tokens.go` | `currentAccessToken` refreshes only when `ExpiresAt <= now + 5min` (`refreshSkew`). A token injected with a far-future expiry is used verbatim, forever, with no refresh |
| Env access | `env_default.go` / `env_workers.go` | `getenv` already abstracts `os.Getenv` vs `cloudflare.Getenv`, so env vars work identically in the CLI and the Worker |

**Consequence:** any provider's key can already be used as the upstream bearer token
via `POST /admin/tokens`, with no code change. The only hard blocker is the base URL.

---

## 3. What Is Hardcoded to xAI

| Constant | Location | Needed for other providers? |
|---|---|---|
| `apiURL = "https://api.x.ai/v1"` | `auth_tokens.go:22` | **Yes** — the one that matters |
| `clientID` | `auth_tokens.go:18` | No — OAuth only |
| `scope` (`grok-cli:access api:access`) | `auth_tokens.go:19` | No — OAuth only |
| `deviceCodeURL` | `auth_tokens.go:20` | No — OAuth only |
| `tokenURL` | `auth_tokens.go:21` | No — OAuth only |
| `authURL` (browser login) | `main.go:24` | No — OAuth only |
| `trustedVerificationURL` allowlist (`https://auth.x.ai/`) | `device_auth.go:291` | No — OAuth only |
| `extraModels` → injects `grok-composer-2.5-fast` into every `/v1/models` | `models.go` | No, and it is **wrong** for other providers |
| MCP tool descriptions mentioning the xAI API | `mcp_handler.go:72` | No — cosmetic |

Only `apiURL` and `extraModels` are actually in the non-OAuth request path. The other
seven matter only when the OAuth device flow is in use, so they can be left alone and
simply become inert when a static key is configured.

---

## 4. Design

Keep it KISS. Two env vars, one conditional, no provider registry.

```go
// resolved at startup, defaults preserve current behaviour exactly
upstreamBaseURL = getenvDefault("UPSTREAM_BASE_URL", "https://api.x.ai/v1")
upstreamAPIKey  = getenv("UPSTREAM_API_KEY")   // empty => existing OAuth flow
```

Behaviour:

| `UPSTREAM_API_KEY` | Mode | Token source |
|---|---|---|
| unset (default) | xAI OAuth | `GROK_AUTH` KV / `auth.json`, device flow + refresh as today |
| set | Static bearer | The env var itself; no OAuth, no refresh |

`UPSTREAM_BASE_URL` overrides the base URL in both modes. **Superseded by §9:** OAuth mode is xAI-only, and a non-xAI base URL requires `UPSTREAM_API_KEY`. Defaulting to the current
value means an existing xAI deployment is byte-for-byte unchanged with no env vars set.

**Why an env var rather than reusing `POST /admin/tokens`:** injecting a token works
today, but it is imperative state that can be clobbered by a later OAuth refresh, and
it cannot be declared in config. A startup-time env var is declarative and idempotent.
`POST /admin/tokens` stays as the manual escape hatch.

---

## 5. Work Breakdown

### 5.1 Base URL becomes configurable — DONE

Implemented in the new `upstream.go`:

- `upstreamBaseURL()` returns `UPSTREAM_BASE_URL` when set, trimmed of whitespace and
  a trailing slash, and otherwise `defaultUpstreamBaseURL` (`https://api.x.ai/v1`).
- Every former `apiURL` call site now uses it: `proxy_helpers.go` (routing),
  `models.go` (`/models`), `mcp_handler.go` (`/chat/completions`), and `main.go`'s
  reverse-proxy target.
- The old `apiURL` constant is gone entirely, so a hardcoded host cannot creep back
  into a new call site unnoticed.
- Tests: default unchanged, override honoured, trailing slash trimmed, whitespace
  trimmed, and routing follows the configured base.

### 5.2 Static API key mode — DONE

One short-circuit in each of two functions covers every call path:

- `currentAccessToken()` returns `&AuthTokens{AccessToken: key}` when
  `UPSTREAM_API_KEY` is set, *before* touching the token store. Because it returns
  ahead of the expiry comparison, no refresh is ever attempted.
- `forceRefreshToken()` returns the same fixed key, so the existing single 401-retry
  re-sends it and surfaces the upstream's real error rather than a synthetic refresh
  failure.

This was enough because `ensureAccessToken`, `mcp_handler.go`, and `models.go` all
funnel through those two functions — no handler needed changing, which keeps the diff
small.

Tests: static key used verbatim and trimmed; zero HTTP calls during
`currentAccessToken` and `forceRefreshToken`; and with no static key configured,
missing credentials still fail closed.

One follow-on gap, found after the first implementation and since closed: the
OAuth-only admin endpoints (`/admin/auth/start`, `/admin/auth/status`,
`/admin/tokens`) were still live in static-key mode. They would either contact
`auth.x.ai` pointlessly or silently store credentials that nothing reads. They now
return **409** with an explanation, and `/admin/status` reports an `authMode` field
so a static-key deployment no longer looks unconfigured merely because the OAuth
store is empty.

### 5.3 Provider-conditional extras — DONE

- `providerExtraModels()` returns `extraModels` only when `isXAIUpstream()`. Both
  call sites (`handleModelsList`, `mcpAskGrokModels`) use it, so
  `grok-composer-2.5-fast` is no longer advertised against a non-xAI provider.
- MCP tool descriptions are now provider-aware, via `upstreamName()`,
  `upstreamAuthBlurb()`, and `upstreamModelsBlurb()`. In static-key mode they say a
  static key is used; against a non-xAI upstream they no longer claim "the xAI API".
  Tool **names** deliberately stay `ask_grok` / `ask_grok_models`, because they are
  public MCP surface and renaming them would break clients — that constraint is
  documented in the code so a future reader does not "fix" it.
- **Deferred by design:** per-provider extra headers (OpenRouter's `HTTP-Referer`,
  API version headers). Not worth building until a provider actually requires one.

Tests: descriptions follow the configured upstream, and change shape in static-key
mode.

### 5.4 Config surface — DONE

- `mise.toml`: added `mock_upstream` and `proxy_local_mock`. The latter sets
  `UPSTREAM_BASE_URL`, `UPSTREAM_API_KEY`, and `ADMIN_API_KEY` inline, so the entire
  provider-agnostic path runs in one command with no configuration.
- Worker-side `[vars]`/secret wiring was deliberately **not** added, because the
  deployed Worker is still on xAI/OAuth. It is documented in the README instead, and
  becomes a config change whenever it is wanted.
- Local runs read the same variables from the environment, which is exactly what the
  tasks do.

### 5.5 Docs — DONE

- `AGENTS.md` restructured around **"Rule zero: use mise"** — task tables for Go,
  Cloudflare, and the local mock, a new **Upstream provider** section covering
  `UPSTREAM_*`, and the `t.Setenv` hermeticity requirement.
- `README.md` Workers section: Go 1.25.5 → 1.27, tunnel steps replaced with KV-only
  setup, raw `wrangler` commands replaced with mise tasks, fnox named as the
  credential source, and a new "Using another provider" section.
- `COSTS.md`: added a note that the proxy is not tied to xAI, so a local or
  free-tier provider costs nothing.

---

## 6. Testing Strategy — cost ordered

| Step | Cost | What it proves |
|---|---|---|
| 1. Local OpenAI-compatible server (Ollama / llama.cpp) | **$0** | Routing, `/v1/` handling, streaming SSE, admin routes, MCP |
| 2. Stub upstream (10-line mock) | **$0** | Exact wire behaviour, error passthrough, non-200 handling — deterministic |
| 3. xAI pay-as-you-go key via `UPSTREAM_API_KEY` | Small credit top-up | Real xAI models and real streaming, without the $30/mo commitment |
| 4. SuperGrok subscription | $30/mo | The original design |

Steps 1–2 are the reason this plan exists: they validate everything except
provider-specific entitlement, for nothing.

**This also finally closes the open question** from the deployment plan — whether long
streaming responses survive tunnel-free egress. A local upstream proves the proxy
streams correctly; step 3 proves it streams through Cloudflare against a real provider.

### 6.1 Result — steps 1–2 DONE, $0

Built as `tools/mock-upstream` (dependency-free Go), driven by `mise run mock_upstream`
and `mise run proxy_local_mock`. Verified end to end on 2026-09-15:

| Check | Result |
|---|---|
| `GET /v1/models` through the proxy | Returned the mock's own models (`mock-model`, `mock-reasoning`) — routing follows `UPSTREAM_BASE_URL`, not a hardcoded xAI host |
| xAI-only alias gate | `grok-composer-2.5-fast` **absent** — the §5.3 gate works |
| Streaming SSE | 5 chunks then `[DONE]`, ~150 ms apart, timestamps incrementing — genuinely incremental, not one buffered write |
| Upstream credential | Mock logged `auth=mock... (len=8)` — the static key was forwarded verbatim as the bearer token |
| Unauthenticated request | `401`, and the mock logged only 3 requests, so it never reached the upstream |
| Proxy-side timing | Chat took **756 ms** wall-clock — 5 chunks × the mock's 150 ms delay, plus overhead |

The 756 ms row is the strongest evidence for streaming: it comes from the proxy's own
log, independent of the client, and is only explicable if the response was streamed
through rather than buffered. A buffering proxy would have answered in single-digit
milliseconds.

The unauthenticated row matters too: `ADMIN_API_KEY` gating happens *before* the proxy
hop, so an unauthenticated caller cannot reach the upstream at all. Worth preserving.

The mock logs only a masked token plus a short SHA-256, so it stays safe to run even
if someone points it at a real key.

**Closed in §6.2:** step 3 was satisfied by deploying the mock as a second Worker and
pointing the proxy at it, then later confirming the same path against real xAI on API
credits.

### 6.2 Deployed verification — §6 step 3 CLOSED, 2026-09-15

A localhost mock is unreachable from a Worker, so the mock was itself deployed as a
second Worker (`e2e-mock-provider/`) and the real proxy was pointed at it. This
closes the last open line in the plan, still at $0.

Result, through the deployed Worker at `grok-oauth-proxy.<subdomain>.workers.dev`:

| Check | Result |
|---|---|
| `GET /v1/models` | Returned the mock's models — the deployed Worker honours `UPSTREAM_BASE_URL` |
| xAI alias gate | `grok-composer-2.5-fast` absent |
| Streaming `POST /v1/chat/completions` | 5 chunks then `[DONE]`, **1.28 s** wall-clock (5 × 200 ms) — streamed, not buffered |
| Worker logs | Three POSTs logged `Ok`, with `GROK_EGRESS is not bound; using direct Worker fetch` |

**The gotcha, now documented in `mise.toml`:** the first attempt failed with
Cloudflare `error code: 1042`. Cloudflare's error reference states that a Worker
fetching another Worker on the same zone requires the `global_fetch_strictly_public`
compatibility flag. Two things follow:

1. That flag is needed **only** for Worker-to-Worker fetches. Production talks to
   `api.x.ai`, an ordinary external host, so `wrangler.toml` deliberately does not
   set it — only the E2E task does.
2. A CLI `--compatibility-flags` value **replaces** the config file's list rather
   than appending to it, so `streams_enable_constructors` must be repeated.
   Omitting it would have silently broken streaming, which is a nasty way to fail.
   The task therefore passes both.

A second, more mundane cause: after redeploying with the flag, one POST still
1042'd while a models GET succeeded. That was almost certainly edge propagation,
not a config error — the retry a minute later passed consistently. Worth knowing
before chasing a phantom.

Commands: `mise run e2e_mock_deploy`, then `mise run cf_deploy_mock_upstream` to
point the proxy at the mock, then `mise run cf_deploy` to revert. **Production has
been reverted** to xAI defaults, and a final `mise run cf_deploy` shipped the
committed code, so the deployed Worker matches `HEAD`.

---

## 7. Decisions — AGREED 2026-09-15

| # | Decision | Choice |
|---|---|---|
| 1 | Env var names | **`UPSTREAM_BASE_URL`**, **`UPSTREAM_API_KEY`** — generic, since the code stops being xAI-specific |
| 2 | One provider or many? | **One upstream per deployment.** Model-prefix routing can be added later without rework |
| 3 | OAuth still the default? | **Yes.** No env vars set = today's exact behaviour |
| 4 | First-pass scope | **Base URL + static key, plus the two xAI leaks** (`extraModels` in `/v1/models`, MCP wording) |
| 5 | Test provider | **Local first** (free), then a free-tier hosted provider for the deployed check |

Consequence of decision 3: an existing xAI deployment is unchanged with no env vars
set, so this is safe to land without touching the running Worker.

**Tooling note:** no local model server is installed (`ollama`, `llama-server`, `vllm`
all absent), so §6 step 2 — a stub upstream — is the practical first target. It is
deterministic, needs no download, and still exercises SSE streaming.

---

## 8. Definition of Done — ALL MET, 2026-09-15

| Criterion | Status |
|---|---|
| No env vars set = identical behaviour (xAI OAuth, `api.x.ai/v1`) | Met — `defaultUpstreamBaseURL`, and the OAuth path is untouched while `UPSTREAM_API_KEY` is empty |
| `UPSTREAM_BASE_URL` alone repoints the upstream | Met — verified against the mock |
| `UPSTREAM_API_KEY` alone sends that key as the bearer, never refreshing | Met — zero HTTP calls during token resolution |
| Extra models not injected for non-xAI upstreams | Met — `grok-composer-2.5-fast` absent from the mock's `/v1/models` |
| `go_test`, `go_build`, `go_build_worker` pass | Met |
| The $0 path is documented and demonstrated | Met — via `tools/mock-upstream`, **not** Ollama, since none is installed. The mock is deterministic and exercises SSE streaming in a way a real model server would not do reproducibly |

One correction to the original wording: this criterion said "the $0 Ollama path".
The practical equivalent turned out to be the bundled mock, which needs no download
and is a permanent test fixture rather than an optional local install.

---

## 9. Post-completion audit — two defects fixed, 2026-09-15

An audit after this plan was marked complete found two defects that its tests could
not catch, because every test and mock run used xAI OAuth or a base URL of exactly
`/v1`. Both were reproduced against `tools/mock-upstream` before being fixed.

| # | Defect | Cause | Fix |
|---|---|---|---|
| 1 | **Grok OAuth tokens sent to other providers.** A non-xAI `UPSTREAM_BASE_URL` without `UPSTREAM_API_KEY` forwarded the stored subscription token as the bearer. On a 401 it refreshed against `auth.x.ai` and sent the new token too. Affected the Worker and the local proxy | §4 deliberately let `UPSTREAM_BASE_URL` apply "in both modes" | `currentAccessToken()` / `forceRefreshToken()` fail closed with `errOAuthUpstreamNotXAI`. Requests return 500 "Upstream misconfigured", and the OAuth admin and login routes and `auth` refuse via `oauthUnavailableReason()`. `/admin/status` reports `authMode: misconfigured` |
| 2 | **Local proxy doubled the version path** for base URLs not ending in exactly `/v1`: `…/openai/v1` produced `/openai/v1/v1/chat/completions`. `/v1/models` still worked, which hid it. The Worker was unaffected | §5.1 made the base URL configurable but left `main.go`'s single-host director join, which only worked for `/v1` | The local proxy now uses `upstreamRequestURL()`, the Worker's mapping |

Also fixed: OAuth browser routes and `grok-oauth-proxy auth` now refuse when OAuth is
unusable, an invalid `UPSTREAM_BASE_URL` fails at startup, and six tests that
depended on the ambient environment now pin it. Three of those already failed with
an exported `UPSTREAM_API_KEY` before this audit.

The regression tests (`TestOAuthCredentialsNeverSentToNonXAIUpstream`,
`TestLocalProxyWithholdsOAuthTokenFromNonXAIUpstream`,
`TestLocalProxyJoinsNonV1BasePath`) were confirmed to fail against the previous
logic.

The §4 statement "`UPSTREAM_BASE_URL` overrides the base URL in both modes" is
superseded: OAuth mode is xAI-only.
