# Costs

Last checked 2026-09-15. Prices change — verify at the links below.

## Summary

| Component | Cost |
|---|---|
| Cloudflare Workers + KV | **$0/month** (free plan) |
| xAI | **$30/month** SuperGrok, or pay-per-token API credits |

No VPC tunnel and no always-on host, so nothing to rent.

## Cloudflare

| | Free | Paid ($5/mo) |
|---|---|---|
| Worker requests | 100,000/day | 10M/mo included, then $0.30/M |
| CPU time | 10 ms per invocation | 30M CPU-ms/mo included, then $0.02/M |
| KV reads | 100,000/day | 10M/mo included, then $0.50/M |
| KV writes | 1,000/day | 1M/mo included, then $5.00/M |

Personal use fits the free plan.

Sources:
- https://developers.cloudflare.com/workers/platform/pricing/

## xAI

### Subscription

| Tier | Price |
|---|---|
| SuperGrok Lite | $10/mo |
| **SuperGrok** | **$30/mo** |
| SuperGrok Plus | $100/mo |
| SuperGrok Heavy | $300/mo |
| Business | $30/mo per user |

### API credits (alternative to a subscription)

Per million tokens.

| Model | Input | Cached input | Output |
|---|---|---|---|
| grok-4.6 | $2.00 | $0.50 | $6.00 |
| grok-4.5 | $2.00 | $0.30 | $6.00 |
| grok-4.3 | $1.25 | $0.20 | $2.50 |
| grok-4.20-0309-non-reasoning | $1.25 | $0.20 | $2.50 |
| grok-4.20-0309-reasoning | $1.25 | $0.20 | $2.50 |
| grok-4.20-multi-agent-0309 | $1.25 | $0.20 | $2.50 |
| grok-build-0.1 | $1.00 | $0.20 | $2.00 |

Prompts reaching 200k tokens are billed at double for all tokens in the request.

Sources:
- https://grok.com/supergrok
- https://docs.x.ai/docs/models

## Notes

- **Why infrastructure is free:** an earlier plan budgeted $3–5/month for a VPS to
  run `cloudflared` for the Workers VPC tunnel. Direct Worker egress to `api.x.ai`
  was measured to work, so the tunnel was dropped and that cost disappeared. See
  `.plan/done/cloudflare-workers-deployment.md` §4.1.
- **Possible $5/month:** the Worker is a 10.9 MB Go/WASM bundle. Startup time is not
  billed as CPU time and proxying is mostly I/O, but if per-request CPU exceeds the
  free plan's 10 ms the account needs Workers Paid. Check the Worker's metrics.
- **Break-even:** $30/month of grok-4.20 output credits is ~12M tokens, so the
  subscription wins above that. Driving the API from a subscription rather than
  per-token billing is the point of this proxy.
- **Confirmed 2026-09-15:** the xAI account page for `gedw99@gmail.com` offers
  "Get SuperGrok", so the account used for the device flow holds **no
  subscription**. That is the cause of `personal-team-blocked:spending-limit`, and
  it also confirms the flow authorised the intended account.
- **Two ways forward:** subscribe (SuperGrok from $10/mo, though $30 is the safe bet
  for API entitlement), or skip the subscription and buy **API credits**, then set
  `UPSTREAM_API_KEY` so the proxy uses a static key instead of OAuth. At $1.25/$2.50
  per 1M tokens for grok-4.20, credits are by far the cheaper way to test.
- **Still unverified:** whether SuperGrok Lite ($10) satisfies the API entitlement.
- **Two separate credit systems — verified 2026-09-15.** Adding $5 of API credits at
  `console.x.ai` did **not** unblock the OAuth path: it still returns
  `personal-team-blocked:spending-limit`, and that error links to `grok.com`, not the
  API console. The OAuth path impersonates the Grok CLI, so it consumes *consumer*
  entitlement and needs a subscription regardless of API credits. API credits serve
  the **API-key** path only, which is what `UPSTREAM_API_KEY` switches on.
- **Confirmed working 2026-09-15:** with `UPSTREAM_API_KEY` set, the deployed Worker
  returns live xAI models and real streaming completions. **$5 of API credits was
  enough to prove the entire path**, with no subscription. That is the cheapest route
  to a working endpoint, and it needs no SuperGrok commitment.
- **Or pay nothing at all.** The upstream is configurable, so the proxy is not tied
  to xAI: set `UPSTREAM_BASE_URL` and `UPSTREAM_API_KEY` to point it at any
  OpenAI-compatible provider, including a local one. `mise run mock_upstream` plus
  `mise run proxy_local_mock` exercises the whole path for **$0**, and a local
  Ollama/llama.cpp or a free-tier hosted provider costs nothing to run. See
  `.plan/done/any-provider-support.md`.
