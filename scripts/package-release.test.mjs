import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import assert from "node:assert/strict";
import test from "node:test";

const script = new URL("./package-release.mjs", import.meta.url).pathname;
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
function candidate(t) {
  const parent = mkdtempSync(path.join(tmpdir(), "sf-package-"));
  t.after(() => rmSync(parent, { recursive: true, force: true }));
  const source = path.join(parent, "candidate"), output = path.join(parent, "packages");
  mkdirSync(source);
  const manifest = { format: 1, kind: "serverfleet-core-unsigned-candidate", version: "0.9.0", sequence: 1, commit: "a".repeat(40), committedAt: "2026-11-12T00:00:00Z", acceptance: "UNVERIFIED", tools: {}, optionalPlugins: [], files: [] };
  function file(name, bytes) {
    bytes = Buffer.from(bytes);
    mkdirSync(path.dirname(path.join(source, name)), { recursive: true });
    writeFileSync(path.join(source, name), bytes);
    manifest.files.push({ path: name, bytes: bytes.length, sha256: hash(bytes) });
  }
  file("deployment/README.md", "Deployment instructions\n");
  for (const arch of ["amd64", "arm64"]) {
    for (const component of ["controller", "agent"]) {
      const elf = Buffer.alloc(64); elf.write("\x7fELF"); elf[4] = 2; elf[5] = 1; elf.writeUInt16LE(arch === "amd64" ? 62 : 183, 18);
      file(`linux-${arch}/${component}`, elf);
      file(`linux-${arch}/${component}.manifest.json`, JSON.stringify({ format: 1, component, version: manifest.version, sequence: 1, os: "linux", architecture: arch, sha256: hash(elf), bytes: elf.length }));
    }
    file(`linux-${arch}/web/index.html`, "<html>Web</html>");
    file(`linux-${arch}/installation.manifest.json`, "{}");
  }
  function seal() {
    const bytes = JSON.stringify(manifest);
    writeFileSync(path.join(source, "manifest.json"), bytes);
    writeFileSync(path.join(source, "build-status.json"), JSON.stringify({ status: "BUILT_UNSIGNED_UNVERIFIED", source: manifest.commit, manifestSHA256: hash(bytes) }));
  }
  seal();
  return { source, output, manifest, seal, run: () => spawnSync(process.execPath, [script, source, output], { encoding: "utf8" }) };
}

test("archives contain the correct architecture, private modes, web, provenance and verifiable digests", t => {
  const fixture = candidate(t), result = fixture.run();
  assert.equal(result.status, 0, result.stderr);
  execFileSync("sha256sum", ["--check", "SHA256SUMS"], { cwd: fixture.output });
  for (const arch of ["amd64", "arm64"]) {
    const name = `serverfleet_0.9.0_linux_${arch}`;
    const listing = execFileSync("tar", ["-tvzf", path.join(fixture.output, `${name}.tar.gz`)], { encoding: "utf8" });
    assert.match(listing, /-rwx------.*\/controller\n/);
    assert.match(listing, /-rw-------.*\/web\/index.html\n/);
    assert.doesNotMatch(listing, /node_modules|private\.key|linux-(amd64|arm64)/);
    const info = JSON.parse(execFileSync("tar", ["-xOzf", path.join(fixture.output, `${name}.tar.gz`), `${name}/release-info.json`], { encoding: "utf8" }));
    assert.equal(info.architecture, arch);
    assert.equal(info.commit, fixture.manifest.commit);
    assert.equal(info.kind, "serverfleet-core-unsigned-package");
    assert.deepEqual(info.optionalPlugins, []);
    assert.ok(info.files.some(file => file.path === "deployment/README.md"));
  }
});

test("refuses changed bytes, unlisted state, symlinks and incomplete builds", async t => {
  const changes = [
    f => writeFileSync(path.join(f.source, "linux-amd64/controller"), "corrupted"),
    f => writeFileSync(path.join(f.source, "state.db"), "must never ship"),
    f => symlinkSync("deployment/README.md", path.join(f.source, "linked")),
    f => writeFileSync(path.join(f.source, "build-status.json"), '{"status":"INCOMPLETE"}'),
  ];
  for (const [index, change] of changes.entries()) await t.test(`rejection ${index + 1}`, child => {
    const fixture = candidate(child); change(fixture);
    assert.notEqual(fixture.run().status, 0);
  });
});

test("refuses a wrong ELF architecture even when file digests have been updated", t => {
  const fixture = candidate(t), filename = "linux-arm64/agent", bytes = readFileSync(path.join(fixture.source, filename));
  bytes.writeUInt16LE(62, 18); writeFileSync(path.join(fixture.source, filename), bytes);
  fixture.manifest.files.find(file => file.path === filename).sha256 = hash(bytes); fixture.seal();
  const result = fixture.run();
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Wrong ELF architecture/);
});
