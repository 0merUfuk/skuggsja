"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const test = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");

// Run the complete production script with a local aggregate response. This
// verifies rendered text and status classes; it is not a browser layout test.
async function render(data, subsequentFetch) {
  class Element {
    constructor() {
      this.textContent = "";
      this.hidden = true;
      this.children = [];
      this.attributes = {};
      this.listeners = new Map();
      this.classes = new Set();
      this.classList = {
        add: (...names) => names.forEach((name) => this.classes.add(name)),
        remove: (...names) => names.forEach((name) => this.classes.delete(name))
      };
    }
    append(...children) { this.children.push(...children); }
    appendChild(child) { this.children.push(child); return child; }
    replaceChildren(...children) { this.children = children; }
    setAttribute(name, value) { this.attributes[name] = value; }
    addEventListener(name, handler) { this.listeners.set(name, handler); }
    focus() { document.activeElement = this; }
    scrollIntoView(options) { this.scrollOptions = options; }
  }
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  }
  const document = {
    activeElement: null,
    getElementById: element,
    querySelector: element,
    querySelectorAll: () => [],
    createElement: () => new Element(),
    createElementNS: () => new Element()
  };
  let requests = 0;
  vm.runInNewContext(source, {
    document,
    window: { matchMedia: () => ({ matches: true }) },
    AbortController,
    fetch: async (url) => {
      assert.equal(url, "/api/rewind");
      if (requests++ > 0 && subsequentFetch) return subsequentFetch();
      return { ok: true, json: async () => data };
    }
  });
  await new Promise(setImmediate);
  assert.equal(element("error-state").hidden, true, element("error-message").textContent);
  assert.equal(element(data.totals.sessions ? "rewind" : "empty-state").hidden, false);
  assert.equal(document.activeElement, null, "initial load must not move keyboard focus");
  elements.activeElement = () => document.activeElement;
  return elements;
}

test("source guarantee is outside all conditional page states", () => {
  const access = html.indexOf('class="source-status"');
  assert.ok(access > html.indexOf('<main id="main-content">'));
  assert.ok(access < html.indexOf('id="loading-state"'));
  assert.match(html.slice(access, html.indexOf('id="loading-state"')), /skuggsja does not write to source paths\./);
  assert.match(html, /id="source-activity" aria-live="polite"/);
});

test("retry clears previous source activity before a failed response", async () => {
  let rejectRetry;
  const pendingRetry = new Promise((resolve, reject) => { rejectRetry = reject; });
  const data = {
    totals: { sessions: 0 }, providers: [], warnings: [],
    privacy: { source_access: "read-only", source_observation: "observed", source_audit: { changed_files: 2 } }
  };
  const elements = await render(data, () => pendingRetry);
  const activity = elements.get("source-activity");
  assert.match(activity.textContent, /2 files changed/);
  assert.equal(activity.hidden, false);

  const retry = elements.get("empty-retry-button").listeners.get("click")();
  assert.equal(elements.get("loading-state").hidden, false);
  assert.equal(elements.get(".folio-nav").hidden, true);
  assert.equal(activity.hidden, true);
  assert.equal(activity.textContent, "");

  rejectRetry(new Error("Synthetic local endpoint failure"));
  await retry;
  assert.equal(elements.get("error-state").hidden, false);
  assert.equal(elements.get(".folio-nav").hidden, true);
  assert.equal(elements.activeElement(), elements.get("retry-button"));
  assert.equal(activity.hidden, true);
  assert.equal(activity.textContent, "");
});

test("chapter navigation follows report availability and retry focuses its visible result", async () => {
  assert.match(html, /<nav class="folio-nav"[^>]* hidden>/);
  for (const sessions of [0, 1]) {
    const response = { totals: { sessions }, providers: [], warnings: [] };
    const elements = await render({ totals: { sessions: 0 } }, async () => ({ ok: true, json: async () => response }));
    assert.equal(elements.get(".folio-nav").hidden, true);
    await elements.get("empty-retry-button").listeners.get("click")();
    assert.equal(elements.get(".folio-nav").hidden, sessions === 0);
    const heading = elements.get(sessions ? "hero-title" : "empty-title");
    assert.equal(elements.activeElement(), heading);
    assert.equal(heading.attributes.tabindex, "-1");
  }
});

