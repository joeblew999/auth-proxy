#!/bin/sh
# Sends identical requests to the standard Go and TinyGo Workers running under
# `mise run cf_dev_go` and `mise run cf_dev_tinygo` (both against
# `mise run mock_upstream`), and prints status, size, and latency side by side.
#
# workers-go instantiates a fresh wasm module on every request, so the latency
# columns include that startup cost, which is where the toolchains differ most.
set -eu

GO_URL=${GO_URL:-http://127.0.0.1:8791}
TINYGO_URL=${TINYGO_URL:-http://127.0.0.1:8792}
KEY=${ADMIN_API_KEY:-mock-admin}
RUNS=${RUNS:-20}

CHAT='{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}'
STREAM='{"model":"mock-model","stream":true,"messages":[{"role":"user","content":"hi"}]}'
MCP='{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# probe BASE METHOD PATH [BODY] -> "code bytes seconds"
probe() {
  if [ -n "${4:-}" ]; then
    curl -sS -o /dev/null -w '%{http_code} %{size_download} %{time_total}' -X "$2" \
      -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
      -H 'Accept: application/json, text/event-stream' -d "$4" "$1$3"
  else
    curl -sS -o /dev/null -w '%{http_code} %{size_download} %{time_total}' -X "$2" \
      -H "Authorization: Bearer $KEY" "$1$3"
  fi
}

# avg_ms BASE METHOD PATH [BODY] -> mean latency in ms over $RUNS requests
avg_ms() {
  i=0 total=0
  while [ "$i" -lt "$RUNS" ]; do
    t=$(probe "$@" | cut -d' ' -f3)
    total=$(echo "$total + $t" | bc -l)
    i=$((i + 1))
  done
  printf '%.1f' "$(echo "$total * 1000 / $RUNS" | bc -l)"
}

row() {
  name=$1; shift
  g=$(probe "$GO_URL" "$@"); t=$(probe "$TINYGO_URL" "$@")
  ga=$(avg_ms "$GO_URL" "$@"); ta=$(avg_ms "$TINYGO_URL" "$@")
  printf '%-18s | %4s %7s B %8s ms | %4s %7s B %8s ms\n' "$name" \
    "${g%% *}" "$(echo "$g" | cut -d' ' -f2)" "$ga" \
    "${t%% *}" "$(echo "$t" | cut -d' ' -f2)" "$ta"
}

echo "go: $GO_URL   tinygo: $TINYGO_URL   latency: mean of $RUNS requests"
printf '%-18s | %-26s | %-26s\n' "request" "go   code  size  latency" "tinygo   code  size  latency"
row "GET /health" GET /health
row "GET /admin/status" GET /admin/status
row "GET /v1/models" GET /v1/models
row "POST chat" POST /v1/chat/completions "$CHAT"
row "POST chat stream" POST /v1/chat/completions "$STREAM"
row "POST /mcp" POST /mcp "$MCP"
