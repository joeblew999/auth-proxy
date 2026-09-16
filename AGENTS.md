- Repo: joeblew999/grok-oauth-proxy

# Development instructions

## Rule zero: nothing to remember

- **Every workflow is a mise task.** `mise tasks` lists them. Do not document or
  rely on raw `go`, `wrangler` or `fnox` commands; if a workflow is missing, add a
  task.
- **Every error names its fix.** A config error names the setting, a missing key
  prints `mise run keys:set <provider>`, and so on. Keep it that way in new code.
- **Providers live only in `providers.toml`.** Secret values live only in fnox
  (`mise run keys:set`) and Worker secrets (`mise run keys:push`).
- **hk owns the checks.** `mise run lint` runs them (`hk check --all`: gofmt,
  vet, tidy, whitespace, secrets). Ask the hk MCP server to plan checks before
  execution; scope to changed files; prefer safe fixes. A Stop hook runs
  `hk run check --safe` after every agent turn, and the pre-commit hook runs
  the same on commit. `hk run check` must stay cheap: the expensive,
  login-gated `dev:skills:verify` is bound to **pre-push** only.

## Working rules from the owner

These apply to every AI agent working here. They exist because each was broken
at least once.

- **Nothing outside this repo.** No global Claude Code skills, plugins, memory or
  settings, and no files left in temp or home directories. Skills, agent rules and
  settings live in the repo (`.claude/`, `AGENTS.md`, `CLAUDE.md`) and are
  committed.
- **Skills are in the repo, pinned, synced by mise, and proven to load.** Invoke
  the relevant skill before the work it covers: gsx before any `.gsx`, the
  Cloudflare skills before wrangler config or Durable Objects. GUI work uses only
  the gsx and gsxui CLIs and patterns from gsxui's own site; never hand-invented
  components.
