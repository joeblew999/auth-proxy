#!/usr/bin/env bash
# mise run bench: runs the Go and TinyGo Worker builds locally in workerd, both
# against the mock upstream with tools/mock-upstream/providers.toml, sends them
# identical requests, and prints status, size and latency side by side. Everything
# it starts is stopped on exit.
#
# workers-go instantiates the wasm module on every request, so latency includes
# that startup cost, which is where the toolchains differ most.
set -euo pipefail
cd "$(dirname "$0")/.."

GO_PORT=8791 TINYGO_PORT=8792 KEY=bench-key RUNS=${RUNS:-20}
LOGS=$(mktemp -d)
pids=()
cleanup() {
  for pid in "${pids[@]}"; do pkill -P "$pid" 2>/dev/null || true; kill "$pid" 2>/dev/null || true; done
  rm -rf "$LOGS"
}
trap cleanup EXIT

go build -o bin/mock-upstream ./tools/mock-upstream
bin/mock-upstream >"$LOGS/mock.log" 2>&1 & pids+=($!)

vars=(--var "PROVIDERS_TOML:$(cat tools/mock-upstream/providers.toml)" --var MOCK_API_KEY:mock-key --var "ADMIN_API_KEY:$KEY")
wrangler dev --env "" --port "$GO_PORT" "${vars[@]}" >"$LOGS/go.log" 2>&1 & pids+=($!)
wrangler dev --env tinygo --port "$TINYGO_PORT" "${vars[@]}" >"$LOGS/tinygo.log" 2>&1 & pids+=($!)

echo "Building and starting both Workers (this takes a minute)..."
for log in go tinygo; do
  until grep -qE 'Ready on|ERROR' "$LOGS/$log.log"; do sleep 2; done
  if grep -q ERROR "$LOGS/$log.log"; then cat "$LOGS/$log.log"; exit 1; fi
done

CHAT='{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}'
ROUTED='{"model":"local/mock-model","messages":[{"role":"user","content":"hi"}]}'
STREAM='{"model":"mock-model","stream":true,"messages":[{"role":"user","content":"hi"}]}'
MCP='{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# probe PORT METHOD PATH [BODY] -> "code bytes seconds"
probe() {
  local args=(-sS -o /dev/null -w '%{http_code} %{size_download} %{time_total}' -X "$2" -H "Authorization: Bearer $KEY")
  if [ -n "${4:-}" ]; then
    args+=(-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d "$4")
  fi
  curl "${args[@]}" "http://127.0.0.1:$1$3"
}

avg_ms() {
  local total=0 t
  for _ in $(seq "$RUNS"); do
    t=$(probe "$@" | cut -d' ' -f3)
    total=$(echo "$total + $t" | bc -l)
  done
  printf '%.1f' "$(echo "$total * 1000 / $RUNS" | bc -l)"
}

row() {
  local name=$1; shift
  local g t
  g=$(probe "$GO_PORT" "$@"); t=$(probe "$TINYGO_PORT" "$@")
  printf '%-20s | %4s %7s B %8s ms | %4s %7s B %8s ms\n' "$name" \
    "${g%% *}" "$(echo "$g" | cut -d' ' -f2)" "$(avg_ms "$GO_PORT" "$@")" \
    "${t%% *}" "$(echo "$t" | cut -d' ' -f2)" "$(avg_ms "$TINYGO_PORT" "$@")"
}

echo
for f in build/*/app.wasm; do
  printf '%-24s raw %6s KB   gzip %6s KB\n' "$f" $(( $(wc -c < "$f") / 1024 )) $(( $(gzip -9 -c "$f" | wc -c) / 1024 ))
done
echo
echo "latency: mean of $RUNS requests"
printf '%-20s | %-28s | %-28s\n' "request" "go   code    size   latency" "tinygo   code    size   latency"
row "GET /health" GET /health
row "GET /admin/status" GET /admin/status
row "GET /v1/models" GET /v1/models
row "POST chat (default)" POST /v1/chat/completions "$CHAT"
row "POST chat local/" POST /v1/chat/completions "$ROUTED"
row "POST chat stream" POST /v1/chat/completions "$STREAM"
row "POST /mcp" POST /mcp "$MCP"
