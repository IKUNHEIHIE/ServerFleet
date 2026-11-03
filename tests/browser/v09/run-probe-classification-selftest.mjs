#!/usr/bin/env node
/**
 * V09-CLOUD-WB-01 · 恢复探针分类逻辑的控制性自测（WB-R1 回归）
 *
 * 背景：`run-recovery-prerequisite-probe.mjs` 的 R08—R10 曾经对任何 HTTP 结果都记 BLOCKED，
 * 于是 500/401/403 与错误 shape 都能伪装成「产品未实现」。首轮独立审查用三段伪造 500
 * 直接复现了这个问题（R08/R09/R10 全部 BLOCKED、reportedFailure=false）。
 *
 * 本脚本把真实脚本里的 helper 与 R08—R10 片段原样抽出来，在 vm 里喂各种响应，
 * 断言：只有精确匹配才 BLOCKED，其余一律 FAIL。它不启动产品、不碰数据库，只测分类器本身。
 *
 * 用法：node tests/browser/v09/run-probe-classification-selftest.mjs
 * 退出码：0 = 全部分类符合预期；1 = 有分类错误。
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";

const here = dirname(fileURLToPath(import.meta.url));
const target = join(here, "run-recovery-prerequisite-probe.mjs");
const source = readFileSync(target, "utf8");

const helperStart = source.indexOf("function record(");
const helperEnd = source.indexOf("\nfunction sleep(", helperStart);
if (helperStart < 0 || helperEnd < 0) throw new Error("helper block not found in probe script");
const helper = source.slice(helperStart, helperEnd);

const segStart = source.indexOf('    await probeBlocked("R08"');
const segEnd = source.indexOf('    await step("R11"', segStart);
if (segStart < 0 || segEnd < 0) throw new Error("R08-R10 segment not found in probe script");
const segment = source.slice(segStart, segEnd);

const problem = (status, code) => ({
  status,
  contentType: "application/problem+json",
  json: { type: "about:blank", title: "t", status, code },
  text: JSON.stringify({ type: "about:blank", title: "t", status, code }),
});

const GOOD_PREVIEW = {
  status: 200,
  contentType: "application/json",
  json: {
    backupId: "wb01-dr-backup",
    eligible: false,
    verified: true,
    checks: [
      { name: "backup_integrity", passed: true },
      { name: "target_controller_identity", passed: false, reason: "target_identity_not_observed" },
      { name: "old_controller_fenced", passed: false, reason: "fencing_evidence_unavailable" },
      { name: "recovery_key_reference", passed: true },
    ],
  },
  text: "{}",
};

/** 按端点精确分发：只替换被测端点，其余端点维持「预期内」响应。 */
const good = path => {
  if (path.includes("/jobs/")) return problem(404, "RECOVERY_JOB_NOT_FOUND");
  if (path.endsWith("/jobs")) return problem(501, "RECOVERY_EXECUTOR_UNAVAILABLE");
  return GOOD_PREVIEW;
};

const fiveHundred = () => ({
  status: 500,
  contentType: "application/json",
  json: { code: "INTERNAL_SERVER_ERROR" },
  text: '{"code":"INTERNAL_SERVER_ERROR"}',
});

/** 在预期预览基础上替换 checks，其余字段（eligible=false / verified=true）保持不变。 */
const previewWith = checks => ({
  ...GOOD_PREVIEW,
  json: { ...GOOD_PREVIEW.json, checks },
});

const GOOD_CHECKS = GOOD_PREVIEW.json.checks;

