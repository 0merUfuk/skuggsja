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
if (args.revision) assert.equal(args.revision, "true", "--revision accepts true");
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
  chrome_sha256: hash(fs.readFileSync(args.chrome)), verifier_sha256: hash(fs.readFileSync(__filename)), assertions: [], requests: [],
  intercepted: [], exceptions: [], unhandled_targets: [], browser_internal_targets: [], screenshots: [], pass: false };
let chrome;
let cdp;
let stopping = false;
const activeSessions = new Map();
const attachments = new Map();
const knownTargets = new Set();
const failures = [];
const syntheticResponses = new Map();
const heldSyntheticRequests = new Map();

class CDP {
  constructor(ws) {
    this.ws = ws; this.next = 1; this.pending = new Map(); this.handlers = []; this.inFlightHandlers = new Set();
    ws.addEventListener("message", (event) => {
      const message = JSON.parse(String(event.data));
      if (message.id) {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        this.pending.delete(message.id); clearTimeout(pending.timer);
        message.error ? pending.reject(new Error(JSON.stringify(message.error))) : pending.resolve(message.result);
      } else {
        for (const handle of this.handlers) {
          const handling = Promise.resolve().then(() => handle(message))
            .catch((error) => failures.push(String(error)))
            .finally(() => this.inFlightHandlers.delete(handling));
          this.inFlightHandlers.add(handling);
        }
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

// Child targets that cannot carry product traffic are recorded, not counted as
// product scope violations: a component extension or DevTools page owns a
// chrome://, chrome-extension:// or devtools:// origin. Every http(s), blob:,
// data: and about: child target still fails the product scope check.
function browserInternalTarget(target) {
  return /^(chrome|chrome-extension|chrome-untrusted|chrome-search|devtools|chrome-error):/.test(target.url || "");
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
// A single capture that materialises the whole report at 2x closes the DevTools
// socket mid-capture on this Chrome build (reproduced on the previous report
// layout as well, so it is not a page property). The identical 1:1 device
// pixels are captured in vertical slices instead: nothing is resampled, no
// region is skipped, and each slice stays inside what the software rasterizer
// returns. Fixed elements are painted once, at their viewport position.
const CAPTURE_DEVICE_BUDGET = 12e6;
const CAPTURE_DEVICE_HEIGHT = 8e3;
async function fullPageScreenshot(session, name, width, height, scale = 1) {
  const sliceHeight = Math.max(1, Math.min(Math.floor(CAPTURE_DEVICE_BUDGET / (width * scale)),
    Math.floor(CAPTURE_DEVICE_HEIGHT / scale)));
  const slices = Math.ceil(height / sliceHeight);
  if (slices <= 1) {
    await screenshot(session, name, { x: 0, y: 0, width, height, scale });
    return;
  }
  for (let index = 0; index < slices; index++) {
    const y = index * sliceHeight;
    await screenshot(session, name.replace(/\.png$/, `-${String(index + 1).padStart(2, "0")}.png`),
      { x: 0, y, width, height: Math.min(sliceHeight, height - y), scale });
  }
}
async function settled(session) {
  await evaluate(session, "document.fonts.ready.then(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))");
  await delay(500);
}

// Exercise disclosure state, hit targets and return navigation through browser
// input. Reading hidden rows from the DOM does not establish a usable Show more.
async function disclosureChecks(session) {
  result.disclosure_checks = [];
  for (const width of [1280, 1440, 1728, 1920, 320, 390]) {
    const mobile = width < 600;
    await cdp.send("Emulation.setDeviceMetricsOverride", { width, height: mobile ? 844 : 1000, deviceScaleFactor: 2, mobile }, session);
    await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: mobile, maxTouchPoints: 1 }, session);
    await cdp.send("Page.navigate", { url: args.url }, session);
    await until(() => evaluate(session, "Boolean(document.querySelector('#rewind')&&!document.querySelector('#rewind').hidden)"), "fresh disclosure Rewind");
    await settled(session);
    async function press(selector, trailingEdge = false) {
      const point = await evaluate(session, `(() => {const e=document.querySelector(${JSON.stringify(selector)});e.scrollIntoView({block:'center',behavior:'instant'});const b=e.getBoundingClientRect();return{x:${trailingEdge ? "b.right-12" : "b.x+b.width/2"},y:b.y+b.height/2};})()`);
      if (mobile) {
        await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [point] }, session);
        await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] }, session);
      } else {
        await cdp.send("Input.dispatchMouseEvent", { type: "mousePressed", ...point, button: "left", clickCount: 1 }, session);
        await cdp.send("Input.dispatchMouseEvent", { type: "mouseReleased", ...point, button: "left", clickCount: 1 }, session);
      }
      await settled(session);
    }
    async function keyboard(selector, key, code, virtualKeyCode) {
      await evaluate(session, `document.querySelector(${JSON.stringify(selector)}).focus()`);
      await cdp.send("Input.dispatchKeyEvent", { type: "keyDown", key, code, text: key === "Enter" ? "\r" : " ", unmodifiedText: key === "Enter" ? "\r" : " ", windowsVirtualKeyCode: virtualKeyCode }, session);
      await cdp.send("Input.dispatchKeyEvent", { type: "keyUp", key, code, windowsVirtualKeyCode: virtualKeyCode }, session);
      await settled(session);
    }
    const groupCount = await evaluate(session, "document.querySelectorAll('.model-provider-index').length");
    for (let i = 0; i < groupCount; i++) {
      const selector = `.model-provider-index:nth-child(${i + 1})`;
      if (!await evaluate(session, `document.querySelector(${JSON.stringify(selector)}).open`)) await press(selector + " > summary");
    }
    const indices = await evaluate(session, "[...document.querySelectorAll('.more-index')].map((e,i)=>{e.dataset.qaDisclosure=String(i);return i;})");
    for (const index of indices) {
      const selector = `.more-index[data-qa-disclosure="${index}"]`;
      const summary = selector + " > summary";
      const read = `(() => {const d=document.querySelector(${JSON.stringify(selector)}),s=d.querySelector('summary'),list=d.querySelector('ol'),b=d.getBoundingClientRect(),sb=s.getBoundingClientRect(),lb=list.getBoundingClientRect();return {open:d.open,label:s.textContent,summaryWidth:sb.width,summaryHeight:sb.height,width:b.width,height:b.height,listHeight:lb.height,rows:list.children.length,hasBottomControl:Boolean(d.querySelector('.more-index__collapse')),documentHeight:document.documentElement.scrollHeight,scrollY,summaryTop:sb.top,summaryBottom:sb.bottom,focused:document.activeElement===s,rowsPainted:[...list.children].every(e=>e.checkVisibility()),listContained:lb.left>=b.left-1&&lb.right<=b.right+1&&lb.bottom<=b.bottom+1,rowsSeparate:[...list.children].every((e,i,a)=>!i||e.getBoundingClientRect().top>=a[i-1].getBoundingClientRect().bottom-1)};})()`;
      await evaluate(session, `document.querySelector(${JSON.stringify(summary)}).scrollIntoView({block:'center',behavior:'instant'})`);
      await settled(session);
      const before = await evaluate(session, read);
      check(`${width} disclosure ${index} starts closed`, before.open, false);
      check(`${width} disclosure ${index} names exact hidden count`, before.label, `Show ${new Intl.NumberFormat("en-US").format(before.rows)} more`);
      check(`${width} disclosure ${index} full-width 44px target`, before.summaryWidth >= before.width - 2 && before.summaryHeight >= 44, true);
      await press(summary, true);
      const expanded = await evaluate(session, read);
      check(`${width} disclosure ${index} trailing-edge ${mobile ? "tap" : "click"} opens`, expanded.open, true);
      check(`${width} disclosure ${index} offers collapse`, expanded.label, "Show fewer");
      check(`${width} disclosure ${index} repeats collapse only for a long tail`, expanded.hasBottomControl, before.rows > 10);
      check(`${width} disclosure ${index} actually grows for its rows`, expanded.height >= before.height + expanded.listHeight + (expanded.hasBottomControl ? 44 : 0), true);
      check(`${width} disclosure ${index} rows painted and contained`, expanded.rowsPainted && expanded.listContained && expanded.rowsSeparate, true);
      check(`${width} disclosure ${index} opening keeps summary stationary`, Math.abs(expanded.summaryTop - before.summaryTop) <= 1, true);
      await screenshot(session, `rewind-${width}-disclosure-${index}-expanded.png`);
      if (expanded.hasBottomControl) {
        await evaluate(session, `document.querySelector(${JSON.stringify(selector + " > .more-index__collapse")}).scrollIntoView({block:'center',behavior:'instant'})`);
        await screenshot(session, `rewind-${width}-disclosure-${index}-end.png`);
      }
      await press(expanded.hasBottomControl ? selector + " > .more-index__collapse" : summary, true);
      const collapsed = await evaluate(session, read);
      check(`${width} disclosure ${index} ${expanded.hasBottomControl ? "bottom control" : "summary"} closes`, collapsed.open, false);
      check(`${width} disclosure ${index} collapsed label restored`, collapsed.label, before.label);
      check(`${width} disclosure ${index} closed height restored`, collapsed.height, before.height);
      check(`${width} disclosure ${index} document height restored`, collapsed.documentHeight, before.documentHeight);
      check(`${width} disclosure ${index} focus returns to visible summary`, collapsed.focused && collapsed.summaryTop >= -1 && collapsed.summaryBottom <= (mobile ? 844 : 1000) + 1, true);
      await keyboard(summary, "Enter", "Enter", 13);
      check(`${width} disclosure ${index} Enter expands`, await evaluate(session, read).then(r => r.open && r.label === "Show fewer"), true);
      await keyboard(summary, " ", "Space", 32);
      check(`${width} disclosure ${index} Space collapses`, await evaluate(session, read).then(r => !r.open && r.label === before.label), true);
      check(`${width} disclosure ${index} keyboard focus is visible`, await evaluate(session, `(() => {const e=document.querySelector(${JSON.stringify(summary)}),s=getComputedStyle(e);return e.matches(':focus-visible')&&s.outlineStyle!=='none'&&parseFloat(s.outlineWidth)>=2;})()`), true);
      result.disclosure_checks.push({ width, index, input: mobile ? "touch and keyboard" : "mouse and keyboard", before, expanded, collapsed });
    }
    const projects = await evaluate(session, "[...document.querySelectorAll('#project-list .rank-row')].map(e=>({value:e.querySelector('meter').value,label:e.querySelector('.rank-row__value').textContent}))");
    check(`${width} project rows retain exact session counts`, projects.map(p=>p.value), expected.projects.map(p=>p.sessions).sort((a,b)=>b-a));
    check(`${width} project rows label singular and plural sessions`, projects.every(p=>p.label===new Intl.NumberFormat('en-US').format(p.value)+(p.value===1?' session':' sessions')), true);
    const otherDetails = await evaluate(session, "[...document.querySelectorAll('details:not(.more-index)')].map((e,i)=>{e.dataset.qaDetails=String(i);return {index:i,open:e.open};})");
    for (const detail of otherDetails) {
      const selector = `details[data-qa-details="${detail.index}"]`;
      await press(selector + " > summary", true);
      check(`${width} native disclosure ${detail.index} toggles`, await evaluate(session, `document.querySelector(${JSON.stringify(selector)}).open`), !detail.open);
      await keyboard(selector + " > summary", "Enter", "Enter", 13);
      check(`${width} native disclosure ${detail.index} keyboard restores`, await evaluate(session, `document.querySelector(${JSON.stringify(selector)}).open`), detail.open);
    }
    const links = await evaluate(session, "[...document.querySelectorAll('.folio-nav a')].map(e=>e.getAttribute('href'))");
    for (const href of links) {
      await press(`.folio-nav a[href="${href}"]`);
      const destination = await evaluate(session, `({hash:location.hash,top:document.querySelector(${JSON.stringify(href)}).getBoundingClientRect().top})`);
      check(`${width} navigation ${href} updates hash`, destination.hash, href);
      check(`${width} navigation ${href} exposes heading`, destination.top >= -1 && destination.top < (mobile ? 844 : 1000) - 40, true);
    }
    await press(".wordmark");
    check(`${width} wordmark returns to page top`, await evaluate(session, "scrollY"), 0);
    check(`${width} disclosure interactions create no horizontal overflow`, await evaluate(session, "document.documentElement.scrollWidth<=innerWidth"), true);
  }
}

