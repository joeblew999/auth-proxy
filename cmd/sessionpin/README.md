# sessionpin

One file decides which Claude Code skills a repo has.

Skills reach a session from several places at once: a repo's own `.claude/skills`,
marketplace plugins installed per-developer, and skills synced from claude.ai.
They can collide — the same skill name, from the same upstream, at two versions,
and which one a session loads is luck. `sessionpin` makes `session.toml` the only
thing that decides, and proves it.

## Adopting it

Two things. A `session.toml` in the repo root:

```toml
# How your repo runs this, quoted back by every error a sync would fix.
sync_command = "mise run skills:sync"

# Skills vendored from a GitHub repo at a pinned commit.
[source.cloudflare]
repo = "cloudflare/skills"
ref  = "b052c32bab7dd493513260228a36c88294f343f1"
skills = ["wrangler", "durable-objects"]

# Skills read out of a Go module in the local module cache, at the version
# the named directory's go.mod pins.
[source.gsx]
module     = "github.com/joeblew999/gsx"
module_dir = "."
skills     = ["gsx"]

# The rest of the session, generated into .claude/settings.json. Leave a key
# out and sessionpin does not manage that setting at all -- a repo never loses
# something by not mentioning it. Omit [claude] entirely and no settings file
# is written.
[claude]
blocked_plugins      = ["cloudflare@cloudflare"]
claude_ai_connectors = false   # opt out of claude.ai connectors here
approve_mcp_servers  = true    # trust this repo's own .mcp.json
```

And a way to run it:

```
go install github.com/joeblew999/grok-oauth-proxy/cmd/sessionpin@latest
```

That is all. No task runner is required, nothing is added to your Go module, and
`sessionpin` reads no file it is not pointed at. With mise:

```toml
[tools]
"go:github.com/joeblew999/grok-oauth-proxy/cmd/sessionpin" = "latest"
```

## The four commands

| | |
|---|---|
| `sessionpin sync` | write `.claude/skills` from the pins, and the `.claude/settings.json` keys `[claude]` implies |
| `sessionpin check` | fail when either has drifted. Put this in CI and in your test task |
| `sessionpin verify` | ask a fresh Claude Code session which skills it *actually* sees |
| `sessionpin bump [source]` | move github pins to upstream HEAD |

`sync` writes only what it owns: skills it wrote before, and three settings keys.
Hooks, permissions and anything else in `.claude/settings.json` are left alone,
and so is any skill you wrote by hand.

## Why check and verify are different

`check` compares files to `SKILLS.lock` — cheap, offline, catches drift and
hand-edits.

`verify` starts a real headless session and asks what it can see. It catches
what files cannot: a `SKILL.md` that never loads because its frontmatter is
wrong, and a plugin shipping `<plugin>:<your skill name>` — a second copy of
something you pinned, at a version you do not control. Add that plugin to
`blocked_plugins` and it stops loading.

`[claude]` cannot close every hole. Skills synced from claude.ai are not read
from project settings at all, so no repo file can block them. `verify` is what
notices them.

## Settings precedence, and why blocking works

`enabledPlugins` is read user < project < local, so `false` in a repo's
`.claude/settings.json` overrides a developer's own `true`. The repo decides,
not whoever installed a marketplace plugin once.

`disableClaudeAiConnectors` is any-source-true: a repo can opt out of claude.ai
connectors, but cannot force them back on for someone who turned them off.
