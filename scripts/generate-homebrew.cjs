#!/usr/bin/env node
// Release tooling only. Generate a prebuilt-binary formula from verified checksums.
"use strict";
const fs = require("node:fs");
const assert = require("node:assert/strict");
const [tag, checksumFile, output] = process.argv.slice(2);
assert(/^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(tag || ""), "expected a stable vMAJOR.MINOR.PATCH tag");
assert(checksumFile && output && process.argv.length === 5, "usage: generate-homebrew.cjs TAG CHECKSUMS OUTPUT");
const version = tag.slice(1);
const sums = new Map();
for (const line of fs.readFileSync(checksumFile, "utf8").trim().split(/\r?\n/)) {
  const match = /^([a-f0-9]{64})\s+\*?([A-Za-z0-9_.-]+)$/.exec(line);
  assert(match && !sums.has(match[2]), "invalid or duplicate checksum entry");
  sums.set(match[2], match[1]);
}
function platform(os) {
  return ["arm64", "amd64"].map((arch) => {
    const name = `skuggsja_${version}_${os}_${arch}.tar.gz`;
    assert(sums.has(name), `missing archive checksum: ${name}`);
    return `    on_${arch === "arm64" ? "arm" : "intel"} do\n      url "https://github.com/0merUfuk/skuggsja/releases/download/${tag}/${name}"\n      sha256 "${sums.get(name)}"\n    end`;
  }).join("\n");
}
const formula = `# frozen_string_literal: true

# Generated from the release checksums; do not edit download values manually.
class Skuggsja < Formula
  desc "Local history retrospective for AI coding agents"
  homepage "https://github.com/0merUfuk/skuggsja"
  version "${version}"
  license "MIT"

  on_macos do
${platform("darwin")}
  end

  on_linux do
${platform("linux")}
  end

  def install
    bin.install "skuggsja"
    generate_completions_from_executable(bin/"skuggsja", "completion")
  end

  test do
    assert_equal "skuggsja #{version}", shell_output("#{bin}/skuggsja version").strip
    assert_match "--no-open", shell_output("#{bin}/skuggsja --help")
  end
end
`;
fs.writeFileSync(output, formula, { flag: "wx" });
console.log("PASS: four checksummed platform downloads in Homebrew formula");
