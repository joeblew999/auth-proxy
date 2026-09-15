#!/usr/bin/env bash
# Pushes every secret providers.toml needs from fnox to a Worker environment.
#   tools/push-keys.sh          the main Worker
#   tools/push-keys.sh tinygo   the TinyGo Worker
# Exits non-zero, naming the fix, when a secret is missing from fnox.
set -uo pipefail
cd "$(dirname "$0")/.."
env_name=${1:-}

missing=0
while IFS=$'\t' read -r name provider; do
  if fnox get "$name" >/dev/null 2>&1; then
    if fnox exec -- sh -c 'printf %s "$(printenv "$1")" | wrangler secret put "$1" --env "$2"' _ "$name" "$env_name" >/dev/null 2>&1; then
      echo "pushed  $name"
    else
      echo "failed  $name (run: mise run logs, or retry mise run keys:push)"
      missing=1
    fi
  else
    echo "missing $name -> mise run keys:set $provider"
    missing=1
  fi
done < <(bin/grok-oauth-proxy keys)
exit $missing
