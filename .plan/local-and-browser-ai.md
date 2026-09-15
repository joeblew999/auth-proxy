# Plan: remote, local and browser AI without a mess

**Status: PROPOSED, facts checked 2026-09-15, awaiting the decisions in §6.**
Builds on the owner's notes [webbrowsers-too.md](webbrowsers-too.md) and
[kronk-too.md](kronk-too.md).

## 1. The idea in one line

A model can run in three places, and a chat GUI should be able to use any of them:

| Where the model runs | Example | How the GUI reaches it |
|---|---|---|
| **Remote** | xAI, Groq, OpenRouter | the proxy, over OpenAI-compatible HTTP |
| **Local** | kronk on the user's machine | the proxy, over OpenAI-compatible HTTP |
| **Browser** | yzma (llama.cpp compiled to wasm) in a Web Worker | inside the page; no server involved |

## 2. Facts that decide the design

Checked against the repositories on 2026-09-15; sources are in each project.

**kronk** (ardanlabs/kronk v1.32.6, Apache-2.0)
- A model server plus Go SDK built on yzma. It serves OpenAI-compatible
  `/v1/chat/completions` (SSE streaming), `/v1/models`, `/v1/embeddings` and more,
  and also Anthropic `/v1/messages`.
- The default address is `127.0.0.1:11435`. In the default "open" mode it needs
  no API key.
- No cgo. llama.cpp libraries and GGUF models are downloaded at runtime into
  `~/.kronk/`. mise can install it with `go:github.com/ardanlabs/kronk/cmd/kronk`
  (needs Go 1.27, which this repo already uses).
- Model IDs contain slashes, e.g. `unsloth/Qwen3-0.6B-Q8_0`.

**yzma** (hybridgroup/yzma v1.27.0, Apache-2.0)
- Go bindings for llama.cpp using purego/ffi, with no cgo. GGUF models only.
  Linux, macOS and Windows, with GPU backends, and **WebAssembly**.
- Native (`pkg/llama`) and browser (`pkg/llamawasm`) are separate packages with
  the same function names. There is no shared Go interface.

**yzma-wasm-example** (hybridgroup, Apache-2.0)
- llama.cpp is compiled to wasm with Emscripten (about 13 MB). A TinyGo wasm
  program drives it from a **Web Worker**.
- It picks WebGPU (Chrome/Edge 137+), multi-threaded CPU (needs
  `SharedArrayBuffer`, so COOP/COEP headers), or single-threaded CPU.
- It exposes **no OpenAI API**: only JS globals (`yzmaAsk`, `yzmaLoadModel`, …)
  and `postMessage` token events.
- The browser downloads models (220–770 MB suggested, 2 GB per file) straight from
  Hugging Face. Whether they stay cached between visits is unverified: the README
  says yes, the code fetches with `cache: "no-store"`.

**gsx / gsxui** (gsxhq, MIT)
- gsx is a JSX-style Go templating language (v0.1.1, 2026-09-15) and gsxui a
  shadcn-style component library (no releases; pin a commit). Both install only
  with `go install`, which mise's `go:` backend handles.
- gsx ships Claude Code skills in `skills/gsx/SKILL.md` and
  `skills/templ-to-gsx-migration/SKILL.md`, but documents nowhere to install them.
- Claude Code loads project skills from `.claude/skills/<name>/SKILL.md`. A skills
  directory created during a session is only seen after a restart.

**go-htmx4** (joeblew999/go-htmx4), the existing stack
- Server-rendered gsx/gsxui, htmx 4 with `hx-ws`, TinyGo on workerd, deployed
  through the Cloudflare REST API. No wrangler, no Node, no client wasm.
- Live updates go browser ⇄ a **JavaScript** Durable Object `Room` over
  hibernating WebSockets. Go writes to D1 and publishes HTML fragments to the Room
  via `stub.Fetch`. The Room coalesces pushes every 200 ms.
- **No AI chat exists yet**, and **no skills are installed** (`.claude/` has only
  `settings.json`). That matches the note's "skills were not loaded".

**workers-go and Durable Objects**
- v0.35.0 can **call** a Durable Object from Go but **cannot define** one, and has
  no WebSocket support. That is why go-htmx4's Room is JavaScript. Go-defined
  Durable Objects with storage, alarms and hibernating WebSockets exist only in
  **unmerged PR #219** (`exp/`); see also issue #220.
- Go SSE streaming works. Each request gets a fresh wasm instance, so shared live
  state has to live in a Durable Object.

### Corrections to the notes

- *"The GUI runs off Cloudflare with a DO using workers-go v0.35.0"*: the GUI
  does, but the Durable Object is JavaScript. A Go Durable Object needs PR #219.
- *"mise installs the CLIs and skills in the well known place"*: mise installs the
  CLIs, but nothing installs the skills today. The well-known place is
  `.claude/skills/` in the repo that uses them.

