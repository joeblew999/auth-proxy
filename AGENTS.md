- Repo: joeblew999/auth-proxy

# Development instructions

## Rule zero: nothing to remember

- **Every workflow is a mise task.** `mise tasks` lists them. Do not document or
  rely on raw `go`, `wrangler` or `fnox` commands; if a workflow is missing, add a
  task. **A task names a stage of a command, `dev` does it:** `dev build|run|
  check|deploy|url|logs DIR` reads the directory and knows what a Go main, a
  package.json, gsx sources, a wrangler.toml and a fly.toml need. A task is one
  line in `mise.toml`; anything with a branch, a loop, a parse or an API call is
  in the `dev` tool (`github.com/joeblew999/dev`, pinned under `[tools]`), with
  a test. No shell scripts behind tasks; the Claude Code hook in
  `.claude/hooks/` is the one script left, because it has to work before anything
  is built.
- **Every error names its fix.** A config error names the setting, a missing key
  prints `mise run secrets:set <provider>`, and so on. Keep it that way in new code.
- **Providers live only in `providers.toml`.** Secret values live only in fnox
  (`mise run secrets:set`) and Worker secrets (`mise run secrets:push`).
- **hk owns the checks.** `mise run lint` runs them (`hk check --all`: gofmt,
  vet, tidy, whitespace, secrets). Ask the hk MCP server to plan checks before
  execution; scope to changed files; prefer safe fixes. A Stop hook runs
  `hk run check --safe` after every agent turn, and the pre-commit hook runs
  the same on commit. `hk run check` must stay cheap: the expensive,
  login-gated `session:verify` is bound to **pre-push** only.

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
  on every write, by any tool; `mise run hooks:check` proves it. GUI work
  uses only the gsx and gsxui CLIs and patterns from gsxui's own site; never
  hand-invented components.
