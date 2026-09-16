# Plan: one source for the Claude Code session

**Status: PHASE 1 DONE. Every check now runs without being asked: `check` in
`mise run test`, `verify` and the MCP health check at pre-push, and the hooks
themselves installed by `mise install`. Items 1-3 turned out to be impossible
and are struck out below. Item 9 is the only thing left and it needs a yes.
2026-09-16**

## The question

Can this repo decide, on its own and provably, which skills, plugins and MCP
servers a Claude Code session here has — so that a fresh clone on someone else's
machine gets the same session as the owner's?

## Answers

| Question | Answer |
|---|---|
| Did skills alone decide the session? | **No.** `enabledPlugins` lives in `~/.claude/settings.json`, so a marketplace plugin nobody here named added 11 skills, 4 of them second copies of skills this repo pins |
| Were the copies the same? | **No.** Same upstream, six months apart: the repo pinned `b052c32` (2026-09-08), the plugin shipped `d311303` (2026-03-03). Which one a session loaded was luck |
| Can a repo override a developer's own setting? | **Yes.** `enabledPlugins` reads user < project < local, so `false` in `.claude/settings.json` wins. `disableClaudeAiConnectors` is any-source-true: a repo can opt out, never back in |
| Can every source be pinned? | **No.** Skills synced from claude.ai are not read from project settings at all. No repo file can block them; the most a repo can do is notice them |
| Does mise already own skills? | **Yes, for any tool that ships one.** `mise skills ls` maps skill → tool → version → path, and `[settings.skills] auto_sync` links them. gsx, gsxui, hk-configure and hk-debug arrive that way, declared nowhere but `[tools]` |
| Then why does `skills.toml` exist? | **One gap only:** `cloudflare/skills` has no packslip release, so mise cannot pin it. Everything under `[source.*]` is scaffolding around that single missing package |

## The shape this has to take

mise is the spine, and every tool hangs off it the same way: pinned under
`[tools]`, driven by a task in `mise-tasks/`, bootstrapped by `[hooks]`. fnox
holds the secrets, packslip ships the verified releases and the skills that
travel with them, hk runs the checks, pitchfork runs the services, goreleaser
and gh publish. None of them invents its own mechanism, and none is reached
except through mise.

Two kinds of upstream, and the difference is not ours to choose:

- **Ships a packslip** (fnox, hk, gsx, gsxui). mise pins it under `[tools]` and
  links its skill by version. Nothing else to do, and nothing skillpin should
  touch.
- **Ships nothing** (`cloudflare/skills` has no releases at all, which is why its
  pin is a bare commit). packslip is vendor-side by design — *"a vendor runs
  `packslip create` in its release job"* — so a consumer cannot package it. The
  only ways to make mise own it are for the vendor to adopt packslip, or for us
  to run a mirror repo with a release job and re-release on every upstream move.
  Two hops and more staleness, per upstream, forever.

So vendoring stays. It is the answer for every upstream that does not package
itself, which is most of them. What was wrong was never the fetcher — it was
that its **declaration** lived in a second file. `mise.toml` declares the tools;
it should declare these too, and skillpin reads it from there.

| Stays | Why |
|---|---|
| `fetch.go`, `lock.go`, `pins.go` | the only way to pin an upstream that ships no releases |
| `bump.go` / `dev:skills:bump` | `mise up` moves a tool pin; nothing else moves a bare commit |
| `SKILLS.lock` | mise knows tool versions; nothing else knows what a commit produced |

`mise install` is then the whole bootstrap: one command on a fresh clone leaves
the tools pinned, the skills linked, the settings generated, the git hooks
installed and the MCP servers approved. Anything needing a second command, or a
sentence in a chat window, is not done.

## What is left

| # | Item | Why | Proof |
|---|---|---|---|
| ~~1~~ | Move the source declarations out of `skills.toml` into `mise.toml`, keeping the fetcher | **DROPPED.** mise rejects a nested `[vars.claude]` outright: *"Environment variable 'claude' has no value."* The declaration cannot move into `mise.toml`, and `skills.toml` is skillpin's config file the way `hk.pkl` is hk's and `providers.toml` is the providers' | — |
| ~~2~~ | Decide how `mise.toml` carries a structured source list: `[vars]` is native, a custom table warns `unknown field`, task flags keep it in the task | **ANSWERED: it cannot.** Nested tables are rejected, a custom top-level table warns `unknown field`, and a scalar blob is unreadable | — |
| ~~3~~ | Rehouse the `[claude]` settings the same way | **DROPPED** with item 1 | — |
| 4 ✅ | `verify` locks the **whole** session: record every skill a fresh session reports, fail on any arrival | Today it checks locked skills are present and flags namespaced twins. A plugin nobody named, or a claude.ai-synced skill, passes clean. This is what makes it an allowlist | Add a skill from any source; verify names it |
| 5 ✅ | Fail on **any** namespaced (`plugin:name`) skill unless explicitly allowed | `cloudflare:sandbox-sdk` passes today; it shadows nothing but is still unpinned | Unblock a plugin, confirm every one of its skills is named |
| 6 ✅ | `mise install` installs the git hooks (`hk install` in `[hooks] postinstall`) | hk registers hooks in `.git/config` (`hook.hk-pre-commit.command`), not `.git/hooks/`, and `.git/config` is per-clone and never committed. So the hook works here but exists on no fresh clone until something installs it, and nothing did. **Note:** an earlier draft of this plan claimed AGENTS.md was wrong to say a pre-commit hook runs. AGENTS.md was right; the check looked in `.git/hooks/` | `git clone` to a temp dir, `mise install`, then `git config --get-regexp '^hook\.'` returns the hk entries |
| 7 ✅ | Add a **pre-push-only** step running `verify` | `hk run pre-push --plan` says *Hook 'pre-push' not found*: `hk.pkl` is a flat `steps = linters`, which hk maps to pre-commit/check/fix only. Bind the step to pre-push alone — `hk run check` is what the Stop hook runs, and a step leaking into it makes every agent turn ~7s and login-gated. Use the `hk-configure` skill | Unblock a plugin, confirm the push is refused and `hk run check --plan` does not list the step |
| 8 ✅ | `dev:mcp` task failing on any declared server that does not connect | Nothing noticed four servers shipped at "Needs authentication" | Declare a bad server, confirm it fails |
| 9 | Ship skillpin as a packslip release in its own repo, pinned under `[tools]`; `cmd/rel` moves in the same pass | Adoption becomes one `[tools]` line, the same path as fnox, hk, gsx and gsxui, carrying its own skill. Doing `rel` separately pays the repo-creation and wiring cost twice | Both consumed as tool pins; neither directory left here |