// Verification only: inspect actual computed geometry at CSS-pixel widths and
// DPR 2. The retained aggregate stays unchanged; the temporary hidden probe only
// resolves custom-property lengths and is removed before taking screenshots.
async function responsiveMatrix(session) {
  result.responsive_matrix = [];
  for (const width of (args.revision ? [1280, 1440, 1728, 1920, 320, 390] : [1280, 1440, 1728, 1920])) {
    await cdp.send("Emulation.setDeviceMetricsOverride", { width, height: width < 600 ? 844 : 1000, deviceScaleFactor: 2, mobile: width < 600 }, session);
    await evaluate(session, "scrollTo({top:0,left:0,behavior:'instant'})");
    await settled(session);
    const measurement = await evaluate(session, `(() => {
      const round = n => Math.round(n * 1000) / 1000;
      const rect = r => ({x:round(r.x),y:round(r.y),width:round(r.width),height:round(r.height),right:round(r.right),bottom:round(r.bottom)});
      const label = e => e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + [...e.classList].map(c => '.' + c).join('');
      const visible = e => {
        if (!e.getClientRects().length || getComputedStyle(e).visibility === 'hidden' || e.closest('.visually-hidden,[hidden]')) return false;
        // Chromium can expose layout rectangles for descendants of a closed
        // details element. Only its summary is actually presented to the user.
        for(let p=e.parentElement;p;p=p.parentElement) {
          if(p.tagName==='DETAILS'&&!p.open&&!p.querySelector(':scope > summary')?.contains(e)) return false;
        }
        return true;
      };
      const rootStyle = getComputedStyle(document.documentElement);
      const tokens = Object.fromEntries([...rootStyle].filter(k => k.startsWith('--')).map(k => [k,rootStyle.getPropertyValue(k).trim()]));
      const probe = document.createElement('span');
      Object.assign(probe.style,{position:'absolute',visibility:'hidden',padding:'0',border:'0',height:'0',display:'block'});
      document.body.append(probe);
      const resolvedTokens = {};
      for (const [key,value] of Object.entries(tokens)) {
        if (!/^(--text|--space|--page-gutter|--content-max|--measure|--font-size)/.test(key)) continue;
        probe.style.width = 'var(' + key + ')';
        resolvedTokens[key] = {declared:value,pixels:getComputedStyle(probe).width};
      }
      probe.remove();
      const selectors = ['html','body','main','.masthead','.hero','.hero h1','.hero-total__number','.hero__narrative','.chapter','.chapter-heading h2','.chapter-number','.proof-strip > div','.proof-strip dd','.provider-entry','.provider-entry summary','.provider-body','.provider-facts','.provider-facts dd','.provider-session-count','.folio-facts','.folio-fact__primary','.prompt-lead strong','.longest-session','.index-heading h3','.model-index','.model-bubbles','.usage-bubbles','.coverage-notice'];
      const samples = Object.fromEntries(selectors.map(selector => {
        const e = [...document.querySelectorAll(selector)].find(visible);
        if (!e) return [selector,null];
        const s = getComputedStyle(e);
        return [selector,{element:label(e),rect:rect(e.getBoundingClientRect()),fontFamily:s.fontFamily,fontSize:s.fontSize,lineHeight:s.lineHeight,fontWeight:s.fontWeight,maxWidth:s.maxWidth,width:s.width,padding:s.padding,margin:s.margin,gap:s.gap,minHeight:s.minHeight,letterSpacing:s.letterSpacing}];
      }));
      const typeScale = [...new Map([...document.querySelectorAll('body *')].filter(e=>e instanceof HTMLElement && visible(e)).map(e=>{const s=getComputedStyle(e);return [s.fontSize+' / '+s.lineHeight,{fontSize:s.fontSize,lineHeight:s.lineHeight,example:label(e)}]})).values()].sort((a,b)=>parseFloat(a.fontSize)-parseFloat(b.fontSize));
      const overflows = [];
      for (const e of document.querySelectorAll('body *')) {
        if (!(e instanceof HTMLElement) || !visible(e) || !e.parentElement || e.classList.contains('skip-link')) continue;
        const p = e.parentElement, s=getComputedStyle(e), ps=getComputedStyle(p), b=e.getBoundingClientRect(), pb=p.getBoundingClientRect();
        if (!b.width || !b.height || ps.display==='contents') continue;
        const content = {left:pb.left+parseFloat(ps.borderLeftWidth)+parseFloat(ps.paddingLeft),right:pb.right-parseFloat(ps.borderRightWidth)-parseFloat(ps.paddingRight),top:pb.top+parseFloat(ps.borderTopWidth)+parseFloat(ps.paddingTop),bottom:pb.bottom-parseFloat(ps.borderBottomWidth)-parseFloat(ps.paddingBottom)};
        const outsideX = b.left < content.left-1 || b.right > content.right+1;
        const outsideY = b.top < content.top-1 || b.bottom > content.bottom+1;
        if (!outsideX && !outsideY) continue;
        let scrollContainer = null;
        for(let a=p;a && a!==document.body;a=a.parentElement){const as=getComputedStyle(a);if(/auto|scroll/.test(as.overflowX)){scrollContainer=label(a);break;}}
        overflows.push({element:label(e),parent:label(p),rect:rect(b),parentContent:Object.fromEntries(Object.entries(content).map(([k,v])=>[k,round(v)])),horizontal:outsideX,vertical:outsideY,display:s.display,position:s.position,transform:s.transform,scrollContainer,inlineFormatting:s.display==='inline'||ps.display==='inline'});
      }
      const blockOverflows = overflows.filter(o=>!o.scrollContainer&&!o.inlineFormatting&&o.position!=='fixed');
      const keyValues = [...document.querySelectorAll('#hero-session-count,#proof-prompts,#proof-projects,#proof-days,#longest-session-duration,.provider-session-count,.provider-facts dd')].filter(visible).map(e=>({element:label(e),text:e.textContent,width:e.clientWidth,scrollWidth:e.scrollWidth,height:e.clientHeight,scrollHeight:e.scrollHeight}));
      return {width:innerWidth,height:innerHeight,dpr:devicePixelRatio,rootFontSize:rootStyle.fontSize,bodyFontSize:getComputedStyle(document.body).fontSize,bodyLineHeight:getComputedStyle(document.body).lineHeight,page:{clientWidth:document.documentElement.clientWidth,scrollWidth:document.documentElement.scrollWidth,height:document.documentElement.scrollHeight},tokens:resolvedTokens,samples,typeScale,parentContentOverflows:overflows,uncontainedBlockOverflows:blockOverflows,uncontainedHorizontalOverflows:blockOverflows.filter(o=>o.horizontal),keyValues};
    })()`);
    result.responsive_matrix.push(measurement);
    if (args.revision) {
      measurement.chartText = [];
      for (const metric of ["sessions", "prompts"]) {
        const point=await evaluate(session, `(()=>{const e=document.querySelector('.usage-metric[data-metric="${metric}"]');e.scrollIntoView({block:'center',behavior:'instant'});const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2};})()`);
        await cdp.send("Input.dispatchMouseEvent", {type:"mousePressed",...point,button:"left",clickCount:1}, session);
        await cdp.send("Input.dispatchMouseEvent", {type:"mouseReleased",...point,button:"left",clickCount:1}, session);
        const labels=await evaluate(session, `(()=>[...document.querySelectorAll('.usage-bubble')].map(g=>{const c=g.querySelector('circle'),t=g.querySelector('text'),b=t.getBBox(),m=t.getScreenCTM(),x=Number(c.getAttribute('cx')),y=Number(c.getAttribute('cy')),r=Number(c.getAttribute('r'));return {harness:g.dataset.harness,text:t.textContent,cssFontSize:parseFloat(getComputedStyle(t).fontSize)*Math.hypot(m.a,m.b),insideCircle:[[b.x,b.y],[b.x+b.width,b.y],[b.x,b.y+b.height],[b.x+b.width,b.y+b.height]].every(([a,b])=>Math.hypot(a-x,b-y)<=r+0.001)};}))()`);
        const layout=await evaluate(session, "document.querySelector('#usage-chart').dataset.layout");
        measurement.chartText.push({metric,layout,labels});
      }
      await evaluate(session, "document.querySelector('.usage-metric[data-metric=\"sessions\"]').click();scrollTo({top:0,left:0,behavior:'instant'})");
      await settled(session);
    }
    check(`${width}px DPR 2 applied`, measurement.dpr, 2);
    check(`${width}px root font size`, measurement.rootFontSize, "16px");
    check(`${width}px no page overflow`, measurement.page.scrollWidth <= measurement.page.clientWidth, true);
    await screenshot(session, `rewind-${width}-dpr2.png`);
    const full = await cdp.send("Page.getLayoutMetrics", {}, session);
    await fullPageScreenshot(session, `rewind-${width}-dpr2-full.png`, width, Math.ceil(full.cssContentSize.height));
  }
  save("layout-measurements.json", JSON.stringify(result.responsive_matrix, null, 2) + "\n");
  if (args.revision) {
    for (const layout of result.responsive_matrix) {
      check(`${layout.width}px no uncontained block overflow`, layout.uncontainedBlockOverflows, []);
      check(`${layout.width}px key values are fully visible`, layout.keyValues.filter(v => v.scrollWidth > v.width + 1 || v.scrollHeight > v.height + 1), []);
      for(const {metric,layout:chartLayout,labels} of layout.chartText) {
        if(chartLayout==='packed') {
          check(`${layout.width}px ${metric} circle labels are at least 12 CSS px`, labels.length>0&&labels.every(t=>t.cssFontSize>=12), true);
          check(`${layout.width}px ${metric} circle labels fit their circles`, labels.every(t=>t.insideCircle), true);
        } else {
          check(`${layout.width}px ${metric} sparse layout is bars`,chartLayout,'bars');
          check(`${layout.width}px ${metric} bars have no fabricated circle labels`,labels.length,0);
        }
      }
    }
  }
}

