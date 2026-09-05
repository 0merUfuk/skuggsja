#!/usr/bin/env node
// Verification-only, Node 22+. Chrome is controlled directly over CDP.
// Protocol references: https://chromedevtools.github.io/devtools-protocol/
// No browser profile, screenshot, or real aggregate belongs in the repository.
"use strict";
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { setTimeout: delay } = require("node:timers/promises");

const args = {};
for (let i = 2; i < process.argv.length; i += 2) {
  assert(process.argv[i].startsWith("--") && process.argv[i + 1], "expected --key value");
  args[process.argv[i].slice(2)] = process.argv[i + 1];
}
for (const name of ["chrome", "url", "report", "evidence-dir"]) assert(args[name], `missing --${name}`);
function loopback(raw) {
  try {
    const url = new URL(raw);
    return ["http:", "https:", "ws:", "wss:"].includes(url.protocol) &&
      ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
  } catch { return false; }
}
assert(loopback(args.url), "only a loopback product URL is allowed");
assert(fs.statSync(args.chrome).isFile(), "Chrome executable missing");
const evidence = path.resolve(args["evidence-dir"]);
fs.mkdirSync(evidence, { mode: 0o700 }); // Never overwrite another evidence run.
const profile = path.join(evidence, "isolated-chrome-profile");
const reportBytes = fs.readFileSync(args.report);
const expected = JSON.parse(reportBytes);
const hash = (bytes) => crypto.createHash("sha256").update(bytes).digest("hex");
const save = (name, data) => fs.writeFileSync(path.join(evidence, name), data, { mode: 0o600 });
const result = { started_at: new Date().toISOString(), report_sha256: hash(reportBytes),
  chrome_sha256: hash(fs.readFileSync(args.chrome)), assertions: [], requests: [],
  intercepted: [], exceptions: [], unhandled_targets: [], screenshots: [], pass: false };
let chrome;
let cdp;
let stopping = false;
const activeSessions = new Map();
const attachments = new Map();
const knownTargets = new Set();
const failures = [];

class CDP {
  constructor(ws) {
    this.ws = ws; this.next = 1; this.pending = new Map(); this.handlers = [];
    ws.addEventListener("message", (event) => {
      const message = JSON.parse(String(event.data));
      if (message.id) {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        this.pending.delete(message.id); clearTimeout(pending.timer);
        message.error ? pending.reject(new Error(JSON.stringify(message.error))) : pending.resolve(message.result);
      } else {
        for (const handle of this.handlers) Promise.resolve(handle(message)).catch((error) => failures.push(String(error)));
      }
    });
    ws.addEventListener("close", () => {
      for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(new Error("CDP closed")); }
      this.pending.clear();
    });
  }
  send(method, params = {}, sessionId) {
    return new Promise((resolve, reject) => {
      const id = this.next++;
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`CDP timeout: ${method}`)); }, 30000);
      this.pending.set(id, { resolve, reject, timer });
      this.ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
    });
  }
}

