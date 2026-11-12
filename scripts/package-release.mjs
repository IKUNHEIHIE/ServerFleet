// Package only files admitted by the unsigned candidate builder. No signing or installation.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, copyFileSync, existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from "node:fs";
import path from "node:path";

const args = process.argv.slice(2);
if (args.length !== 2 || args.some(value => !path.isAbsolute(value))) throw new Error("Usage: node scripts/package-release.mjs /candidate /new-output");
const source = realpathSync(args[0]), output = path.resolve(args[1]);
const inside = (base, target) => { const rel = path.relative(base, target); return rel === "" || rel !== ".." && !rel.startsWith(`..${path.sep}`) && !path.isAbsolute(rel); };
if (existsSync(output) || inside(source, output) || path.dirname(output) !== realpathSync(path.dirname(output))) throw new Error("Output must be new and outside the candidate");
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
const regular = filename => { const stat = lstatSync(filename); if (!stat.isFile() || stat.nlink !== 1) throw new Error(`Refuse linked or non-regular file: ${filename}`); return readFileSync(filename); };
const manifestBytes = regular(path.join(source, "manifest.json"));
const manifest = JSON.parse(manifestBytes), status = JSON.parse(regular(path.join(source, "build-status.json")));
if (status.status !== "BUILT_UNSIGNED_UNVERIFIED" || status.source !== manifest.commit || status.manifestSHA256 !== hash(manifestBytes)
    || manifest.kind !== "serverfleet-core-unsigned-candidate" || manifest.format !== 1 || manifest.acceptance !== "UNVERIFIED"
    || !/^\d+\.\d+\.\d+$/.test(manifest.version) || !/^[a-f0-9]{40}$/.test(manifest.commit)
    || !Number.isSafeInteger(manifest.sequence) || manifest.sequence < 1 || !Array.isArray(manifest.optionalPlugins) || manifest.optionalPlugins.length
    || !Array.isArray(manifest.files) || !Number.isFinite(Date.parse(manifest.committedAt))) throw new Error("Incomplete or invalid unsigned candidate provenance");
const files = new Map();
for (const entry of manifest.files) {
  if (!entry || typeof entry.path !== "string" || !/^[A-Za-z0-9_./-]+$/.test(entry.path) || entry.path.split("/").some(part => !part || part === "." || part === ".." || part.startsWith("."))
      || !/^(deployment\/|linux-(amd64|arm64)\/)/.test(entry.path) || files.has(entry.path)) throw new Error("Invalid or duplicate candidate path");
  const bytes = regular(path.join(source, entry.path));
  if (entry.bytes !== bytes.length || entry.sha256 !== hash(bytes)) throw new Error(`Candidate digest mismatch: ${entry.path}`);
  files.set(entry.path, entry);
}
function walk(directory, prefix = "") {
  for (const name of readdirSync(directory)) {
    const relative = prefix + name, filename = path.join(directory, name), stat = lstatSync(filename);
    if (stat.isSymbolicLink()) throw new Error("Refuse candidate symlink");
    if (stat.isDirectory()) walk(filename, relative + "/");
    else if (!files.has(relative) && relative !== "manifest.json" && relative !== "build-status.json") throw new Error(`Unlisted candidate file: ${relative}`);
  }
}
walk(source);
for (const arch of ["amd64", "arm64"]) {
  for (const component of ["controller", "agent"]) {
    const name = `linux-${arch}/${component}`, entry = files.get(name);
    if (!entry) throw new Error(`Missing component: ${name}`);
    const bytes = regular(path.join(source, name));
    if (bytes.length < 64 || bytes.subarray(0, 4).toString("hex") !== "7f454c46" || bytes[4] !== 2 || bytes[5] !== 1 || bytes.readUInt16LE(18) !== (arch === "amd64" ? 62 : 183)) throw new Error(`Wrong ELF architecture: ${name}`);
    const signedInput = JSON.parse(regular(path.join(source, `${name}.manifest.json`)));
    if (signedInput.component !== component || signedInput.version !== manifest.version || signedInput.sequence !== manifest.sequence || signedInput.os !== "linux" || signedInput.architecture !== arch || signedInput.sha256 !== entry.sha256 || signedInput.bytes !== entry.bytes) throw new Error("Release manifest mismatch");
  }
  if (!files.has(`linux-${arch}/web/index.html`) || !files.has(`linux-${arch}/installation.manifest.json`) || !files.has("deployment/README.md")) throw new Error("Incomplete installation files");
}
mkdirSync(output, { mode: 0o700 });
const assets = [];
for (const arch of ["amd64", "arm64"]) {
  const name = `serverfleet_${manifest.version}_linux_${arch}`, staging = path.join(output, name);
  mkdirSync(staging, { mode: 0o700 });
  const selected = [];
  for (const entry of files.values()) {
    const prefix = `linux-${arch}/`;
    if (!entry.path.startsWith(prefix) && !entry.path.startsWith("deployment/")) continue;
    const relative = entry.path.startsWith(prefix) ? entry.path.slice(prefix.length) : entry.path;
    const target = path.join(staging, relative);
    mkdirSync(path.dirname(target), { recursive: true, mode: 0o700 });
    copyFileSync(path.join(source, entry.path), target);
    chmodSync(target, relative === "controller" || relative === "agent" ? 0o700 : 0o600);
    selected.push({ ...entry, path: relative });
  }
  writeFileSync(path.join(staging, "release-info.json"), JSON.stringify({ ...manifest, kind: "serverfleet-core-unsigned-package", architecture: arch, files: selected }, null, 2) + "\n", { mode: 0o600 });
  const filename = `${name}.tar.gz`;
  execFileSync("tar", ["--sort=name", `--mtime=${manifest.committedAt}`, "--owner=0", "--group=0", "--numeric-owner", "-czf", path.join(output, filename), "-C", output, name], { env: { ...process.env, GZIP: "-n" }, stdio: "pipe" });
  assets.push(filename);
}
copyFileSync(path.join(source, "manifest.json"), path.join(output, "build-manifest.json"));
assets.push("build-manifest.json");
writeFileSync(path.join(output, "SHA256SUMS"), assets.map(name => `${hash(readFileSync(path.join(output, name)))}  ${name}\n`).join(""), { mode: 0o600 });
console.log(`Packaged unsigned ${manifest.version} from ${manifest.commit}; no deployment or signing performed.`);