- **Use tools the way their authors document them.** Do not work around a tool's
  intended path (for example gsx's Vite starter).
- **Once something works, put it in mise and the plan.** No manual steps, no
  reminders in chat (such as "restart Claude Code"): a task does it or tells the
  developer.
- **It must work for every developer**, who all use mise and fnox. No personal
  account IDs, URLs or paths in committed files.
- **One-time, per machine:** mise lists a packslip tool's releases through
  GitHub's API, anonymously unless it has a token, and gh keeps its token in
  the keyring where mise cannot read it. Tell mise once, in your own global
  settings (a project may not):
  `mise settings set github.credential_command "gh auth token"`.
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

Four commands, a stage each, and `dev <stage> <dir>` reads the directory
and does the rest. `mise tasks` lists everything; these are the ones to know.

| Task | Purpose |
|---|---|
| `mise run proxy:run` (alias `dev`) / `proxy:run:mock` | the proxy on `127.0.0.1:56121` with real providers, or against the bundled mock |
| `mise run proxy:deploy` (alias `deploy`) / `proxy:deploy:tinygo` | validate `providers.toml`, then deploy the proxy Worker; or its TinyGo build as the `-tinygo` Worker |
| `mise run proxy:url` (alias `url`) / `proxy:logs` (alias `logs`) / `proxy:workerd` | this clone's proxy Worker URL, its live logs, the Worker on local workerd |
| `mise run gui:run` / `gui:run:dev` / `gui:workerd` / `gui:deploy` / `gui:url` | the GUI natively, with live reload, on local workerd, deployed, its URL |
| `mise run proxy:delete` / `gui:delete` (`--name OLD`, `--yes`) | remove a deployed Worker and the KV namespace wrangler provisioned for it: a developer's suffixed copy, or one a rename left behind |
| `mise run status` / `models` / `chat <model> [prompt]` / `login` | the proxy CLI against the local proxy; `proxy:status:worker` and the rest against the deployed Worker |
| `mise run setup` | one-time: Worker URL, client key if missing, every key pushed, what is still missing |
| `mise run secrets:set <name\|provider\|admin> [--generate]` / `secrets:push [--env tinygo]` | store a secret in fnox and push it; push everything the project's `secrets` task lists |
| `mise run secrets:ci <name>...` | give this repo's GitHub Actions a secret from fnox (`PACKSLIP_SIGNING_KEY`, so the on-demand release signs with the same key) |
| `mise run build` / `<cmd>:build` | every command, or one; a Worker's wasm per environment (`--env tinygo`) |
| `mise run check` / `<cmd>:check` | the project's checks, or one command's: gsx fmt, vet, test, the workerd round trip, the browser probe |
| `mise run test` / `lint` | lint, the session and hook checks, then `check`; hk alone |
| `mise run bench` | Go vs TinyGo proxy Worker in local workerd against the mock |
| `mise run svc:start <daemon>` / `svc:stop` / `svc:status` / `svc:logs <daemon>` | the daemons in `pitchfork.toml` (`proxy`, `proxy-mock`, `mock`), so nothing blocks a terminal |
| `mise run session:sync` / `session:verify` / `session:bump` | re-sync the pinned Claude Code skills; hold a fresh session against `SESSION.lock` (`--update` re-records); move github pins to upstream HEAD |
| `mise run bootstrap` / `session:mcp` / `hooks:check` | wire a fresh clone (runs after `mise install`); every MCP server connects; the skill hook fires right |
| `mise run deps:list` / `deps:upgrade` | Go module upgrades in every module |
| `mise run release <version>` / `release:snapshot` | publish a GitHub Release of the proxy from this machine, signed with the key in fnox (`release --keygen` makes it once); or build, sign and verify without publishing. The release workflow runs the same on demand, for a large body of work |

Every task is one line in `mise.toml`. The file has two halves: **the stack**,
which names nothing of this project and is meant to be identical in every repo
on it, reaching the project only through `[vars]` and three tasks the project
supplies (`check`, what `test` runs; `validate`, what `deploy` runs first;
`secrets`, the `NAME<TAB>OWNER` lines `keys:*` work from); and **this
project**, a line per command and stage. What a line cannot say it hands to
`dev`, the stack's tool, pinned like fnox and hk (`github.com/joeblew999/dev`;
`dev` alone lists every verb, and its skill is linked into `.claude/skills/dev`
by `mise install`). Run `mise run lint` after every Go change, `mise run
test` before committing.

## Code layout

