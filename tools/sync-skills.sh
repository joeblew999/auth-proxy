#!/usr/bin/env bash
# Puts the skills this project depends on into .claude/skills/, so every Claude
# Code session in this repo loads them. Nothing is installed globally.
#
#   tools/sync-skills.sh          write .claude/skills/ (run by mise install)
#   tools/sync-skills.sh --check  fail when .claude/skills/ differs from the pins
#
# Sources and pins:
#   gsx        skills/ from github.com/gsxhq/gsx, at the version in
#              spikes/hello-world/go.mod, so the skill always matches the gsx
#              the build uses
#   cloudflare skills/ from github.com/cloudflare/skills (Apache-2.0), pinned below
set -euo pipefail
cd "$(dirname "$0")/.."

CLOUDFLARE_SKILLS_REF=b052c32bab7dd493513260228a36c88294f343f1
CLOUDFLARE_SKILLS=(wrangler durable-objects workers-best-practices cloudflare)
GSX_SKILLS=(gsx templ-to-gsx-migration)

check=false
[ "${1:-}" = "--check" ] && check=true

# elapsed_seconds converts ps etime ([[dd-]hh:]mm:ss) to seconds.
elapsed_seconds() {
	local e=$1 days=0 rest secs=0 part
	case "$e" in *-*) days=${e%%-*}; e=${e#*-};; esac
	local IFS=:
	for part in $e; do secs=$((secs * 60 + 10#$part)); done
	echo $((days * 86400 + secs))
}

# warn_stale_sessions reports Claude Code sessions in this repo that started
# before the skills were written: those sessions cannot see them, because Claude
# Code reads .claude/skills at startup.
warn_stale_sessions() {
	local newest session_pids pid started started_epoch newest_epoch
	newest=$(find .claude/skills -type f -newer .claude/skills 2>/dev/null | head -1)
	newest_epoch=$(find .claude/skills -type f -print0 2>/dev/null |
		xargs -0 stat -f '%m' 2>/dev/null || find .claude/skills -type f -printf '%T@\n' 2>/dev/null)
	newest_epoch=$(printf '%s\n' "$newest_epoch" | cut -d. -f1 | sort -rn | head -1)
	[ -n "$newest_epoch" ] || return 0

	session_pids=$(ps -eo pid,command 2>/dev/null |
		grep -E '(native-binary|bin)/claude( |$)' | grep -v grep | awk '{print $1}' || true)
	for pid in $session_pids; do
		# Only sessions working in this repo.
		lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | grep -q "^n$PWD$" || continue
		# Elapsed time, not lstart: ps prints lstart in the local format, which
		# differs between machines and locales.
		started=$(ps -o etime= -p "$pid" 2>/dev/null | tr -d ' ') || continue
		started_epoch=$(( $(date '+%s') - $(elapsed_seconds "$started") ))
		if [ "$started_epoch" -lt "$newest_epoch" ]; then
			echo
			echo "Claude Code (pid $pid, started $(date -r "$started_epoch" '+%b %e %H:%M')) is older than"
			echo "the skills ($(date -r "$newest_epoch" '+%b %e %H:%M')), so that session cannot see them."
			echo "Restart Claude Code (/exit and reopen, or reload the VS Code window)."
		fi
	done
}

dest=.claude/skills
# Unpack inside the repo (gitignored), never in a system temp directory.
work=.tmp/skills-sync
rm -rf "$work"
mkdir -p "$work"
trap 'rm -rf "$work"' EXIT
staged="$work/skills"
mkdir -p "$staged"

# gsx: from the module cache, at the version the spike's go.mod pins.
gsx_version=$(cd spikes/hello-world && go list -m -f '{{.Version}}' github.com/gsxhq/gsx)
(cd spikes/hello-world && go mod download github.com/gsxhq/gsx)
gsx_dir=$(cd spikes/hello-world && go list -m -f '{{.Dir}}' github.com/gsxhq/gsx)
for name in "${GSX_SKILLS[@]}"; do
	cp -R "$gsx_dir/skills/$name" "$staged/$name"
	echo "$name	github.com/gsxhq/gsx@$gsx_version" >>"$staged/SKILLS.lock"
done

# cloudflare: the pinned commit's tarball, so no clone is left behind.
curl -fsSL "https://codeload.github.com/cloudflare/skills/tar.gz/$CLOUDFLARE_SKILLS_REF" |
	tar -xzf - -C "$work"
cf_dir="$work/skills-$CLOUDFLARE_SKILLS_REF"
for name in "${CLOUDFLARE_SKILLS[@]}"; do
	cp -R "$cf_dir/skills/$name" "$staged/$name"
	echo "$name	github.com/cloudflare/skills@${CLOUDFLARE_SKILLS_REF:0:12}" >>"$staged/SKILLS.lock"
done
LC_ALL=C sort -o "$staged/SKILLS.lock" "$staged/SKILLS.lock"
chmod -R u+w "$staged"

if $check; then
	if diff -r -q "$staged" "$dest" >/dev/null 2>&1; then
		warn_stale_sessions
		exit 0
	fi
	echo "skills in $dest do not match their pins:" >&2
	diff -r -q "$staged" "$dest" >&2 || true
	echo "fix with: mise run skills:sync" >&2
	exit 1
fi

had=$([ -d "$dest" ] && echo yes || echo no)
before=$(find "$dest" -type f 2>/dev/null | LC_ALL=C sort | xargs shasum 2>/dev/null | shasum | cut -c1-12 || true)
mkdir -p "$(dirname "$dest")"
rm -rf "$dest"
cp -R "$staged" "$dest"
after=$(find "$dest" -type f | LC_ALL=C sort | xargs shasum | shasum | cut -c1-12)

echo "skills in $dest:"
sed 's/^/  /' "$dest/SKILLS.lock"
if [ "$before" != "$after" ]; then
	if [ "$had" = no ]; then
		echo
		echo "These are new: Claude Code reads .claude/skills at startup."
	fi
fi
warn_stale_sessions
