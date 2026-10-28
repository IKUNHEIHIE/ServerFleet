import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "./lib.mjs";
import { checkWebSource } from "./web-boundaries.mjs";

const file = path.join(root, "apps/web/src/features/fixture.tsx");
test("production may use UI but not test data or Go implementation", () => {
  assert.doesNotThrow(() => checkWebSource('import { Badge } from "@/components/ui/badge"', file));
  for (const text of ['import x from "@/test/fixtures"', 'const x = import("@/test/fixtures")', 'export * from "../../../../internal/auth"', 'new WebSocket("wss://agent.invalid")']) assert.throws(() => checkWebSource(text, file));
  assert.doesNotThrow(() => checkWebSource('import x from "@/test/fixtures"', file.replace("fixture.tsx", "fixture.test.tsx")));
});
