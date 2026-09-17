# Plan: one dev tool for every repo on this stack

**Status: DONE 2026-09-17, step (d) included. The tool is
`github.com/joeblew999/dev`, released by packslip and pinned here and in the
two gsx forks; `dev init` scaffolds a new repo, proven on an empty one.**

## The question

The owner, 2026-09-16: the file tasks in `mise-tasks/` are a mess. Tasks belong
in the root `mise.toml`, using what mise does well (`depends`, `sources` and
`outputs` for idempotency, `usage` for arguments), and anything complex belongs
in a Go `dev` command. The same stack (mise, fnox, hk, packslip, pitchfork,
wrangler, Go, gsx and gsxui, a pinned Claude Code session) is used on many
projects, so getting a new repo going with it must be easy. Is that a good idea?

## Answer

**Yes, with one rule that makes it hold: a task orchestrates, `dev` computes.**

What was actually wrong was not files versus TOML. It was logic in bash. The
tree had 41 task files and two helper scripts, and the ones with branches,
loops and parsing were where the bugs lived: a `ps` parser that was wrong twice
before it became Go, a `sed` that missed one space in a JSON reply, a `grep`
whose empty result failed a deploy that had succeeded. None of it had a test.
Bash has no way to say "this error names its fix" except by hand, every time.

What mise does well is orchestration: which tasks a task needs, which files
mean a build is already done, what arguments it takes. That is a line or two
per task, and TOML in one file is a better index than 41 files for a human and
for an agent alike. What Go does well is everything else, with a test.

So the rule: a task is `run`, `depends`, `sources`/`outputs`, `usage`, `dir`,
`raw`. A flag that only fills in a command line stays a flag (`--worker`). A
flag that used to pick between two command sequences becomes a sibling task
(`build:tinygo`, `deploy:tinygo`, `dev:mock`, `svc:start proxy-mock`), because
that branch would otherwise be bash again. Anything with a branch, a loop, a
parse or an API call is a subcommand of `dev`.

The caveat, and the reason for step 3: a `cmd/dev` copied into every project
drifts, and drift across projects is what "generalised" is meant to end. The
generic part must be one released tool, pinned like fnox and hk.

## What changed today (steps 1 and 2)

| Before | After |
|---|---|
| 41 files in `mise-tasks/`, `tools/bench.sh`, `tools/push-keys.sh` | 43 tasks in `mise.toml`, none longer than five `run` lines, none with a branch |
| `build`, `build:cli`, `dev:build`, `build:worker` rebuilt every time | each has `sources` and `outputs`; a second `mise run build` finishes in 12 ms with "sources up-to-date, skipping" |
| `dev --mock`, `build --tinygo`, `deploy --tinygo`, `svc:start --mock` | `dev:mock`, `build:tinygo`, `deploy:tinygo`, `svc:start proxy-mock` |
| `setup/url` in bash, `PROXY_URL` guards in five tasks | `dev url [--worker] [--env] [--local]`: every `--worker` flag resolves through it, the subdomain is read once into `mise.local.toml`, a missing credential names its fix |
| `deploy` in bash: copy, wrangler, `grep` for ids | `dev worker deploy`: the same, reporting created ids or "inherited" from a TOML diff |
| `push-keys.sh`, `keys/set` | `dev secrets push` (names on stdin) and `keys set` (value hidden, generated, or piped) |
| `bench.sh`: background pids, `pkill -P`, "Terminated: 15" on every run | `dev bench`: process groups, clean exit, same table |
| `deps/*`: a hard-coded list of two modules | `dev deps list|upgrade` finds every `go.mod` |
| `dev/mcp`: `grep -i` over `claude mcp list` | `dev mcp check` |
| sizes in `printf` arithmetic, twice | `dev sizes` |

`cmd/dev` is now one package per thing, named as the tasks name it: `stage`,
`worker`, `secrets`, `session`, `release`, `deps`, `fnox`. Every verb has the
one shape in `internal/cli`; each package has tests. `golang.org/x/term` was added for the hidden prompt.

