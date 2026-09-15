# Cloudflare Workers Deployment Plan

| | |
|---|---|
| **Status** | COMPLETE — infrastructure done and verified. §5.5 remains, blocked on an **external** xAI account entitlement issue, not on code. Continued in `.plan/any-provider-support.md` |
| **Created** | 2026-09-15 |
| **Repo** | `joeblew999/grok-oauth-proxy` (fork of `dvcrn/grok-oauth-proxy`) |
| **Goal** | Access Grok from anywhere via Cloudflare Workers |

---

## 1. Objective

Deploy `grok-oauth-proxy` to Cloudflare Workers so an OpenAI-compatible endpoint is
reachable from any device (phone, CI, remote machines) without running anything
locally.

**Hard constraint:** no always-on machine is available. This collides directly with
the current upstream design — see Section 3.

---

## 2. Current State

### 2.1 Completed

| Item | Detail |
|---|---|
| Fork created | `joeblew999/grok-oauth-proxy`, **public** (accepted risk) |
| Local clone | `~/workspace/go/src/github.com/joeblew999/grok-oauth-proxy` |
| `origin` | fork (push target) |
| `upstream` | `dvcrn/grok-oauth-proxy` (pull-only) |
| Fix committed | `20ed7c6` — `mise run` task `go run main.go` → `go run .` |
| Divergence | fork is 1 commit ahead of `upstream/main` (`041025f`) |
| Fresh clone build | `mise run build` OK, produces `bin/grok-oauth-proxy` (12.6 MB) |

The `mise run` task was broken because `go run main.go` compiled only `main.go`,
leaving symbols defined in sibling package files (`AuthTokens`, `clientID`,
`requestTokens`, `saveTokens`, `ensureAccessToken`, `isModelsListRequest`)
undefined. Building the package path fixes it.

### 2.2 Toolchain (installed globally via mise)

| Tool | Version | Note |
|---|---|---|
| node | 24.19.0 | |
| npm | 11.17.0 | |
| wrangler | 4.97.0 | Wrangler 4 required by upstream README |
| cloudflared | 2026.9.1 | must be ≥ 2025.7.0 |
| go | 1.27.1 | repo pins `go = "1.27"`; README's "1.25.5 or newer" is now stale |

`node`, `wrangler`, and `cloudflared` shims previously errored with
`No version is set for shim` — all three are now pinned globally.

### 2.3 Cloudflare Account

| Field | Value |
|---|---|
| Email | `gedw99@gmail.com` |
| Account ID | `7384af54e33b8a54ff240371ea368440` |
| Auth | Wrangler OAuth token, working |

Token scopes relevant to this project include `workers (write)`, `workers_kv (write)`,
and **`connectivity (admin)`** — the last is required to bind a tunnel directly to a
Worker. This is a common blocker and is already satisfied.

---

## 3. The Blocker: Mandatory VPC Egress

`worker.go` calls this unconditionally from `main()`:

```go
func newWorkersHTTPClient() *workersHTTPClient {
	binding := cloudflare.GetBinding("GROK_EGRESS")  // no nil check, no fallback
	namespace := js.Global().Get("Object").New()
	namespace.Set("fetch", binding.Get("fetch").Call("bind", binding))
	...
}
```

This client is registered via `configureRuntime(store, client)` as `runtimeClient`,
and **every** upstream call (`sendWorkerUpstreamRequest`) plus all OAuth traffic goes
through it. There is no direct-fetch code path.

`wrangler.toml` correspondingly declares:

```toml
[[vpc_networks]]
binding = "GROK_EGRESS"
tunnel_id = "ce0561f2-73c2-486f-98ee-1a2f2e72ffa8"
```

### Consequence

```
Client ──► Worker (Cloudflare) ──► GROK_EGRESS ──► cloudflared ──► api.x.ai
                front door            forced          YOUR BOX        upstream
```

**Workers mode as shipped still requires an always-on box running `cloudflared`.**
That box needs outbound UDP 7844 (QUIC) and no inbound ports or public IP.

### Why was VPC added?

Not documented anywhere. The most likely reason is that direct Workers egress to
`api.x.ai` is blocked or degraded — otherwise the Worker would be self-contained.
This is an inference from design, **not** a confirmed fact, and it is directly
testable (Section 4).

---

## 4. Decision Point: Test Direct Egress

Before acquiring any always-on infrastructure, verify empirically whether the VPC
tunnel is actually necessary.

**Procedure:** deploy a throwaway Worker that fetches `https://api.x.ai/v1/models`
with no credentials and reports the status code plus raw response.

**Interpretation:**

