# cmd/gui

A spike, not a product: a separate Go module that proves the GUI toolchain for
`.plan/local-and-browser-ai.md` before the real app depends on it. The root
module and the proxy are untouched by anything in here.

Run everything through mise from the repo root; nothing here needs remembering.

| Task | What it does |
|---|---|
| `mise run hello:dev` | Vite plus the Go server with live reload, on http://localhost:5173 |
| `mise run hello:serve` | build and run the binary on http://localhost:7777 |
| `mise run hello:build` | `vite build` → `gsx generate` → `go build` (that order: `vite build` empties `dist/`, which the binary embeds) |
| `mise run hello:check` | formatting, vet, tests, a TinyGo wasm build, the browser check and the workerd check — also run by `mise run test` |
| `mise run hello:workerd` | the same app as a Cloudflare Worker on local workerd (`wrangler dev`) |
| `mise run hello:smoke` | build the Worker, start it on workerd, request one page and check it |
| `mise run hello:deploy` | deploy it as `grok-oauth-proxy-gui` and wait for its page; `hello:url` prints where |
| `mise run dev:browser` | drive the page in a headless Chrome and check the picker actually works (this spike is what it defaults to) |

## What it proves

The picker is the GUI's central question — **where should this model run, and
which model** — so it is what the spike builds.

- **One routing rule, in the code.** A model ID carries its provider prefix, the
  prefix names the provider, and the provider says where it runs. So a mode owns
  its models (`views.Mode.Models`): picking a mode narrows the list, and the two
  choices can never disagree. `views.ModeOf` runs the rule backwards — a `?model=`
  link alone opens that model's own mode.
- **It works without JavaScript.** The state is `?mode=` and `?model=` in the URL,
  so choosing a model is an ordinary link. Every mode's panel is rendered, so
  gsxui's `tabs.js` switches modes in the browser with no round trip — and because
  its triggers are buttons, a `<noscript>` block offers the same modes as links.
- **Every state a model can be in** renders: in use, ready, downloading with
  progress, and not downloaded. A mode that cannot be used renders gsxui's `Empty`
  with the problem and the command that fixes it.
- **The same components compile under TinyGo** (`cmd/tinygo-render`), because the
  browser topology runs this markup inside a Service Worker.
- **The same app is a Cloudflare Worker.** `app.go` builds the handler once;
  `main.go` serves it natively and `worker.go` (built for `js && wasm`) hands it
  to workers-go. The Vite build and `public/` are inside the wasm, so the Worker
  needs no bindings, and `wrangler.toml` sits beside the code like every Worker's.

## Rules for working in here

- **Components come from `gsxui add`**, never hand-written, and compositions
  follow gsxui's own site. Invoke the `gsx` skill before editing any `.gsx`.
- **`*.x.go` is generated** by `gsx generate` and git-ignored. Do not edit it.
- **`ui/` is vendored by gsxui** and tracked by hashes in `gsxui.json`; `gsxui add`
  refuses to overwrite a file you changed.
- **The sample data in `views/sample.go` is made up.** It stands in for what the
  proxy will report in Stage B; the shapes are the real ones.
