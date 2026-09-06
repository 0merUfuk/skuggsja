"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const test = require("node:test");

const app = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
const start = app.indexOf("// BEGIN SKUGGSJA USAGE CHART");
const end = app.indexOf("// END SKUGGSJA USAGE CHART");
assert.ok(start >= 0 && end > start, "the chart must ship inside the existing embedded app.js");
const source = app.slice(start, end);

function harnesses(counts, prompts) {
  return counts.map((count, index) => ({
    id: ["claude", "codex", "cursor", "hermes"][index] || "additional-" + index,
    name: "Harness " + index,
    sessions: count,
    prompts: prompts ? prompts[index] : count,
    coverage: { status: "completeness unknown" }
  }));
}

function setup(providers) {
  class Element {
    constructor(tag) { this.tag = tag; this.children = []; this.attributes = {}; this.listeners = {}; }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    append(...nodes) { this.children.push(...nodes); }
    replaceChildren(...nodes) { this.children = nodes; }
    addEventListener(name, listener) { this.listeners[name] = listener; }
  }
  const document = {
    createElement: (tag) => new Element(tag),
    createElementNS: (namespace, tag) => new Element(tag),
    getElementById: () => ({ namespaceURI: "test-only-svg-namespace" })
  };
  const window = {};
  vm.runInNewContext(source, { window, document });
  const container = new Element("div");
  const chart = window.SkuggsjaUsageChart;
  if (providers !== undefined) chart.render(container, providers);
  const all = (node = container) => [node, ...node.children.flatMap((child) => all(child))];
  const byClass = (name) => all().filter((node) => node.className === name || node.attributes.class === name);
  const metrics = () => byClass("usage-metric");
  return { container, chart, all, byClass, metrics };
}

function assertGeometry(values) {
  const { chart } = setup();
  const layout = chart.pack(values);
  assert.equal(layout.circles.length, values.length);
  assert.deepEqual(JSON.parse(JSON.stringify(chart.pack(values))), JSON.parse(JSON.stringify(layout)), "packing is deterministic");
  layout.circles.forEach((circle, index) => {
    assert.ok(Math.abs(circle.r ** 2 - values[index]) <= values[index] * 1e-12, "area must remain proportional to the exact count");
    assert.ok(circle.x - circle.r >= layout.x - 1e-8);
    assert.ok(circle.y - circle.r >= layout.y - 1e-8);
    assert.ok(circle.x + circle.r <= layout.x + layout.width + 1e-8);
    assert.ok(circle.y + circle.r <= layout.y + layout.height + 1e-8);
    layout.circles.slice(index + 1).forEach((other) => {
      assert.ok(Math.hypot(circle.x - other.x, circle.y - other.y) >= circle.r + other.r - 1e-8, "circles must not overlap");
    });
  });
}

for (const scenario of [
  { name: "equal counts", counts: [1, 1, 1, 1] },
  { name: "uneven counts", counts: [91, 43, 27, 16] },
  { name: "long tail", counts: [74, ...Array(30).fill(1)] },
  { name: "extreme count ratio", counts: [1e12, 6, 3, 1] }
]) {
  test("packing preserves area, bounds and separation: " + scenario.name, () => assertGeometry(scenario.counts));
}

test("three or more usable harnesses receive circles and metric switching changes only the selected unit", () => {
  const providers = harnesses([91, 43, 27, 16], [81, 33, 24, 12]);
  providers.forEach((provider) => { provider.token_usage = { input: 999999999 }; provider.turns = 777777; });
  const original = JSON.stringify(providers);
  const ui = setup(providers);
  assert.equal(ui.container.attributes["data-layout"], "packed");
  assert.equal(ui.container.attributes["data-metric"], "sessions");
  assert.deepEqual(ui.byClass("usage-bubble").map((node) => Number(node.attributes["data-count"])), [91, 43, 27, 16]);
  assert.equal(ui.metrics()[0].attributes["aria-pressed"], "true");
  ui.metrics()[1].listeners.click();
  assert.equal(ui.container.attributes["data-metric"], "prompts");
  assert.equal(ui.container.attributes["data-layout"], "packed");
  assert.deepEqual(ui.byClass("usage-bubble").map((node) => Number(node.attributes["data-count"])), [81, 33, 24, 12]);
  assert.equal(ui.metrics()[1].attributes["aria-pressed"], "true");
  assert.equal(ui.metrics()[0].attributes["aria-pressed"], "false");
  assert.equal(JSON.stringify(providers), original, "presentation must not mutate the report");
  assert.doesNotMatch(ui.all().map((node) => node.textContent || "").join(" "), /999999999|777777/);
});

for (const counts of [[], [7], [7, 3]]) {
  test(counts.length + " positive entities use an honest empty or bar fallback", () => {
    const ui = setup(harnesses(counts));
    assert.equal(ui.container.attributes["data-layout"], "bars");
    assert.equal(ui.byClass("usage-bubble").length, 0);
    assert.deepEqual(ui.byClass("usage-key-value").map((node) => node.textContent), counts.map(String));
    assert.equal(ui.byClass("usage-bar").length, counts.length);
  });
}

