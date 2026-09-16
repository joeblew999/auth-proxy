- Repo: joeblew999/grok-oauth-proxy

# Development instructions

## Rule zero: nothing to remember

- **Every workflow is a mise task.** `mise tasks` lists them. Do not document or
  rely on raw `go`, `wrangler` or `fnox` commands; if a workflow is missing, add a
  task. **A task names a stage of a command, `dev` does it:** `bin/dev build|run|
  check|deploy|url|logs DIR` reads the directory and knows what a Go main, a
  package.json, gsx sources and a wrangler.toml need. A task is one line in
  `mise.toml`; anything with a branch, a loop, a parse or an API call is in
  `cmd/dev`, with a test. No shell scripts behind tasks; the Claude Code hook in
  `.claude/hooks/` is the one script left, because it has to work before anything
  is built.
- **Every error names its fix.** A config error names the setting, a missing key
  prints `mise run keys:set <provider>`, and so on. Keep it that way in new code.
- **Providers live only in `providers.toml`.** Secret values live only in fnox
  (`mise run keys:set`) and Worker secrets (`mise run keys:push`).
- **hk owns the checks.** `mise run lint` runs them (`hk check --all`: gofmt,
  vet, tidy, whitespace, secrets). Ask the hk MCP server to plan checks before
  execution; scope to changed files; prefer safe fixes. A Stop hook runs
  `hk run check --safe` after every agent turn, and the pre-commit hook runs
  the same on commit. `hk run check` must stay cheap: the expensive,
  login-gated `dev:session:verify` is bound to **pre-push** only.

## Working rules from the owner

These apply to every AI agent working here. They exist because each was broken
at least once.

- **Nothing outside this repo.** No global Claude Code skills, plugins, memory or
  settings, and no files left in temp or home directories. Skills, agent rules and
  settings live in the repo (`.claude/`, `AGENTS.md`, `CLAUDE.md`) and are
  committed.
- **Skills are in the repo, pinned, synced by mise, and proven to load.** Invoke
  the relevant skill before the work it covers: gsx before any `.gsx`, the
  Cloudflare skills before wrangler config or Durable Objects. A PreToolUse hook
  (`.claude/hooks/skill-gate`) does this for `.gsx`, `.pkl` and wrangler config
  on every write, by any tool; `mise run dev:hooks:check` proves it. GUI work
  uses only the gsx and gsxui CLIs and patterns from gsxui's own site; never
  hand-invented components.
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

Four commands, a stage each, and `bin/dev <stage> <dir>` reads the directory
and does the rest. `mise tasks` lists everything; these are the ones to know.

| Task | Purpose |
|---|---|
| `mise run server:run` (alias `dev`) / `server:mock` | the proxy on `127.0.0.1:56121` with real providers, or against the bundled mock |
| `mise run worker:deploy` (alias `deploy`) / `worker:deploy:tinygo` | validate `providers.toml`, then deploy the proxy Worker; or its TinyGo build as the `-tinygo` Worker |
| `mise run worker:url` (alias `url`) / `worker:logs` (alias `logs`) / `worker:run` | this clone's proxy Worker URL, its live logs, the Worker on local workerd |
| `mise run gui:run` / `gui:dev` / `gui:workerd` / `gui:deploy` / `gui:url` | the GUI natively, with live reload, on local workerd, deployed, its URL |
| `mise run status` / `models` / `chat <model> [prompt]` / `login` | the proxy CLI against the local proxy; `--worker` for the deployed one |
| `mise run setup` | one-time: Worker URL, client key if missing, every key pushed, what is still missing |
| `mise run keys:set <name\|provider\|admin>` / `keys:push` | store a secret in fnox and push it; push everything the project's `secrets` task lists |
| `mise run build` / `<cmd>:build` | every command, or one; a Worker's wasm per environment (`--env tinygo`) |
| `mise run check` / `<cmd>:check` | the project's checks, or one command's: gsx fmt, vet, test, the workerd round trip, the browser probe |
| `mise run test` / `lint` | lint, the session and hook checks, then `check`; hk alone |
| `mise run bench` | Go vs TinyGo proxy Worker in local workerd against the mock |
| `mise run svc:start <daemon>` / `svc:stop` / `svc:status` / `svc:logs <daemon>` | the daemons in `pitchfork.toml` (`proxy`, `proxy-mock`, `mock`), so nothing blocks a terminal |
| `mise run dev:session:sync` / `dev:session:verify` / `dev:session:bump` | re-sync the pinned Claude Code skills; hold a fresh session against `SESSION.lock` (`--update` re-records); move github pins to upstream HEAD |
| `mise run dev:bootstrap` / `dev:mcp` / `dev:hooks:check` / `dev:browser` | wire a fresh clone (runs after `mise install`); every MCP server connects; the skill hook fires right; drive an app in a headless Chrome |
| `mise run deps:list` / `deps:upgrade` | Go module upgrades in every module |
| `mise run release <version>` / `release:snapshot` | publish a GitHub Release fully locally; build the artifacts only |

