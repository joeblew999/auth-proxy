# Plan: TinyGo builds

**Status: WORKING side by side with the standard Go build, minus `/mcp`. `/mcp` is blocked by 8 TinyGo gaps, all reported in [tinygo-org/tinygo#5684](https://github.com/tinygo-org/tinygo/issues/5684). 2026-09-15. Re-checked 2026-09-16: the issue is still open with no reply, 0.42.0 (2026-09-01) is still the latest TinyGo release and the one pinned here, so there is nothing to retest yet.**

## The question

Can the Worker be built with TinyGo for a smaller footprint, with mise owning the
toolchain, building and running both, and reporting sizes, so the better one can
be chosen on evidence?

## Answers

| Question | Answer |
|---|---|
| Do latest Go, TinyGo, and workers-go work together? | **Yes.** TinyGo 0.42.0 supports Go 1.27, and workers-go v0.35.0 builds with exactly Go 1.27.1 + TinyGo 0.42.0 (its Makefile) |
| Does this Worker build with TinyGo? | **Yes, except `/mcp`**, which returns 501 in the TinyGo build |
| Can mise install, build, run, and measure both? | **Yes.** Tasks listed at the bottom |
| Do both behave the same? | **Yes** for everything except `/mcp`: identical `/v1/models` and chat bodies, and streaming arrives chunk by chunk on both |
| What exactly blocks `/mcp`? | **8 TinyGo gaps**, listed below. With all 8 patched locally, `/mcp` works under TinyGo and matches the Go build, so the list is complete for this project |

## The blockers

Found 2026-09-15 by patching each gap in turn on scratch copies (patched
`TINYGOROOT` plus forks of go-sdk and jsonschema-go; nothing committed) until
`/mcp` worked. Versions: TinyGo 0.42.0, Go 1.27.1, go-sdk v1.8.0, jsonschema-go
v0.4.3, segmentio/encoding v0.5.4. Each has a minimal repro in the upstream issue.

| # | TinyGo gap | Kind | Hit by | Local workaround used |
|---|---|---|---|---|
| 1 | `hash/maphash` doesn't compile on Go 1.27 | compile, Go 1.27 regression | jsonschema-go `uniqueItems` | fork jsonschema-go and use `hash/fnv`. Safe because the hash only buckets and `equalValue` decides; its tests pass |
| 2 | `crypto/rand.Text` missing | compile | go-sdk `auth`, `mcp/server.go` session IDs | add Go's implementation to patched `src/crypto/rand` |
| 3 | `net.Dialer.Control` missing | compile | go-sdk `oauthex` | add the field, ignored |
| 4 | `http.Transport.DialTLSContext` missing | compile | go-sdk `oauthex` | add the field, ignored |
| 5 | `os.OpenRoot` / `os.Root` missing | compile | go-sdk `mcp/resource.go` | stub returning `ErrNotImplemented` |
| 6 | `http.CrossOriginProtection` missing | compile | go-sdk `mcp/streamable.go` | copy Go 1.27's `net/http/csrf.go` unchanged |
| 7 | `reflect.NewAt` unimplemented; panics with the misleading message `unimplemented: reflect.New()` | runtime, at startup | segmentio/encoding/json via go-sdk `internal/json` | swap go-sdk's internal JSON to `encoding/json`. This loses case-sensitive field matching, and segmentio on TinyGo is untested |
| 8 | `reflect.VisibleFields` returns fields whose `Type` panics (unsafe slice cast skips `toStructField`) | runtime, at startup, **also on Go 1.26** | jsonschema-go `ForType` | convert each field with `toStructField` |

Notes for anyone repeating this:
- Gaps 1–6 fail at compile time, one after another. Gaps 7 and 8 compile but
  panic while the MCP server is being built, which kills the whole Worker (even
  `/health`). workerd reports that as "code had hung", not as a panic.
- `hash/` is not in TinyGo's override list (`loader/goroot.go`
  `pathsToOverride`), so gap 1 cannot be patched in TINYGOROOT without rebuilding
  the compiler; hence the fork. Gaps 2–8 are in packages TinyGo owns completely.
- Build the patched TINYGOROOT as an APFS clone (`cp -cR`), not with symlinks,
  because TinyGo's GOROOT merge rejects symlinked directories.
- TinyGo prints no stack trace on wasm. Reproduce natively (`tinygo build`, no
  target) and run under `lldb --batch -o run -k bt`.

Result with all 8 patched: `initialize`, `tools/list`, both tools, and
input-schema validation errors all worked. 6 of 7 MCP responses matched the Go
build byte for byte; the 7th differed only in map order within an error message.
The wasm was 2,738 KB raw and 929 KB gzip.

Not carried in the repo: a jsonschema-go fork, a go-sdk fork that changes JSON
semantics, and a patched TinyGo stdlib are too much to maintain for one endpoint.

Workaround in place: `internal/mcp` is `//go:build !tinygo`, and its TinyGo stub
returns no handler, so the proxy serves 501 on `/mcp`. The standard Go build is unchanged.

## Measurements (local workerd, wrangler 4.131.1, against the mock upstream; before the multi-provider redesign)

| | Go 1.27.1 | TinyGo 0.42.0 | TinyGo + 8 local patches |
|---|---|---|---|
| `app.wasm` raw | 15,340 KB | 1,513 KB (**10x smaller**) | 2,738 KB |
| `app.wasm` gzip | 3,708 KB | 549 KB | 929 KB |
| build time | ~2 s | ~11 s | |
| `GET /health`, mean of 20 | 8.9 ms | 5.4 ms | |
| `GET /v1/models` | 8.3 ms | 5.5 ms | |
| `POST` chat | 8.8 ms | 7.3 ms | |
| `POST` chat stream (mock paces chunks ~150 ms) | 770.9 ms | 766.7 ms | |
| `/mcp` | 200 | 501 | 200 |

Deployed on Cloudflare (2026-09-15, after the multi-provider redesign, measured
from Thailand, mean of 20):

| | Go (15.6 MB) | TinyGo (1.9 MB) |
|---|---|---|
| `GET /health` | 77 ms | 70 ms |
| `GET /admin/status` | 77 ms | 68 ms |
| `GET /v1/models` (includes xAI) | 318 ms | 302 ms |

TinyGo is about 10% faster end to end; network time dominates both.

workers-go instantiates the wasm module on every request, so the latency gap is
mostly per-request startup. Deployed, the gap is small (table above).

Cloudflare limits (since 2026-09-04): 64 MiB uncompressed on all plans, with no
compressed limit, and 1 s startup. Size alone does not force TinyGo; startup time
and CPU per request are the real argument.

## Paths forward

1. **TinyGo fixes the 8 gaps.** Tracked in tinygo-org/tinygo#5684: the issue
   covers gap 1, and a comment covers all 8 with repros. Gap 8 comes with a
   ready-made fix.
2. When a TinyGo release lands, retest with `mise run bench`
   after removing `internal/mcp/mcp_tinygo.go` and the `!tinygo` tags. Gap 7 may also
   need segmentio/encoding to work on TinyGo, which is untested.

Until then, `mise run deploy` ships the standard Go build.

## Tasks

- `mise run build:tinygo`: build both Workers and compare sizes
- `mise run bench`: run both Workers locally in workerd against the mock and compare every endpoint
- `mise run deploy:tinygo`: deploy the TinyGo build as the separate `grok-oauth-proxy-tinygo` Worker