- **Use tools the way their authors document them.** Do not work around a tool's
  intended path (for example gsx's Vite starter).
- **Once something works, put it in mise and the plan.** No manual steps, no
  reminders in chat (such as "restart Claude Code"): a task does it or tells the
  developer.
- **It must work for every developer**, who all use mise and fnox. No personal
  account IDs, URLs or paths in committed files.
- **Test for real before saying done:** run the actual tasks locally, in workerd,
  and on Cloudflare, and say plainly what could not be exercised.
- **Prove a hello world round trip in every topology** before building on it.
- **Think it through before presenting a plan:** as a new developer on a fresh
  clone, as an end user in the GUI, and by where the code runs and what it shares,
  not by incidental tooling.
- **One routing rule** decides everything: the model's provider prefix, and that
  provider's runtime (remote, local or browser). The GUI asks for the mode first,
  then that mode's models.
- **Commit only files you name.** The owner edits `.plan/` at the same time; never
  `git add -A` or a whole directory you did not create.

## Tasks

| Task | Purpose |
|---|---|
| `mise run dev` / `dev --mock` | local proxy on `127.0.0.1:56121` (real providers, or the bundled mock) |
| `mise run svc:start` / `svc:stop` / `svc:status` / `svc:logs` | the same proxy as pitchfork daemons, so `dev` never blocks the terminal |
| `mise run status` / `status --worker` | every provider's readiness, with the fix for each problem |
| `mise run models` / `chat <model> [prompt]` | list models, stream a prompt (`--worker` for the deployed Worker) |
| `mise run login` / `login --worker` | SuperGrok login for `auth = "xai-oauth"` providers |
| `mise run keys:set <provider>` / `keys:push` | store a key in fnox and push it; push everything |
| `mise run lint` | hk checks: gofmt, vet, tidy, whitespace, secrets |
| `mise run test` | lint, wasm vet, all tests, skills check, spike check |
| `mise run dev:skills:sync` / `dev:skills:verify` / `dev:skills:bump` | re-sync the repo's pinned Claude Code skills; hold a fresh session against `SESSION.lock` (`--update` re-records it); move github pins to upstream HEAD |
| `mise run dev:bootstrap` / `dev:mcp` | install the git hooks and sync skills (runs after `mise install`); check every declared MCP server connects |
| `mise run dev:browser` | drive an app in a headless Chrome and run its probe script |
| `mise run hello:*` | the gsx + gsxui spike: `dev`, `build`, `serve`, `check` |
| `mise run deps:list` / `deps:upgrade` | list / interactively apply Go module upgrades in every module |
| `mise run build` / `build --tinygo` | local binary and Worker; optionally TinyGo plus sizes |
| `mise run deploy` / `deploy --tinygo` | validate `providers.toml`, then deploy |
| `mise run release` / `release:snapshot` | publish a GitHub Release (packslip manifest included by the workflow); build the artifacts locally |
| `mise run logs`, `setup`, `bench` | Worker logs, one-time setup, Go vs TinyGo comparison |

Every task is a file in `mise-tasks/` (groups are directories: `svc/*`,
`hello/*`, `dev/skills/*`); only `test` and the hidden `build:*` helpers
stay in `mise.toml`. Run `mise run lint` after every Go change, `mise run test`
before committing.

## Code layout

| Path | Owns |
|---|---|
| `providers.toml` | the providers, built into every binary and the Worker |
| `internal/config` | parsing and validating providers, resolving secrets; **the only code that reads the environment** (through the `getenv` it is given) |
| `internal/router` | model name → provider, URL joining, model rewriting, merged `/v1/models`; no I/O |
| `internal/proxy` | the HTTP handler used by **both** runtimes: client key, routing, provider auth, streaming, admin routes |
| `internal/xaiauth` | the Grok login: browser PKCE, device flow, refresh, token stores |
| `internal/mcp` | MCP tools `ask` and `list_models` (excluded from TinyGo builds) |
| `main.go` | local CLI: `serve`, `status`, `models`, `chat`, `login`, `keys` |
| `worker.go` | Worker entry point: fetch client, KV token store |
| `config.go` | which providers file is used: `--config`, `PROVIDERS_TOML`, or the built-in one |
| `tools/mock-upstream` | OpenAI-compatible mock plus its two-provider config |
| `cmd/skillpin` | the skills/plugins/MCP pinner. `sync` writes `.claude/skills` and the `.claude/settings.json` keys; `check` compares both to the pins; `verify` holds a real session against `.claude/skills/SESSION.lock`, which lists every skill the session is allowed — the only way to catch one arriving from a marketplace plugin or from claude.ai. Tool versions come from `mise ls`, never from parsing `mise.toml` |
| `cmd/dev` | developer tooling, not shipped. `cmd/dev/main.go` only parses arguments; `cmd/dev/browser` holds the code. Every mise task that drives it is named after the command (`dev:browser`) |
| `mise-tasks/` | every task as a file: groups are directories (`svc/`, `hello/`, `keys/`, `deps/`, `release/`, `dev/skills/`), top-level scripts sit at the root |
| `skills.toml` | the single source for the Claude Code session: `[source.*]` blocks (gomod or github) and `[claude]` (blocked marketplace plugins, connectors, MCP approval). The only place a skill, a pin or a plugin is named; `mise run dev:skills:sync` generates `.claude/skills` and the `.claude/settings.json` keys from it |
| `skills/` | the skill this repo's releases ship via packslip |
| `spikes/hello-world` | separate module: the GUI toolchain spike (`mise run hello:*`); see its README |

Rules that keep the design working:

- **One handler.** Never add runtime-specific proxy logic to `main.go` or
  `worker.go`; they only supply an HTTP client and a token store. The Worker and
  the local proxy drifted apart before this layout existed.
- **No package globals and no env reads outside `internal/config`.** Tests build a
  `config.Config` with `config.Load` and a fake `getenv`.
- **Grok tokens only go to api.x.ai.** `config.Load` rejects `auth = "xai-oauth"`
  on any other host; do not add paths around that check.
- **Provider base URLs carry their version path** (`/v1`, `/api/v1`,
  `/openai/v1`). Test routing against more than plain `/v1`.
- **Requests without a body get a nil body.** The Worker's fetch throws on a GET
  with any body, even an empty one, and Go's own client hides this in tests.
- **Check Worker behaviour in workerd, not only with `go test`.** `mise run bench`
  runs both Worker builds locally against the mock.

## Rules

- Keep API keys, Grok tokens and `ADMIN_API_KEY` out of source, logs and commits.
- Cloudflare credentials come from fnox; tasks wrap `fnox exec --`.
- The TinyGo build has no `/mcp` until TinyGo fixes the gaps in
  `.plan/tinygo.md` (tinygo-org/tinygo#5684).