| Result | Meaning | Action |
|---|---|---|
| `401` (or any well-formed auth error) | Egress works fine | Drop the VPC binding, patch the client to fetch directly. No box needed — goal achieved |
| `403`, TLS reset, timeout, or empty | Egress genuinely blocked | VPC tunnel is mandatory. Proceed to Section 5.2 |

### 4.1 Result — RESOLVED 2026-09-15

**Direct Workers egress to `api.x.ai` works. The VPC tunnel is not required.**

Probe: the throwaway Worker in `egress-test/`, deployed to
`https://grok-egress-test.gedw99.workers.dev` with **no** `vpc_networks` binding
(`bindingPresent: false`).

| Target | Via | Status | Body |
|---|---|---|---|
| `api.x.ai/v1/models` | direct `fetch` | **401** | `{"code":"unauthenticated:no-credentials","error":"No credentials presented."}` |
| `example.com` (control) | direct `fetch` | 200 | HTML |
| `api.openai.com/v1/models` (control) | direct `fetch` | 401 | `Missing bearer authentication in header` |

The 401 body is xAI's own well-formed JSON error, which proves DNS, TCP, TLS, and
HTTP all completed end-to-end with no intermediary interference. Took 202 ms from
colo `BKK`. The two controls rule out a general Workers egress fault — so the
result is attributable to `api.x.ai` being reachable, not to egress working by luck.

Note that the xAI response carries `server: cloudflare`; `api.x.ai` is itself
fronted by Cloudflare, so this request never leaves Cloudflare's network. That is
the likely mechanical reason the tunnel is unnecessary. It is also a plausible
reason upstream added one anyway: an always-on `cloudflared` provides a stable,
non-Cloudflare egress identity should xAI ever begin filtering Cloudflare source
ranges, and it keeps the option open.

**Consequence for Section 3:** the blocker is cleared. `newWorkersHTTPClient` can
use plain `fetch`, the `vpc_networks` block can be dropped, and no always-on box is
needed. Section 2.1's "no always-on machine available" constraint is therefore no
longer in conflict with the design.

**Caveat:** this probe measures reachability and a single short request, not
sustained throughput or long-lived streaming. Confirm with a real streaming chat
completion (Section 5.5) before treating the tunnel as permanently dead weight.

> **Superseded — see 4.1.** Retained only in case direct egress ever regresses.

**Fallback if egress is blocked** (cheapest first):

| Option | Cost | Note |
|---|---|---|
| Oracle Cloud free tier | $0 | Always-on ARM instance |
| Raspberry Pi / spare NUC | one-off | Runs `cloudflared` only |
| Cheapest VPS | ~$3–5/mo | Boring and reliable |
| Existing laptop | $0 | Breaks whenever it sleeps |

**Design note:** if the Worker is deployed, the always-on box runs *only*
`cloudflared`. Skipping the Worker means that box must run the proxy binary, manage
OAuth token refresh, and expose an endpoint. The Worker still earns its keep.

**Upstream-design caveat:** `AGENTS.md` states Workers provider and OAuth traffic
*must* use the `GROK_EGRESS` VPC binding. Bypassing it diverges from the intended
architecture. Acceptable for a personal fork; do not upstream such a change.

---

## 5. Remaining Work

### 5.1 Resolve egress (Section 4) — DONE

Resolved in §4.1: direct egress works, so this was never the blocking dependency it
was assumed to be. No tunnel, no VPS, no `cloudflared`.

### 5.2 Configure `wrangler.toml` — DONE 2026-09-15

All three maintainer identifiers are replaced with account-local values:

| Field | Value now in `wrangler.toml` |
|---|---|
| `account_id` | `7384af54e33b8a54ff240371ea368440` |
| `kv_namespaces[0].id` | `da13e11270bb4d749c73c60dad41c1ea` (created 2026-09-15) |
| `vpc_networks[0]` | **removed** — see §4.1 |

The tunnel step is gone entirely: nothing to create, no `cloudflared` to run.