async function until(predicate, label, milliseconds = 20000) {
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) {
    if (failures.length) throw new Error(failures.join("\n"));
    const value = await predicate();
    if (value) return value;
    await delay(100);
  }
  throw new Error(`Timed out: ${label}`);
}
function check(name, actual, wanted) {
  assert.deepEqual(actual, wanted, name);
  result.assertions.push({ name, actual, expected: wanted, pass: true });
}
async function evaluate(session, expression) {
  const reply = await cdp.send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true }, session);
  if (reply.exceptionDetails) throw new Error(JSON.stringify(reply.exceptionDetails));
  return reply.result.value;
}
async function configure(session, scope) {
  activeSessions.set(session, scope);
  await cdp.send("Network.enable", {}, session);
  await cdp.send("Network.setCacheDisabled", { cacheDisabled: true }, session);
  await cdp.send("Network.setBypassServiceWorker", { bypass: true }, session);
  await cdp.send("Fetch.enable", { patterns: [{ urlPattern: "*", requestStage: "Request" }] }, session);
  await cdp.send("Runtime.enable", {}, session);
  await cdp.send("Page.enable", {}, session);
  // A new child target stays paused until it has interception. This product has
  // no workers/popups; unexpected targets are a failure, never invisible traffic.
  await cdp.send("Target.setAutoAttach", { autoAttach: true, waitForDebuggerOnStart: true, flatten: true }, session);
}
async function newPage(scope) {
  const { targetId } = await cdp.send("Target.createTarget", { url: "about:blank" });
  knownTargets.add(targetId);
  const { sessionId } = await until(() => attachments.get(targetId), "paused browser target");
  await configure(sessionId, scope);
  await cdp.send("Runtime.runIfWaitingForDebugger", {}, sessionId);
  return { targetId, sessionId };
}
async function screenshot(session, name, clip) {
  const shot = await cdp.send("Page.captureScreenshot", { format: "png", captureBeyondViewport: Boolean(clip), fromSurface: true, ...(clip ? { clip } : {}) }, session);
  const bytes = Buffer.from(shot.data, "base64");
  save(name, bytes); result.screenshots.push({ name, bytes: bytes.length, sha256: hash(bytes) });
}
async function settled(session) {
  await evaluate(session, "document.fonts.ready.then(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))");
  await delay(500);
}