test("tool counts stay in provider folios and ignore a legacy global total", async () => {
  const elements = await render({
    totals: { sessions: 2, tool_calls: 999999 },
    providers: [
      { id: "claude", name: "Claude Code", sessions: 1, tool_calls: 2, tool_calls_available: true },
      { id: "hermes", name: "Hermes Agent", sessions: 1, tool_calls: 7, tool_calls_available: true },
      { id: "cursor", name: "Cursor", sessions: 0, tool_calls: 0, tool_calls_available: false }
    ]
  });
  assert.doesNotMatch(html, /tool-call-count|from reporting harnesses/);
  assert.match(html, /Tool-call units differ by harness/);
  assert.equal(elements.has("tool-call-count"), false);
  const folios = elements.get("provider-list").children;
  assert.deepEqual(folios.map((folio) => folio.attributes["data-harness"]), ["claude", "hermes", "cursor"]);
  const counts = folios.map((folio) => {
    const facts = folio.children[1].children.find((child) => child.className === "provider-facts");
    const toolFact = facts.children.find((fact) => fact.children[0].textContent === "Tool calls · native count");
    return toolFact.children[1].textContent;
  });
  assert.deepEqual(counts, ["2", "7", "Not available"]);
});

test("model rankings and meter scales restart within each harness", async () => {
  const elements = await render({
    totals: { sessions: 2 },
    providers: [{ id: "claude", name: "Claude Code" }, { id: "hermes", name: "Hermes Agent" }],
    models: [
      { harness: "hermes", name: "api-model", turns: 12 },
      { harness: "claude", name: "response-model-small", turns: 1 },
      { harness: "claude", name: "response-model-large", turns: 3 }
    ]
  });
  const groups = elements.get("model-list").children;
  assert.deepEqual(groups.map((group) => group.attributes["data-harness"]), ["claude", "hermes"]);
  const rows = groups.map((group) => group.children[1].children[0].children);
  assert.deepEqual(rows.map((groupRows) => groupRows[0].children[0].textContent), ["01", "01"]);
  assert.deepEqual(rows.map((groupRows) => groupRows.map((row) => row.children[2].max)), [[3, 3], [12]]);
  assert.deepEqual(rows.map((groupRows) => groupRows.map((row) => row.children[2].value)), [[3, 1], [12]]);
  assert.equal(rows[0][1].children[3].textContent, "1 native event");
  assert.equal(rows[0][1].children[2].attributes["aria-label"], "response-model-small: 1 native event");
  assert.doesNotMatch(elements.get("hero-narrative").textContent, /appears most often|api-model|response-model/);
});

test("long ranked lists show their disclosure state and return focus when collapsed from the end", async () => {
  const elements = await render({
    totals: { sessions: 21 },
    projects: Array.from({ length: 21 }, (_, index) => ({ name: "Project " + index, sessions: 21 - index }))
  });
  const [primary, more] = elements.get("project-list").children;
  const [summary, rest, collapse] = more.children;
  assert.equal(primary.children.length, 10);
  assert.equal(rest.children.length, 11);
  assert.equal(rest.start, 11);
  assert.equal(summary.textContent, "Show 11 more");
  assert.equal(collapse.type, "button");
  assert.equal(collapse.textContent, "Show fewer");

  more.open = true;
  more.listeners.get("toggle")();
  assert.equal(summary.textContent, "Show fewer");
  collapse.listeners.get("click")();
  assert.equal(more.open, false);
  assert.equal(elements.activeElement(), summary);
  assert.equal(summary.scrollOptions.block, "nearest");
  assert.equal(summary.scrollOptions.behavior, "instant");
  more.listeners.get("toggle")();
  assert.equal(summary.textContent, "Show 11 more");
  assert.equal(rest.children[0].children[0].textContent, "11");
  assert.equal(rest.children[0].children[2].value, 11);
});

