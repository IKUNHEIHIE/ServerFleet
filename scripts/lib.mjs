import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const root = fileURLToPath(new URL("..", import.meta.url));
const suffix = process.platform === "win32" ? ".exe" : "";
const localGo = path.join(root, ".tools/go/bin", `go${suffix}`);
// Buf formatting uses GNU diff. Git for Windows includes a verified copy.
const gitLocation = process.platform === "win32" ? spawnSync("where.exe", ["git"], { encoding: "utf8" }).stdout?.trim().split(/\r?\n/)[0] : undefined;
const gitUnix = gitLocation ? path.resolve(path.dirname(gitLocation), "../usr/bin") : "";
export const go = existsSync(localGo) ? localGo : "go";
export const env = {
  ...process.env,
  PATH: [path.join(root, ".tools/bin"), go === "go" ? "" : path.dirname(go), gitUnix, process.env.PATH].filter(Boolean).join(path.delimiter),
  GOBIN: path.join(root, ".tools/bin"),
  GOCACHE: path.join(root, ".cache/go-build"),
  GOPATH: path.join(root, ".cache/gopath"),
  GOTOOLCHAIN: "local",
  NO_COLOR: "1",
  // The Redocly SDK has a separate telemetry switch from the CLI.
  REDOCLY_TELEMETRY: "off",
  REDOCLY_SDK_TELEMETRY: "off",
  DO_NOT_TRACK: "1",
};

export function run(command, args, { name = path.basename(command), allowFailure = false, ...options } = {}) {
  console.log(`> ${name}: ${args.join(" ")}`);
  const result = spawnSync(command, args, { cwd: root, env, encoding: "utf8", maxBuffer: 32 * 1024 * 1024, ...options });
  const output = `${result.stdout ?? ""}${result.stderr ?? ""}${result.error ? `\n${result.error.message}` : ""}`;
  mkdirSync(path.join(root, "reports"), { recursive: true });
  writeFileSync(path.join(root, "reports", `${name.replace(/[^a-z0-9-]/gi, "-")}.log`), output);
  if (output) console.log(output.trimEnd());
  if ((result.status !== 0 || result.error) && !allowFailure) throw new Error(`${name} failed (exit ${result.status ?? "unavailable"})`);
  return { ...result, output };
}

export function npm(args, options) {
  const cli = process.env.npm_execpath ?? path.join(path.dirname(process.execPath), "node_modules/npm/bin/npm-cli.js");
  if (!existsSync(cli)) throw new Error("Please invoke this check with npm run so the npm CLI can be found");
  return run(process.execPath, [cli, ...args], options);
}

export const tool = (name, args, options = {}) => npm(["exec", "--", name, ...args], { name, ...options });
export const goTool = (name, args, options = {}) => run(path.join(env.GOBIN, name + suffix), args, { name, ...options });
