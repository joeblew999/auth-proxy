# Plan: Make the proxy easy to use with many providers

**Status: PROPOSED, awaiting decisions in §6. 2026-09-15**

## 1. The problem, measured

Using this proxy with more than one provider is hard, and the repo is hard to find
your way around. Specifically:

| Area | Today |
|---|---|
| Providers | **One per deployment.** The Go code reads only `UPSTREAM_BASE_URL`, `UPSTREAM_API_KEY` and `ADMIN_API_KEY`. xAI and Groq cannot be used at the same time |
| Adding a provider | Overwrite the single `UPSTREAM_API_KEY` secret (which kills the xAI key), then edit `wrangler.toml` or deploy with `--var`, since no task sets the base URL. The previous provider stops working |
| Where config lives | Five places: `wrangler.toml`, Worker secrets, fnox, `--var` flags inside mise tasks, and `.plan` docs explaining them |
| mise tasks | **38**, with mixed prefixes (`go_`, `cf_`, `xai_`, `egress_test_`, `e2e_mock_`, `npm_`). They mix daily use, one-time setup, xAI-only chores, two throwaway experiments, and publishing. `cf_deploy_mock_upstream` redeploys **production** pointed at a mock |
| Go layout | Two programs in one `package main`, split by build tags: `main.go` (local, 342 lines) and `worker.go` (Cloudflare, 209 lines). They proxy differently: `httputil.ReverseProxy` versus a hand-rolled fetch, which is how the path bug fixed today affected only one of them |
| Shared state | Package globals (`runtimeStore`, `runtimeClient`, `stateVerifiers`, `isAuthMode`), and env vars read at call time from several files. Every test has to pin the environment |
| Naming | Everything says Grok (`ask_grok`, `GROK_AUTH`, `GROK_BASE_URL`, the module name), even when talking to other providers |
| Leftovers | `egress-test/` and `e2e-mock-provider/` are throwaway experiments, still tracked with their own tasks |

## 2. What using it should look like

A single base URL and a single client key. The model name picks the provider.

```bash
# the client picks the provider through the model name
curl $PROXY/v1/chat/completions -H "Authorization: Bearer $KEY" \
  -d '{"model":"groq/llama-3.3-70b-versatile","messages":[...]}'
curl $PROXY/v1/chat/completions ... -d '{"model":"xai/grok-4.3", ...}'   # via Grok OAuth
curl $PROXY/v1/models ...   # every provider's models, prefixed
```

Adding a provider becomes three steps, and nothing else breaks:

```toml
# providers.toml: committed, contains no secrets
[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key      = "GROQ_API_KEY"          # name of the secret, never the value
```
```bash
mise run keys:set groq     # prompts, stores in fnox, pushes to the Worker
mise run deploy
```

The same `providers.toml` drives the local proxy and the Worker.

### 2.1 Rule: nothing to remember

Every design choice below is judged against this rule. A user should never need to
recall a command, a variable name, or a hidden step.

- **`mise tasks` lists everything** (about 12 tasks, each with a one-line
  description). No other entry point exists: no raw `wrangler` or `fnox` commands in
  the docs.
- **`mise run status` says what is wrong and prints the exact fix.** For example:
  `groq: key GROQ_API_KEY missing on Worker → mise run keys:set groq`.
- **Every error names its fix.** Config validation, a missing key, an OAuth refusal:
  the message says which provider, what is missing, and the command that fixes it.
- **Arguments are declared, not memorised.** Tasks take real arguments
  (`mise run chat <model> [prompt] --worker`), so `mise run chat` with none prints
  usage instead of failing silently.
- **One file of config.** Provider names, URLs and secret names live in
  `providers.toml` only. Secret values live only in fnox and are pushed by tasks.
- **Safe by default.** No task can deploy test configuration over production.

## 3. Design

### 3.1 Provider config: `providers.toml`