test("short ranked-list disclosures do not repeat the collapse control", async () => {
  const elements = await render({
    totals: { sessions: 11 },
    projects: Array.from({ length: 11 }, (_, index) => ({ name: "Project " + index, sessions: 11 - index }))
  });
  const more = elements.get("project-list").children[1];
  assert.equal(more.children.length, 2);
  assert.equal(more.children[1].children[0].children[3].textContent, "1 session");
  more.open = true;
  more.listeners.get("toggle")();
  assert.equal(more.children[0].textContent, "Show fewer");
});

test("weekday meters name exact zero, singular and plural session counts", async () => {
  const elements = await render({
    totals: { sessions: 3 },
    rhythm: { weekdays: [0, 1, 2, 0, 0, 0, 0] }
  });
  const meters = elements.get("weekday-list").children.map((row) => row.children[1]);
  assert.deepEqual(meters.map((meter) => meter.attributes["aria-label"]), [
    "Monday: 0 sessions", "Tuesday: 1 session", "Wednesday: 2 sessions",
    "Thursday: 0 sessions", "Friday: 0 sessions", "Saturday: 0 sessions", "Sunday: 0 sessions"
  ]);
  assert.deepEqual(meters.map((meter) => meter.value), [0, 1, 2, 0, 0, 0, 0]);
  assert.equal(meters.every((meter) => meter.max === 2), true);
});

for (const scenario of [
  { name: "quiet", observation: "observed", audit: { files: 8, verified: true }, want: /No concurrent source changes were observed/ },
  { name: "incomplete equality is neutral", observation: "observed", audit: { files: 8, verified: false }, want: /No concurrent source changes were observed/ },
  { name: "concurrent files", observation: "observed", audit: { files: 8, changed_files: 2, verified: false }, want: /2 files changed during the run by another process; skuggsja does not write to source paths\./ },
  { name: "directory changes", observation: "observed", audit: { files: 8, directory_changes: 2 }, want: /2 directory listings changed during the run by another process\./, absent: /2 files changed/ },
  { name: "files and directory changes", observation: "observed", audit: { files: 8, changed_files: 2, directory_changes: 3 }, want: /2 files changed.*3 directory listings changed/, absent: /5 files changed/ },
  { name: "disabled", observation: "disabled", audit: {}, want: /observation was skipped.*Source access remains read-only/ },
  { name: "unavailable", observation: "unavailable", audit: {}, want: /observation was unavailable.*Source access remains read-only/ },
  { name: "equality flag cannot mask concurrent activity", observation: "observed", audit: { changed_files: 2, verified: true }, want: /2 files changed during the run by another process/ },
  { name: "legacy aggregate", audit: { changed_files: 2, manifest_before: "a", manifest_after: "b" }, want: /2 files changed during the run by another process/ }
]) {
  test(scenario.name + " stays neutral in full and empty reports", async () => {
    for (const sessions of [0, 1]) {
      const elements = await render({
        totals: { sessions }, providers: [], warnings: [],
        privacy: { source_access: "read-only", source_observation: scenario.observation,
          source_audit: scenario.audit, raw_content_persisted: false, absolute_paths_persisted: false }
      });
      const activity = elements.get("source-activity");
      assert.equal(activity.hidden, false);
      assert.match(activity.textContent, scenario.want);
      if (scenario.absent) assert.doesNotMatch(activity.textContent, scenario.absent);
      assert.doesNotMatch(activity.textContent, /verified|inconclusive|warning|failure/i);
      assert.equal(activity.classes.size, 0);
      if (sessions) {
        assert.equal(elements.get("proof-audit").textContent, "Read-only");
        assert.equal(elements.get("privacy-source-access").textContent, "Read-only");
        assert.equal(elements.get("privacy-source-activity").textContent, activity.textContent);
        assert.equal(elements.get("privacy-audit-files").classes.size, 0);
        assert.equal(elements.get("warning-count").textContent, "0");
      }
    }
  });
}