async function main() {
  const chromeArgs = ["--headless", "--lang=en-US", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
    `--user-data-dir=${profile}`, "--no-first-run", "--no-default-browser-check", "--disable-gpu",
    "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
    "--disable-domain-reliability", "--disable-breakpad", "--metrics-recording-only",
    "--disable-features=MediaRouter,OptimizationHints,AutofillServerCommunication", "--password-store=basic",
    "--use-mock-keychain", "about:blank"];
  const policy = path.join(__dirname, "macos-network-deny.sb");
  const sandboxed = process.platform === "darwin" && fs.existsSync("/usr/bin/sandbox-exec");
  result.browser_network_policy = sandboxed ? "OS remote-network denial plus CDP interception" : "CDP interception";
  // Chromium's child Seatbelt initialization cannot nest inside sandbox-exec.
  // This fresh verification browser uses the outer inherited network policy;
  // no existing browser/profile is touched and the product is not reconfigured.
  if (sandboxed) chromeArgs.push("--no-sandbox");
  result.browser_child_sandbox = sandboxed ? "outer Seatbelt policy" : "Chrome default";
  chrome = spawn(sandboxed ? "/usr/bin/sandbox-exec" : args.chrome,
    sandboxed ? ["-f", policy, args.chrome, ...chromeArgs] : chromeArgs, { stdio: ["ignore", "pipe", "pipe"] });
  let output = "";
  for (const stream of [chrome.stdout, chrome.stderr]) stream.on("data", (chunk) => {
    output += chunk.toString(); save("chrome.log", output);
  });
  chrome.on("error", (error) => failures.push(String(error)));
  chrome.on("exit", (code, signal) => { if (!stopping) failures.push(`Chrome exited: ${code} ${signal}`); });
  const endpoint = await until(() => output.match(/ws:\/\/127\.0\.0\.1:\d+\/devtools\/browser\/[^\s]+/)?.[0], "Chrome CDP startup");
  const ws = new WebSocket(endpoint);
  await new Promise((resolve, reject) => { ws.addEventListener("open", resolve, { once: true }); ws.addEventListener("error", reject, { once: true }); });
  cdp = new CDP(ws);
  result.browser = await cdp.send("Browser.getVersion");
  cdp.handlers.push(async (message) => {
    const scope = activeSessions.get(message.sessionId);
    if (message.method === "Fetch.requestPaused") {
      const raw = message.params.request.url;
      const allowed = loopback(raw) || raw === "about:blank" || raw.startsWith("data:");
      result.intercepted.push({ scope: scope || "unknown", url: raw, allowed });
      await cdp.send(allowed ? "Fetch.continueRequest" : "Fetch.failRequest",
        { requestId: message.params.requestId, ...(allowed ? {} : { errorReason: "BlockedByClient" }) }, message.sessionId);
    } else if (message.method === "Network.requestWillBeSent") {
      const raw = message.params.request.url;
      if (/^(https?|wss?):/.test(raw)) result.requests.push({ scope: scope || "unknown", url: raw,
        loopback: loopback(raw), type: message.params.type, method: message.params.request.method });
    } else if (message.method === "Runtime.exceptionThrown" && scope === "product") {
      result.exceptions.push(message.params.exceptionDetails);
    } else if (message.method === "Network.webSocketCreated") {
      // Fetch does not cover WebSocket handshakes. Observe attempts explicitly;
      // the macOS policy also denies actual non-loopback sockets independently.
      result.requests.push({ scope: scope || "unknown", url: message.params.url,
        loopback: loopback(message.params.url), type: "WebSocket", method: "handshake" });
    } else if (message.method === "Target.attachedToTarget") {
      const info = message.params.targetInfo;
      attachments.set(info.targetId, { sessionId: message.params.sessionId, type: info.type, url: info.url });
      // Keep unexpected child code paused. Failing closed prevents a new worker
      // from escaping an otherwise page-scoped request observer.
    }
  });
  for (const target of (await cdp.send("Target.getTargets")).targetInfos) knownTargets.add(target.targetId);
  await cdp.send("Target.setDiscoverTargets", { discover: true });
  await cdp.send("Target.setAutoAttach", { autoAttach: true, waitForDebuggerOnStart: true, flatten: true });
  const control = await newPage("control");
  await evaluate(control.sessionId, "fetch('https://example.invalid/__skuggsja_interception_control__').then(() => 'unexpected').catch(() => 'blocked')");
  await until(() => result.intercepted.some((r) => r.scope === "control" && !r.allowed), "interception negative control");
  check("external control intercepted and denied", result.intercepted.filter((r) => r.scope === "control" && !r.allowed).length, 1);
  check("external control observed", result.requests.some((r) => r.scope === "control" && !r.loopback), true);
  await cdp.send("Target.closeTarget", { targetId: control.targetId });
  const page = await newPage("product");
  const session = page.sessionId;
  await cdp.send("Emulation.setLocaleOverride", { locale: "en-US" }, session);
  await cdp.send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false }, session);
  await cdp.send("Page.navigate", { url: args.url }, session);
  await until(() => evaluate(session, "Boolean(document.querySelector('#rewind') && !document.querySelector('#rewind').hidden)"), "rendered real Rewind", 30000);
  await settled(session);
  const actual = await evaluate(session, "fetch('/api/rewind').then(r => r.json())");
  check("API is the retained real-data report", actual, expected);
  // Keep full aggregate out of the compact assertion ledger; its private source
  // file and SHA-256 are retained separately, while values below trace the cards.
  result.assertions[result.assertions.length - 1] = { name: "API is the retained real-data report", pass: true };
  check("current schema", actual.schema_version, 3);
  check("real sessions are present", actual.totals.sessions > 0, true);
  check("no global tool-call field", Object.hasOwn(actual.totals, "tool_calls"), false);
  const overview = await evaluate(session, `({
    sessions: document.querySelector('#hero-session-count').textContent,
    prompts: document.querySelector('#proof-prompts').textContent,
    projects: document.querySelector('#proof-projects').textContent,
    days: document.querySelector('#proof-days').textContent,
    readOnly: document.querySelector('.source-status').textContent.includes('skuggsja does not write to source paths'),
    globalToolCounter: Boolean(document.querySelector('#tool-call-count')),
    cards: document.querySelectorAll('.provider-entry[data-harness]').length,
    overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    loadingHidden: document.querySelector('#loading-state').hidden,
    errorHidden: document.querySelector('#error-state').hidden
  })`);
  result.desktop_layout = await evaluate(session, `({width:innerWidth,clientWidth:document.documentElement.clientWidth,scrollWidth:document.documentElement.scrollWidth,
    overflowing:[...document.querySelectorAll('body *')].filter(e=>{const r=e.getBoundingClientRect();return r.width>0&&(r.right>innerWidth+1||r.left< -1)}).slice(0,30).map(e=>({tag:e.tagName,id:e.id,class:e.className.baseVal??e.className,left:e.getBoundingClientRect().left,right:e.getBoundingClientRect().right,width:e.getBoundingClientRect().width}))})`);
  await screenshot(session, "rewind-desktop.png");
  const number = (v) => new Intl.NumberFormat("en-US").format(v);
  check("hero session value", overview.sessions, number(actual.totals.sessions));
  check("prompt card value", overview.prompts, number(actual.totals.prompts));
  check("project card value", overview.projects, number(actual.totals.projects));
  check("active-days value", overview.days, number(actual.totals.active_days));
  check("read-only guarantee rendered", overview.readOnly, true);
  check("no global tool counter", overview.globalToolCounter, false);
  check("provider card count", overview.cards, actual.providers.length);
  check("desktop no page overflow", overview.overflow, false);
  check("desktop full duration fits", await evaluate(session, "(() => {const n=document.querySelector('#longest-session-duration');return n.scrollWidth<=n.clientWidth;})()"), true);
  check("loading state completed", overview.loadingHidden, true);
  check("error state hidden", overview.errorHidden, true);
  // Exercise actual disclosure controls with CDP pointer input.
  for (const provider of actual.providers) {
    const selector = `.provider-entry[data-harness=${JSON.stringify(provider.id)}]`;
    const position = await evaluate(session, `(() => { const d=document.querySelector(${JSON.stringify(selector)}); if(d.open)return null; const s=d.querySelector('summary');s.scrollIntoView({block:'center',behavior:'instant'});const b=s.getBoundingClientRect();return {x:b.x+b.width/2,y:b.y+b.height/2};})()`);
    if (position) {
      await cdp.send("Input.dispatchMouseEvent", { type: "mousePressed", ...position, button: "left", clickCount: 1 }, session);
      await cdp.send("Input.dispatchMouseEvent", { type: "mouseReleased", ...position, button: "left", clickCount: 1 }, session);
    }
    const card = await evaluate(session, `(() => {const d=document.querySelector(${JSON.stringify(selector)});return {open:d.open,sessions:d.querySelector('.provider-session-count').textContent,coverage:d.querySelector('.provider-summary-coverage').textContent,facts:Object.fromEntries([...d.querySelectorAll('.provider-facts > div')].map(n=>[n.querySelector('dt').textContent,n.querySelector('dd').textContent]))};})()`);
    check(`${provider.id} disclosure opens`, card.open, true);
    check(`${provider.id} sessions`, card.sessions, number(provider.sessions));
    check(`${provider.id} prompts`, card.facts.Prompts, number(provider.prompts));
    check(`${provider.id} native tool count`, card.facts["Tool calls · native count"], provider.tool_calls_available ? number(provider.tool_calls) : "Not available");
    check(`${provider.id} coverage visible`, card.coverage.includes(provider.coverage.status), true);
  }
  // Scroll each chapter into view so its production IntersectionObserver reveal
  // runs before a full-page capture. Do not force visibility through CSS/DOM edits.
  const chapters = await evaluate(session, "[...document.querySelectorAll('#rewind .chapter')].map(e=>e.id).filter(Boolean)");
  for (const id of chapters) {
    await evaluate(session, `document.getElementById(${JSON.stringify(id)}).scrollIntoView({block:'start',behavior:'instant'})`);
    await delay(700);
  }
  check("all chapter reveals visible", await evaluate(session, "[...document.querySelectorAll('#rewind .will-reveal')].filter(e=>e.getClientRects().length>0).every(e=>Number(getComputedStyle(e).opacity)===1)"), true);
  await settled(session);
  const layout = await cdp.send("Page.getLayoutMetrics", {}, session);
  await screenshot(session, "rewind-desktop-full.png", { x: 0, y: 0, width: 1440, height: Math.ceil(layout.cssContentSize.height), scale: 1 });
  await evaluate(session, "document.querySelector('#harnesses').scrollIntoView({block:'start',behavior:'instant'})");
  await screenshot(session, "rewind-provider-cards.png");
  await cdp.send("Emulation.setDeviceMetricsOverride", { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, session);
  await evaluate(session, "scrollTo({top:0,left:0,behavior:'instant'})");
  await settled(session);
  check("mobile no page overflow", await evaluate(session, "document.documentElement.scrollWidth > document.documentElement.clientWidth"), false);
  check("mobile full duration fits", await evaluate(session, "(() => {const n=document.querySelector('#longest-session-duration');return n.scrollWidth<=n.clientWidth;})()"), true);
  check("mobile real sessions", await evaluate(session, "document.querySelector('#hero-session-count').textContent"), number(actual.totals.sessions));
  await screenshot(session, "rewind-mobile.png");
  const mobileLayout = await cdp.send("Page.getLayoutMetrics", {}, session);
  await screenshot(session, "rewind-mobile-full.png", { x: 0, y: 0, width: 390, height: Math.ceil(mobileLayout.cssContentSize.height), scale: 1 });
  await delay(1000);
  result.unhandled_targets = [...attachments.entries()].filter(([id]) => !knownTargets.has(id))
    .map(([targetId, info]) => ({ targetId, type: info.type, url: info.url }));
  check("no uninstrumented child targets", result.unhandled_targets.length, 0);
  check("no application JavaScript exceptions", result.exceptions.length, 0);
  check("zero product non-loopback requests", result.requests.filter((r) => r.scope === "product" && !r.loopback).length, 0);
  check("zero denied product requests", result.intercepted.filter((r) => r.scope === "product" && !r.allowed).length, 0);
  for (const pathname of ["/", "/styles.css", "/app.js", "/api/rewind"]) {
    check(`browser requested ${pathname}`, result.requests.some((r) => r.scope === "product" && new URL(r.url).pathname === pathname), true);
  }
  result.pass = true;
}