async function usageChart(session, report, options = {}) {
  const chartCheck = (name, actual, wanted) => check((options.prefix || "") + name, actual, wanted);
  const chartResults = [];
  const available = v => Number.isSafeInteger(v) && v >= 0;
  const number = v => available(v) ? new Intl.NumberFormat("en-US").format(v) : "Not available";
  const labelledCount = (value, metric) => available(value) ? number(value) + " " +
    (metric === "sessions" ? (value === 1 ? "session" : "sessions") : (value === 1 ? "prompt" : "prompts")) : "Not available";
  const center = async selector => evaluate(session, `(() => {const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw new Error('Missing chart control');e.scrollIntoView({block:'center',behavior:'instant'});const b=e.getBoundingClientRect();return {x:b.x+b.width/2,y:b.y+b.height/2};})()`);
  const click = async selector => {
    const point = await center(selector);
    await cdp.send("Input.dispatchMouseEvent", {type:"mousePressed",...point,button:"left",clickCount:1}, session);
    await cdp.send("Input.dispatchMouseEvent", {type:"mouseReleased",...point,button:"left",clickCount:1}, session);
    await settled(session);
  };
  if (!options.synthetic) result.usage_chart = chartResults;
  chartCheck("usage chart present", await evaluate(session, "Boolean(document.querySelector('#usage-chart'))"), true);
  const weekdays = await evaluate(session, "[...document.querySelectorAll('#weekday-list meter')].map(e=>({value:e.value,label:e.getAttribute('aria-label')}))");
  const weekdayNames = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
  chartCheck("weekday meters retain exact counts and singular or plural names", weekdays, weekdayNames.map((day,index) => {
    const value = report.rhythm?.weekdays?.[index] || 0;
    return {value,label:day+": "+labelledCount(value,"sessions")};
  }));
  for (const metric of ["sessions", "prompts"]) {
    await click(`.usage-metric[data-metric="${metric}"]`);
    const actual = await evaluate(session, `(() => {
      const root=document.querySelector('#usage-chart'),svg=root.querySelector('.usage-svg');
      return {layout:root.dataset.layout,metric:root.dataset.metric,pressed:[...root.querySelectorAll('.usage-metric[aria-pressed="true"]')].map(e=>e.dataset.metric),viewBox:svg?svg.getAttribute('viewBox').split(/\\s+/).map(Number):null,
        bubbles:[...root.querySelectorAll('.usage-bubble')].map(e=>{const c=e.querySelector('circle');return {harness:e.dataset.harness,count:Number(e.dataset.count),role:e.getAttribute('role'),tabindex:e.getAttribute('tabindex'),label:e.getAttribute('aria-label'),cx:Number(c.getAttribute('cx')),cy:Number(c.getAttribute('cy')),r:Number(c.getAttribute('r')),fill:getComputedStyle(c).fill};}),
        keys:[...root.querySelectorAll('.usage-key-button')].map(e=>({harness:e.dataset.harness,name:e.querySelector('.usage-key-name').textContent,value:e.querySelector('.usage-key-value').textContent,coverage:e.querySelector('.usage-key-coverage').textContent,label:e.getAttribute('aria-label')})),bars:[...root.querySelectorAll('.usage-bar')].map(e=>({value:e.value,max:e.max})),detailLive:root.querySelector('.usage-detail').getAttribute('aria-live')};
    })()`);
    chartResults.push(actual);
    const positive = report.providers.filter(p => Number.isSafeInteger(p[metric]) && p[metric] > 0);
    chartCheck(`${metric} selection is exclusive`, actual.pressed, [metric]);
    chartCheck(`${metric} chart metric`, actual.metric, metric);
    chartCheck(`${metric} chart retains every provider`, actual.keys.length, report.providers.length);
    for (const provider of report.providers) {
      const key = actual.keys.find(k => k.harness === provider.id) || actual.keys.find(k => k.name === provider.name);
      chartCheck(`${metric} ${provider.id} key exact value`, key?.value, number(provider[metric]));
      chartCheck(`${metric} ${provider.id} key keeps coverage`, key?.coverage.includes(provider.coverage?.status || "Coverage assessment unavailable"), true);
      chartCheck(`${metric} ${provider.id} key names the exact unit`, key?.label, provider.name+": "+labelledCount(provider[metric],metric)+". "+(provider.coverage?.status||"Coverage assessment unavailable"));
    }
    chartCheck(`${metric} detail is accessible`, actual.detailLive, "polite");
    chartCheck(`${metric} chart selects a supported presentation`,["packed","bars"].includes(actual.layout),true);
    if (actual.layout === "packed") {
      chartCheck(`${metric} packed layout has enough positive entities`, positive.length>=3&&positive.length<=24,true);
      chartCheck(`${metric} bubbles have exact provider counts`, actual.bubbles.map(b=>({harness:b.harness,count:b.count})).sort((a,b)=>a.harness.localeCompare(b.harness)), positive.map(p=>({harness:p.id,count:p[metric]})).sort((a,b)=>a.harness.localeCompare(b.harness)));
      const scales=actual.bubbles.map(b=>b.r*b.r/b.count);
      chartCheck(`${metric} bubble area is proportional to recorded count`, scales.every(s=>Math.abs(s/scales[0]-1)<0.00001), true);
      const [x,y,w,h]=actual.viewBox;
      chartCheck(`${metric} every circle stays inside the viewBox`, actual.bubbles.every(b=>b.cx-b.r>=x-0.001&&b.cy-b.r>=y-0.001&&b.cx+b.r<=x+w+0.001&&b.cy+b.r<=y+h+0.001), true);
      chartCheck(`${metric} circles do not overlap`, actual.bubbles.every((a,i)=>actual.bubbles.slice(i+1).every(b=>Math.hypot(a.cx-b.cx,a.cy-b.cy)>=a.r+b.r-0.001)), true);
      chartCheck(`${metric} harness colors are distinct`, new Set(actual.bubbles.map(b=>b.fill)).size, positive.length);
      chartCheck(`${metric} bubbles expose button role and keyboard focus`, actual.bubbles.every(b=>b.role==="button"&&b.tabindex==="0"), true);
      chartCheck(`${metric} bubbles name the exact singular or plural unit`, actual.bubbles.map(b=>b.label), actual.bubbles.map(b=>{
        const provider=positive.find(p=>p.id===b.harness);
        return provider.name+": "+labelledCount(provider[metric],metric)+". "+(provider.coverage?.status||"Coverage assessment unavailable");
      }));
    } else {
      const values=report.providers.filter(p=>available(p[metric])).map(p=>p[metric]).sort((a,b)=>b-a);
      chartCheck(`${metric} bars retain all measured counts including zero`,actual.bars.map(b=>b.value),values);
      chartCheck(`${metric} bars keep the actual maximum`,actual.bars.every(b=>b.max===Math.max(1,...values)),true);
      chartCheck(`${metric} bars do not invent circle geometry`,actual.bubbles.length,0);
    }
    for (const provider of (actual.layout==='packed'?positive:report.providers)) {
      const index=actual.keys.findIndex(k=>k.name===provider.name);
      const selector=actual.layout==='packed'?`.usage-bubble[data-harness="${provider.id}"]`:`.usage-key-row:nth-child(${index+1}) .usage-key-button`;
      const point=await center(selector);
      await cdp.send("Input.dispatchMouseEvent", {type:"mouseMoved",...point}, session);
      const detail=await evaluate(session, "document.querySelector('.usage-detail').textContent");
      chartCheck(`${metric} ${provider.id} hover has real detail`, detail.includes(provider.name)&&detail.includes(number(provider[metric])), true);
      chartCheck(`${metric} ${provider.id} selected detail names the exact unit`,detail,provider.name+" · "+labelledCount(provider[metric],metric)+" · "+(provider.coverage?.status||"Coverage assessment unavailable")+".");
    }
  }
  const promptLayout=chartResults.at(-1).layout;
  const promptTargets=promptLayout==='packed'?report.providers.filter(p=>p.prompts>0):report.providers;
  const first=promptTargets[0],last=promptTargets.at(-1);
  if(first) {
  const targetSelector=provider=>promptLayout==='packed'?`.usage-bubble[data-harness="${provider.id}"]`:`.usage-key-row:nth-child(${chartResults.at(-1).keys.findIndex(k=>k.name===provider.name)+1}) .usage-key-button`;
  const focusSelector=targetSelector(first);
  await evaluate(session, `document.querySelector(${JSON.stringify(focusSelector)}).focus()`);
  for (const key of ["Enter"," "]) {
    await cdp.send("Input.dispatchMouseEvent", {type:"mouseMoved",x:0,y:0}, session);
    const other=await center(targetSelector(last));
    await cdp.send("Input.dispatchMouseEvent", {type:"mouseMoved",...other}, session);
    chartCheck(`${key===" "?"Space":key} precondition uses another harness detail`, (await evaluate(session, "document.querySelector('.usage-detail').textContent")).includes(last.name), true);
    // Include the character phase: native HTML buttons activate on the complete
    // keyboard sequence, whereas the SVG button handles keydown directly.
    await cdp.send("Input.dispatchKeyEvent", {type:"keyDown",key,text:key==="Enter"?"\r":" ",unmodifiedText:key==="Enter"?"\r":" ",code:key==="Enter"?"Enter":"Space",windowsVirtualKeyCode:key==="Enter"?13:32}, session);
    await cdp.send("Input.dispatchKeyEvent", {type:"keyUp",key,code:key==="Enter"?"Enter":"Space",windowsVirtualKeyCode:key==="Enter"?13:32}, session);
    const detail=await evaluate(session, "document.querySelector('.usage-detail').textContent");
    result.chart_keyboard ||= [];
    result.chart_keyboard.push({scenario:options.prefix||"retained real report",key,detail,expected:first.name,focus:await evaluate(session,"({tag:document.activeElement.tagName,harness:document.activeElement.dataset.harness,text:document.activeElement.textContent})")});
    chartCheck(`${key===" "?"Space":key} opens exact focused bubble detail`, detail.includes(first.name)&&detail.includes(number(first.prompts)), true);
  }
  await cdp.send("Emulation.setDeviceMetricsOverride", {width:390,height:844,deviceScaleFactor:2,mobile:true}, session);
  await cdp.send("Emulation.setTouchEmulationEnabled", {enabled:true,maxTouchPoints:1}, session);
  const point=await center(`.usage-key-row:nth-child(${chartResults.at(-1).keys.findIndex(k=>k.name===last.name)+1}) .usage-key-button`);
  await cdp.send("Input.dispatchTouchEvent", {type:"touchStart",touchPoints:[{...point,radiusX:1,radiusY:1,force:1}]}, session);
  await cdp.send("Input.dispatchTouchEvent", {type:"touchEnd",touchPoints:[]}, session);
  const tapDetail=await evaluate(session, "document.querySelector('.usage-detail').textContent");
  chartCheck("mobile tap reveals exact provider detail", tapDetail.includes(last.name)&&tapDetail.includes(number(last.prompts)), true);
  await cdp.send("Emulation.setTouchEmulationEnabled", {enabled:false}, session);
  await cdp.send("Emulation.setDeviceMetricsOverride", {width:1440,height:1000,deviceScaleFactor:1,mobile:false}, session);
  } else {
    chartCheck("empty harness chart has no interactive keys",await evaluate(session,"document.querySelectorAll('.usage-key-button').length"),0);
  }
  await click('.usage-metric[data-metric="sessions"]');
  const groups=await evaluate(session, "[...document.querySelectorAll('.model-provider-index')].map(e=>({harness:e.dataset.harness,open:e.open}))");
  chartCheck("models retain one disclosure for each recorded harness", groups.map(g=>g.harness), [...new Set(report.models.map(m=>m.harness))].sort());
  chartCheck("initial model disclosure hierarchy", groups.map(g=>g.open), groups.map((_,i)=>i===0));
  for(const group of groups) {
    const selector=`.model-provider-index[data-harness="${group.harness}"]`;
    if(!group.open) await click(`${selector} > summary`);
    chartCheck(`${group.harness} model disclosure opens`, await evaluate(session, `document.querySelector(${JSON.stringify(selector)}).open`), true);
    const moreSelector=`${selector} .more-index > summary`;
    const hasMore=await evaluate(session, `Boolean(document.querySelector(${JSON.stringify(moreSelector)}))`);
    if(hasMore) await click(moreSelector);
    const rows=await evaluate(session, `(()=>{const e=document.querySelector(${JSON.stringify(selector)});return [...e.querySelectorAll('.rank-row')].map(r=>({position:r.querySelector('.rank-row__position').textContent,value:r.querySelector('meter').value,max:r.querySelector('meter').max,text:r.querySelector('.rank-row__value').textContent,painted:r.checkVisibility()}));})()`);
    const values=report.models.filter(m=>m.harness===group.harness).map(m=>m.turns).sort((a,b)=>b-a);
    chartCheck(`${group.harness} model bars retain exact native values`, rows.map(r=>r.value), values);
    chartCheck(`${group.harness} model rank restarts at one`, rows[0]?.position, "01");
    chartCheck(`${group.harness} model bars keep harness-local scale`, rows.every(r=>r.max===Math.max(1,...values)), true);
    chartCheck(`${group.harness} expanded model values are visible and labelled`, rows.every(r=>r.painted&&r.text===number(r.value)+(r.value===1?" native event":" native events")), true);
    if(hasMore) await click(moreSelector);
    if(!group.open) await click(`${selector} > summary`);
  }
  await evaluate(session, "document.querySelector('#usage-chart').closest('section').scrollIntoView({block:'start',behavior:'instant'})");
  if (!options.synthetic) await screenshot(session, "rewind-usage-chart.png");
  await click('.usage-metric[data-metric="prompts"]');
  await evaluate(session, "document.querySelector('#usage-chart').closest('section').scrollIntoView({block:'start',behavior:'instant'})");
  if (!options.synthetic) await screenshot(session, "rewind-usage-chart-prompts.png");
  await click('.usage-metric[data-metric="sessions"]');
  await evaluate(session, "scrollTo({top:0,left:0,behavior:'instant'})");
  return chartResults;
}