**Credential path:** Cloudflare auth comes from **fnox**
(`~/.config/fnox/config.toml`), not from `wrangler login` state. fnox supplies
`CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, and the Access client pair.
Every Cloudflare task in `mise.toml` is wrapped in `fnox exec --`, so the tasks
are non-interactive and leave no credentials on disk.

**Gotcha hit during this step:** `CLOUDFLARE_ACCOUNT_ID=<id> wrangler kv namespace
create` failed with `Authentication error [code: 10000]`. The wrangler log showed
it had used the maintainer's `account_id` from `wrangler.toml`; the environment
variable did **not** override the config file. Fix the config first, then create
resources.

### 5.3 Build and deploy — DEPLOYED 2026-09-15

```bash
mise run cf_deploy       # wraps: fnox exec -- wrangler deploy
mise run cf_deploy_dry   # same, with --dry-run, no upload
```

Deployed to **https://grok-oauth-proxy.gedw99.workers.dev**, version
`d9dd8cc1-9e00-4fcd-b1ac-bc74a7e2ce0a`, with `env.GROK_AUTH` as the only binding.

`mise run cf_deploy` needs no `depends` chain: `wrangler.toml`'s
`[build] command = "mise run go_build_worker"` fires automatically before
bundling, confirmed in the deploy log.

Verified live:

| Route | Result | Meaning |
|---|---|---|
| `/health` | 200 `{"status":"ok"}` | Worker boots with **no** `GROK_EGRESS` binding — the optional-binding fallback in `worker.go` works |
| `/v1/models`, `/` | 500 `AdDONE

Set from fnox and verified: unauthenticated `/v1/models` moved from 500
(`Admin API not configured`) to 401, so the middleware now enforces the key.

Note for anyone repeating this: `wrangler secret put` reads the value from **stdin**,
and its hidden TTY prompt fails inside the `mise` → `fnox` layers. The
`cf_secret_admin_key_sync` task therefore pipes the value instead.ot configured` | `adminMiddleware` fails closed when `ADMIN_API_KEY` is unset |

### 5.3b Set the admin key — REMAINING

Either set it once, by hand:

```bash
mise run cf_secret_admin_key    # prompts for the value on stdin
```

Or, preferred, store it in fnox first so the step is reproducible and the value
never lands in shell history or the repo:

```bash
fnox set ADMIN_API_KEY -g       # hidden interactive prompt
mise run cf_secret_admin_key_sync
```

`wrangler secret put` reads the value from **stdin**, not from the environment, so
the sync task pipes it rather than relying on `fnox exec` alone. The value is
deliberately absent from the repo, `mise.toml`, and this plan.

### 5.4 Authenticate against xAI (device flow) — IN PROGRESS

```bash
BASE_URL="https://grok-oauth-proxy.gedw99.workers.dev"
curl -X POST "$BASE_URL/admin/auth/start" -H "Authorization: Bearer $ADMIN_API_KEY"
```

`POST /admin/auth/start` returned 200:

| Field | Value |
|---|---|
| `verificationUrl` | `https://accounts.x.ai/oauth2/device?user_code=Q8D3-SPRR` |
| `userCode` | `Q8D3-SPRR` |
| `expiresAt` | `2026-09-15T05:57:33.866Z` |
| `retryAfterSeconds` | 5 |

**This is stronger evidence than the §4.1 probe.** Serving `/admin/auth/start`
required the Worker to make a genuine outbound OAuth request to `accounts.x.ai`
and get a device code back. So the tunnel-free path carries real authenticated xAI
traffic, not just an unauthenticated `GET`. The egress decision holds.

Also confirmed in the same step: unauthenticated `/v1/models` moved from 500 to
**401**, so `ADMIN_API_KEY` is live and `adminMiddleware` now enforces it.

Remaining: open `verificationUrl`, approve, then poll `POST /admin/auth/status` no
faster than `retryAfterSeconds`. Stop on `authenticated`, `denied`, `expired`, or
`failed`. Confirm with `GET /admin/status`.

### 5.5 Verify from a remote client — RESOLVED, see §5.5a

`GET /v1/models` succeeded once, returning live xAI data
(`grok-4.20-0309-non-reasoning`, `grok-4.20-0309-reasoning`, real pricing fields,
`owned_by: xai`). That was a genuine live upstream response, not a fixture:
`handleModelsList` calls `apiURL + "/models"` on every request and merges the body
via `mergeModelsList`, with no caching layer.

**The identical call returned `403` minutes later**, carrying the entitlement error
below. So the account flipped from permitted to blocked mid-session, and blocked is
now the consistent state. Note the status code varies by route for the same error
code: `402` on `/v1/chat/completions`, `403` on `/v1/models`.

```json
{"code":"personal-team-blocked:spending-limit",
 "error":"You have run out of credits or need a Grok subscription. Add credits at
 https://grok.com/?_s=usage or upgrade at https://grok.com/supergrok."}
```

**This is not an egress failure.** Only xAI could produce that response, so the
request reached `api.x.ai`, was authenticated, and was rejected on entitlement
rather than identity (an invalid token would return 401). Egress is confirmed four
times over: unauthenticated probe, OAuth device handshake, authenticated model list
(200), and these entitlement rejections.

