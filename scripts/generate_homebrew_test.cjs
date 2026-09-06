"use strict";
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const script = path.join(__dirname, "generate-homebrew.cjs");
const names = ["darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"];
function scenario(t, tag = "v0.1.0", transform = (lines) => lines) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "skuggsja-formula-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const checksum = path.join(root, "checksums.txt"), output = path.join(root, "skuggsja.rb");
  const lines = names.map((name, index) => `${String(index + 1).repeat(64)}  skuggsja_0.1.0_${name}.tar.gz`);
  fs.writeFileSync(checksum, transform(lines).join("\n") + "\n");
  const result = spawnSync(process.execPath, [script, tag, checksum, output], { encoding: "utf8" });
  return { result, output, formula: fs.existsSync(output) ? fs.readFileSync(output, "utf8") : "" };
}
test("formula binds each platform to its own checksum without runtime build dependencies", (t) => {
  const { result, formula } = scenario(t);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual([...formula.matchAll(/^# Skuggsja release version: (.+)$/gm)].map((match) => match[1]), ["0.1.0"]);
  assert.doesNotMatch(formula, /^\s*version\b/m);
  const urls = [...formula.matchAll(/^\s*url "([^"]+)"$/gm)].map((match) => match[1]);
  assert.deepEqual(urls, names.map((name) => `https://github.com/0merUfuk/skuggsja/releases/download/v0.1.0/skuggsja_0.1.0_${name}.tar.gz`));
  names.forEach((name, index) => assert(formula.includes(`skuggsja_0.1.0_${name}.tar.gz"\n      sha256 "${String(index + 1).repeat(64)}"`)));
  assert(formula.includes('bin.install "skuggsja"'));
  assert(formula.includes('generate_completions_from_executable'));
  assert.doesNotMatch(formula, /xattr|quarantine|depends_on|system.*go.*build/);
});
test("missing or duplicate download checksums fail before output", (t) => {
  for (const transform of [(lines) => lines.slice(1), (lines) => lines.concat(lines[0])]) {
    const { result, output } = scenario(t, "v0.1.0", transform);
    assert.notEqual(result.status, 0); assert(!fs.existsSync(output));
  }
});
test("unsafe and prerelease tags cannot become executable Ruby", (t) => {
  for (const tag of ['v0.1.0"; system("bad")', "v0.1.0-rc1", "v01.2.3", "../v0.1.0"]) {
    const { result, output } = scenario(t, tag); assert.notEqual(result.status, 0); assert(!fs.existsSync(output));
  }
});
test("a checksum list for another release fails instead of silently using stale artifacts", (t) => {
  const { result, output } = scenario(t, "v0.2.0"); assert.notEqual(result.status, 0); assert(!fs.existsSync(output));
});
