// Build an UNSIGNED core candidate outside the checkout. Never install, deploy,
// generate signing keys or bundle optional plugin executables / runtime state.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import path from "node:path";
import { root, go } from "./lib.mjs";

if (process.platform !== "linux") throw new Error("This candidate builder requires a Linux build host");
const values = Object.create(null);
for (const arg of process.argv.slice(2)) {
  const match = /^--(version|sequence|output)=(.+)$/.exec(arg);
  if (!match || Object.hasOwn(values, match[1])) throw new Error("Expected unique --version=X.Y.Z --sequence=N --output=/new/private/directory");
  values[match[1]] = match[2];
}
if (!/^\d+\.\d+\.\d+$/.test(values.version ?? "") || !/^[1-9]\d*$/.test(values.sequence ?? "") || !Number.isSafeInteger(Number(values.sequence))) throw new Error("Explicit stable version and positive sequence are required");
if (!values.output || !path.isAbsolute(values.output)) throw new Error("Output must be an absolute new directory outside this checkout");
const output = path.resolve(values.output);
const parent = realpathSync(path.dirname(output));
const checkout = realpathSync(root);
const within = (base, target) => { const relative = path.relative(base, target); return relative === "" || relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative); };
if (within(checkout, output) || within(checkout, parent) || path.dirname(output) !== parent || existsSync(output)) throw new Error("Refuse existing, aliased or checkout output directory");
const execute = (command, args, env = {}) => execFileSync(command, args, { cwd: root, env: { ...process.env, GOTOOLCHAIN: "local", ...env }, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], maxBuffer: 32 << 20 });
const versions = JSON.parse(readFileSync(path.join(root, "scripts/tool-versions.json"), "utf8"));
const goVersion = execute(go, ["version"]).trim();
if (!goVersion.includes(` go${versions.go} `) || process.versions.node !== versions.node) throw new Error(`Fixed toolchain required: Go ${versions.go}, Node ${versions.node}`);
const npm = process.platform === "win32" ? "npm.cmd" : "npm";
const npmVersion = execute(npm, ["--version"]).trim();
if (!npmVersion.startsWith("11.")) throw new Error("npm 11 required");
if (execute("git", ["status", "--porcelain", "--untracked-files=normal"]).trim()) throw new Error("Tracked source must be committed and checkout clean before building a reproducible candidate");
const commit = execute("git", ["rev-parse", "HEAD"]).trim();
const committedAt = execute("git", ["show", "-s", "--format=%cI", "HEAD"]).trim();
function assertSource() {
  if (execute("git", ["rev-parse", "HEAD"]).trim() !== commit || execute("git", ["status", "--porcelain", "--untracked-files=normal"]).trim()) throw new Error("Source changed during candidate assembly; partial output is not a coherent release");
}

mkdirSync(output, { mode: 0o700 });
const statusFile = path.join(output, "build-status.json");
writeFileSync(statusFile, JSON.stringify({ status: "INCOMPLETE", source: commit }) + "\n", { mode: 0o600, flag: "wx" });
const manifest = { format: 1, kind: "serverfleet-core-unsigned-candidate", version: values.version, sequence: Number(values.sequence), commit, committedAt, acceptance: "UNVERIFIED", tools: { go: goVersion, node: process.versions.node, npm: npmVersion }, optionalPlugins: [], files: [] };
const record = name => {
  const filename = path.join(output, name), bytes = readFileSync(filename);
  manifest.files.push({ path: name.replaceAll(path.sep, "/"), bytes: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex") });
};
function copyWeb(source, target, relative = "", limits = { bytes: 0, files: 0 }) {
  mkdirSync(target, { mode: 0o700 });
  for (const name of readdirSync(source).sort()) {
    const origin = path.join(source, name), destination = path.join(target, name), stat = lstatSync(origin);
    if (stat.isSymbolicLink() || name.startsWith(".") || !/^[A-Za-z0-9_.-]+$/.test(name) || stat.nlink > 1 && !stat.isDirectory()) throw new Error("Refuse linked or hidden Web assets");
    if (stat.isDirectory()) { if (relative || name !== "assets") throw new Error("Unexpected Web directory"); copyWeb(origin, destination, name, limits); }
    else {
      if (!stat.isFile() || !relative && name !== "index.html" && name !== "favicon.ico" || relative && !/\.(js|css|svg|png|jpe?g|webp|ico|woff2?)$/.test(name)) throw new Error("Unexpected Web build file");
      limits.bytes += stat.size; limits.files++;
      if (stat.size <= 0 || stat.size > (8 << 20) || limits.bytes > (32 << 20) || limits.files > 1024) throw new Error("Web assets exceed the Controller startup bounds");
      writeFileSync(destination, readFileSync(origin), { mode: 0o600, flag: "wx" });
      record(path.relative(output, destination));
    }
  }
}
try {
  execute(npm, ["run", "build", "--workspace", "@serverfleet/web"]);
  for (const arch of ["amd64", "arm64"]) {
    const directory = path.join(output, `linux-${arch}`); mkdirSync(directory, { mode: 0o700 });
    for (const component of ["controller", "agent"]) {
      const destination = path.join(directory, component);
      const symbol = component === "agent" ? `-X main.agentBuildVersion=${values.version} -X serverfleet.local/serverfleet/apps/agent/runtime.DefaultVersion=${values.version}` : `-X main.controllerBuildVersion=${values.version}`;
      assertSource();
      execute(go, ["build", "-trimpath", "-buildvcs=true", "-ldflags", symbol, "-o", destination, `./apps/${component}`], { GOOS: "linux", GOARCH: arch, CGO_ENABLED: "0" });
      assertSource();
      const buildInfo = execute(go, ["version", "-m", destination]);
      if (!buildInfo.includes(`vcs.revision=${commit}\n`) || !buildInfo.includes("vcs.modified=false")) throw new Error("Executable provenance differs from the captured source commit");
      chmodSync(destination, 0o700); record(path.relative(output, destination));
      {
        const artifact = manifest.files.at(-1);
        // Exact canonical ReleaseManifest bytes for later OFFLINE signing.
        const release = { format: 1, component, version: values.version, sequence: Number(values.sequence), os: "linux", architecture: arch, sha256: artifact.sha256, bytes: artifact.bytes };
        writeFileSync(path.join(directory, `${component}.manifest.json`), JSON.stringify(release), { mode: 0o600, flag: "wx" });
        record(path.relative(output, path.join(directory, `${component}.manifest.json`)));
      }
    }
    copyWeb(path.join(root, "apps/web/dist"), path.join(directory, "web"));
  }
  assertSource();
  manifest.files.sort((a, b) => a.path.localeCompare(b.path));
  const manifestBytes = JSON.stringify(manifest, null, 2) + "\n";
  writeFileSync(path.join(output, "manifest.json"), manifestBytes, { mode: 0o600, flag: "wx" });
  writeFileSync(statusFile, JSON.stringify({ status: "BUILT_UNSIGNED_UNVERIFIED", source: commit, manifestSHA256: createHash("sha256").update(manifestBytes).digest("hex") }) + "\n", { mode: 0o600 });
  process.stdout.write(`Unsigned candidate built: ${output}\nSource: ${commit}\nNo signing, installation, deployment or release acceptance performed.\n`);
} catch (error) {
  process.stderr.write(`Candidate build failed; partial output retained with build-status=INCOMPLETE: ${output}\n`);
  throw error;
}