| Path | Owns |
|---|---|
| `cmd/proxy/internal/bootstrap/providers.toml` (`providers.toml` symlinks to it) | the providers, built into every binary and the Worker |
| `cmd/proxy/internal/config` | parsing and validating providers, resolving secrets; **the only code that reads the environment** (through the `getenv` it is given) |
| `cmd/proxy/internal/router` | model name → provider, URL joining, model rewriting, merged `/v1/models`; no I/O |
| `cmd/proxy/internal/proxy` | the HTTP handler used by **both** runtimes: client key, routing, provider auth, streaming, admin routes |
| `cmd/proxy/internal/xaiauth` | the Grok login: browser PKCE, device flow, refresh, token stores |
| `cmd/proxy/internal/mcp` | MCP tools `ask` and `list_models` (excluded from TinyGo builds) |
| `cmd/proxy` | local CLI: `serve`, `status`, `models`, `chat`, `login`, `keys` |
| `cmd/proxy` | the proxy, its own Go module: `main.go` is the CLI (`serve`, `status`, `models`, `chat`, `login`, `keys`), `worker.go` the Cloudflare Worker (fetch client, KV token store), **its own `wrangler.toml`**, its gitignored `build/`, and `internal/` with everything the two share. A command owns everything about itself; nothing of it lives at the root |
| `cmd/gui` | the GUI spike, its own Go module: a native server and a Cloudflare Worker from one handler, with its own `wrangler.toml` (`mise run gui:*`); see its README |
| `dev` (pinned tool, `github.com/joeblew999/dev`) | the stack's developer tool, not this repo's code: `stage` (`build`, `wasm`, `check`, `run`, `workerd`, from what a command directory holds), `app` (`deploy`, `url`, `logs`, `smoke`, `wait`, `delete`, to Cloudflare Workers or Fly by whether the directory holds a `wrangler.toml` or a `fly.toml`), `secrets`, `session` (the Claude Code session, MCP servers included), `release`, `deps`. Every verb has one shape; each package has tests; `dev skill` renders its skill from the verbs' own usage. A fix to it is a release there and a pin bump here |
| `cmd/bench` | this proxy's Go vs TinyGo comparison in local workerd; its own module, project code, so not in the `dev` tool |
| `go.work` | the one file at the root that knows Go: it lists the four modules, so a build or test in any of them sees the others |
| `cmd/proxy/internal/bootstrap` | which providers file is used: `--config`, `PROVIDERS_TOML`, or the built-in one |
| `cmd/mock-upstream` | OpenAI-compatible mock plus its two-provider config |
| `dev release` | `dev release DIR [VERSION] [--snapshot]`: goreleaser, packslip, gh, from conventions (binary named after the repo, every `skills/*` shipped, goreleaser config generated) |
| `.claude/skills/SESSION.lock` | every skill a session here is allowed to have, this repo's and Claude Code's alike, and which Claude Code recorded it. Written by `mise run session:verify --update`, checked at pre-push. A Claude Code upgrade changes the built-ins, so verify re-records the lock for it and says what moved; anything arriving without an upgrade fails: a marketplace plugin, or a skill synced from claude.ai, which no project setting can block, only notice |
| `skills/` | the skill this repo's releases ship via packslip |
| `session.toml` + `dev session` | what this repo pins: `[source.*]` blocks (a GitHub repo at a commit, for an upstream that ships no releases) and `[claude]` (blocked marketplace plugins, connectors, MCP approval). `mise run session:sync` generates `.claude/skills` and the `.claude/settings.json` keys from it |

Rules that keep the design working:

- **One handler.** Never add runtime-specific proxy logic to `cmd/proxy` or
  `cmd/proxy`; they only supply an HTTP client and a token store. The Worker and
  the local proxy drifted apart before this layout existed.
- **No package globals and no env reads outside `cmd/proxy/internal/config`.** Tests build a
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
  (`cmd/proxy`, `cmd/gui`). Every `dev` stage takes the directory, so a second
  Worker is another directory and a few one-line tasks, not another set of
  tooling. An environment whose `main` lives under `build/tinygo` is built with
  TinyGo; any other with Go.
- **Deploying beside someone:** `mise set --file mise.local.toml DEPLOY_SUFFIX=<you>`
  once, and every Worker you deploy is `<name>-<you>`, with its URL, logs and
  secrets following. Unset, as in CI and on the shared production deploy, means
  the committed name. Nothing personal reaches a committed file.
- **`wrangler.toml` names no account and no resource id.** The account is
  `CLOUDFLARE_ACCOUNT_ID` from fnox, the KV namespace is provisioned per account
  on the first deploy and stays linked, and `deploy` runs wrangler on a throwaway
  copy because wrangler writes ids back into the config it deploys from. The
  Worker URL is per account too: `dev url` reads the account's workers.dev
  subdomain once into gitignored `mise.local.toml`, and `mise run url` prints
  it.

## Rules

- **Releases are local and signed with one long-lived key.** The key lives in
  fnox as `PACKSLIP_SIGNING_KEY` and in the repo's Actions secrets; its public
  half is what a consumer pins (`pubkey` on the packslip tool). mise remembers
  a project's signer the way SSH remembers hosts, so a change of signer is
  refused until `mise packslip forget <project>` says so, once per machine.
- Keep API keys, Grok tokens and `ADMIN_API_KEY` out of source, logs and commits.
- Cloudflare credentials come from fnox; tasks wrap `fnox exec --`.
- The TinyGo build has no `/mcp` until TinyGo fixes the gaps in
  `.plan/done/tinygo.md` (tinygo-org/tinygo#5684).