4-8 are done and committed in `a2665ab`. Each was proved by breaking what it
guards: unblocking `cloudflare@cloudflare` makes pre-push refuse the push and
name all 11 of that plugin's skills, where the old check caught only 4. A fresh
clone was cloned, bootstrapped and checked: no hooks before, `pre-commit` and
`pre-push` after, `check` clean with nothing else done.

Only item 9 is left.

Item 9 creates repos and needs the owner's yes first. Nothing else does — that
was the point of not mirroring upstreams.

**AGENTS.md changes with items 1, 3, 6 and 9**, and is not done until it does.
It currently calls `skills.toml` "the single source for the Claude Code session"
and `cmd/skillpin` "standalone ... Nothing in it names mise" — both become false,
and both are the opposite of what this plan concludes. It also already claims a
pre-commit hook runs. An instruction file that describes an older design is
worse than none, because every agent reads it first.

## Done means

Not "the code is written" — each is a thing someone can run and watch.

| | Done when | How it is shown |
|---|---|---|
| A | `mise install` on a fresh clone leaves nothing else to do | `git clone` to a temp dir, `mise install`, and with no second command: skills linked, settings generated, `.git/hooks` populated, MCP approved, and a session there reports the same skills as this repo, name for name |
| B | `mise.toml` is the only place anything is declared | No `skills.toml`. Grep for a second file naming a skill, a pin or a plugin and find none. The fetcher may stay; a second declaration may not |
| C | No check depends on anyone remembering | `check` in `mise run test`; `verify` and the MCP check in pre-push; the hooks themselves installed by `mise install` |
| D | Every check has been seen to fail | For each: break the thing it guards, run it, keep the failing output in this file. A check only watched passing is not evidence |
| E | A committed file holds nothing personal | No absolute path, no account id, no server needing a login only one developer has. `checkPortablePaths` covers the first; item 8 covers the third |
| F | The gap is written down, not implied | claude.ai-synced skills cannot be blocked by any repo file. The README and this plan say so plainly, and `verify` reports them |
| G | AGENTS.md describes what is actually true | Its `skills.toml`, `cmd/skillpin` and hk rows match the code |

## What is already done

| Commit | What | Survives? |
|---|---|---|
| `95214f0` | `skills.toml` gains `[claude]`; sync generates the `.claude/settings.json` keys, check fails on drift | **The generation survives, the file does not** — item 3 rehouses it |
| `f46ba84` | Pinner split out as `cmd/skillpin`, standalone; proved in a directory holding only a `skills.toml` | **Partly.** Being installable is right; being *mise-agnostic* solved the wrong problem. Item 9 makes it a packslip tool instead |
| `6ac4381` | `verify` fixed — it had never worked, reading every row of `SKILLS.lock` including the file rows | **Yes**, and items 4-5 build on it |
| `d17cbda` | Machine paths out of both committed Claude files; `.mcp.json` cut to servers that connect without a personal login | **Yes** |

Result today, from a fresh session: 25 skills — the repo's 8 and Claude Code's
17 built-ins. Before: 36, including 11 from a plugin nothing here controlled.

## Open decisions

- **claude.ai connectors are off in this repo** (`claude_ai_connectors = false`),
  so Gmail, Drive, Calendar, IBKR and Exa do not load here. A blunt fix for a
  Cloudflare problem — the setting is all-or-nothing. One line reverts it.
- **Item 9 creates repos** (skillpin and rel). Ask for both together.

## Notes for whoever picks this up

- There is a **Stop hook** in `.claude/settings.json` running
  `mise exec -- hk run check --safe --format json` after every agent turn. Keep
  it cheap. It is why `check` is the fast file-to-file comparison and why
  `verify`, needing a Claude Code login and ~7s, belongs at push time. Do not
  move login-gated or network work into it.
- Two regressions went in during this work and no check caught either:
  committed machine paths (`/opt/homebrew/bin/mise`), and four MCP servers
  needing a login nobody had. The owner caught both by reading the diff. The
  first now has `checkPortablePaths`; the second is item 8.
- `verify` is worth more than it looks. Every bug fixed in `6ac4381` was found
  by running it, not by reasoning about it. Run things.
- Prove a check by breaking the thing it guards, not by watching it pass.
- The mistake this plan corrects: reaching for a new file instead of asking what
  mise already does. `mise skills ls` answers that in one command.