**The shape that makes step 3 a copy, not a port (2026-09-16):** the owner's
rule, "a handful of commands, a task per stage of each, the dev tooling does
the rest". `dev build|run|check DIR` reads a command directory (Go main,
package.json, gsx sources, wrangler.toml) and knows what each needs; the Worker
stages take the directory too. So the project half of `mise.toml` is a grid,
one line per command and stage, and `mise.toml` has a stack half and a project
half. The stack half names nothing of this
project. It reaches the project through `[vars]` (`app`, `worker`) and three
tasks the project must supply: `check` (what `test` runs after the stack's own
checks), `validate` (what `deploy` runs first) and `secrets` (`NAME<TAB>OWNER`
lines; `secrets:push` pipes them to `dev secrets push`, `secrets:set` resolves a
provider name or `admin` against them). `dev init` will write the stack half
verbatim and stub the three.

**Proved by running**, not by reading: `status`, `status --worker`,
`models --worker`, `url`, `url --env tinygo`, `build` twice, `build:tinygo`,
`session:check`, `hooks:check`, `mcp:check`, `deps:list`, `bench`,
`deploy` (inherited binding, Worker green), `deploy:tinygo` (inherited, both
keys pushed, hostname waited for), `svc:start proxy-mock`, `models` through it,
`svc:stop`, and `mise run test`. The one thing that failed on the way was the
TinyGo Worker's status check right after its deploy: the hostname answered
`/health` once and then Cloudflare error 1042 on the next request, so `wait`
now demands three 200s in a row.

## Step 3, later: the tool in its own repo

| Generic (all of `cmd/dev`, moves) | Project-specific (stays) |
|---|---|
| `url`, `worker`, `session`, `browser`, `mcp`, `deps`, `sizes`, `release` | `cmd/bench` (this proxy's requests), `cmd/mock-upstream`, the proxy binary's own `status`, `keys`, `chat` |

Layout, settled 2026-09-16 on the owner's question "should the wrangler be
with each Worker too": yes. A Worker owns its `wrangler.toml` and its `build/`
in its own directory (`cmd/proxy`); `dev url` and `dev worker ...` take
`--dir`, and the tasks pass `{{vars.worker}}`. The second Worker, the GUI,
proved it the same day: `cmd/gui/wrangler.toml`, a `worker.go` beside
`main.go`, four one-line tasks, and no new tooling beyond `dev worker smoke`,
which any Worker directory can use. `cmd/dev` holds nothing of this project any more: the bench moved
to `cmd/bench` and the mock to `cmd/mock-upstream`.

- **Repo** `github.com/joeblew999/<name>`, releasing `dev` with goreleaser and
  packslip; `dev release` already does that. Pinned in every project as
  `packslip:github.com/joeblew999/<name>`, so mise links its skill by version,
  the same path as fnox, hk, gsx and gsxui. `cmd/dev` here is deleted and the
  tasks call `dev` from mise's PATH.
- **`dev init`** scaffolds a new repo from this one's files as templates:
  `mise.toml` (tools, hooks, settings, the generic tasks), `hk.pkl`,
  `session.toml`, `.claude/settings.json` and `hooks/skill-gate`, `.mcp.json`,
  the two workflows, `.gitignore`, and an `AGENTS.md` skeleton with the rules.
- **Sequence:** (a) move `cmd/dev` as it is, tests included; (b) release
  v0.1.0; (c) pin it here, delete `cmd/dev`, `mise run test` green;
  (d) `dev init`; (e) prove it on an empty repo: `mise use`, `dev init`,
  `mise install`, `mise run test`, and CI green, with nothing copied by hand.
- **Where project-specific Go tooling lives once `cmd/dev` is gone:** as
  subcommands of the project's own binary where they fit (`bench` could be
  `grok-oauth-proxy bench`), else `cmd/<project>tool`.

This supersedes item 9 of `.plan/done/claude-session.md`, which moved only
the session pinner and the release tool.

## Step 3, 2026-09-16 evening: what happened

Sequence (a) and the local half of (b) are done; (c) waits on the repo.

- **(a) moved.** `cmd/dev` is the root of `github.com/joeblew999/dev`
  (module `github.com/joeblew999/dev`, `main.go` at the root, the same
  packages). Its own stack files: `mise.toml` (build, check, test, skill,
  release, release:snapshot; `bootstrap` after `mise install`), `hk.pkl`, the
  two workflows, `AGENTS.md`. Tests moved with it; `mise run test` is green.