```toml
default = "xai"                       # used when a model has no prefix

[providers.xai]
base_url = "https://api.x.ai/v1"
auth     = "xai-oauth"                # the Grok subscription login; valid only for api.x.ai

[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key      = "GROQ_API_KEY"

[providers.openrouter]
base_url = "https://openrouter.ai/api/v1"
key      = "OPENROUTER_API_KEY"
headers  = { "HTTP-Referer" = "https://github.com/joeblew999/grok-oauth-proxy" }

[providers.cloudflare]                 # Cloudflare AI Gateway works as just another provider
base_url = "https://api.cloudflare.com/client/v4/accounts/<account_id>/ai/v1"
key      = "CLOUDFLARE_AI_TOKEN"      # Cloudflare API token with AI Gateway Run
```

Cloudflare's own model IDs already contain a slash (`openai/gpt-5.5`), so only the first
segment is treated as the prefix: `cloudflare/openai/gpt-5.5` → `openai/gpt-5.5`. The
REST API above is Cloudflare's recommended OpenAI-compatible endpoint. The older
`gateway.ai.cloudflare.com/.../compat` endpoint is deprecated for single-model calls,
but still required for AI Gateway dynamic routes; it authenticates with a
`cf-aig-authorization` header, which the provider `headers` field can carry.

- It is embedded into both binaries with `go:embed`, so the Worker needs no KV or vars
  for config.
- It is validated once at startup: unknown `auth`, a missing key secret, an `xai-oauth`
  provider not on `api.x.ai`, or an invalid URL fails immediately with the provider's
  name in the message.
- Keys are always referenced by secret name. Values come from fnox locally and from
  Worker secrets on Cloudflare, through the existing `getenv` abstraction.
- Backward compatibility: when there is no `providers.toml`, the current
  `UPSTREAM_BASE_URL` / `UPSTREAM_API_KEY` vars produce a single `default` provider,
  so dvcrn-style setups keep working.

### 3.2 Routing

- `provider/model` selects the provider, and the prefix is stripped before forwarding
  (`groq/llama-3.3-70b` → `llama-3.3-70b` to Groq).
- A model with no prefix goes to `default`, which is today's behaviour.
- Optional `[aliases]`, e.g. `fast = "groq/llama-3.1-8b-instant"`.
- `/v1/models` fetches every provider concurrently, prefixes the IDs, and merges them.
  A failing provider is logged and skipped, not fatal.
- The xAI-only extras (`grok-composer-2.5-fast`, the MCP wording) attach to the
  `xai-oauth` provider rather than to a hostname check.

### 3.3 Go structure

Replace "two programs in one `package main`" with one handler and two thin entry
points:

| Package | Owns |
|---|---|
| `internal/config` | Loading and validating `providers.toml` into a `Config` struct; the only place env is read |
| `internal/router` | Model → provider resolution, URL joining, header and auth injection, `/v1/models` merge |
| `internal/xaiauth` | Grok OAuth: PKCE browser login, device flow, refresh, and the token store interface |
| `internal/proxy` | The HTTP handler: client-key check, routing, streaming. **One implementation for both runtimes** |
| `internal/mcp` | Tools `ask` and `list_models`, taking a model with a provider prefix; `ask_grok` / `ask_grok_models` kept as aliases |
| `main.go` | Local entry point: file token store, `net/http` client, listen |
| `worker.go` | Worker entry point: KV token store, fetch client, `workers.Serve` |

Dependencies are passed in explicitly, with no package globals, so tests build a
`Config` directly instead of setting env vars. The HTTP client interface is the only
thing that differs per runtime; `httputil.ReverseProxy` goes away, so the local proxy
and the Worker can no longer drift apart.

### 3.4 mise tasks: 38 → about 12

| Task | Does |
|---|---|
| `dev` | Local proxy plus mock upstream, with a test provider config |
| `test` | `gofmt` check, `go vet` for native and wasm, `go test ./...` |
| `build` | Local binary plus the Worker (Go); `build --tinygo` also builds the TinyGo Worker and reports sizes |
| `deploy` | Build and deploy the Worker |
| `keys:set <provider>` | Prompt, store in fnox, push that provider's key to the Worker |
| `keys:push` | Push every key named in `providers.toml` (after rotating, or on a new machine) |
| `login` | Grok OAuth, local (browser) or `--worker` (device flow, polls until done) |
| `status` | Per provider: configured, key present, reachable, model count |
| `chat <model> [prompt]` | Smoke test any provider through the proxy, local or deployed |
| `logs` | `wrangler tail` |
| `setup` | One-time: KV namespace, admin key, prints what is still missing |
| `release` | goreleaser plus the npm package (was `publish` / `npm_publish`) |

