// browser-check drives a headless Chrome over the DevTools protocol and checks
// the one thing the picker cannot prove on the server: that choosing a mode in
// a real browser swaps to that mode's models, and that the page's own bundled
// JavaScript runs.
//
// It talks to Chrome with Node's built-in WebSocket, so it needs nothing
// installed. gsxui tests its own behaviour modules with Playwright; if this
// grows past a handful of checks, switch to that rather than growing this.
//
// Run it through mise: `mise run browser` starts the server and Chrome, with
// this spike and this script as the defaults.
//
//   node browser-check.mjs <devtools-endpoint> <page-url>

const [, , endpoint, pageURL] = process.argv;
if (!endpoint || !pageURL) {
  console.error("usage: node browser-check.mjs <devtools-endpoint> <page-url>");
  process.exit(2);
}

const failures = [];
const check = (ok, description, detail) => {
  console.log(`${ok ? "ok  " : "FAIL"} ${description}${detail === undefined ? "" : `: ${detail}`}`);
  if (!ok) failures.push(description);
};

// --- connect ----------------------------------------------------------------

const targets = await (await fetch(`${endpoint}/json/list`)).json();
const page = targets.find((t) => t.type === "page");
if (!page) {
  console.error(`no page target at ${endpoint}`);
  process.exit(1);
}
const socket = new WebSocket(page.webSocketDebuggerUrl);
await new Promise((resolve, reject) => {
  socket.onopen = resolve;
  socket.onerror = reject;
});

let nextID = 1;
const pending = new Map();
const consoleErrors = [];
socket.onmessage = (event) => {
  const message = JSON.parse(event.data);
  if (message.id && pending.has(message.id)) {
    pending.get(message.id)(message);
    pending.delete(message.id);
    return;
  }
  if (message.method === "Runtime.exceptionThrown") {
    consoleErrors.push(message.params.exceptionDetails.text ?? "exception");
  }
  if (message.method === "Runtime.consoleAPICalled" && message.params.type === "error") {
    consoleErrors.push(message.params.args.map((a) => a.value ?? a.description).join(" "));
  }
};
const send = (method, params = {}) =>
  new Promise((resolve) => {
    const id = nextID++;
    pending.set(id, resolve);
    socket.send(JSON.stringify({ id, method, params }));
  });

const evaluate = async (expression) => {
  const reply = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
  const details = reply.result?.exceptionDetails;
  if (details) throw new Error(`${expression}: ${details.text} ${details.exception?.description ?? ""}`);
  return reply.result.result.value;
};
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// --- load -------------------------------------------------------------------

await send("Runtime.enable");
await send("Page.enable");
await send("Page.navigate", { url: pageURL });

let loaded = false;
for (let attempt = 0; attempt < 100 && !loaded; attempt++) {
  loaded = await evaluate(
    `document.readyState === "complete" && !!document.querySelector("[data-gsxui-slot-tabs]")`,
  );
  if (!loaded) await sleep(100);
}
check(loaded, "the page loads and renders a tab group");
if (!loaded) process.exit(1);

// --- the picker -------------------------------------------------------------

const openPanel = () =>
  evaluate(`document.querySelector('[data-gsxui-slot-tabs-content][data-state="active"]:not([hidden])')?.dataset.value ?? null`);
const visibleModels = () =>
  evaluate(`Array.from(document.querySelectorAll(
    '[data-gsxui-slot-tabs-content][data-state="active"] [data-gsxui-slot-item-title]',
  )).map((e) => e.textContent.trim())`);
const visibleEmptyState = () =>
  evaluate(`document.querySelector(
    '[data-gsxui-slot-tabs-content][data-state="active"] [data-gsxui-slot-empty-title]',
  )?.textContent.trim() ?? null`);
const activeCount = () =>
  evaluate(`document.querySelectorAll('[data-gsxui-slot-tabs-content][data-state="active"]').length`);

const modes = await evaluate(
  `Array.from(document.querySelectorAll('[data-gsxui-slot-tabs-trigger]')).map((e) => e.dataset.value)`,
);
check(modes.length > 1, "the page offers more than one mode", modes.join(", "));

const requested = new URL(pageURL).searchParams.get("mode");
if (requested) {
  check((await openPanel()) === requested, `?mode=${requested} opens that panel at first paint`);
}

const seen = new Map();
for (const mode of modes) {
  await evaluate(`document.querySelector('[data-gsxui-slot-tabs-trigger][data-value="${mode}"]').click()`);
  await sleep(150);
  const open = await openPanel();
  check(open === mode, `clicking ${mode} opens the ${mode} panel`, open);
  check((await activeCount()) === 1, `only one panel is open in ${mode} mode`);
  const models = await visibleModels();
  const empty = await visibleEmptyState();
  check(
    models.length > 0 || empty !== null,
    `${mode} shows either models or a reason it has none`,
    models.length > 0 ? models.join(", ") : empty,
  );
  seen.set(mode, models.join("|"));
}

const withModels = [...seen.entries()].filter(([, models]) => models !== "");
check(
  new Set(withModels.map(([, models]) => models)).size === withModels.length,
  "each mode shows its own models, not a shared list",
);

// --- the app's own bundled JavaScript ---------------------------------------

const before = await evaluate(`document.querySelector("#counter")?.textContent ?? null`);
if (before !== null) {
  await evaluate(`document.querySelector("#counter").click()`);
  const after = await evaluate(`document.querySelector("#counter").textContent`);
  check(after !== before, "the Vite-bundled counter script runs", `${before} -> ${after}`);
}

check(consoleErrors.length === 0, "the page logs no errors", consoleErrors.join("; ") || "none");

socket.close();
if (failures.length > 0) {
  console.error(`\n${failures.length} browser check(s) failed:\n  ${failures.join("\n  ")}`);
  process.exit(1);
}
console.log(`\nall browser checks passed against ${pageURL}`);
