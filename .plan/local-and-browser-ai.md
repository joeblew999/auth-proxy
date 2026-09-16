# Plan: one app for remote, local and browser AI

**Status: PROPOSED, facts checked 2026-09-15, awaiting the decisions in §7.
Re-checked 2026-09-16: A1 and A2 done, A3 and A4 untouched, nothing of Stage B
or C started. Names below follow the move to `cmd/` and `dev:session:*`
(`c762bd6`); the dated results keep the names they had.**
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

### Choosing a model: pick the mode, then the model

Each mode has its own models, so the GUI asks in that order:

1. **Mode:** a three-way choice, **Remote**, **Local** or **In browser**, each with a
   one-line explanation: remote runs at the provider and costs credits; local runs
   on your machine; in browser is private and runs on this device. The mode itself
   shows whether it can be used here, and the action if not:

   | Mode | Unavailable when | Shown action |
   |---|---|---|
   | Remote | never on the hosted GUI (a missing provider key shows per model) | none |
   | Local | kronk not reachable, or local network access not allowed | "allow local network access" (Chrome's prompt) · "start kronk" |
   | In browser | no WebGPU and no usable storage | "not supported in this browser, try Chrome" |

2. **Model:** the list shows **only that mode's models**, each with its own status
   and action:

   | Mode | Model statuses and actions |
   |---|---|
   | Remote | ready · provider key missing (admins see the `mise` fix) |
   | Local | ready · "pull with kronk" when the model is not downloaded |
   | In browser | downloaded ✓ · "download 639 MB" with progress · "install the app to keep models offline" on Safari |

Because a model can only be picked inside its mode, the two choices can never
disagree. Underneath it is still one routing rule: the mode is the provider's
`runtime`, and the chosen model ID carries the provider prefix.

- **When a mode stops working mid-chat** (kronk shut down, a browser download
  fails, a provider key is revoked), the message fails in place with the same
  status and action the pickers show ("start kronk", "retry download"). The
  conversation is kept, and the user can switch mode or model and resend. The
  same routing rule produces these errors in every runtime, so they read the same
  everywhere.
- **Defaults:** Remote on first visit, because it works everywhere. Mode and model
  are remembered per device, since local and in-browser models only exist there.
- **Built from the same routing:** each mode's model list is a fragment request.
  Remote lists come from the Worker; the Service Worker answers Local (kronk's
  `/v1/models`) and In browser (`providers.toml` plus our model store's download
  state). Without a Service Worker, only Remote is offered.
- **Every message is labelled** with its mode and model, so a shared room shows that
  one person asked a remote model and another a model in their browser.
- **Each user picks their own mode and model** in a shared room.
- **Built from gsxui components** added with `gsxui add`, never hand-written.
  Proven in the spike (A2 result): `tabs` for the mode, `item` + `badge` +
  `progress` + `spinner` + `icon` for the models, and `empty` for a mode that
  cannot be used, carrying the command that fixes it.

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
  Face.
- **Browser models are not cached (seen by the owner in Safari; explained by the
  code).** yzma's `pkg/llamawasm/fs.go` `FetchModelFile` fetches with
  `cache: "no-store"` on purpose, because Firefox's HTTP cache aborts the stream
  for files that large. It then streams the model into Emscripten's in-memory file
  system, so every page load downloads the model again in every browser. yzma also
  has `WriteModelFile(name, data)`, so a model can be supplied from our own
  storage.
- **Safari storage** (WebKit, Safari 17+): up to 60% of the disk per origin and 80%
  overall. Cache API, IndexedDB, Service Worker and file-system storage are evicted
  together per origin, least recently used, under storage pressure or after a
  period without user interaction (MDN: 7 days). `navigator.storage.persist()`
  exempts an origin, and WebKit grants it mainly to sites installed as a web app
  (Home Screen on iOS, Add to Dock on macOS).
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
- **gsx** and **gsxui**: upstream (gsxhq) has gsx v0.1.1 and no gsxui releases,
  both `go install` only; gsx ships `skills/gsx/SKILL.md` and
  `skills/templ-to-gsx-migration/SKILL.md`, gsxui ships none. Since 2026-09-16
  this repo pins the owner's forks as packslip releases instead
  (`packslip:github.com/joeblew999/gsx` 0.1.2, `.../gsxui` 0.1.0), which carry
  the `gsx` and `gsxui` skills so mise links them at the tool's version. gsxui's
  CLI (`gsxui add`) is how components get into the repo.
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
| `cmd/server`, `cmd/worker`, `cmd/sw/` | entry points: native, Cloudflare, Service Worker | one each |

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

Checked 2026-09-15: the repo hard-coded the owner's account ID and KV namespace
ID in `wrangler.toml` and the owner's `*.gedw99.workers.dev` URLs in `mise.toml`.
**Gone since 2026-09-16 (A3).** What replaced each:

| Value | From now on |
|---|---|
| Cloudflare account | `CLOUDFLARE_ACCOUNT_ID`, provided by fnox; wrangler reads it from the environment |
| KV namespace, D1, R2 | bindings **without IDs**. Wrangler provisions them per account on the first deploy and inherits them after. **Verified 2026-09-16:** deploying `GROK_AUTH` with no id reported `env.GROK_AUTH (inherited)` and created nothing. It writes created ids back into the config it deployed from, so `deploy` runs wrangler on a throwaway copy, `wrangler.deploy.toml`, gitignored |
| Worker URL | `dev url` derives it from the Worker name in `wrangler.toml` and the account's workers.dev subdomain, which it reads from the Cloudflare API once into gitignored `mise.local.toml`. Every `--worker` task goes through it, so there is no step to remember; `mise run url` prints it |
| Secrets | fnox, as today. Each developer keeps their own values; `setup` prompts for any that fnox does not have |
| Worker name | a per-developer name for development and one shared production name, see §7 |

### 5.2 Skills: in the repo, automated, verified, enforced

Last time the AI ignored the gsx and gsxui CLIs because the skills were not
loaded. So **every skill this project relies on lives in the repo**, pinned, and is
proven to load. Nothing comes from a global or plugin install on someone's
machine.

| Skills | Source, pinned | State on 2026-09-16 |
|---|---|---|
| `gsx`, `gsxui` | the packslip releases of the `joeblew999/gsx` and `joeblew999/gsxui` forks, pinned under `[tools]` in `mise.toml`; mise links each skill at the tool's version (`[settings.skills] auto_sync`) | linked. Upstream gsx also ships `templ-to-gsx-migration`; the fork's release does not, and nothing here needs it |
| `hk-configure`, `hk-debug` | hk's packslip release, the same way | linked |
| `wrangler`, `durable-objects`, `workers-best-practices`, `cloudflare` | `skills/` in github.com/cloudflare/skills at the commit in `session.toml`, vendored into `.claude/skills` with every file hashed in `SKILLS.lock` | vendored. cloudflare/skills has no releases, which is the one reason `session.toml` exists; `.plan/done/claude-session.md` has the whole story. On 2026-09-15 they reached this machine only through a stale global plugin, which the generated settings now block |

1. **`dev:session:sync`, run by `mise install`** (its `postinstall` hook runs
   `dev:bootstrap`: sync, then `hk install`), vendors the cloudflare skills at
   their pinned commit into `.claude/skills/` and generates the
   `.claude/settings.json` keys; mise links the packslip skills itself. The
   result is committed. **Never global.** When it adds or changes a skill, the
   task itself tells the developer to restart Claude Code, so no one has to
   remember it.
2. **`dev:session:verify` proves they load:** it runs a fresh headless Claude
   Code session (`claude -p`) in the repo and holds what it reports against
   `SESSION.lock`, failing on anything missing or anything extra. It needs a
   login and ~7s, so it is bound to **pre-push**. `mise run test` runs the cheap
   file-level `dev:session:check` (skills present and matching their pins,
   settings matching `session.toml`), which is also all CI could run.
3. **Enforced by tests:** `mise run test` fails when `.claude/skills/` differs from
   the pins or when `.gsx` files are unformatted. Generated `*.x.go` files are
   gitignored and regenerated by `hello:build`, so there is no stale copy to
   check for.
4. **Enforced for AI agents:** a committed PreToolUse hook,
   `.claude/hooks/skill-gate`, loads the skills that cover a file before any
   tool changes it: `.gsx` gets gsx and gsxui, `.pkl` gets hk-configure,
   `wrangler.toml` (or `.json`, `.jsonc`) gets wrangler, and a Bash command
   counts only when it is shaped like a write. `dev:hooks:check` in
   `mise run test` runs its case matrix. Durable Object files get nothing yet,
   because none exist; map them when B4 creates one. The `*.x.go` edit block
   from the first draft was never built, and with the files gitignored and
   regenerated on every build there is nothing for it to protect.
5. **GUI rules** in AGENTS.md follow gsxui's own site and gsx's test corpus
   (`internal/corpus/testdata`), which the gsx skill names as the canonical
   reference: components only via `gsxui add`, compositions copied from gsxui's
   site, never invented.
6. **Done 2026-09-16.** Eight skills are in `.claude/skills`, four vendored and
   four linked from packslip tools, synced by `mise install` and proven to load
   by `dev:session:verify`. The picker written without them was replaced (A2
   result below).

### 5.3 Proven on a clean machine

A CI workflow runs `mise install` and `mise run test` on a fresh Linux runner on
every push. It installs every tool, syncs the skills and runs all checks,
including the Service Worker and Worker builds. If a setup step only works on the
owner's machine, CI fails. Deploys need Cloudflare secrets, so CI stops before
them.

## 6. Phases

Four stages, strictly in order, and nothing moves on until the stage before it is
proven: **tooling**, then a **hello world round trip in every topology**, then the
**spikes**, then **build**.

### Stage A: tooling, validated by running it

Every tool is run and proven to work with the others, the way its authors document
it (gsx's Vite starter, not a Node-free workaround). Nothing is assumed from a
successful install.

| # | Phase | Done when |
|---|---|---|
| A1 | **Toolchain in mise**, pinned: gsx CLI (same version as go.mod's tool), gsxui (by commit), kronk, plus Go, TinyGo, binaryen, Node and wrangler | each tool runs; the documented flow works end to end: `gsx init --yes` → `gsxui init` (Vite mode) → `gsxui add` → `go tool gsx generate` / `fmt` → `npm run build` → `go build` → page served with components and CSS; the same components render under TinyGo |
| A2 | **Automation, nothing left behind** (§5.2): a mise `postinstall` hook sets go.mod's gsx tool to the mise gsx pin and syncs the gsx skills into `.claude/skills/`; npm install scripts (esbuild) approved in committed config; `mise run test` fails on any drift | a fresh clone needs only `mise install`; the skills are listed in a fresh Claude Code session and used for all `.gsx` work |
| A3 | **No personal values** (§5.1): account from fnox, bindings without IDs, Worker URL computed by `setup`. First check wrangler's automatic provisioning on a throwaway Worker | `mise run setup` + `mise run deploy` work on a second Cloudflare account with no edits |
| A4 | **CI on a clean runner** (§5.3) | `mise install` + `mise run test` pass on every push |

#### A1 result, 2026-09-15: done and validated

Stages A and B are built in **`cmd/gui/`** (`spikes/hello-world/` until
`c762bd6`), a separate Go module in this repo (the root module and its tests
are untouched). Stage C folds it into the root app.

- **Scaffolded the documented way:** `gsx init hello-world --module … --yes` (Vite
  starter with `@gsxhq/vite-plugin-gsx` and the `github.com/gsxhq/vite` Go module),
  then `gsxui init` (Vite mode: Tailwind through `@tailwindcss/vite`), then
  `gsxui add button badge tabs item progress` (also pulls `separator`).
- **Proven:** a mode and model picker (`views/picker.gsx`) built only from gsxui
  components renders 14 component slots with its states. It works through
  `npm run build` → `go tool gsx generate` → `go build` → served page with
  Vite-built CSS, through the `gsx dev` loop, and under TinyGo (native run and
  `-target wasm` build).
- **Tools pinned in mise:** gsx CLI v0.1.1 (same version as the spike's go.mod
  tool), gsxui `5d973c7`, kronk 1.32.6. Standalone Tailwind was removed: gsxui's
  documented path is Vite. (gsx and gsxui have since moved to packslip releases
  of the owner's forks; see A2's re-check.)
- **Fixed on the way, not skipped:**
  - `gsx init` writes `go 1.26.0`; aligned to the repo's Go 1.27.
  - npm 11 blocks install scripts by default: `esbuild` (needed by Vite) and
    `fsevents` (macOS watcher) are approved in `package.json` `allowScripts`, so
    `npm ci` runs with no warnings.
  - `vite build` empties `dist/` (deleting the starter's `dist/.gitkeep`), and the
    Go binary embeds `dist/`, so any Go build before the npm build fails. The tasks
    run the documented order.
  - A `go get -tool` run by hand had bumped `golang.org/x/tools` in the root
    module; the root `go.mod` was restored.
- **mise tasks** (`mise tasks | grep hello`): `hello:dev`, `hello:build`,
  `hello:serve`, `hello:check` (build, `gsx fmt -l`, `go vet`, TinyGo wasm), and
  hidden `hello:install` (`npm ci`, skipped when the lockfile is unchanged).
  `mise run test` runs `hello:check`. Each task was run through mise, from a
  fresh-clone state, with nothing left running.
- **Still open, moved to A2:** nothing yet keeps the mise gsx pin and the spike's
  go.mod tool pin in step, and no skills are installed. The picker was written
  without the gsx skill loaded and is to be removed (§5.2).

#### A2 result, 2026-09-16: skills load, and the picker was rebuilt with them

- **Skills** (§5.2): six of them (`gsx`, `templ-to-gsx-migration`, `cloudflare`,
  `wrangler`, `durable-objects`, `workers-best-practices`) are in
  `.claude/skills`, pinned in `SKILLS.lock`, synced by the mise `postinstall`
  hook, drift-checked by `mise run test`, and proven loadable by
  `mise run skills:verify` (a headless `claude -p` session lists all six). The
  tooling is Go (`cmd/dev`) with tests, after the shell version mis-parsed `ps`
  output twice. `skills:check` warns when a Claude Code session predates the
  skills, since a session only reads them at startup. Since then: the tasks
  are `dev:session:sync|check|verify`; gsx and gsxui come from packslip
  releases carrying their own skills, so `templ-to-gsx-migration` went and
  `gsxui`, `hk-configure` and `hk-debug` arrived; and `verify` holds the whole
  session against `SESSION.lock`. `.plan/done/claude-session.md` has that work.
- **The picker was rebuilt** with the gsx skill loaded and gsxui's own site as
  the pattern source. What changed, beyond looks:
  - **A mode now owns its models** (`Mode.Models`), instead of the model list
    sitting beside the tabs. The old one showed the same list whatever mode you
    picked — the routing rule was in the prose and not in the code.
  - **Every panel is rendered**, so gsxui's `tabs.js` switches modes in the
    browser with no round trip; the server only decides which one opens first.
  - **The state is in the URL** (`?mode=`, `?model=`). Choosing a model is an
    ordinary link, and `?model=` alone opens that model's own mode — the routing
    rule, applied (`views.ModeOf`). gsxui's tab triggers are buttons, so with
    JavaScript off they do nothing; a `<noscript>` block offers the same modes as
    links, carrying the chosen model. Checked by walking the page with no
    JavaScript, and pinned by a test, since a browser check cannot cover it.
  - **A mode that cannot be used** renders gsxui's `Empty` with the problem and
    the exact command, matching the proxy's "every error names its fix" rule.
  - **The starter's CSS was removed.** It hard-coded a `#242424` page and its own
    fonts, while gsxui's dark palette hangs off a `dark` class on `<html>`. On a
    dark-mode machine that gave light components on a dark page. The page now
    uses gsxui's tokens, with the theme script from gsxui's own site; both
    palettes were rendered and checked.
  - **The page buffers its render.** The gsx scaffold renders straight to the
    `ResponseWriter`, so a failure part way through logged "superfluous
    WriteHeader" and served half a page.
- **`mise run dev:browser`** (`hello:browser` when it landed) is the new check:
  `bin/dev browser` serves the app on a free port, starts a headless Chrome, and
  a probe drives it over the
  DevTools protocol (Node's built-in WebSocket, no dependency). It proves the
  thing curl cannot: clicking a mode really swaps to that mode's models, one
  panel at a time, and the Vite-bundled JS runs with no console errors. It is
  part of `hello:check`, so `mise run test` runs it. Without Chrome it says so
  loudly and passes; `CHROME=/path/to/chrome` points it at one. gsxui tests its
  own behaviour modules with Playwright — if these checks grow past a handful,
  switch to that rather than growing the probe.
- **Measured**, TinyGo 0.42.0, `-target wasm -no-debug`: the picker and its gsxui
  components are **1,277 KB raw / 386 KB gzip**. Of that, only 75 KB raw / 19 KB
  gzip is the vendored 1,748-icon Lucide set; the rest is the TinyGo runtime plus
  gsx. That is the floor for B5's Service Worker.
- **Still open, re-checked 2026-09-16:**
  - The mise gsx pin and `cmd/gui/go.mod`'s tool pin are two numbers for one
    code: the packslip fork's 0.1.2 is upstream gsxhq/gsx v0.1.1 plus release
    wiring only (compared 2026-09-16: `.goreleaser.yml`, the release workflow,
    `mise.toml`, one doc), and its module path is still `github.com/gsxhq/gsx`.
    `hello:*` builds with `go tool gsx`, so the packslip binary is only what
    mise links the skill from. Nothing checks the two stay the same code. The
    one-pin fix is a `replace github.com/gsxhq/gsx => github.com/joeblew999/gsx`
    in `cmd/gui/go.mod` held to the mise pin by `deps:*`, or dropping the fork
    once upstream ships a packslip. Owner's call.
  - A3 (no personal values): **done 2026-09-16**, see §5.1. Not exercised: a
    first deploy on a second account, which nothing here has. The changelog says
    wrangler creates and links the namespace then, and `deploy` prints what it
    created.
  - A4 (CI on a clean runner): **done 2026-09-16**. `.github/workflows/test.yml`
    runs `mise install` and `mise run test` on every push. Its first runs found
    two things a clean runner needs that this machine did not show: the
    packslip tools pinned exactly, and the owner's own forks exempt from mise's
    24h release wait. The one failure left is hk 2.0.1 being under a day old;
    that clears at 2026-09-16T18:57Z, and no green run has been seen yet.

### Stage B: hello world round trip in every topology

**B1, B2 and B3 done 2026-09-16; B4 to B8 not started.** B1 is `hello:serve`
plus `dev:browser`: browser, Go server, gsx page with Vite assets, and a click
that switches panels (no htmx fragment yet, since the picker needs none). B2 is
`hello:smoke`: `dev worker smoke --dir cmd/gui` starts `wrangler dev` on
workerd, requests `/?mode=remote` and checks the page; `mise run test` runs it.
B3 is `hello:deploy`: the same wasm live as `grok-oauth-proxy-gui`, waited for
at `/healthz`, the page checked on its URL. The Worker is standard Go (decision
4), embeds the Vite build, and needs no bindings. What made it one Worker
among two: `cmd/gui/wrangler.toml` beside the code, `worker.go` next to
`main.go` sharing `newHandler`, and the same `dev worker ...` with `--dir`.

The smallest possible "click → request → Go handler → gsx fragment → swapped into
the page", once per topology the design needs, before anything real is built. A
topology that cannot round-trip changes the design before code depends on it.

| # | Topology | Round trip that must work |
|---|---|---|
| B1 | **Native binary** | browser → Go server → gsx/gsxui fragment (htmx swap), with Vite assets |
| B2 | **Cloudflare Worker, `wrangler dev`** | the same handler and page on workerd through wrangler, with Vite's built assets served as Workers Static Assets |
| B3 | **Cloudflare Worker, `wrangler deploy`** | the same round trip on the live URL |
| B4 | **Room Durable Object** | two browsers: one click publishes through Go → JS Room → WebSocket, and the other browser's page updates |
| B5 | **Browser Service Worker (TinyGo)** | the same handler answers from the Service Worker, served from Cloudflare, and the page cannot tell the difference |
| B6 | **Remote engine** | the Worker handler calls a remote provider and returns its hello |
| B7 | **Local engine** | kronk serves a tiny model; the native binary gets a hello, and the browser on the hosted GUI gets one through Local Network Access |
| B8 | **Browser engine** | yzma in a Web Worker returns a hello, and the second visit loads the model from our storage without downloading it |

### Stage B2: spikes (depth, after the round trips work)

**Not started as of 2026-09-16.**

| # | Spike | Done when |
|---|---|---|
| S1 | **Service Worker handler at full size**: the real chat handler and gsxui page in TinyGo | size and startup measured; otherwise fall back to a small JS adapter |
| S2 | **Browser model performance and storage** on Safari and Chrome | load time, tokens per second, and a week of storage on an installed Safari web app measured; go or no-go |
| S3 | **Local Network Access in practice**: prompt wording, denial, CORS | the local mode's picker states and actions are proven |

### Stage C: build

**Not started as of 2026-09-16.** `providers.toml` has no `runtime` key, and no
`internal/engine` or `internal/chat` exists.

| # | Phase | Done when |
|---|---|---|
| C1 | **Engine refactor:** `internal/engine/openai`, `runtime` in providers.toml, a local kronk provider | today's tests pass; a local kronk chat works through the native binary |
| C2 | **GUI on the Worker:** chat page, the mode and model pickers (§2), room Durable Object (JS) through wrangler, streaming for remote models | shared live chat on Cloudflare with xAI; the Remote mode lists its models with status |
| C3 | **Browser and local models in the Service Worker**, as far as B2 and B3 allow, including their modes, statuses and actions (download with progress, allow local network access) | one GUI serves every mode that passed its spike |

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
| 8 | Should browser models require installing the app (Add to Dock or Home Screen) for reliable caching on Safari? | Recommend it rather than require it: cache in the browser tab too, and show a "keep models offline" prompt that explains installation |
