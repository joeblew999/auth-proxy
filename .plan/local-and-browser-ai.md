# Plan: one app for remote, local and browser AI

**Status: PROPOSED, facts checked 2026-09-15, awaiting the decisions in §7.**
Replaces an earlier draft that split the proxy and the GUI into two repos over
tooling differences. Both are Go compiled to wasm on Cloudflare through
workers-go, so they are one app.

## 0. Requirements from the owner

- Models run in three places: **remote** providers, a **local** server (kronk,
  built on yzma), and **in the browser** (yzma compiled to wasm, as in
  hybridgroup/yzma-wasm-example).
- **No mess:** one smart, unified design rather than parallel code paths.
- **The GUI runs on Cloudflare**, not locally. It uses **gsx and gsxui**, and not
  yzma-wasm-example's own GUI. A Durable Object keeps every user updated live
  while chatting with the AI.
- **wrangler** deploys and runs everything.
- **mise installs the CLIs and the skills.** Skills go in the repo, never
  globally. **The AI must load and use them:** last time in go-htmx4 the gsx and
  gsxui CLIs were not used, because the skills were not loaded, and the GUI came
  out wrong.

## 1. The idea

**One Go codebase, one routing rule, one set of handlers, three runtimes.**

| Runtime | Built with | Runs | Reaches |
|---|---|---|---|
| **Cloudflare Worker** | Go or TinyGo, deployed by wrangler | the GUI, the chat, the OpenAI API, MCP, admin | remote providers |
| **Browser Service Worker** | TinyGo | the same chat handlers, intercepting chat requests for in-browser and local models | the in-browser model, and the user's own machine |
| **Native binary** | Go | the same handler on the user's machine | local providers such as kronk |

Every request is routed the way the proxy routes today, by the model's provider
prefix. A provider now also says **where it can be reached**:

```toml
[providers.xai]                     # remote: served by the Cloudflare Worker
base_url = "https://api.x.ai/v1"
key = "XAI_API_KEY"

[providers.kronk]                   # local: reached from the browser or the native binary
base_url = "http://127.0.0.1:11435/v1"
auth = "none"
runtime = "local"

[providers.browser]                 # in the page: yzma wasm in a Web Worker
runtime = "browser"
models = ["unsloth/Qwen3-0.6B-GGUF/Qwen3-0.6B-Q8_0.gguf"]
```

The same `providers.toml` is built into all three runtimes, so the model list,
the prefixes and the error messages are identical everywhere.

## 2. How a chat message flows

The user is on the GUI served by Cloudflare and presses Send with model `M`. htmx
posts to `/chat/send`.

1. **The Service Worker sees the request first.** It runs the shared router on
   `M`:
   - **remote provider:** it does nothing, and the request goes to Cloudflare.
     The Worker's chat handler calls the provider and streams tokens back.
   - **browser provider:** the Service Worker runs the **same chat handler** in
     TinyGo, with the in-browser engine. It returns the same gsx-rendered
     fragments, so the page cannot tell the difference.
   - **local provider:** the Service Worker runs the same handler and calls
     `127.0.0.1` with `fetch(…, {targetAddressSpace: "local"})`. Chrome asks the
     user once for Local Network Access.
2. **Everyone in the room sees the finished message.** The handler publishes it to
   the room's Durable Object, which pushes it to every connected browser over
   hibernating WebSockets.

### Two engines, not three