(async () => {
  try { await main(); }
  catch (error) { result.error = String(error.stack || error); process.exitCode = 1; }
  finally {
    stopping = true;
    if (cdp) { try { await cdp.send("Browser.close"); } catch {} cdp.ws.close(); }
    if (chrome && chrome.exitCode === null) {
      chrome.kill("SIGTERM");
      await Promise.race([new Promise((resolve) => chrome.once("exit", resolve)), delay(3000)]);
      if (chrome.exitCode === null && chrome.signalCode === null) {
        chrome.kill("SIGKILL");
        await Promise.race([new Promise((resolve) => chrome.once("exit", resolve)), delay(3000)]);
      }
    }
    if (chrome && chrome.exitCode === null && chrome.signalCode === null) {
      failures.push("Chrome termination could not be confirmed; isolated profile retained");
    } else {
      fs.rmSync(profile, { recursive: true, force: true });
    }
    // Shutdown can emit final network/target/exception events. Judge the complete
    // ledger after Chrome has stopped, not only the pre-close observation window.
    result.unhandled_targets = [...attachments.entries()].filter(([id]) => !knownTargets.has(id))
      .map(([targetId, info]) => ({ targetId, type: info.type, url: info.url }));
    if (result.pass) {
      try {
        check("final zero product non-loopback requests", result.requests.filter((r) => r.scope === "product" && !r.loopback).length, 0);
        check("final zero denied product requests", result.intercepted.filter((r) => r.scope === "product" && !r.allowed).length, 0);
        check("final no uninstrumented child targets", result.unhandled_targets.length, 0);
        check("final no application JavaScript exceptions", result.exceptions.length, 0);
      } catch (error) {
        result.pass = false; result.error = String(error.stack || error); process.exitCode = 1;
      }
    }
    result.finished_at = new Date().toISOString();
    result.observer_failures = failures;
    if (failures.length) { result.pass = false; process.exitCode = 1; }
    save("browser-result.json", JSON.stringify(result, null, 2) + "\n");
    console.log(JSON.stringify({ pass: result.pass, assertions_passed: result.assertions.length,
      product_non_loopback_requests: result.requests.filter((r) => r.scope === "product" && !r.loopback).length,
      screenshots: result.screenshots.length, error: result.error || null }, null, 2));
  }
})();
