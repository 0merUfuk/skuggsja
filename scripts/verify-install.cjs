#!/usr/bin/env node
"use strict";

// Installed-binary smoke test. Every source is disposable synthetic data;
// this is not a live-source equality record or a package-manager lifecycle test.
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { createHash } = require("node:crypto");
const fs = require("node:fs/promises");
const { constants } = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");

const sourceOverrides = {
  SKUGGSJA_CLAUDE_PROJECTS: "claude/projects",
  SKUGGSJA_CLAUDE_HISTORY: "claude/history.jsonl",
  SKUGGSJA_CLAUDE_STATS: "claude/stats-cache.json",
  SKUGGSJA_CLAUDE_GLOBAL_STATE: "claude/global-state.json",
  SKUGGSJA_CLAUDE_DESKTOP_SESSIONS: "claude/desktop-sessions",
  SKUGGSJA_CLAUDE_CODE_SESSIONS: "claude/code-sessions",
  SKUGGSJA_CODEX_SESSIONS: "codex/sessions",
  SKUGGSJA_CODEX_ARCHIVED: "codex/archived",
  SKUGGSJA_CODEX_RECOVERY: "codex/recovery",
  SKUGGSJA_CODEX_HISTORY: "codex/history.jsonl",
  SKUGGSJA_CODEX_SESSION_INDEX: "codex/session_index.jsonl",
  SKUGGSJA_CODEX_EXTERNAL_IMPORTS: "codex/external-imports.json",
  SKUGGSJA_CODEX_STATE_DATABASE: "codex/state.sqlite",
  SKUGGSJA_CODEX_CATALOG_DATABASE: "codex/catalog.sqlite",
  SKUGGSJA_CODEX_THREAD_HISTORY_DATABASE: "codex/thread-history.sqlite",
  SKUGGSJA_HERMES_DATABASE: "hermes/state.db",
  SKUGGSJA_CURSOR_DATABASE: "cursor/state.vscdb",
};
const expectedCSP = "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'";
const children = new Set();
let checks = 0;
let taskRoot;
let commandEnv;
let commandCwd;
let commandName;
let interrupted = false;

function interrupt() {
  interrupted = true;
  for (const running of children) running.child.kill("SIGTERM");
}
process.on("SIGINT", interrupt);
process.on("SIGTERM", interrupt);

function check(name, action) {
  action();
  checks += 1;
  console.log(`PASS ${name}`);
}

function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

