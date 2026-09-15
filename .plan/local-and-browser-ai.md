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

## 5. Skills first: the AI must use gsx and gsxui properly

This comes **before any GUI code**, because it is exactly what went wrong last
time.

1. `mise.toml` pins gsx (also in `go.mod` as a tool) and gsxui (a `go:` backend,
   by commit), plus standalone Tailwind.
2. `mise run skills` copies gsx's `skills/*` at the pinned version into the repo's
   `.claude/skills/`, and they are committed. **Never global.**
3. **Restart Claude Code**, then confirm `gsx` and `templ-to-gsx-migration` are in
   the skill list. No GUI work starts until they are.
4. AGENTS.md gains the GUI rules proven in go-htmx4: components only via
   `gsxui add`; `.gsx` generated with `go tool gsx generate` and formatted with
   `go tool gsx fmt`; never edit `*.x.go`; no hand-written CSS or components;
   htmx 4 syntax only.
5. `mise run test` enforces it: gsx formatting, generated files up to date, and a
   check that the skills in `.claude/skills/` match the pinned gsx version.
6. gsxui has no skill, so AGENTS.md points agents at its CLI and at
   `.upstream/gsxui` (a gitignored checkout) as the source for patterns.

## 6. Phases

Spikes come first, so the risky parts are proven or dropped before the refactor.

| # | Phase | Done when |
|---|---|---|
| 0 | **Skills and GUI toolchain** (§5), then restart | skills listed; `gsxui add button` works; `mise run test` checks them |
| 1 | **Spike: Go chat handler in a TinyGo Service Worker**, served by wrangler from Cloudflare, returning a gsx fragment | the same handler answers on the Worker and in the Service Worker |
| 2 | **Spike: yzma in a Web Worker** driven by that Service Worker, one small model in Chrome | download size, load time and tokens per second measured; go or no-go |
| 3 | **Spike: browser to localhost** through Local Network Access to kronk or the native binary, streaming | the permission prompt and CORS work end to end, or the local path is native-only |
| 4 | **Engine refactor:** `internal/engine/openai`, `runtime` in providers.toml, kronk in mise with a local provider | today's tests pass; a local kronk chat works through the native binary |
| 5 | **GUI on the Worker:** chat page, room Durable Object (JS) through wrangler, streaming for remote models | shared live chat on Cloudflare with xAI |
| 6 | **Browser and local in the Service Worker** | one GUI serves all three kinds of model |
| 7 | **Go room** once workers-go PR #219 merges and TinyGo is verified at runtime | the JS Durable Object is gone |

## 7. Decisions needed

| # | Question | Recommendation |
|---|---|---|
| 1 | Rename the app, since it is no longer a Grok proxy? | Yes, before phase 5, while nothing external depends on the name |
| 2 | Where are chats stored? | Durable Object SQLite per room: no extra binding, and it survives hibernation |
| 3 | Is the Local Network Access prompt acceptable for local models on the hosted GUI? | Yes: one prompt per site. Without it, local models work only through the native binary |
| 4 | Worker built with Go or TinyGo? | Go for now, because MCP needs it. The Service Worker is TinyGo either way, so all shared code must stay TinyGo-safe (tested) |
| 5 | Import go-htmx4's `kit/` or copy the parts? | Import `kit/live` and `kit/httpx`; copy only the room Durable Object's JS |
