import path from "node:path";
import { root, tool } from "./lib.mjs";

const original = path.join(root, "docs/reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2");
tool("buf", ["build", "-o", path.join(root, "reports/original-agent.binpb")], { cwd: original, name: "original-protobuf-build" });
const lint = tool("buf", ["lint"], { cwd: original, name: "original-protobuf-lint", allowFailure: true });
if (lint.status !== 0 && !/must be within a directory/.test(lint.output)) throw new Error("Unexpected original lint error");
const template = JSON.stringify({ version: "v2", plugins: [{ local: "protoc-gen-go", out: path.join(root, "reports/original-proto-go"), opt: ["paths=source_relative"] }] });
const result = tool("buf", ["generate", "--template", template], { cwd: original, name: "original-protobuf-generation", allowFailure: true });
if (result.status !== 0) {
  if (!/unable to determine Go import path/.test(result.output)) throw new Error("Unexpected original generation failure; inspect the log");
  console.log("CONFIRMED: original candidate compiles, original Buf root fails lint, and Go generation lacks go_package. Use independent M0-P1-r1 candidate and module-root config.");
} else throw new Error("Original candidate now generates; review the recorded diagnosis");