Removed:
- The `egress_test_*` and `e2e_mock_*` tasks and their directories. The mock upstream
  stays as a Go test fixture.
- `cf_deploy_mock_upstream`, which overwrote production. End-to-end tests deploy a
  separate `e2e` environment instead.
- `xai_console` and `xai_check_key`, replaced by `status`.
- `cf_verify_*`, replaced by `status` and `chat`.
- `cf_secret_*`, replaced by `keys:*`.
- The duplicate aliases.

Arguments use mise's `usage` spec, so `mise run chat groq/llama-3.3-70b "hi"` works
and `mise tasks` stays readable.

Verified 2026-09-15 on mise 2026.9.8: `usage` arguments, optional arguments with
defaults, `--flags`, and `name:sub` task names all work, and a missing required
argument fails with an error. `go:embed` of a TOML file also works for the Go wasm,
TinyGo wasm, and local builds, so embedding `providers.toml` is safe for both Worker
toolchains.

### 3.5 Docs

- **README:** a quick start of what it is, the three steps from §2, and a client setup
  example. Provider, Workers and MCP details move below the fold.
- **AGENTS.md:** the package map and the rule that config is read only in
  `internal/config`.
- `COSTS.md` and `.plan/done/*`: kept as history, and no longer linked from code
  comments or tasks.

## 4. Phases

Each phase ships on its own with tests green, and nothing is deployed until phase 5.

| # | Phase | Result |
|---|---|---|
| 0 | **Done 2026-09-15.** Commit the provider fixes (OAuth token withheld from non-xAI hosts, path join) together with this plan | A clean base |
| 1 | `internal/config` and `providers.toml`, with the env-var fallback | One place for config; existing setups unchanged |
| 2 | `internal/router`, `internal/proxy` and a unified handler; delete `httputil.ReverseProxy` and the globals | Many providers at once; local and Worker behave identically |
| 3 | `internal/xaiauth` and `internal/mcp` (`ask`, `list_models` plus aliases) | The Grok parts become one provider among others |
| 4 | Rewrite the mise tasks, delete the leftovers, rewrite the README and AGENTS.md | About 12 tasks and a short quick start |
| 5 | Deploy; verify xAI (static key), one keyed provider, and the mock through `status` and `chat` | Proven on Cloudflare |

Tests are carried forward package by package. The regression tests added today (OAuth
token withheld from non-xAI hosts, `/openai/v1` joining, and so on) move into
`internal/router` and `internal/proxy` and must stay green through every phase.

## 5. Out of scope

- Per-client API keys, rate limits and spend caps. The single admin key stays until
  this redesign lands. (The live Worker returns 401 on every route without the key;
  checked 2026-09-15.)
- Fallbacks and load balancing across providers. If wanted later, a provider entry
  pointing at an AI Gateway dynamic route (the `/compat` endpoint, see §3.1) gets
  them without code.
- Non-OpenAI request formats (Anthropic `/v1/messages`, Gemini native).

## 6. Decisions needed

| # | Question | Recommendation |
|---|---|---|
| 1 | How clients pick a provider | `provider/model` prefix, plus optional aliases |
| 2 | Where provider config lives | `providers.toml` committed and embedded at build. The alternative is KV, editable at runtime through the admin API, which needs no redeploy but is harder to review |
| 3 | Relationship with dvcrn upstream | Accept that the fork diverges. The env-var fallback keeps their setup working; offer them the router later as a PR if they want it |
| 4 | MCP tool names | Add `ask` / `list_models` and keep `ask_grok` / `ask_grok_models` as aliases, so existing clients do not break |
