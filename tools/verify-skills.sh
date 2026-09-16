#!/usr/bin/env bash
# Proves the repo's skills actually load, by asking a fresh headless Claude Code
# session which skills it can see. This catches what the file check cannot: a
# broken SKILL.md (bad frontmatter, wrong folder name) is present but never loads.
#
# It starts its own session, so it does not depend on the session you are in.
# Not part of `mise run test`: it needs a Claude Code login, which CI has not got.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v claude >/dev/null 2>&1; then
	echo "claude CLI not found; install Claude Code to run this check" >&2
	exit 1
fi

expected=$(cut -f1 .claude/skills/SKILLS.lock)
out=.tmp/skills-verify.txt
mkdir -p .tmp
rm -f "$out"

claude -p 'List the names of every skill available to you, one per line, nothing else.' >"$out" 2>&1 &
claude_pid=$!
for _ in $(seq 60); do
	kill -0 "$claude_pid" 2>/dev/null || break
	sleep 2
done
if kill -0 "$claude_pid" 2>/dev/null; then
	kill "$claude_pid" 2>/dev/null || true
	echo "claude did not answer within 2 minutes" >&2
	exit 1
fi

missing=""
for name in $expected; do
	grep -qxF "$name" "$out" || missing="$missing $name"
done
if [ -n "$missing" ]; then
	echo "a fresh Claude Code session cannot see:$missing" >&2
	echo "it answered:" >&2
	sed 's/^/  /' "$out" >&2
	echo "check the SKILL.md frontmatter, then: mise run skills:sync" >&2
	exit 1
fi

echo "a fresh Claude Code session sees every skill in .claude/skills:"
printf '%s\n' $expected | sed 's/^/  /'
rm -f "$out"