async function snapshot(directory) {
  const result = [];
  async function walk(current) {
    for (const entry of (await fs.readdir(current, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
      const file = path.join(current, entry.name);
      const relative = path.relative(directory, file);
      assert(!entry.isSymbolicLink(), "synthetic source contains an unexpected symlink");
      if (entry.isDirectory()) {
        result.push({ path: relative, directory: true });
        await walk(file);
      } else {
        assert(entry.isFile(), "synthetic source contains an unexpected special file");
        const bytes = await fs.readFile(file);
        result.push({ path: relative, bytes: bytes.length, sha256: digest(bytes) });
      }
    }
  }
  await walk(directory);
  return result;
}

function launch(args) {
  assert(!interrupted, "installation smoke was interrupted");
  const child = spawn(commandName, args, {
    cwd: commandCwd, env: commandEnv, shell: false,
    windowsHide: true, stdio: ["ignore", "pipe", "pipe"],
  });
  const running = { child, stdout: "", stderr: "", settled: false, error: null };
  children.add(running);
  for (const stream of ["stdout", "stderr"]) {
    child[stream].setEncoding("utf8");
    child[stream].on("data", (chunk) => {
      if (running[stream].length + chunk.length > 4 * 1024 * 1024) {
        running.error = new Error("installed command exceeded the output limit");
        child.kill("SIGTERM"); // Only the product process created immediately above.
      } else {
        running[stream] += chunk;
      }
    });
  }
  running.done = new Promise((resolve) => {
    child.on("error", (error) => { running.error = error; });
    child.on("close", (code, signal) => {
      running.settled = true;
      children.delete(running);
      resolve({ code, signal });
    });
  });
  return running;
}

async function waitForExit(running, timeout) {
  let timer;
  try {
    return await Promise.race([
      running.done,
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error("owned product child did not exit in time")), timeout); }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

async function stop(running) {
  if (running.settled) return running.done;
  running.child.kill("SIGTERM");
  try {
    return await waitForExit(running, 6000);
  } catch (error) {
    // Failure cleanup is restricted to this exact child object, never a name,
    // process group, discovered PID, or an owning agent/harness process.
    if (!running.settled) running.child.kill("SIGKILL");
    await waitForExit(running, 6000);
    throw error;
  }
}

async function run(args) {
  const running = launch(args);
  try {
    const result = await waitForExit(running, 30000);
    if (running.error) throw running.error;
    assert.equal(result.code, 0, `${args.join(" ")} failed: ${running.stderr}`);
    return running.stdout;
  } finally {
    if (!running.settled) await stop(running);
  }
}

async function readyURL(running) {
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    if (running.error) throw running.error;
    const match = running.stdout.match(/http:\/\/127\.0\.0\.1:\d+/);
    if (match) return new URL(match[0]);
    assert(!running.settled, `server exited before readiness: ${running.stderr}`);
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("installed server did not print a loopback URL in time");
}

function request(base, route) {
  assert.equal(base.hostname, "127.0.0.1");
  assert.equal(base.protocol, "http:");
  return new Promise((resolve, reject) => {
    const req = http.get(new URL(route, base), { agent: false }, (response) => {
      const chunks = [];
      let length = 0;
      response.on("data", (chunk) => {
        length += chunk.length;
        if (length > 4 * 1024 * 1024) response.destroy(new Error("local response exceeded the size limit"));
        else chunks.push(chunk);
      });
      response.on("error", reject);
      response.on("end", () => resolve({ status: response.statusCode, headers: response.headers, body: Buffer.concat(chunks).toString("utf8") }));
    });
    const timer = setTimeout(() => req.destroy(new Error("local HTTP request timed out")), 10000);
    req.on("close", () => clearTimeout(timer));
    req.on("error", reject);
  });
}

async function main() {
  const [binaryArg, expectedVersion, ...extra] = process.argv.slice(2);
  if (!binaryArg || extra.length) throw new Error("usage: node scripts/verify-install.cjs BINARY [EXPECTED_VERSION]");
  assert(Number(process.versions.node.split(".")[0]) >= 22, "Node.js 22+ is required");
  const binary = await fs.realpath(path.resolve(binaryArg));
  assert((await fs.stat(binary)).isFile(), "binary must be a regular file");
  const originalBinary = await fs.readFile(binary);
  const originalHash = digest(originalBinary);

  // Fail before launching anything if a future CLI override is not isolated.
  const cliSource = await fs.readFile(path.join(__dirname, "../internal/cli/cli.go"), "utf8");
  const declared = [...new Set(cliSource.match(/SKUGGSJA_[A-Z_]+/g))].sort();
  check("every CLI source override is isolated", () => assert.deepEqual(declared, [...Object.keys(sourceOverrides), "SKUGGSJA_CLAUDE_EXTRA_HOMES"].sort()));

  taskRoot = await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), "skuggsja-install-")));
  await fs.chmod(taskRoot, 0o700);
  const prefix = path.join(taskRoot, "prefix");
  const binDirectory = path.join(prefix, "bin");
  const sources = path.join(taskRoot, "sources");
  const output = path.join(taskRoot, "output");
  commandCwd = path.join(taskRoot, "unrelated-cwd");
  for (const directory of [binDirectory, sources, commandCwd]) await fs.mkdir(directory, { recursive: true, mode: 0o700 });
  const sourceCanary = path.join(sources, "unrelated.txt");
  const installCanary = path.join(prefix, "unrelated.txt");
  await fs.writeFile(sourceCanary, "synthetic source canary\n", { mode: 0o600 });
  await fs.writeFile(installCanary, "unrelated installation canary\n", { mode: 0o600 });
  commandName = process.platform === "win32" ? "skuggsja.exe" : "skuggsja";
  const installed = path.join(binDirectory, commandName);
  await fs.copyFile(binary, installed, constants.COPYFILE_EXCL);
  await fs.chmod(installed, 0o700);

  // Preserve HOME, CODEX_HOME, and other harness settings. Explicit source
  // overrides disable native discovery; PATH is changed only for our children.
  const inheritedPathKey = Object.keys(process.env).find((key) => key.toUpperCase() === "PATH");
  commandEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.toUpperCase().startsWith("SKUGGSJA_") && key.toUpperCase() !== "PATH"));
  commandEnv.PATH = binDirectory + path.delimiter + (process.env[inheritedPathKey] || "");
  for (const [name, relative] of Object.entries(sourceOverrides)) commandEnv[name] = path.join(sources, ...relative.split("/"));
  commandEnv.SKUGGSJA_CLAUDE_EXTRA_HOMES = "";
  commandEnv.SKUGGSJA_OUTPUT_DIRECTORY = output;
  const artifact = path.join(output, "rewind.json");
  const installedHash = digest(await fs.readFile(installed));
  check("isolated installed executable matches input", () => assert.equal(installedHash, originalHash));

  const version = (await run(["version"])).trim();
  check("version works from unrelated cwd through subprocess PATH", () => expectedVersion === undefined ? assert.match(version, /^skuggsja \S+$/) : assert.equal(version, `skuggsja ${expectedVersion}`));
  const help = await run(["--help"]);
  check("help lists the installed CLI controls", () => {
    for (const flag of ["clean", "completion", "version", "--once", "--json", "--no-open", "--no-source-audit", "--port"]) assert(help.includes(flag), `help omitted ${flag}`);
  });
  for (const shell of ["bash", "fish", "powershell", "zsh"]) {
    const completion = await run(["completion", shell]);
    check(`${shell} completion is generated`, () => assert(completion.includes("skuggsja") && completion.length > 100));
  }

  const emptyBefore = await snapshot(sources);
  const firstOutput = await run(["--once"]);
  const emptyReport = JSON.parse(await fs.readFile(artifact, "utf8"));
  check("first run writes an empty, read-only report", () => {
    assert.equal(emptyReport.totals.sessions, 0);
    assert.equal(emptyReport.totals.prompts, 0);
    assert.equal(emptyReport.privacy.source_access, "read-only");
    assert(firstOutput.includes("skuggsja does not write to source paths"));
    assert.equal(emptyReport.providers.length, 4);
    for (const provider of emptyReport.providers) assert.equal(provider.status, "not found");
  });
  const emptyAfter = await snapshot(sources);
  check("empty generation preserves declared synthetic sources", () => assert.deepEqual(emptyAfter, emptyBefore));
  const jsonOutput = JSON.parse(await run(["--json"]));
  const jsonArtifact = JSON.parse(await fs.readFile(artifact, "utf8"));
  check("JSON stdout equals its persisted aggregate", () => assert.deepEqual(jsonOutput, jsonArtifact));
  const jsonAfter = await snapshot(sources);
  check("JSON generation preserves declared synthetic sources", () => assert.deepEqual(jsonAfter, emptyBefore));

  const project = path.join(commandEnv.SKUGGSJA_CLAUDE_PROJECTS, "synthetic-project");
  await fs.mkdir(project, { recursive: true, mode: 0o700 });
  const fixture = path.join(project, "synthetic-session.jsonl");
  const rows = [
    { type: "user", uuid: "synthetic-prompt", sessionId: "synthetic-session", cwd: "/synthetic/project", timestamp: "2026-01-02T10:00:00Z", message: { role: "user", content: "Inspect the synthetic fixture." } },
    { type: "assistant", uuid: "synthetic-response", sessionId: "synthetic-session", cwd: "/synthetic/project", timestamp: "2026-01-02T10:01:00Z", message: { id: "synthetic-message", role: "assistant", model: "synthetic-model", content: [] } },
  ];
  await fs.writeFile(fixture, rows.map((row) => JSON.stringify(row)).join("\n") + "\n", { mode: 0o600 });
  const fixtureBefore = await snapshot(sources);
  await run(["--once"]);
  const fixtureReport = JSON.parse(await fs.readFile(artifact, "utf8"));
  check("installed binary derives exact synthetic session, prompt, and model counts", () => {
    assert.equal(fixtureReport.totals.sessions, 1);
    assert.equal(fixtureReport.totals.prompts, 1);
    assert.equal(fixtureReport.totals.child_sessions, 0);
    const claude = fixtureReport.providers.find((provider) => provider.id === "claude");
    assert.equal(claude.sessions, 1);
    assert.equal(claude.prompts, 1);
    assert.deepEqual(fixtureReport.models, [{ harness: "claude", name: "synthetic-model", turns: 1 }]);
    assert.equal(fixtureReport.privacy.source_access, "read-only");
    assert.equal(fixtureReport.privacy.raw_content_persisted, false);
    assert.equal(fixtureReport.privacy.absolute_paths_persisted, false);
    const serialized = JSON.stringify(fixtureReport);
    for (const secret of ["Inspect the synthetic fixture.", "synthetic-session", "/synthetic/project", sources]) assert(!serialized.includes(JSON.stringify(secret).slice(1, -1)), "synthetic content or source identity leaked");
  });
  const fixtureAfter = await snapshot(sources);
  check("synthetic generation leaves source hashes and membership unchanged", () => assert.deepEqual(fixtureAfter, fixtureBefore));

  const server = launch(["--no-open", "--port", "0"]);
  try {
    const url = await readyURL(server);
    check("installed UI binds an ephemeral IPv4 loopback URL", () => { assert.equal(url.hostname, "127.0.0.1"); assert(Number(url.port) > 0); });
    for (const [route, type] of [["/healthz", "text/plain"], ["/", "text/html"], ["/styles.css", "text/css"], ["/app.js", "javascript"], ["/api/rewind", "application/json"]]) {
      const response = await request(url, route);
      check(`${route} serves local content with the unchanged CSP`, () => {
        assert.equal(response.status, 200);
        assert(response.headers["content-type"].includes(type));
        assert.equal(response.headers["content-security-policy"], expectedCSP);
        assert.equal(response.headers["x-content-type-options"], "nosniff");
        assert.equal(response.headers["x-frame-options"], "DENY");
        assert.equal(response.headers["referrer-policy"], "no-referrer");
        assert(response.body.length > 0);
        if (route === "/healthz") assert.equal(response.body, "ok\n");
        if (route === "/") assert(response.body.includes('/app.js') && response.body.includes('/styles.css'));
      });
      if (route === "/api/rewind") {
        const persisted = JSON.parse(await fs.readFile(artifact, "utf8"));
        check("served API equals the persisted fixture aggregate", () => {
          assert.deepEqual(JSON.parse(response.body), persisted);
          assert.equal(persisted.totals.sessions, 1);
          assert.equal(persisted.totals.prompts, 1);
          assert.equal(response.headers["cache-control"], "no-store");
        });
      }
    }
  } finally {
    const stopped = await stop(server);
    check(process.platform === "win32" ? "owned server terminates (Windows signal semantics)" : "owned server shuts down gracefully", () => {
      if (process.platform !== "win32") assert.equal(stopped.code, 0);
      assert(server.settled);
      if (server.error) throw server.error;
    });
  }
  const serverAfter = await snapshot(sources);
  check("serving preserves synthetic source hashes and membership", () => assert.deepEqual(serverAfter, fixtureBefore));

  const outputCanary = path.join(output, "unrelated.txt");
  await fs.writeFile(outputCanary, "unrelated output canary\n", { mode: 0o600 });
  await run(["clean"]);
  const artifactMissing = await fs.stat(artifact).then(() => false, (error) => { if (error.code !== "ENOENT") throw error; return true; });
  check("clean removes the generated aggregate", () => assert(artifactMissing));
  const outputCanaryAfter = await fs.readFile(outputCanary, "utf8");
  check("clean preserves its nonempty directory and canary", () => assert.equal(outputCanaryAfter, "unrelated output canary\n"));
  const cleanAfter = await snapshot(sources);
  check("clean preserves synthetic sources and their canary", () => assert.deepEqual(cleanAfter, fixtureBefore));

  await fs.copyFile(binary, installed);
  await fs.chmod(installed, 0o700);
  const reinstalledHash = digest(await fs.readFile(installed));
  const reinstalledVersion = (await run(["version"])).trim();
  check("reinstall keeps exact binary bytes and a working command", () => { assert.equal(reinstalledHash, originalHash); assert.equal(reinstalledVersion, version); });
  await fs.unlink(installed);
  await assert.rejects(fs.stat(installed), { code: "ENOENT" });
  const installCanaryAfter = await fs.readFile(installCanary, "utf8");
  check("uninstall removes only the task-owned executable", () => assert.equal(installCanaryAfter, "unrelated installation canary\n"));
  const inputAfter = digest(await fs.readFile(binary));
  check("input binary remains unchanged", () => assert.equal(inputAfter, originalHash));
  assert(!interrupted, "installation smoke was interrupted");
  console.log(`Installed CLI smoke: ${checks}/${checks} checks passed (${process.platform}/${process.arch}; ${version}).`);
  console.log("Scope: isolated installation, synthetic sources, and loopback HTTP. No real histories or package-manager lifecycle tested; this is not a live-source equality or zero-outbound observation record.");
}

(async () => {
  try {
    await main();
  } catch (error) {
    console.error(`FAIL after ${checks} passed checks: ${error.message}`);
    process.exitCode = 1;
  } finally {
    try {
      for (const running of [...children]) await stop(running);
      if (taskRoot) await fs.rm(taskRoot, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
    } catch (error) {
      console.error(`FAIL cleanup: ${error.message}`);
      process.exitCode = 1;
    }
  }
})();