| Engine | Used for | Where it runs |
|---|---|---|
| **OpenAI HTTP** (today's `proxy.Upstream`) | remote and local providers; kronk already speaks the OpenAI API | Worker (fetch), native (net/http), Service Worker (fetch) |
| **yzma wasm** | browser providers | a dedicated Web Worker in the page, driven by the Service Worker |

The OpenAI API (`/v1`), MCP and the GUI chat are all adapters over those two
engines. None of them knows where a model runs.

## 3. Facts behind it

Checked 2026-09-15.

- **kronk** v1.32.6: OpenAI-compatible (`/v1/chat/completions` with SSE,
  `/v1/models`), default `127.0.0.1:11435`, no key in open mode, no cgo, mise
  installs it (`go:github.com/ardanlabs/kronk/cmd/kronk`, Go 1.27). Model IDs
  contain slashes; the router already splits on the first one only.
- **yzma** v1.27.0: llama.cpp through purego/ffi, no cgo, GGUF only.
  `pkg/llama` (native) and `pkg/llamawasm` (`js && wasm`) have the same function
  names.
- **yzma-wasm-example:** llama.cpp compiled to wasm with Emscripten (about 13 MB),
  driven by TinyGo from a dedicated Web Worker. Backends: WebGPU (Chrome/Edge 137+),
  multi-threaded CPU (needs COOP/COEP), or single-threaded CPU. No OpenAI API,
  only JS globals and `postMessage` tokens. Models of 220–770 MB come from Hugging
  Face; whether they stay cached is unverified.
- **Go in a Service Worker:** nlepage/go-wasm-http-server runs a Go
  `http.Handler` in a Service Worker and supports TinyGo (410 stars, MIT, no
  releases, last push 2026-04). The handler code is the same `net/http` code the
  Worker runs.
- **WebGPU in Service Workers:** available since Chrome 124. yzma's
  SharedArrayBuffer threads inside a Service Worker are unverified, so inference
  stays in a dedicated Web Worker, where yzma-wasm-example already proves it.
- **Browser to localhost:** since Chrome 142, a public site calling a loopback
  address triggers a Local Network Access permission prompt. Annotate the call
  with `targetAddressSpace: "local"`; it is exempt from mixed-content blocking.
  Whether kronk sends CORS headers is unverified, and the native binary can add
  them if it does not.
- **gsx** v0.1.1 and **gsxui** (no releases; pin a commit): `go install` only, so
  mise's `go:` backend. gsx ships `skills/gsx/SKILL.md` and
  `skills/templ-to-gsx-migration/SKILL.md`. gsxui ships no skills, and its CLI
  (`gsxui add`) is how components get into the repo.
- **Claude Code skills** load from `.claude/skills/<name>/SKILL.md` in the repo. A
  skills directory created during a session appears only after a restart.
- **workers-go** v0.35.0 cannot define Durable Objects in Go (only unmerged
  PR #219) and has no WebSockets. So the room Durable Object is JavaScript, as
  in go-htmx4, and wrangler's `main` is a small JS entry that exports it and
  forwards everything else to Go. Go SSE streaming works.
- **go-htmx4** has what the GUI needs (gsx views, gsxui components, the JS `Room`
  with hibernation, `kit/live` publishing, `kit/httpx`), but no AI chat and no
  skills. Its `kit/` packages are designed to be imported.
- **TinyGo limits for shared code:** pre-Go 1.22 `ServeMux` (plain paths only),
  `html/template` panics, and reflection needs a runtime test. The proxy already
  builds and runs under TinyGo, except MCP.

## 4. Code layout

One module, this repo. The app outgrows the name `grok-oauth-proxy`; see §7.

| Path | Owns | Runtimes |
|---|---|---|
| `providers.toml` | providers, with `runtime` = remote (default), `local` or `browser` | all |
| `internal/config`, `internal/router` | as today, plus `runtime` | all |
| `internal/engine/openai` | today's `proxy.Upstream`: HTTP to OpenAI-compatible providers | all |
| `internal/engine/yzma` | in-browser inference over a Web Worker | browser only (`js && wasm && browser`) |
| `internal/api` | the `/v1` OpenAI API (today's `internal/proxy`) | Worker, native |
| `internal/mcp` | `ask`, `list_models` | Worker (standard Go), native |
| `internal/xaiauth` | Grok login | Worker, native |
| `internal/chat` + `views/` | chat pages, send and stream handlers, gsx components from `gsxui add` | all three |
| `worker/index.mjs`, `worker/room.mjs` | wrangler entry and the room Durable Object (JS until PR #219) | Worker |
| `web/sw.js`, `web/engine.js` | loaders only: start the TinyGo Service Worker and the yzma Web Worker | browser |
| `main.go`, `worker.go`, `cmd/sw/` | entry points: native, Cloudflare, Service Worker | one each |

**One deploy:** `mise run deploy` builds the Go Worker and the TinyGo Service
Worker, then runs `wrangler deploy`. Static Assets hold the gsxui CSS, the Service
Worker wasm and yzma's llama.cpp wasm. COOP/COEP headers apply only to the chat
page.

## 5. Works for every developer, with nothing manual

Every developer uses **mise** and **fnox**; nothing else is assumed. A new
developer runs **`mise install`** and then **`mise run setup`**, and that is the
whole setup. Nothing in the repo belongs to one person, and every step below
is a task or a hook, never a README instruction. CI proves it on a fresh machine.

### 5.1 No personal values in the repo

Checked 2026-09-15: the repo currently hard-codes the owner's account ID and KV
namespace ID in `wrangler.toml` and the owner's `*.gedw99.workers.dev` URLs in
`mise.toml`. All of that goes.

| Value | From now on |
|---|---|
| Cloudflare account | `CLOUDFLARE_ACCOUNT_ID`, provided by fnox; wrangler reads it from the environment |
| KV namespace, D1, R2 | bindings **without IDs**. Wrangler (4.45+) creates them per account on deploy. Whether it writes IDs back into the committed config needs verifying, and if it does, the deployed config is generated into a gitignored file from a committed template |
| Worker URL | computed by `setup` from the Worker name and the account's workers.dev subdomain (Cloudflare API), stored in gitignored `mise.local.toml` |
| Secrets | fnox, as today. Each developer keeps their own values; `setup` prompts for any that fnox does not have |
| Worker name | a per-developer name for development and one shared production name, see §7 |

### 5.2 Skills: automated, committed, enforced

Last time the AI ignored the gsx and gsxui CLIs because the skills were not
loaded. So:

1. **Pinned tools:** `mise.toml` pins gsxui (`go:` backend, by commit) and
   standalone Tailwind. gsx is pinned in `go.mod` as a tool.
2. **Automatic sync:** a mise `postinstall` hook runs the hidden `skills:sync`
   task on every `mise install`. It copies gsx's `skills/*` at the pinned version
   into the repo's `.claude/skills/`. **Never global.**
3. **Committed:** the skills are in git, so a fresh clone has them before the first
   Claude Code session. No developer ever has to restart to load them; only the
   very first creation of the directory, done once here, needs a restart.
4. **Enforced by tests:** `mise run test` fails when `.claude/skills/` differs from
   the pinned gsx version, when `.gsx` files are unformatted, or when generated
   `*.x.go` files are stale.
5. **Enforced for AI agents:** a committed `.claude/settings.json` hook blocks
   edits to generated `*.x.go` files and to gsxui components outside `gsxui add`.
   CLAUDE.md tells agents to invoke the gsx skill before touching `.gsx`. It works
   the same for every developer's Claude Code.
6. **Rules:** AGENTS.md carries go-htmx4's proven GUI rules (components only via
   `gsxui add`, `go tool gsx generate` / `fmt`, htmx 4 syntax, no hand-written CSS).
   gsxui has no skill, so the rules point at its CLI and at a gitignored
   `.upstream/gsxui` checkout, fetched by a task, for patterns.

### 5.3 Proven on a clean machine

A CI workflow runs `mise install` and `mise run test` on a fresh Linux runner on
every push. It installs every tool, syncs the skills and runs all checks,
including the Service Worker and Worker builds. If a setup step only works on the
owner's machine, CI fails. Deploys need Cloudflare secrets, so CI stops before
them.

## 6. Phases

Three stages, in order: **tooling** first, so every later step runs the same way
for every developer; then **spikes**, so the unproven parts are proven or dropped
before any refactor; then **build**.

### Stage A: tooling

| # | Phase | Done when |
|---|---|---|
| A1 | **Toolchain in mise:** gsxui (by commit), gsx (go.mod tool), standalone Tailwind, kronk, and the existing Go, TinyGo, binaryen and wrangler pins | `mise install` on a clean machine installs everything |
| A2 | **Skills automated** (§5.2): postinstall `skills:sync` into `.claude/skills/`, committed; test check; Claude Code hook; CLAUDE.md and AGENTS.md GUI rules | the gsx skills are listed in a fresh session; `mise run test` fails if they drift |
| A3 | **No personal values** (§5.1): account from fnox, bindings without IDs, Worker URL computed by `setup`. First check wrangler's automatic provisioning on a throwaway Worker | `mise run setup` + `mise run deploy` work on a second Cloudflare account with no edits |
| A4 | **CI on a clean runner** (§5.3) | `mise install` + `mise run test` pass on every push |

### Stage B: spikes (unproven parts)

| # | Spike | Done when |
|---|---|---|
| B1 | **Go chat handler in a TinyGo Service Worker**, served by wrangler from Cloudflare, returning a gsx fragment built with `gsxui add` | the same handler answers on the Worker and in the Service Worker; otherwise fall back to a small JS adapter |
| B2 | **yzma in a Web Worker**, driven by that Service Worker, one small model in Chrome | download size, caching, load time and tokens per second measured; go or no-go |
| B3 | **Browser to localhost** through Local Network Access to kronk, streaming | the permission prompt and CORS work end to end; otherwise local models are native-only |

### Stage C: build

| # | Phase | Done when |
|---|---|---|
| C1 | **Engine refactor:** `internal/engine/openai`, `runtime` in providers.toml, a local kronk provider | today's tests pass; a local kronk chat works through the native binary |
| C2 | **GUI on the Worker:** chat page, room Durable Object (JS) through wrangler, streaming for remote models | shared live chat on Cloudflare with xAI |
| C3 | **Browser and local models in the Service Worker**, as far as B2 and B3 allow | one GUI serves every kind of model that passed its spike |

### Later

| # | Phase | Depends on |
|---|---|---|
| D1 | **Room Durable Object in Go** | workers-go PR #219 merged, TinyGo verified at runtime |

## 7. Decisions needed

| # | Question | Recommendation |
|---|---|---|
| 1 | Rename the app, since it is no longer a Grok proxy? | Yes, before phase 5, while nothing external depends on the name |
| 2 | Where are chats stored? | Durable Object SQLite per room: no extra binding, and it survives hibernation |
| 3 | Is the Local Network Access prompt acceptable for local models on the hosted GUI? | Yes: one prompt per site. Without it, local models work only through the native binary |
| 4 | Worker built with Go or TinyGo? | Go for now, because MCP needs it. The Service Worker is TinyGo either way, so all shared code must stay TinyGo-safe (tested) |
| 5 | Import go-htmx4's `kit/` or copy the parts? | Import `kit/live` and `kit/httpx`; copy only the room Durable Object's JS |
| 6 | One shared Worker, or one per developer? | Per developer for development (Worker name suffixed with the developer's name, set by `setup`), and one shared production Worker deployed from `main` |
| 7 | Which platforms must setup support? | macOS now. **fnox on Linux is deferred** (owner, 2026-09-15), so Linux developers cannot deploy yet. CI on Linux still runs `mise install` + `mise run test`, which need no secrets |