- **The skill is generated.** `dev skill` renders `skills/dev/SKILL.md` from
  the verb table's own usage strings, and `dev skill --check` (in `check`)
  fails when it drifts. This is the owner's "take a golang cli and generate
  the stuff an AI needs": the manual is the code's. MCP tool schemas can come
  from the same table later.
- **Fly beside Cloudflare** (owner, same evening: the gsx forks must stay
  deployable the way upstream does it, and upstream uses Fly for what is not
  a Worker). `app` reads the directory: `wrangler.toml` means Workers,
  `fly.toml` means Fly, both is an error. `deploy`, `url`, `logs`, `wait` and
  `secrets` are the same verbs on both; a Fly deploy runs `flyctl deploy
  --config DIR/fly.toml .` from the repo root as build context, which is how
  gsxhq/gsx and gsxhq/gsxui deploy, with anything after `--` going to flyctl
  (`--ha=false`, `--remote-only`). Secrets go through `flyctl secrets import`
  on stdin. **Not exercised for real:** no Fly account or token exists on this
  machine, so the Fly path is proven only through the tool's exec seam.
- **Names generalised:** `WORKER_SUFFIX` is `DEPLOY_SUFFIX` (a Fly app is not a
  Worker), `dev url --worker` is `dev url --deployed`. The `worker` package is
  `cloudflare`.
- **A snapshot before the first tag** derived a bare commit as the version,
  which packslip rejects; fixed (`0.0.0-<commit>`), and the snapshot now signs
  and verifies with the skill in the manifest.
- **(b) and (c) done 2026-09-17.** The public repo exists; the release
  workflow publishes on a tag. Three releases in an hour, each for a real
  defect the pin found: v0.1.0's manifest had no download URLs (packslip
  infers none from `--source-repo`; the official action passes `--url-base`,
  so `dev release` now does too); a bare `--update` parsed as false, so
  `session:verify --update` refused the change it was asked to record. v0.1.2
  is pinned. `mise install` puts `dev` on PATH and links `.claude/skills/dev`;
  the skill shows up in a session; `mise run test` is green with it, the
  proxy deployed for real through it and answered its status check.
- **Two things a developer may hit.** mise reads GitHub's API to list a
  packslip tool's releases, anonymously by default; a machine that has used
  its 60 requests an hour gets a 403 until the hour turns, and mise caches
  that failure (`mise cache clear`). A token in `GITHUB_TOKEN` or
  `MISE_GITHUB_TOKEN` lifts both; CI has one from mise-action. No repo secret
  is involved anywhere: a release signs with the workflow's own identity and
  uploads with the token every run has.

## Later, per the owner (2026-09-16)

Rename this repo to `auth-proxy` and detach it from the `dvcrn` fork: it is
not Grok-specific and far from what it started as. Cheap now: the binary, the
Workers and the skill are named from the repo slug, so a rename is the module
path in five `go.mod` files, the `name` in two `wrangler.toml`, the skill
directory, and the docs. Detaching a fork is a GitHub support request or a
fresh repo with the history pushed. Not started.

## 2026-09-17, later: (d) init, the forks, local releases

- **`dev init` (v0.2.0).** Writes the stack into a new repo from templates
  embedded in the binary: each is the file this repo proved (mise.toml with
  the tools pinned and the stack tasks, hk.pkl, session.toml, .mcp.json, the
  Claude Code settings and skill hook with its test, the two workflows,
  .gitignore, AGENTS.md, CLAUDE.md, README.md) plus a first command,
  `cmd/<name>` answering `/health`, its module and go.work. The module path
  comes from the git remote. Existing files are left alone and named. It pins
  the version of the `dev` that ran it. **Proven on an empty repo:** `mise x
  packslip:github.com/joeblew999/dev@0.2.0 -- dev init`, `mise install`,
  `mise run test` green (lint, session check, hook test, the command's vet
  and tests), `/health` answering. Its CI run was not exercised: that needs a
  repo created for it, which is the one act these sessions cannot do.