async function fulfillSynthetic(session, requestId, response) {
  await cdp.send("Fetch.fulfillRequest", {requestId,responseCode:response.status || 200,
    responseHeaders:[{name:"Content-Type",value:"application/json"}],
    body:Buffer.from(JSON.stringify(response.body || {})).toString("base64")}, session);
}

// Separate page, explicitly synthetic API responses. The primary page and all
// Rewind screenshots retain the real report; labelled synthetic screenshots
// capture edge cases separately. All requests use the same outbound ledger.
async function syntheticPresentationChecks() {
  const page=await newPage("product"),session=page.sessionId;
  result.synthetic_scenarios=[];
  result.synthetic_viewports=[];
  async function mobileViewport() {
    await cdp.send("Emulation.setDeviceMetricsOverride", {width:390,height:844,deviceScaleFactor:2,mobile:true}, session);
    await cdp.send("Emulation.setTouchEmulationEnabled", {enabled:true,maxTouchPoints:1}, session);
  }
  async function verifyMobileViewport(scenario) {
    const viewport=await evaluate(session,"({width:innerWidth,height:innerHeight,dpr:devicePixelRatio,touchPoints:navigator.maxTouchPoints})");
    result.synthetic_viewports.push({scenario,...viewport});
    check(`synthetic ${scenario} uses the intended mobile viewport`,viewport,{width:390,height:844,dpr:2,touchPoints:1});
  }
  await cdp.send("Emulation.setEmulatedMedia", {features:[{name:"prefers-reduced-motion",value:"reduce"}]}, session);
  async function pointer(selector) {
    const point=await evaluate(session, `(()=>{const e=document.querySelector(${JSON.stringify(selector)});e.scrollIntoView({block:'center',behavior:'instant'});const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2};})()`);
    await cdp.send("Input.dispatchMouseEvent", {type:"mousePressed",...point,button:"left",clickCount:1}, session);
    await cdp.send("Input.dispatchMouseEvent", {type:"mouseReleased",...point,button:"left",clickCount:1}, session);
  }
  for(const scenario of [
    {name:"one entity",counts:[7]},
    {name:"two entities",counts:[7,3]},
    {name:"one recorded session",counts:[1],capture:true},
    {name:"three single-session harnesses",counts:[1,1,1],packed:true,capture:true},
    {name:"three-entity tiny minority",counts:[1000000,3,1]},
    {name:"40-entity long tail",counts:[120,...Array(39).fill(1)]},
    {name:"sessions without positive prompts",counts:[7,3,2,1],zeroPrompts:true}
  ]) {
    // usageChart restores its desktop viewport on exit. Each independent
    // scenario must explicitly re-establish mobile metrics and touch input.
    await mobileViewport();
    const counts=scenario.counts;
    const providers=counts.map((count,index)=>({id:["claude","codex","cursor","hermes"][index]||"synthetic-"+index,name:"Synthetic harness "+index,sessions:count,prompts:scenario.zeroPrompts?0:count,coverage:{status:"completeness unknown"}}));
    const weekdayCounts=counts.reduce((days,count,index)=>{days[index%7]+=count;return days;},Array(7).fill(0));
    const body={schema_version:3,totals:{sessions:counts.reduce((a,b)=>a+b,0)},providers,models:[],warnings:[],rhythm:{weekdays:weekdayCounts}};
    syntheticResponses.set(session,{body});
    await cdp.send("Page.navigate", {url:args.url}, session);
    await until(()=>evaluate(session,`document.querySelectorAll('#usage-chart .usage-key-button').length===${counts.length}`),"synthetic chart controls");
    await verifyMobileViewport(scenario.name);
    const actual=await evaluate(session,"(()=>{const r=document.querySelector('#usage-chart');return {circles:r.querySelectorAll('.usage-bubble').length,values:[...r.querySelectorAll('.usage-bar')].map(e=>e.value),names:[...r.querySelectorAll('.usage-key-name')].map(e=>e.textContent),controls:r.querySelectorAll('.usage-metric').length};})()");
    if(scenario.packed) {
      check(`synthetic ${scenario.name} packs every positive count`,actual.circles,counts.length);
      check(`synthetic ${scenario.name} does not also render bars`,actual.values,[]);
    } else if(!scenario.zeroPrompts) {
      check(`synthetic ${scenario.name} fallback has no invented circles`,actual.circles,0);
      check(`synthetic ${scenario.name} fallback retains exact bars`,actual.values,[...counts].sort((a,b)=>b-a));
    }
    check(`synthetic ${counts.length}-entity fallback retains every label`,actual.names.length,counts.length);
    check(`synthetic ${counts.length}-entity fallback retains two unit controls`,actual.controls,2);
    const chart=await usageChart(session,body,{synthetic:true,prefix:`synthetic ${scenario.name}: `});
    result.synthetic_scenarios.push({name:scenario.name,counts,zero_prompts:Boolean(scenario.zeroPrompts),chart,pass:true});
    if(scenario.capture) {
      await mobileViewport();
      await verifyMobileViewport(scenario.name+" label screenshots");
      for(const metric of ["sessions","prompts"]) {
        await pointer(`.usage-metric[data-metric="${metric}"]`);
        await evaluate(session,"document.querySelector('.usage-detail').scrollIntoView({block:'end',behavior:'instant'})");
        await settled(session);
        await screenshot(session,`synthetic-${scenario.name.replaceAll(' ','-')}-${metric}-390-dpr2.png`);
      }
    }
  }
  await mobileViewport();
  syntheticResponses.set(session,{hold:true});
  await cdp.send("Page.navigate", {url:args.url}, session);
  await until(()=>heldSyntheticRequests.get(session),"held synthetic local API request");
  await verifyMobileViewport("loading/error/empty/retry sequence");
  check("synthetic pending request shows loading",await evaluate(session,"!document.querySelector('#loading-state').hidden"),true);
  check("synthetic loading hides chapter navigation",await evaluate(session,"document.querySelector('.folio-nav').hidden"),true);
  check("synthetic loading retains read-only guarantee",await evaluate(session,"document.querySelector('.source-status').textContent.includes('skuggsja does not write to source paths')"),true);
  await fulfillSynthetic(session,heldSyntheticRequests.get(session),{status:503});
  heldSyntheticRequests.delete(session);
  await until(()=>evaluate(session,"!document.querySelector('#error-state').hidden"),"synthetic failure state");
  check("synthetic failure hides chapter navigation",await evaluate(session,"document.querySelector('.folio-nav').hidden"),true);
  check("synthetic initial failure does not steal focus",await evaluate(session,"document.activeElement===document.body"),true);
  syntheticResponses.set(session,{body:{schema_version:3,totals:{sessions:0},providers:[],warnings:[]}});
  await pointer('#retry-button');
  await until(()=>evaluate(session,"!document.querySelector('#empty-state').hidden"),"synthetic empty retry result");
  check("synthetic empty retry focuses its heading",await evaluate(session,"document.activeElement.id"),"empty-title");
  check("synthetic empty hides chapter navigation",await evaluate(session,"document.querySelector('.folio-nav').hidden"),true);
  syntheticResponses.set(session,{body:expected});
  await pointer('#empty-retry-button');
  await until(()=>evaluate(session,"!document.querySelector('#rewind').hidden"),"real retained report after synthetic retry");
  check("retry restores the real retained session count",await evaluate(session,"document.querySelector('#hero-session-count').textContent"),new Intl.NumberFormat('en-US').format(expected.totals.sessions));
  check("successful retry focuses its heading",await evaluate(session,"document.activeElement.id"),"hero-title");
  check("successful retry restores chapter navigation",await evaluate(session,"document.querySelector('.folio-nav').hidden"),false);
  check("reduced-motion browser preference is active",await evaluate(session,"matchMedia('(prefers-reduced-motion: reduce)').matches"),true);
  check("reduced-motion page exposes all chapters",await evaluate(session,"[...document.querySelectorAll('.chapter')].every(e=>getComputedStyle(e).opacity==='1')"),true);
  await evaluate(session,"document.querySelector('.skip-link').focus()");
  check("keyboard skip link is visible when focused",await evaluate(session,"(()=>{const r=document.querySelector('.skip-link').getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight;})()"),true);
  await cdp.send("Input.dispatchKeyEvent",{type:"keyDown",key:"Enter",code:"Enter",windowsVirtualKeyCode:13},session);
  await cdp.send("Input.dispatchKeyEvent",{type:"keyUp",key:"Enter",code:"Enter",windowsVirtualKeyCode:13},session);
  check("keyboard skip link reaches main content",await evaluate(session,"location.hash"),"#main-content");
  result.synthetic_scenarios.push({name:"loading/error/empty/retry/reduced-motion/keyboard",pass:true});
  syntheticResponses.delete(session);
  await cdp.send("Target.closeTarget",{targetId:page.targetId});
}

