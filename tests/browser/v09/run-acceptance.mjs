/**
 * GM-09 桩验收自动化（Wave 3 · V09-5D-GM）。
 *
 * 与 `run-live-controller-acceptance.mjs` 的分工：
 * - 本脚本针对**候选契约桩**（stub-server.mjs + fixtures.mjs）回归已交付前端的会话、
 *   错误分类与 SSE 行为（对应 Wave 1 GM-09 的手工流程），不依赖 Go 工具链。
 * - 真实 Go Controller + SQLite WAL 的端到端验收见 `run-live-controller-acceptance.mjs`。
 *
 * 桩只监听 127.0.0.1；页面、适配器、会话与 SSE 代码全部是生产构建（apps/web/dist）。
 * 生产请求失败只显示错误分类，绝不回退桩数据。
 *
 * 用法：
 *   node tests/browser/v09/run-acceptance.mjs [--out <截图目录>] [--skip-build] [--keep]
 */
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import {
  clickText,
  connectPage,
  evaluate,
  fillField,
  goto,
  launchHeadlessBrowser,
  screenshot,
  waitForCondition,
} from "./browser-client.mjs";

const here = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(here, "../../..");
/** 与当前 node 同目录的 npm-cli.js（env.ps1 的内置工具链）。Windows 下 spawn .cmd 需要 shell，
 * 直接用 node 调 npm-cli.js 更稳。 */
function npmBuildCommand() {
  const nodeDir = dirname(process.execPath);
  const npmCli = join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js");
  return existsSync(npmCli) ? [process.execPath, [npmCli]] : null;
}

const args = process.argv.slice(2);
function argValue(name, fallback) {
  const index = args.indexOf(`--${name}`);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
}
const hasFlag = name => args.includes(`--${name}`);
const outDir = resolve(argValue("out", join(repoRoot, "docs", "development", "sprints", "v09-five-day", "progress", "gemini", "evidence", "stub")));
const skipBuild = hasFlag("skip-build");
const keepTemp = hasFlag("keep");

const stubPort = 51800 + Math.floor(Math.random() * 900);
const cdpPort = 52230 + Math.floor(Math.random() * 500);
const baseUrl = `http://127.0.0.1:${stubPort}`;
const results = [];

function record(step, ok, detail = "") {
  results.push({ step, ok, detail });
  console.log(`${ok ? "PASS" : "FAIL"}  ${step}${detail ? ` — ${detail}` : ""}`);
}

async function expectText(session, needle, step, timeoutMs = 15_000) {
  try {
    await waitForCondition(
      session,
      `document.body.innerText.includes(${JSON.stringify(needle)})`,
      { timeoutMs, label: `text "${needle}"` },
    );
    record(step, true);
    return true;
  } catch (error) {
    record(step, false, error.message);
    return false;
  }
}

async function stubControl(path, method = "POST", body) {
  const response = await fetch(`${baseUrl}${path}`, {
    method,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`stub control ${method} ${path} -> ${response.status}`);
  return response.json().catch(() => null);
}

async function waitForServer(url, label, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      /* retry */
    }
    await sleep(300);
  }
  throw new Error(`${label} did not become ready at ${url}`);
}

let tempDir = null;
const children = [];
function track(child) {
  children.push(child);
  return child;
}

