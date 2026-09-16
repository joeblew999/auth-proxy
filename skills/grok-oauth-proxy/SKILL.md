---
name: grok-oauth-proxy
description: Run or configure grok-oauth-proxy — one OpenAI-compatible endpoint for many model providers, locally or as a Cloudflare Worker.
---

# grok-oauth-proxy

One OpenAI-compatible endpoint for many model providers. Clients pick the
provider through the model name (`xai/grok-4.3`, `groq/llama-3.3-70b-versatile`,
`openrouter/...`). The proxy adds each provider's credentials, so clients only
ever hold one key.

## Commands

Run everything through mise from the repo root; `mise tasks` lists it all.

- `mise run dev` — proxy locally on `http://127.0.0.1:56121/v1`
- `mise run dev --mock` — same, against the bundled mock, no real keys
- `mise run status` — every provider's readiness, with the fix for each problem
- `mise run models` / `chat <model> [prompt]` — list models, stream a prompt
- `mise run login` — SuperGrok login for `auth = "xai-oauth"` providers
- `mise run keys:set <provider>` / `keys:push` — store keys in fnox, push to Worker
- `mise run deploy` — validate `providers.toml`, then deploy the Worker

Append `--worker` to `status`, `models`, `chat`, `login` to talk to the deployed
Worker instead of the local proxy.

## Providers

Providers live in `providers.toml`. Secret values live in fnox, never in source.

```toml
[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key = "GROQ_API_KEY"   # the secret's name, never its value
```

- `base_url` carries its version path (`/v1`, `/api/v1`, `/openai/v1`).
- `auth = "xai-oauth"` uses a SuperGrok subscription instead of a key (only for `api.x.ai`).
- `auth = "none"` sends no credential, e.g. a local Ollama.

## Code layout

- `internal/proxy` — the HTTP handler both runtimes share. Never add
  runtime-specific proxy logic to `main.go` or `worker.go`.
- `internal/config` — the only code that reads the environment.
- `internal/router` — model name to provider, no I/O.
- `internal/xaiauth` — the Grok login (browser PKCE, device flow, refresh).
- `main.go` — local CLI. `worker.go` — Worker entry point.

Run `mise run test` after every Go change.