Every task is one line in `mise.toml`. The file has two halves: **the stack**,
which names nothing of this project and is meant to be identical in every repo
on it, reaching the project only through `[vars]` and three tasks the project
supplies (`check`, what `test` runs; `validate`, what `deploy` runs first;
`secrets`, the `NAME<TAB>OWNER` lines `keys:*` work from); and **this
project**, a line per command and stage. What a line cannot say it hands to
`bin/dev` (`cmd/dev`). Run `mise run lint` after every Go change, `mise run
test` before committing.

## Code layout

| Path | Owns |
|---|---|
| `internal/bootstrap/providers.toml` (`providers.toml` symlinks to it) | the providers, built into every binary and the Worker |
| `internal/config` | parsing and validating providers, resolving secrets; **the only code that reads the environment** (through the `getenv` it is given) |
| `internal/router` | model name → provider, URL joining, model rewriting, merged `/v1/models`; no I/O |
| `internal/proxy` | the HTTP handler used by **both** runtimes: client key, routing, provider auth, streaming, admin routes |
| `internal/xaiauth` | the Grok login: browser PKCE, device flow, refresh, token stores |
| `internal/mcp` | MCP tools `ask` and `list_models` (excluded from TinyGo builds) |
| `cmd/server` | local CLI: `serve`, `status`, `models`, `chat`, `login`, `keys` |
| `cmd/worker` | the proxy as a Cloudflare Worker: entry point (fetch client, KV token store), **its own `wrangler.toml`** and its gitignored `build/`. A Worker owns everything about itself; nothing of it lives at the root |
| `cmd/gui` | separate module: the GUI spike, a native server and a Cloudflare Worker from one handler, with its own `wrangler.toml` (`mise run gui:*`); see its README |
| `cmd/dev` | the stack's developer tool, not shipped and not project-specific: the stages `build`, `run`, `check` (package `app`, which reads a command directory) and `deploy`, `url`, `logs`, `smoke`, `wait`, `keys` (package `worker`, every one taking the Worker's directory), plus `session`, `browser`, `sizes`, `deps`, `mcp`, `release`. Each is a package with tests |
| `cmd/bench` | this proxy's Go vs TinyGo comparison in local workerd; project code, so not in `cmd/dev` |
| `internal/bootstrap` | which providers file is used: `--config`, `PROVIDERS_TOML`, or the built-in one |
| `cmd/mock-upstream` | OpenAI-compatible mock plus its two-provider config |
| `cmd/dev/rel` | release tooling behind `dev release ...` (`snapshot`, `packslip`, `publish`) |
| `.claude/skills/SESSION.lock` | every skill a session here is allowed to have, this repo's and Claude Code's alike. Written by `mise run dev:session:verify --update`, checked at pre-push. It is what catches a skill arriving from a marketplace plugin or from claude.ai — the latter cannot be blocked by any project setting, only noticed |
| `skills/` | the skill this repo's releases ship via packslip |
| `session.toml` + `cmd/dev/sessionpin` | what this repo pins: `[source.*]` blocks (gomod or github) and `[claude]` (blocked marketplace plugins, connectors, MCP approval). `mise run dev:session:sync` generates `.claude/skills` and the `.claude/settings.json` keys from it |

Rules that keep the design working:

- **One handler.** Never add runtime-specific proxy logic to `cmd/server` or
  `cmd/worker`; they only supply an HTTP client and a token store. The Worker and
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
- **A Worker owns its `wrangler.toml` and its `build/`**, in its own directory
  (`cmd/worker`, `cmd/gui`). Every `dev` stage takes the directory, so a second
  Worker is another directory and a few one-line tasks, not another set of
  tooling. An environment whose `main` lives under `build/tinygo` is built with
  TinyGo; any other with Go.
- **`wrangler.toml` names no account and no resource id.** The account is
  `CLOUDFLARE_ACCOUNT_ID` from fnox, the KV namespace is provisioned per account
  on the first deploy and stays linked, and `deploy` runs wrangler on a throwaway
  copy because wrangler writes ids back into the config it deploys from. The
  Worker URL is per account too: `dev url` reads the account's workers.dev
  subdomain once into gitignored `mise.local.toml`, and `mise run url` prints
  it.

## Rules

- Keep API keys, Grok tokens and `ADMIN_API_KEY` out of source, logs and commits.
- Cloudflare credentials come from fnox; tasks wrap `fnox exec --`.
- The TinyGo build has no `/mcp` until TinyGo fixes the gaps in
  `.plan/done/tinygo.md` (tinygo-org/tinygo#5684).