The blocker has moved from infrastructure to account entitlement — and is now
**confirmed**. The xAI account page for `gedw99@gmail.com` offers "Get SuperGrok",
so that account holds no subscription. That, not a spending-limit artefact, is the
cause of `personal-team-blocked:spending-limit`.

| Possibility | Status |
|---|---|
| No SuperGrok subscription on the authorising account | **Confirmed — this is the cause** |
| Credits exhausted, or a spending limit of 0 on a personal team | Ruled out — there is no plan to hold a limit |
| Wrong xAI account authorised during the device flow | Ruled out — `gedw99@gmail.com` is the intended account |

The cheap escape is already built: buy **API credits** instead of subscribing, then
set `UPSTREAM_API_KEY` on the Worker (`mise run cf_secret_upstream_key`) so the proxy
uses a static key and skips OAuth. API credits bill separately from the consumer
subscription, so no $30/mo commitment is needed to test. See
`.plan/any-provider-support.md`.

### 5.5a RESOLVED 2026-09-15 — running on API credits

The entitlement block was bypassed rather than paid through. With `UPSTREAM_API_KEY`
set on the Worker, the OAuth path is skipped entirely and requests authenticate with a
console.x.ai API key billed against API credits.

Verified live against **real xAI**:

| Check | Result |
|---|---|
| `/admin/status` | `{"authMode":"static-key","configured":true}` |
| `GET /v1/models` | Live model list from `api.x.ai` |
| Streaming `POST /v1/chat/completions` | Real completion: `"content":"OK"`, then `finish_reason:"stop"`, then `[DONE]` |

**This is the first request the whole setup has completed against real xAI** — from a
Cloudflare Worker, with no VPC tunnel, no `cloudflared`, and no always-on host. $5 of
API credits was enough to establish it.

The OAuth path remains blocked and still needs a subscription. That is now a
documented, deliberate choice rather than an unresolved problem — see `COSTS.md` for
the two separate credit systems.

Consequence: **the streaming-SSE question stays open**, but for an entitlement reason
rather than a transport one. It cannot be closed until a completion succeeds.

---

## 6. Risks and Open Questions

| Risk | Impact | Mitigation |
|---|---|---|
| VPC tunnel is mandatory | Needs always-on box; goal partially unmet | Test first (Section 4) |
| Fork is **public** | Cloudflare IDs published if committed | Accepted by owner; keep secrets out regardless |
| Fork tracks a moving upstream | Merge conflicts on future pulls | Keep local changes minimal and isolated |
| `/v1/chat/completions` is buffered, not SSE | No streaming responses | Known upstream limitation |
| Device flow is single-account | No multi-user isolation or rate limiting | Fine for personal use |
| Subscription-auth approach is structurally fragile | Breaks if xAI changes OAuth handling | No mitigation available |

**Open question:** whether the VPC tunnel was added for a transient reason that no
longer applies. Worth checking upstream issues/commits before committing to a box.

---

## 7. Hard Rules

- **Never commit** `ADMIN_API_KEY` or OAuth tokens. Use `wrangler secret put`.
- Keep `ADMIN_API_KEY` out of logs, shell history, and this plan.
- Run `mise run format`, `mise run test`, and `mise run build` after Go changes.
- Use `mise run build-worker` for the Cloudflare Workers WASM build.
- Use the repo's npm lockfile and npm for changes under `npm/`.

---

## 8. Reference: Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `POST /v1/chat/completions` | admin key | OpenAI-compatible completions (buffered) |
| `GET /v1/models` | admin key | Upstream models + proxy-injected entries |
| `POST /mcp` | admin key | Stateless MCP: `ask_grok`, `ask_grok_models` |
| `POST /admin/auth/start` | admin key | Begin xAI device authorization |
| `GET`/`POST /admin/auth/status` | admin key | Poll device flow / store tokens |
| `POST /admin/tokens` | admin key | Manual token injection |
| `GET /admin/status` | admin key | Credential configuration check |
| `GET /health` | admin key | Workers health check |
| `GET /login`, `GET /callback` | **none** | Local browser OAuth only (loopback) |

Admin key accepted as bearer token, `X-API-Key` header, or `?key=` query param.

### Notes for later

- `grok-composer-2.5-fast` is **not** an upstream model. It is injected by
  `models.go` (`extraModels`) into `/v1/models`. `mergeModelsList` de-dupes, so if
  xAI adds it natively the local entry is skipped.
- `ask_grok` is **stateless and one-shot** — no conversation history. The prompt must
  carry all context.
- `ask_grok` returns both `model` and `requested_model`; they can differ when xAI
  resolves an alias.
