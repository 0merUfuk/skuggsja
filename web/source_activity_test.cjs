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
  }
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  }
  const document = {
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
  assert.equal(activity.hidden, true);
  assert.equal(activity.textContent, "");

  rejectRetry(new Error("Synthetic local endpoint failure"));
  await retry;
  assert.equal(elements.get("error-state").hidden, false);
  assert.equal(activity.hidden, true);
  assert.equal(activity.textContent, "");
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
