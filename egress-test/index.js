/**
 * Throwaway egress probe for grok-oauth-proxy.
 *
 * One question: can a plain Cloudflare Worker reach api.x.ai without the
 * GROK_EGRESS Workers VPC tunnel?
 *
 * api.x.ai/v1/models with no credentials returns 401 when reachable. Any 401
 * proves DNS + TCP + TLS + HTTP all worked, so the only thing missing is the
 * bearer token. A 403, TLS reset, timeout, or thrown fetch proves blocking.
 *
 * Controls are included so a failure can be attributed: if example.com also
 * fails, Workers egress is broken generally (unlikely). If example.com succeeds
 * but api.x.ai fails, xAI is specifically unreachable from Cloudflare.
 *
 * Delete this directory once the question is answered.
 */

const TARGETS = [
  {
    name: "xai_models",
    url: "https://api.x.ai/v1/models",
    question: "THE question. 401 = reachable, no VPC tunnel needed.",
  },
  {
    name: "control_example",
    url: "https://example.com/",
    question: "Control. 200 = Workers egress works at all.",
  },
  {
    name: "control_openai",
    url: "https://api.openai.com/v1/models",
    question: "Control. 401 = another AI API is reachable from Workers.",
  },
];

const INTERESTING_HEADERS = [
  "server",
  "content-type",
  "content-length",
  "cf-ray",
  "cf-cache-status",
  "www-authenticate",
  "retry-after",
  "x-request-id",
  "location",
];

function describeError(err) {
  if (!err) return { name: "Unknown", message: "no error object" };
  return {
    name: err.name,
    message: err.message,
    // Workers wraps transport failures in `cause`; this is where TLS resets and
    // DNS failures show up.
    cause: err.cause ? String(err.cause) : undefined,
    stack: typeof err.stack === "string"
      ? err.stack.split("\n").slice(0, 4).join("\n")
      : undefined,
  };
}

async function probe({ name, url, via, run, request }) {
  const started = Date.now();
  const result = { name, url, via };

  try {
    const res = await run(url);
    result.status = res.status;
    result.statusText = res.statusText;
    result.ok = res.ok;
    result.redirected = res.redirected;
    result.headers = {};
    for (const key of INTERESTING_HEADERS) {
      const value = res.headers.get(key);
      if (value !== null) result.headers[key] = value;
    }
    const text = await res.text();
    result.bodyLength = text.length;
    result.body = text.slice(0, 600);
  } catch (err) {
    result.threw = true;
    result.error = describeError(err);
  }

  result.durationMs = Date.now() - started;
  result.colo = request.cf?.colo;
  return result;
}

function verdict(results) {
  const xai = results.find((r) => r.name === "xai_models");
  const example = results.find((r) => r.name === "control_example");

  if (!xai) return "no xai result";

  if (xai.status === 401) {
    return "REACHABLE — got 401 from api.x.ai. DNS/TCP/TLS/HTTP all fine, only the bearer token is missing. The GROK_EGRESS VPC tunnel is NOT required; Workers can be self-contained.";
  }
  if (typeof xai.status === "number") {
    return `REACHED api.x.ai but got ${xai.status} instead of 401. Inspect headers/body — an intermediary may be intercepting.`;
  }
  if (xai.threw && example?.status === 200) {
    return "BLOCKED — api.x.ai failed while example.com succeeded. xAI is specifically unreachable from this Worker, so the VPC tunnel (or off-Cloudflare egress) is required.";
  }
  if (xai.threw && !example?.status) {
    return "INCONCLUSIVE — api.x.ai failed AND the example.com control also failed. Workers egress looks broken generally; re-run before drawing conclusions.";
  }
  return "inspect results manually";
}

export default {
  async fetch(request, env) {
    const params = new URL(request.url).searchParams;

    // Optional single-target override: /?url=https://api.x.ai/v1/models
    const override = params.get("url");
    const targets = override
      ? [{ name: "override", url: override, question: "caller-supplied target" }]
      : TARGETS;

    const viaDirect =
      params.get("via") === "binding" ? null : { label: "direct", run: (u) => fetch(u) };

    const results = [];

    if (viaDirect) {
      for (const target of targets) {
        results.push(
          await probe({
            name: target.name,
            url: target.url,
            via: viaDirect.label,
            run: viaDirect.run,
            request,
          }),
        );
      }
    }

    // Only runs if the binding is present. With no vpc_networks entry this is
    // skipped, which is itself useful confirmation that none is configured.
    if (env.GROK_EGRESS) {
      for (const target of targets) {
        results.push(
          await probe({
            name: target.name,
            url: target.url,
            via: "GROK_EGRESS binding",
            run: (u) => env.GROK_EGRESS.fetch(u),
            request,
          }),
        );
      }
    }

    return Response.json(
      {
        verdict: verdict(results),
        bindingPresent: Boolean(env.GROK_EGRESS),
        bindingNames: Object.keys(env),
        requestingColo: request.cf?.colo,
        requestingCountry: request.cf?.country,
        targets: targets.map((t) => ({ name: t.name, url: t.url, question: t.question })),
        results,
      },
      { headers: { "cache-control": "no-store" } },
    );
  },
};