- **Two defects the proof found and the tool fixed:** the session check
  refused a repo that vendors no skills (an empty `SKILLS.lock` is now
  fine), and the hook test expected gsx and wrangler skills a new repo does
  not have (it now expects only the skills the repo has; this repo's copy is
  the template's source and changed the same way).
- **The forks.** `joeblew999/gsx` and `joeblew999/gsxui` pin the tool.
  `mise run release` is `dev release .`, which names every binary their own
  `.goreleaser.yml` builds (gsx and gsx-typebundle; gsxui and stylegen) in the
  manifest, with their skill; snapshots proven for both. The playground and
  the site deploy to Fly through `dev deploy` from the repo root, as
  upstream's workflows do; the URLs compose, and a deploy without a login
  fails naming its fix. Upstream's Makefiles and workflows are untouched.
- **Releases are local now** (owner, the same evening: GitHub's workflow
  quota makes CI releases slow; a GitHub-run release is for a large body of
  work). `mise run release <version>` tags, builds, signs with a throwaway
  key and uploads. Whether mise installs from a key-signed, unlogged
  manifest is proven the first time one is pinned.
- **This repo is `auth-proxy`.** Renamed on GitHub (the old URL redirects),
  the module paths, both Workers (`auth-proxy`, `auth-proxy-tinygo`,
  `auth-proxy-gui`), the skill and the docs renamed, the Workers deployed
  under the new names with their secrets, and the old ones deleted through
  `dev delete` (below). The hand-made `GROK_AUTH` namespace the first Worker
  used stays; it is the owner's. The Grok login tokens lived in the old
  Worker's KV, so `mise run login --worker` once. Leaving the fork network
  is a button GitHub gives no API for: Settings, Danger Zone, "Leave fork
  network".

## 2026-09-17, later still: delete, and how a release is signed

- **`dev delete DIR [--env] [--name] [--yes]` (v0.3.0).** The owner, on
  seeing the old Workers left behind: the tool needs to delete on
  Cloudflare. A Worker goes with the KV namespaces wrangler provisioned for
  it, found by the title wrangler gives them, `<worker>-<binding>` in
  lowercase, so a namespace made by hand is never touched; a Fly app goes
  with `flyctl apps destroy`. It says what will go and asks unless `--yes`.
  Tasks `proxy:delete` and `gui:delete`; the three old Workers went through
  them, with the one namespace wrangler had provisioned.
- **Releases are local now, and the signing had to change for it.** A local
  release first signed with a throwaway key, unlogged: mise refused (no
  transparency log entry). Logged, mise still refused: a key-signed bundle
  needs its key pinned, and a throwaway key would need a new pin every
  release. So there is one long-lived key: `dev release --keygen` makes it
  once, into fnox as `PACKSLIP_SIGNING_KEY` and into the repo's Actions
  secrets (`dev secrets ci`, gh on stdin), its public half committed as
  `packslip.pub`. Every release signs with it; consumers pin
  `pubkey = "..."` on the tool, and `dev init` writes that pin from the
  public key baked into the binary. mise pins a project's signer the way SSH
  pins hosts: the switch from the workflow's identity to the key was refused
  until `mise packslip forget packslip:github.com/joeblew999/dev`, once per
  machine that had installed an earlier release.
- **A release is published exactly once.** Pushing the tag of a local
  release also ran the CI release, which rebuilt and re-published the same
  version seconds later; a consumer that had fetched the first manifest then
  failed on the second's assets. The release workflow is on demand now, with
  a version input, running the same task with the same key; nothing runs on
  a tag push. The tool, this repo, the scaffold and both forks have that
  workflow.
- **Owner's acts left:** `mise run secrets:ci PACKSLIP_SIGNING_KEY` here and
  in both forks, so their on-demand workflow can sign (an agent here may not
  write a secret store); the fork network; the hand-made namespace.

## Decisions needed

| # | Question | Recommendation |
|---|---|---|
| 0 | When? | Decided 2026-09-16 evening: now ("do it all"). Every improvement to the tool lands in its repo and is proven here by the pin |
| 1 | The tool's name and repo | `joeblew999/dev`, proposed and not objected to. The binary and the repo share the name, which `release` handles (the binary is named after the repo) |
| 2 | Does `bench` move? | No. It knows this proxy's endpoints |
| 3 | `dev init` overwrites nothing or refuses on an existing file? | Refuses, and names the file. Done: it writes what is missing, keeps and names what exists |

## Done means

An empty repo reaches a green `mise run test` and a green CI run with two
commands and no file copied by hand. Until then, this repo is the template.
