import assert from "node:assert/strict";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { root, tool } from "./lib.mjs";

// Isolated mutations prove compiler and compatibility checks actually reject.
const temp = path.join(root, "reports/proto-negative");
mkdirSync(path.join(temp, "serverfleet/agent/v1"), { recursive: true });
writeFileSync(path.join(temp, "buf.yaml"), "version: v2\nbreaking:\n  use: [FILE]\n");
const source = readFileSync(path.join(root, "proto/serverfleet/agent/v1/agent.proto"), "utf8");
const file = path.join(temp, "serverfleet/agent/v1/agent.proto");
writeFileSync(file, source.replace("uint32 protocol_major = 1;", "string protocol_major = 1;"));
const breaking = tool("buf", ["breaking", ".", "--against", path.join(root, "tests/contracts/proto-baseline")], { cwd: temp, name: "protobuf-negative-breaking", allowFailure: true });
assert.notEqual(breaking.status, 0, "breaking field type must fail");
assert.match(breaking.output, /protocol_major.*(type|TYPE)|type.*protocol_major/i);
writeFileSync(file, source.replace("uint32 protocol_major = 1;", "uint32 protocol_major = 2;"));
const compile = tool("buf", ["build", "."], { cwd: temp, name: "protobuf-negative-compile", allowFailure: true });
assert.notEqual(compile.status, 0, "duplicate field number must fail");
assert.match(compile.output, /tag|number|already|duplicate/i);
console.log("PASS: Buf rejected incompatible type and duplicate field number");
