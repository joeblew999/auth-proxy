# Plan: one dev tool for every repo on this stack

**Status: STEPS 1 AND 2 DONE 2026-09-16 in this repo: every task is a line or
two in `mise.toml`, every piece of logic is a `dev` subcommand with a test, and
`mise-tasks/` and the shell helpers are gone. Step 3 is DEFERRED by the owner
the same day: this repo first, made right on this stack, and nothing extracted
until it is. Not `joeblew999/.github` (the fleet task library: nu bodies in
TOML includes, reusable workflows) and not the go-htmx4 way (a template repo
with `mise run rename`); the owner wants neither used here yet.**

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
| `build`, `build:cli`, `build:dev`, `build:worker` rebuilt every time | each has `sources` and `outputs`; a second `mise run build` finishes in 12 ms with "sources up-to-date, skipping" |
| `dev --mock`, `build --tinygo`, `deploy --tinygo`, `svc:start --mock` | `dev:mock`, `build:tinygo`, `deploy:tinygo`, `svc:start proxy-mock` |
| `setup/url` in bash, `PROXY_URL` guards in five tasks | `dev url [--worker] [--env] [--local]`: every `--worker` flag resolves through it, the subdomain is read once into `mise.local.toml`, a missing credential names its fix |
| `deploy` in bash: copy, wrangler, `grep` for ids | `dev worker deploy`: the same, reporting created ids or "inherited" from a TOML diff |
| `push-keys.sh`, `keys/set` | `dev worker keys push` (names on stdin) and `keys set` (value hidden, generated, or piped) |
| `bench.sh`: background pids, `pkill -P`, "Terminated: 15" on every run | `dev bench`: process groups, clean exit, same table |
| `deps/*`: a hard-coded list of two modules | `dev deps list|upgrade` finds every `go.mod` |
| `dev/mcp`: `grep -i` over `claude mcp list` | `dev mcp check` |
| sizes in `printf` arithmetic, twice | `dev sizes` |

`cmd/dev` is now: `url`, `worker` (deploy, wait, keys), `session`, `browser`,
`bench`, `sizes`, `deps`, `mcp`, `release`. Each is a package; the new ones have
tests (`worker` 8, the rest 1 each). `golang.org/x/term` was added for the
hidden prompt.

**Proved by running**, not by reading: `status`, `status --worker`,
`models --worker`, `url`, `url --env tinygo`, `build` twice, `build:tinygo`,
`dev:session:check`, `dev:hooks:check`, `dev:mcp`, `deps:list`, `bench`,
`deploy` (inherited binding, Worker green), `deploy:tinygo` (inherited, both
keys pushed, hostname waited for), `svc:start proxy-mock`, `models` through it,
`svc:stop`, and `mise run test`. The one thing that failed on the way was the
TinyGo Worker's status check right after its deploy: the hostname answered
`/health` once and then Cloudflare error 1042 on the next request, so `wait`
now demands three 200s in a row.

## Step 3, later: the tool in its own repo

| Generic today (moves) | Project-specific (stays) |
|---|---|
| `url`, `worker`, `session`, `browser`, `mcp`, `deps`, `sizes`, `release` | `bench` (this proxy's requests), the proxy binary's own `status`, `keys`, `chat` |

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
sessionpin and rel.

## Decisions needed

| # | Question | Recommendation |
|---|---|---|
| 0 | When? | Owner's call, once this repo is right. Until then every improvement lands here, where it is proven by `mise run test` and a real deploy |
| 1 | The tool's name and repo | anything but `dev`, which is the binary; pick one that reads as the stack's name |
| 2 | Does `bench` move? | No. It knows this proxy's endpoints |
| 3 | `dev init` overwrites nothing or refuses on an existing file? | Refuses, and names the file |

## Done means

An empty repo reaches a green `mise run test` and a green CI run with two
commands and no file copied by hand. Until then, this repo is the template.
