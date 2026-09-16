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
		echo "These are new. Claude Code only picks up a new .claude/skills directory"
		echo "at startup: restart Claude Code (or /exit and reopen) before GUI work."
	else
		echo
		echo "Skills changed. Restart Claude Code if a session is open."
	fi
fi