test("a long tail is retained exactly in bars, without minimum-size inflation or invented Other groups", () => {
  const counts = [120, ...Array(39).fill(1)];
  const ui = setup(harnesses(counts));
  assert.equal(ui.container.attributes["data-layout"], "bars");
  assert.equal(ui.byClass("usage-key-button").length, 40);
  assert.deepEqual(ui.byClass("usage-bar").map((node) => node.value), counts);
  assert.doesNotMatch(ui.all().map((node) => node.textContent || "").join(" "), /Other|estimate/i);
});

test("a tiny minority falls back to bars instead of gaining fake area", () => {
  const ui = setup(harnesses([1000000, 3, 1]));
  assert.equal(ui.container.attributes["data-layout"], "bars");
  assert.deepEqual(ui.byClass("usage-bar").map((node) => node.value), [1000000, 3, 1]);
});

test("missing or invalid metrics remain unavailable while measured zero remains zero", () => {
  const ui = setup(harnesses([0, undefined, -3, 1.5, Number.MAX_SAFE_INTEGER + 1]));
  assert.deepEqual(ui.byClass("usage-key-value").map((node) => node.textContent), ["0", "Not available", "Not available", "Not available", "Not available"]);
  assert.equal(ui.byClass("usage-bar").length, 1);
  assert.equal(ui.byClass("usage-bar")[0].value, 0);
  assert.equal(ui.byClass("usage-bubble").length, 0);
});

test("circle hover, tap and keyboard expose the same exact record and coverage", () => {
  const ui = setup(harnesses([91, 43, 27, 16]));
  const bubble = ui.byClass("usage-bubble")[2];
  assert.equal(bubble.attributes.role, "button");
  assert.equal(bubble.attributes.tabindex, "0");
  for (const event of ["pointerenter", "click", "focus"]) {
    bubble.listeners[event]();
    assert.match(ui.byClass("usage-detail")[0].textContent, /Harness 2 · 27 sessions · completeness unknown/);
    assert.equal(bubble.attributes["data-active"], "true");
  }
  for (const key of ["Enter", " "]) {
    let prevented = false;
    bubble.listeners.keydown({ key, preventDefault() { prevented = true; } });
    assert.equal(prevented, true);
    assert.match(ui.byClass("usage-detail")[0].textContent, /27 sessions/);
  }
  const button = ui.byClass("usage-key-button")[1];
  button.listeners.focus();
  assert.match(ui.byClass("usage-detail")[0].textContent, /Harness 1 · 43 sessions/);
  assert.equal(bubble.attributes["data-active"], "false");
  assert.equal(ui.byClass("usage-detail")[0].attributes["aria-live"], "polite");
});

test("chart names and selected details use singular units for one in both circle and bar layouts", () => {
  for (const scenario of [
    { counts: [1, 0, 2, null], layout: "bars", sessions: ["1 session", "0 sessions", "2 sessions", "Not available"], prompts: ["1 prompt", "0 prompts", "2 prompts", "Not available"] },
    { counts: [1, 1, 1], layout: "packed", sessions: ["1 session", "1 session", "1 session"], prompts: ["1 prompt", "1 prompt", "1 prompt"] }
  ]) {
    const providers = harnesses(scenario.counts);
    const ui = setup(providers);
    for (const [index, metric] of ["sessions", "prompts"].entries()) {
      ui.metrics()[index].listeners.click();
      assert.equal(ui.container.attributes["data-layout"], scenario.layout);
      for (const node of [...ui.byClass("usage-key-button"), ...ui.byClass("usage-bubble")]) {
        const providerIndex = providers.findIndex((provider) => provider.id === node.attributes["data-harness"]);
        const name = providers[providerIndex].name;
        const value = scenario[metric][providerIndex];
        assert.equal(node.attributes["aria-label"], `${name}: ${value}. completeness unknown`);
        for (const event of ["pointerenter", "click", "focus"]) {
          node.listeners[event]();
          assert.equal(ui.byClass("usage-detail")[0].textContent, `${name} · ${value} · completeness unknown.`);
        }
      }
    }
  }
});

test("labels use inert text, retain privacy redaction and never control CSS or markup", () => {
  const providers = harnesses([9, 7, 6]);
  providers[0].name = "<img src=x onerror=alert(1)> /Users/private/project\nname";
  providers[0].id = "claude\" onclick=alert(1)";
  providers[0].coverage.status = "status C:\\private\\token";
  const ui = setup(providers);
  const labels = ui.all().map((node) => node.textContent || "").join(" ");
  assert.doesNotMatch(labels, /\/Users\/private|C:\\private|\n/);
  assert.match(labels, /\[local path\]/);
  assert.match(labels, /<img src=x onerror=alert\(1\)>/);
  assert.equal(ui.byClass("usage-key-button")[0].attributes["data-harness"], "unknown");
  assert.equal(ui.all().some((node) => node.tag === "img"), false);
  assert.equal(ui.all().some((node) => Object.hasOwn(node, "innerHTML")), false);
});

test("metric switching preserves selected harness and replacing a report leaves one set of controls", () => {
  const ui = setup(harnesses([91, 43, 27, 16], [10, 30, 20, 40]));
  ui.byClass("usage-key-button")[1].listeners.click();
  ui.metrics()[1].listeners.click();
  assert.match(ui.byClass("usage-detail")[0].textContent, /Harness 1 · 30 prompts/);
  ui.chart.render(ui.container, harnesses([4]));
  assert.equal(ui.metrics().length, 2);
  assert.equal(ui.byClass("usage-key-button").length, 1);
  assert.equal(ui.container.attributes["data-metric"], "sessions");
});
