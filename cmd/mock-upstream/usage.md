### Serving

- `mock-upstream serve [--addr HOST:PORT]`
  answer `/v1/models` and `/v1/chat/completions` on the address (default
  `127.0.0.1:18080`), deterministically and at no cost. Every request is
  logged with the bearer token masked to a short stable hash, so which
  credential arrived is visible without the log disclosing it. Anything
  outside `/v1/` answers 404.
