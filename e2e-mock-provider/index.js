/**
 * Throwaway OpenAI-compatible provider, deployed to Cloudflare so the *deployed*
 * proxy Worker has a publicly reachable upstream to talk to.
 *
 * Why this exists: a local mock proves the code path, but it cannot prove the
 * full deployed hop chain
 *
 *     client -> proxy Worker -> upstream -> back
 *
 * which was the last open item in .plan/done/any-provider-support.md §6 step 3.
 * A localhost upstream is unreachable from a Worker, so the mock itself has to
 * be deployed.
 *
 * Streaming uses real delays between chunks, so a buffering intermediary is
 * detectable rather than invisible.
 */

const MODELS = [
  { id: "mock-model", object: "model", owned_by: "mock" },
  { id: "mock-reasoning", object: "model", owned_by: "mock" },
];

function json(payload, status = 200) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "content-type": "application/json" },
  });
}

/** Describes the bearer token without disclosing it, so logs stay safe. */
function describeAuth(request) {
  const raw = request.headers.get("authorization") || "";
  if (!raw) return "MISSING";
  const token = raw.startsWith("Bearer ") ? raw.slice(7) : null;
  if (token === null) return "MALFORMED-NO-BEARER-PREFIX";
  if (!token) return "EMPTY";
  return `len=${token.length} prefix=${token.slice(0, 4)}`;
}

/** Streams a short SSE completion with real gaps between chunks. */
function streamCompletion(model) {
  const words = ["hello", " from", " the", " deployed", " mock"];
  const encoder = new TextEncoder();

  return new ReadableStream({
    async start(controller) {
      for (const word of words) {
        const chunk = {
          id: "mock-completion-1",
          object: "chat.completion.chunk",
          created: Math.floor(Date.now() / 1000),
          model,
          choices: [{ index: 0, delta: { content: word } }],
        };
        controller.enqueue(encoder.encode(`data: ${JSON.stringify(chunk)}\n\n`));
        await new Promise((resolve) => setTimeout(resolve, 200));
      }
      controller.enqueue(encoder.encode("data: [DONE]\n\n"));
      controller.close();
    },
  });
}

export default {
  async fetch(request) {
    const url = new URL(request.url);
    console.log(`${request.method} ${url.pathname} auth=${describeAuth(request)}`);

    if (url.pathname.endsWith("/models")) {
      return json({ object: "list", data: MODELS });
    }

    if (url.pathname.endsWith("/chat/completions")) {
      if (request.method !== "POST") {
        return json({ error: "method not allowed" }, 405);
      }

      let body;
      try {
        body = await request.json();
      } catch {
        return json({ error: "invalid JSON body" }, 400);
      }
      if (!body.model) {
        return json({ error: "model is required" }, 400);
      }

      if (body.stream) {
        return new Response(streamCompletion(body.model), {
          headers: {
            "content-type": "text/event-stream",
            "cache-control": "no-store",
          },
        });
      }

      return json({
        id: "mock-completion-1",
        object: "chat.completion",
        created: Math.floor(Date.now() / 1000),
        model: body.model,
        choices: [
          {
            index: 0,
            message: { role: "assistant", content: "hello from deployed mock" },
            finish_reason: "stop",
          },
        ],
      });
    }

    return json({ error: "not found", path: url.pathname }, 404);
  },
};