async function main() {
  if (!skipBuild) {
    console.log("building production bundle (npm run build --workspace @serverfleet/web)...");
    const npm = npmBuildCommand();
    if (!npm) throw new Error("npm-cli.js not found next to the running node; build apps/web manually and pass --skip-build");
    const build = track(spawn(npm[0], [...npm[1], "run", "build", "--workspace", "@serverfleet/web"], {
      cwd: repoRoot, stdio: ["ignore", "inherit", "inherit"], shell: false,
    }));
    const buildCode = await new Promise((resolveCode, reject) => {
      build.on("exit", resolveCode);
      build.on("error", reject);
    });
    if (buildCode !== 0) throw new Error(`web build failed with exit code ${buildCode}`);
  } else if (!existsSync(resolve(repoRoot, "apps/web/dist/index.html"))) {
    throw new Error("--skip-build given but apps/web/dist is missing; run the build first");
  }

  tempDir = await mkdtemp(join(tmpdir(), "gm09-stub-"));
  console.log(`stub evidence out dir: ${outDir}`);

  const stub = track(spawn(process.execPath, [join(here, "stub-server.mjs"), "--port", String(stubPort)], {
    cwd: here, stdio: ["ignore", "pipe", "pipe"],
  }));
  stub.stdout.on("data", () => {});
  stub.stderr.on("data", chunk => process.stderr.write(`[stub] ${chunk}`));
  await waitForServer(`${baseUrl}/healthz`, "stub server");

  const browser = await launchHeadlessBrowser({
    cdpPort, profileDir: join(tempDir, "profile"),
  });
  track(browser.child);
  const session = await connectPage(cdpPort);

  // 1. 匿名访问 → 登录表单
  await goto(session, `${baseUrl}/#/overview`);
  await expectText(session, "登录", "01 anonymous visit shows the login form");
  await fillField(session, "用户名", "admin");
  await fillField(session, "密码", "accept123");
  await screenshot(session, join(outDir, "stub-01-login.png"));
  await clickText(session, "登录", { exact: true });

  // 2. 登录成功 → 概览（真实表单提交、生产构建渲染）
  await expectText(session, "概览", "02 login lands on overview");
  await expectText(session, "Controller 仍为工程骨架", "03 overview honestly reports scaffold healthz");
  await screenshot(session, join(outDir, "stub-02-overview.png"));

  // 3. 刷新 → /auth/me 无 CSRF → 写锁定（契约缺口必须显式可见）
  await goto(session, `${baseUrl}/#/overview`);
  await expectText(session, "安全写入已锁定", "04 reload locks writes without CSRF token");
  await screenshot(session, join(outDir, "stub-03-writes-locked.png"));

  // 4. 重新登录解除写锁（刷新后会话仍有效，提交按钮为“替换当前会话并登录”）
  await clickText(session, "重新登录以获取 CSRF 令牌");
  await expectText(session, "登录", "05 re-login link returns to the login form");
  await fillField(session, "用户名", "admin");
  await fillField(session, "密码", "accept123");
  await clickText(session, "登录");
  await expectText(session, "概览", "06 re-login restores write capability");
  const lockedAfterRelogin = await evaluate(session, "document.body.innerText.includes('安全写入已锁定')");
  record("07 write lock cleared after re-login", lockedAfterRelogin === false);

  // 5. 节点页：在线 0% 与离线无读数（W07 规则）
  await goto(session, `${baseUrl}/#/nodes`);
  await expectText(session, "节点", "08 nodes page loads");
  const nodeRules = await evaluate(session, `(() => {
    const text = document.body.innerText;
    return { hasOnlineZero: text.includes("0%"), hasOfflineNotice: text.includes("离线") };
  })()`);
  record("09 node metric rules rendered", nodeRules.hasOnlineZero && nodeRules.hasOfflineNotice, JSON.stringify(nodeRules));
  await screenshot(session, join(outDir, "stub-04-nodes.png"));

  // 6. 任务 SSE：事件到达且秘密输出永不进入 DOM
  await goto(session, `${baseUrl}/#/tasks/task-1`);
  await expectText(session, "实时事件", "10 task detail opens live events panel");
  const sse = await waitForCondition(session, `(() => {
    const text = document.body.innerText;
    return /游标：[1-9]/.test(text) && text.includes("task.state") && !text.includes("SECRET-STUB-OUTPUT-MUST-NOT-RENDER");
  })()`, { timeoutMs: 20_000, label: "SSE cursor and projected events" });
  record("11 SSE cursor advanced with projected events", sse === true);
  const secretLeak = await evaluate(session, "document.body.innerText.includes('SECRET-STUB-OUTPUT-MUST-NOT-RENDER')");
  record("12 raw SSE payload never rendered", secretLeak === false);
  await screenshot(session, join(outDir, "stub-05-task-sse.png"));

  // 7. 503：会话保留 + 可重试错误（不回退假数据）
  await stubControl("/__stub/faults", "POST", { match: "/api/v1/nodes", status: 503, times: 1, code: "UNAVAILABLE" });
  await goto(session, `${baseUrl}/#/nodes`);
  await expectText(session, "Controller 暂时不可用", "13 503 keeps session and shows retryable error");
  await screenshot(session, join(outDir, "stub-06-503.png"));
  await stubControl("/__stub/faults", "DELETE");

  // 8. 403：越权读取只清当前视图，不登出
  await stubControl("/__stub/faults", "POST", { match: "/api/v1/nodes", status: 403, times: 1, code: "FORBIDDEN" });
  await goto(session, `${baseUrl}/#/nodes`);
  await expectText(session, "没有执行此操作或读取此资源的权限", "14 403 shows forbidden without logout", 20_000);
  await stubControl("/__stub/faults", "DELETE");
  await screenshot(session, join(outDir, "stub-07-403.png"));

  // 9. 服务端撤销会话 → 受保护 API 返回 401，刷新后回到登录页
  await stubControl("/__stub/session/invalidate", "POST");
  const unauthorizedAfterRevoke = await evaluate(session, `fetch("/api/v1/nodes", {
    headers: { "X-Requested-With": "ServerFleetWeb" }, credentials: "include", cache: "no-store",
  }).then(response => response.status)`);
  record("15a revoked session gets 401 from protected API", unauthorizedAfterRevoke === 401, `status=${unauthorizedAfterRevoke}`);
  await goto(session, `${baseUrl}/#/nodes`);
  await expectText(session, "登录", "15 revoked session returns to the login page", 20_000);
  await screenshot(session, join(outDir, "stub-08-session-revoked.png"));

  // 10. 移动端视口冒烟
  await screenshot(session, join(outDir, "stub-09-mobile-420x900.png"), { width: 420, height: 900 });

  await session.close();
}

try {
  await main();
  const failed = results.filter(entry => !entry.ok);
  console.log(`\nstub acceptance: ${results.length - failed.length}/${results.length} steps passed`);
  if (failed.length > 0) {
    console.log("failed steps:");
    for (const entry of failed) console.log(`  - ${entry.step}: ${entry.detail}`);
    process.exitCode = 1;
  }
} catch (error) {
  console.error(`stub acceptance aborted: ${error.stack ?? error}`);
  process.exitCode = 1;
} finally {
  for (const child of children) {
    try {
      child.kill();
    } catch {
      /* already gone */
    }
  }
  if (tempDir && !keepTemp) {
    await rm(tempDir, { recursive: true, force: true }).catch(() => {});
  }
}