async function main() {
  const chromeArgs = ["--headless", "--lang=en-US", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
    `--user-data-dir=${profile}`, "--no-first-run", "--no-default-browser-check", "--disable-gpu",
    "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-extensions",
    // Component extensions ship with Chrome and are not removed by
    // --disable-extensions; this machine's "Google Network Speech" component
    // starts a background service worker inside any profile, including this
    // isolated one.
    "--disable-component-extensions-with-background-pages",
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
      const override=syntheticResponses.get(message.sessionId);
      if(allowed&&loopback(raw)&&new URL(raw).pathname==='/api/rewind'&&override) {
        if(override.hold) heldSyntheticRequests.set(message.sessionId,message.params.requestId);
        else await fulfillSynthetic(message.sessionId,message.params.requestId,override);
        return;
      }
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
  if (args.revision) await usageChart(session, actual);
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
  await responsiveMatrix(session);
  if(args.revision) await disclosureChecks(session);
  if(args.revision) await syntheticPresentationChecks();
  await delay(1000);
  const strayTargets = [...attachments.entries()].filter(([id]) => !knownTargets.has(id))
    .map(([targetId, info]) => ({ targetId, type: info.type, url: info.url }));
  result.browser_internal_targets = strayTargets.filter(browserInternalTarget);
  result.unhandled_targets = strayTargets.filter((target) => !browserInternalTarget(target));
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
    // Install the waiter before shutdown so an early close cannot be missed.
    const socketClosed = !cdp || cdp.ws.readyState === WebSocket.CLOSED ? Promise.resolve(true) :
      new Promise((resolve) => cdp.ws.addEventListener("close", () => resolve(true), { once: true }));
    if (cdp) { try { await cdp.send("Browser.close"); } catch {} }
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
    if (cdp) {
      if (cdp.ws.readyState !== WebSocket.CLOSED) cdp.ws.close();
      if (!await Promise.race([socketClosed, delay(3000, false)])) {
        failures.push("CDP socket closure could not be confirmed");
      }
      if (!await Promise.race([Promise.all([...cdp.inFlightHandlers]).then(() => true), delay(3000, false)])) {
        failures.push("CDP event handlers did not finish before ledger finalization");
      }
    }
    // Shutdown can emit final network/target/exception events. Judge the complete
    // ledger after Chrome has stopped, not only the pre-close observation window.
    const strayTargets = [...attachments.entries()].filter(([id]) => !knownTargets.has(id))
      .map(([targetId, info]) => ({ targetId, type: info.type, url: info.url }));
    result.browser_internal_targets = strayTargets.filter(browserInternalTarget);
    result.unhandled_targets = strayTargets.filter((target) => !browserInternalTarget(target));
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