## 3. Design

### 3.1 One contract: OpenAI chat completions

Everything speaks the OpenAI chat-completions shape: messages in, streamed deltas
out. Remote and local already do, through the proxy. The browser engine gets a
small in-page adapter that accepts the same request and emits the same delta
events, over `postMessage` instead of HTTP. The GUI then has **one chat client
with two transports**: `http` (to the proxy) and `browser` (to the Web Worker).

### 3.2 Local AI is a provider, not new code

kronk already speaks the contract, so local AI is three lines of providers.toml:

```toml
[providers.kronk]
base_url = "http://127.0.0.1:11435/v1"
auth = "none"
local = true            # new: the Worker cannot reach 127.0.0.1
```

Clients use `kronk/unsloth/Qwen3-0.6B-Q8_0`. The router already splits on the
first `/` only, which is tested with Cloudflare's slashed IDs. The one change is
the new `local` flag: the Worker leaves such providers out, `status` shows them as
local-only, and a local provider as `default` fails `deploy` with a message. mise
installs kronk, and a `local:*` task pulls and starts a model.

### 3.3 Browser AI is a GUI feature, not a proxy feature

The proxy never sees browser inference, so nothing about it goes in this repo's
Go code. The GUI serves yzma's wasm assets and a Web Worker, and its chat client
picks the `browser` transport when the user chooses an in-browser model. It needs:

- COOP/COEP headers **only on the chat page**, so other pages and htmx requests
  are unaffected.
- A pinned llama.cpp wasm build and yzma version, because they move weekly.
- A model picker with sizes shown, since downloads are hundreds of MB.
- An explicit exception to go-htmx4's no-client-JS rule, limited to the Web
  Worker and the adapter.

### 3.4 The GUI

Built from go-htmx4 (gsx/gsxui, hx-ws, the JS `Room`, `kit/live`, `kit/cfdeploy`).
A chat page and handler are added:

- **Remote or local model:** the GUI Worker calls the proxy with the client key
  held as a GUI secret. Tokens reach every browser in the room as fragments
  published to the `Room`, since go-htmx4 buffers responses and workers-go has no
  WebSockets.
- **Browser model:** the browser that runs the model renders tokens locally, and
  can publish the finished message to the room so others see it.
- **Room state** stays in D1 or Durable Object storage, never only in memory,
  because Durable Objects hibernate.

### 3.5 Skills, so the AI working on it does not "fuck it all up"

A mise task `skills` copies gsx's `skills/*` at the pinned gsx version into
`.claude/skills/`, and the result is committed. `mise run setup` runs it, and
`status` warns when the skills are missing or out of date. After the first run,
restart Claude Code once.

## 4. Where the code lives

To avoid a mess, keep two repos with one contract between them:

| Repo | Owns | Stack |
|---|---|---|
| **grok-oauth-proxy** (this one) | remote and local providers, the OpenAI contract, keys | Go, wrangler, standard Go or TinyGo Worker |
| **chat GUI** (from go-htmx4) | pages, rooms, the browser engine and its adapter | gsx, TinyGo, workerd, JS Room, REST deploy |

Putting both in one repo would mix two build systems (wrangler vs the REST API,
wasm-in-Worker vs wasm-in-browser) and two sets of agent rules.

## 5. Phases

| # | Phase | Repo | Result |
|---|---|---|---|
| 1 | kronk in mise; `local` provider flag; `local:pull` and `local:start` tasks; verify a real local chat | proxy | Local AI through the same endpoint |
| 2 | `skills` task installing gsx skills into `.claude/skills/`; bump gsx to v0.1.1 and gsxui to the latest commit | GUI | The AI has the gsx skills loaded |
| 3 | Chat page calling the proxy; tokens pushed to the `Room` | GUI | Shared live chat with remote and local models |
| 4 | Browser engine: yzma wasm in a Web Worker, OpenAI-shaped adapter, `browser` transport, COOP/COEP on the chat page | GUI | Models running in the browser |
| 5 | Optional: move the `Room` to Go once workers-go PR #219 is merged and TinyGo runtime is verified | GUI | One language, if still wanted |

## 6. Decisions needed

| # | Question | Recommendation |
|---|---|---|
| 1 | Where does the GUI live? | A separate repo started from go-htmx4 (§4), not inside this proxy |
| 2 | Room in JS now, or wait for Go Durable Objects? | JS now, as go-htmx4 already does; revisit after PR #219 merges |
| 3 | Which local server? | kronk: it already speaks OpenAI and Anthropic, needs no cgo, and mise installs it. Ollama or llama.cpp's server also work as plain `auth = "none"` providers |
| 4 | Do browser-model messages join the shared room? | Yes, as finished messages; streaming stays local to that browser |
| 5 | Skills committed or regenerated? | Committed, so any fresh clone has them before the first prompt |
