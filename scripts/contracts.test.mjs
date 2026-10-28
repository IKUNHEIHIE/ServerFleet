import { test } from "node:test";
import assert from "node:assert/strict";
import { validateContract } from "./contracts.mjs";

const valid = `openapi: 3.1.0
info: {title: Fixture, version: '0'}
paths:
  /nodes/{nodeId}:
    get:
      operationId: getNode
      parameters: [{in: path, name: nodeId, required: true, schema: {type: string}}]
      responses: {'200': {description: OK}}
components:
  schemas:
    Node: {type: string}
`;
test("valid fixture is counted", () => assert.deepEqual(validateContract(valid), { paths: 1, operations: 1 }));
test("broken refs are rejected", () => assert.throws(() => validateContract(valid.replace("{type: string}", "{$ref: '#/components/schemas/Missing'}")), /unresolved/));
test("path parameters must be required", () => assert.throws(() => validateContract(valid.replace("required: true", "required: false")), /path parameter/));
test("duplicate operation IDs are rejected", () => assert.throws(() => validateContract(valid.replace("components:", "  /other:\n    get: {operationId: getNode, responses: {'200': {description: OK}}}\ncomponents:")), /duplicate/));
test("duplicate YAML keys are rejected", () => assert.throws(() => validateContract(valid + "openapi: 3.1.0\n"), /unique|map|key/i));