const scenarios = [
  {
    name: "三个端点全部 500（审查复现口径）",
    match: p => p.endsWith("/preview") || p.includes("/jobs"),
    respond: fiveHundred,
    want: ["FAIL", "FAIL", "FAIL"],
  },
  {
    name: "jobs 返回 401 NOT_AUTHORIZED",
    match: p => p.endsWith("/disaster-recovery/jobs"),
    respond: () => problem(401, "NOT_AUTHORIZED"),
    want: ["BLOCKED", "FAIL", "BLOCKED"],
  },
  {
    name: "preview 返回 503 但 problem 结构正确",
    match: p => p.endsWith("/preview"),
    respond: () => problem(503, "RECOVERY_EXECUTOR_UNAVAILABLE"),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
  },
  {
    name: "jobs 501 但 code 是别的",
    match: p => p.endsWith("/disaster-recovery/jobs"),
    respond: () => problem(501, "SOMETHING_ELSE"),
    want: ["BLOCKED", "FAIL", "BLOCKED"],
  },
  {
    name: "job 404 但结构缺 code",
    match: p => p.includes("/jobs/"),
    respond: () => ({
      status: 404,
      contentType: "application/json",
      json: { error: "not found" },
      text: '{"error":"not found"}',
    }),
    want: ["BLOCKED", "BLOCKED", "FAIL"],
  },
  {
    name: "job 404 但响应泄露路径",
    match: p => p.includes("/jobs/"),
    respond: () => ({
      status: 404,
      contentType: "application/problem+json",
      json: { type: "about:blank", title: "t", status: 404, code: "RECOVERY_JOB_NOT_FOUND" },
      text: '{"code":"RECOVERY_JOB_NOT_FOUND","path":"/var/lib/backups/x.db"}',
    }),
    want: ["BLOCKED", "BLOCKED", "FAIL"],
  },
  {
    name: "preview 少了已知不合格项",
    match: p => p.endsWith("/preview"),
    respond: () => ({
      ...GOOD_PREVIEW,
      json: { ...GOOD_PREVIEW.json, checks: [{ name: "backup_integrity", passed: true }] },
    }),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
  },
  {
    name: "preview eligible 变成 true",
    match: p => p.endsWith("/preview"),
    respond: () => ({ ...GOOD_PREVIEW, json: { ...GOOD_PREVIEW.json, eligible: true } }),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
  },
  {
    name: "preview 检查项原因漂移",
    match: p => p.endsWith("/preview"),
    respond: () => ({
      ...GOOD_PREVIEW,
      json: {
        ...GOOD_PREVIEW.json,
        checks: [
          { name: "backup_integrity", passed: true },
          { name: "target_controller_identity", passed: false, reason: "something_changed" },
          { name: "old_controller_fenced", passed: false, reason: "fencing_evidence_unavailable" },
          { name: "recovery_key_reference", passed: true },
        ],
      },
    }),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
  },
  // —— WB-R2-01：必需成功项与「恰好两项失败」的负向（审查给出的最小复现） ——
  {
    name: "preview recovery_key_reference 失败（审查复现）",
    match: p => p.endsWith("/preview"),
    respond: () =>
      previewWith(
        GOOD_CHECKS.map(check =>
          check.name === "recovery_key_reference"
            ? { name: "recovery_key_reference", passed: false, reason: "recovery_key_corrupted" }
            : check,
        ),
      ),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /recovery_key_reference 未通过|实际失败项集合/,
  },
  {
    name: "preview 缺失 recovery_key_reference（审查复现）",
    match: p => p.endsWith("/preview"),
    respond: () => previewWith(GOOD_CHECKS.filter(check => check.name !== "recovery_key_reference")),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /缺少必需检查项 recovery_key_reference/,
  },
  {
    name: "preview 缺失 backup_integrity",
    match: p => p.endsWith("/preview"),
    respond: () => previewWith(GOOD_CHECKS.filter(check => check.name !== "backup_integrity")),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /缺少必需检查项 backup_integrity/,
  },
  {
    name: "preview 检查项重复（已知项两次）",
    match: p => p.endsWith("/preview"),
    respond: () => previewWith([...GOOD_CHECKS, { name: "recovery_key_reference", passed: true }]),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /检查项重复: recovery_key_reference/,
  },
  {
    name: "preview 多出一个未知失败项",
    match: p => p.endsWith("/preview"),
    respond: () =>
      previewWith([...GOOD_CHECKS, { name: "cluster_capacity", passed: false, reason: "no_free_slot" }]),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /出现未知检查项 cluster_capacity|实际失败项集合/,
  },
  {
    name: "preview 多出一个未知通过项",
    match: p => p.endsWith("/preview"),
    respond: () => previewWith([...GOOD_CHECKS, { name: "audit_ready", passed: true }]),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /出现未知检查项 audit_ready/,
  },
  {
    name: "preview 检查项 passed 非布尔",
    match: p => p.endsWith("/preview"),
    respond: () =>
      previewWith(
        GOOD_CHECKS.map(check =>
          check.name === "recovery_key_reference" ? { ...check, passed: "true" } : check,
        ),
      ),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /不是布尔/,
  },
  {
    name: "preview 已知失败项变成通过（行为可能已实现）",
    match: p => p.endsWith("/preview"),
    respond: () =>
      previewWith(
        GOOD_CHECKS.map(check =>
          check.passed === false ? { name: check.name, passed: true } : check,
        ),
      ),
    want: ["FAIL", "BLOCKED", "BLOCKED"],
    hint: /passed=true != false|实际失败项集合/,
  },
  {
    name: "三个端点全部符合预期",
    match: () => false,
    respond: good,
    want: ["BLOCKED", "BLOCKED", "BLOCKED"],
  },
];

let failures = 0;
for (const scenario of scenarios) {
  const browserFetch = async (_session, _method, path) => (scenario.match(path) ? scenario.respond(path) : good(path));
  const sandbox = {
    console: { log() {}, error() {} },
    results: [],
    env: { ids: {} },
    previewBody: {},
    process: { exitCode: 0 },
    browserFetch,
  };
  vm.createContext(sandbox);
  await vm.runInContext(`${helper}\n(async () => {\n${segment}\n})()`, sandbox);
  const got = sandbox.results.map(item => item.status);
  const hintOk = !scenario.hint || scenario.hint.test(String(sandbox.results[0]?.detail ?? ""));
  const ok = JSON.stringify(got) === JSON.stringify(scenario.want) && hintOk;
  if (!ok) failures += 1;
  console.log(
    `${ok ? "PASS" : "FAIL"} ${scenario.name} -> ${got.join("/")} (want ${scenario.want.join("/")})${
      hintOk ? "" : ` [明细未命中 ${scenario.hint}]`
    }`,
  );
  if (!ok) for (const item of sandbox.results) console.log(`   ${item.id} ${item.status} ${item.detail}`);
}

console.log(failures === 0 ? "\n分类器控制性自测：全部符合预期" : `\n分类器控制性自测失败 ${failures} 项`);
process.exit(failures === 0 ? 0 : 1);
