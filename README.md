# grok-oauth-proxy

One OpenAI-compatible endpoint for many model providers. Clients pick the provider
through the model name: `xai/grok-4.3`, `groq/llama-3.3-70b-versatile`,
`openrouter/...`. The proxy adds each provider's credentials, so clients only
ever hold one key.

It runs locally or as a Cloudflare Worker from the same code. It can use a
SuperGrok subscription login for xAI, and it serves MCP tools at `/mcp`.

## Quick start

You need [mise](https://mise.jdx.dev); it installs everything else, including
[fnox](https://github.com/jdx/fnox) for secrets. Then:

```bash
mise install
mise run setup          # creates the client key, pushes keys, shows what is missing
mise run status         # every provider: ok, or the exact command that fixes it
```

`mise tasks` lists everything else. There is nothing more to remember.

## Adding a provider

1. Add it to [`providers.toml`](providers.toml). It contains commented examples.

   ```toml
   [providers.groq]
   base_url = "https://api.groq.com/openai/v1"
   key = "GROQ_API_KEY"          # the secret's name, never its value
   ```

2. Store the key, which pushes it to the Worker too, then deploy:

   ```bash
   mise run secrets:set groq
   mise run deploy
   ```

3. Use it:

   ```bash
   mise run chat groq/llama-3.3-70b-versatile "hello" --worker
   ```

| Setting | Meaning |
|---|---|
| `base_url` | OpenAI-compatible base URL including the version path |
| `key` | name of the secret holding the API key |
| `auth = "xai-oauth"` | use a SuperGrok subscription instead of a key; then run `mise run login` (only for `api.x.ai`) |
| `auth = "none"` | no credential, e.g. a local Ollama |
| `headers` | extra headers on every request |
| `default` (top level) | provider for model names without a prefix |
| `[aliases]` | short names, e.g. `fast = "groq/llama-3.1-8b-instant"` |

Mistakes such as an unknown setting, a key value pasted where its name belongs,
or a missing default fail `mise run status`, `test` and `deploy` with a message
saying what to change.

## Running it

| Where | Start | Base URL |
|---|---|---|
| Locally | `mise run dev` | `http://127.0.0.1:56121/v1` |
| Locally, no real keys | `mise run proxy:run:mock` | `http://127.0.0.1:56121/v1` |
| Cloudflare | `mise run deploy` | what `mise run url` prints, plus `/v1` |

Clients use the base URL with the client key (`ADMIN_API_KEY`, created by
`mise run setup`) as their API key. For example, OpenCode:

```json
{
  "provider": {
    "proxy": {
      "options": { "baseURL": "http://127.0.0.1:56121/v1", "apiKey": "<ADMIN_API_KEY>" }
    }
  }
}
```

`GET /v1/models` lists every provider's models with their prefixes. With a single
provider, IDs are left unprefixed.

## MCP

`/mcp` is a stateless streamable-HTTP MCP server behind the same client key:

```json
{
  "mcpServers": {
    "models": {
      "type": "http",
      "url": "http://127.0.0.1:56121/mcp",
      "headers": { "Authorization": "Bearer <ADMIN_API_KEY>" }
    }
  }
}
```

| Tool | Input | Result |
|---|---|---|
| `list_models` | none | model IDs across all providers |
| `ask` | `model`, `prompt` | the answer text; one-shot, no history |

## Endpoints

| Endpoint | Purpose |
|---|---|
| `POST /v1/chat/completions` and other `/v1/*` | proxied to the provider chosen by `model` |
| `GET /v1/models` | all providers' models |
| `POST /mcp` | MCP tools |
| `GET /admin/status` | provider readiness with fixes (what `mise run proxy:status:worker` shows) |
| `POST /admin/auth/start`, `GET`/`POST /admin/auth/status` | Grok device login (what `mise run login --worker` drives) |
| `POST /admin/tokens` | store Grok tokens manually |
| `GET /health` | public liveness check |
| `GET /login`, `GET /callback` | local browser Grok login |

Everything except `/health` and the local login requires the client key, sent as
`Authorization: Bearer`, `X-API-Key`, or `?key=`.

## TinyGo

The Worker also builds with TinyGo (`mise run proxy:build --env tinygo`, `mise run bench`).
That build is about 1.9 MB instead of 15 MB, but serves 501 on `/mcp`, because the
MCP SDK needs eight things TinyGo 0.42 lacks or gets wrong
([tinygo-org/tinygo#5684](https://github.com/tinygo-org/tinygo/issues/5684)).
Details are in [.plan/done/tinygo.md](.plan/done/tinygo.md). Deploy the standard build
until TinyGo fixes them.

## Development

```bash
mise run lint     # hk: gofmt, vet, tidy, whitespace, secrets
mise run test     # lint, wasm vet, all tests, skills + spike checks
mise run bench    # both Worker builds in local workerd against the mock
```

Working on the Worker beside someone? `mise set --file mise.local.toml DEPLOY_SUFFIX=<you>`
once, and every Worker you deploy gets that suffix, URL, logs and secrets included;
unset means the shared one.

The Claude Code session here is pinned too: `session.toml` and `mise.toml` name
every skill, `.claude/skills/SESSION.lock` lists every skill a session may have,
and `mise run session:verify` (run at pre-push) fails on anything else. One
thing no repo file can block: skills synced from claude.ai reach every session
whatever the project settings say. verify can only notice them and name them.

The code layout and the rules that keep it simple are in [AGENTS.md](AGENTS.md).
