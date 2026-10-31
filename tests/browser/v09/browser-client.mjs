/**
 * GM 浏览器验收共享 CDP 客户端（Headless Edge / Chrome，仅 127.0.0.1）。
 * 只服务 tests/browser/v09 下的验收脚本；不是产品代码，不进入 dist。
 */
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

export function findBrowserExecutable() {
  const candidates = [
    process.env.SF_BROWSER,
    "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
    "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe",
    "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
    "C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
  ].filter(Boolean);
  return candidates.find(path => {
    try {
      return existsSync(path);
    } catch {
      return false;
    }
  }) ?? null;
}

/** 启动 headless 浏览器（独立临时 profile，不触碰日常浏览器会话），等待 CDP 就绪。 */
export async function launchHeadlessBrowser({ cdpPort, profileDir, width = 1280, height = 900 }) {
  const executable = findBrowserExecutable();
  if (!executable) throw new Error("no Edge/Chrome executable found; set SF_BROWSER");
  await mkdir(profileDir, { recursive: true });
  const child = spawn(executable, [
    "--headless=new",
    `--remote-debugging-port=${cdpPort}`,
    `--user-data-dir=${profileDir}`,
    `--window-size=${width},${height}`,
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-extensions",
    "--disable-background-networking",
    "--disable-sync",
    "--no-service-autorun",
    // 验收用自签证书只用于 127.0.0.1 环回；该开关仅作用于本次独立 profile 进程。
    "--ignore-certificate-errors",
    "about:blank",
  ], { stdio: "ignore", detached: false });
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`http://127.0.0.1:${cdpPort}/json/version`);
      if (response.ok) return { child, executable };
    } catch {
      /* not ready yet */
    }
    await sleep(300);
  }
  child.kill();
  throw new Error(`browser CDP did not come up on 127.0.0.1:${cdpPort}`);
}

/** 连接到第一个 page target。 */
export async function connectPage(cdpPort) {
  const deadline = Date.now() + 15_000;
  let target = null;
  while (Date.now() < deadline && !target) {
    try {
      const response = await fetch(`http://127.0.0.1:${cdpPort}/json`);
      const targets = await response.json();
      target = targets.find(entry => entry.type === "page") ?? null;
    } catch {
      /* retry */
    }
    if (!target) await sleep(300);
  }
  if (!target) throw new Error("no page target on CDP");
  return new CdpSession(target.webSocketDebuggerUrl);
}

export class CdpSession {
  #pending = new Map();
  #id = 0;
  #socket;

  constructor(url) {
    this.#socket = new WebSocket(url);
    this.#socket.addEventListener("message", event => {
      const message = JSON.parse(event.data);
      if (!message.id || !this.#pending.has(message.id)) return;
      const { resolve, reject } = this.#pending.get(message.id);
      this.#pending.delete(message.id);
      if (message.error) reject(new Error(`CDP ${message.error.message}`));
      else resolve(message.result);
    });
  }

  get ready() {
    if (this.#socket.readyState === WebSocket.OPEN) return Promise.resolve();
    if (this.#socket.readyState === WebSocket.CLOSED) return Promise.reject(new Error("CDP socket closed"));
    return new Promise((resolve, reject) => {
      this.#socket.addEventListener("open", () => resolve(), { once: true });
      this.#socket.addEventListener("error", () => reject(new Error("CDP socket error")), { once: true });
    });
  }

  async send(method, params = {}, { timeoutMs = 60_000 } = {}) {
    await this.ready;
    const id = ++this.#id;
    this.#socket.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(new Error(`CDP ${method} timed out after ${timeoutMs}ms`));
      }, timeoutMs);
      this.#pending.set(id, {
        resolve: value => {
          clearTimeout(timer);
          resolve(value);
        },
        reject: error => {
          clearTimeout(timer);
          reject(error);
        },
      });
    });
  }

  close() {
    try {
      this.#socket.close();
    } catch {
      /* already closed */
    }
  }
}

/** 在页面里执行表达式；表达式抛错或返回拒绝的 Promise 都会抛出。 */
export async function evaluate(session, expression) {
  const result = await session.send("Runtime.evaluate", {
    expression,
    returnByValue: true,
    awaitPromise: true,
  });
  if (result.exceptionDetails) {
    const description = result.exceptionDetails.exception?.description ?? JSON.stringify(result.exceptionDetails);
    throw new Error(`page evaluation failed: ${description}`);
  }
  return result.result.value;
}

/** 轮询等待页面内谓词成立；返回谓词的最终值。 */
export async function waitForCondition(session, expression, { timeoutMs = 15_000, intervalMs = 250, label = "condition" } = {}) {
  const deadline = Date.now() + timeoutMs;
  let last = null;
  while (Date.now() < deadline) {
    last = await evaluate(session, expression);
    if (last) return last;
    await sleep(intervalMs);
  }
  throw new Error(`timeout waiting for ${label}`);
}

/** 导航并等待 hash 路由应用完成；同 URL 重复导航也会强制整页刷新（验证真实的刷新恢复路径）。 */
export async function goto(session, url) {
  await session.send("Page.enable");
  await evaluate(session, "location.href");
  const current = await evaluate(session, "location.href").catch(() => "");
  if (current && current.split("#")[0] === url.split("#")[0]) {
    await session.send("Page.navigate", { url: "about:blank" });
    await waitForCondition(session, "document.readyState === 'complete'", { label: "about:blank ready" });
  }
  await session.send("Page.navigate", { url });
  await waitForCondition(session, "document.readyState === 'complete'", { label: `document ready for ${url}` });
  await sleep(400);
}

export async function screenshot(session, path, { width = 1280, height = 900 } = {}) {
  await session.send("Emulation.setDeviceMetricsOverride", {
    width, height, deviceScaleFactor: 1, mobile: width < 600,
  });
  await sleep(500);
  const { data } = await session.send("Page.captureScreenshot", { format: "png" });
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, Buffer.from(data, "base64"));
  return path;
}

/** 候选匹配排序：完全相等 > 首词一致 > 最短包含文本（最具体者胜）。
 * 权限 label 的 textContent 是 key+description 无空格拼接（如 "groups.writeManage …"），
 * 因此不能用首词判定；同含 "groups.write" 的 servicegroups.write 文本更长，最短规则可区分。 */
const MATCH_HELPER = `(candidates, wanted) => {
  const normalized = candidates.map(node => ({ node, text: (node.textContent ?? "").replace(/\\s+/g, " ").trim() }));
  const exact = normalized.filter(entry => entry.text === wanted);
  if (exact.length > 0) return exact[exact.length - 1].node;
  const firstWord = normalized.filter(entry => entry.text.split(" ")[0] === wanted);
  if (firstWord.length > 0) return firstWord[firstWord.length - 1].node;
  const contains = normalized.filter(entry => entry.text.includes(wanted));
  if (contains.length === 0) return null;
  return contains.reduce((best, entry) => (entry.text.length < best.text.length ? entry : best), contains[0]).node;
}`;

/** 页面内辅助：按可见文本查找按钮/链接并点击（React 合成事件由原生 click 触发）。
 * 注意：不能用 offsetParent 判可见——Chrome/Edge 对 position:fixed 容器内的元素返回 null。 */
const CLICK_HELPER = `(text, mode) => {
  const wanted = text.trim();
  const visible = node => {
    const box = node.getBoundingClientRect();
    return box.width > 0 && box.height > 0 && getComputedStyle(node).visibility !== "hidden";
  };
  const nodes = [...document.querySelectorAll("button, a, [role='button']")].filter(visible);
  const target = (${MATCH_HELPER})(nodes, wanted);
  if (!target || (mode === "exact" && (target.textContent ?? "").replace(/\\s+/g, " ").trim() !== wanted)) return false;
  target.scrollIntoView({ block: "center" });
  target.click();
  return true;
}`;

export async function clickText(session, text, { exact = false, timeoutMs = 8_000 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let clicked = false;
  while (Date.now() < deadline) {
    clicked = await evaluate(session, `(${CLICK_HELPER})(${JSON.stringify(text)}, ${exact ? "'exact'" : "'includes'"})`);
    if (clicked) return;
    await sleep(300);
  }
  throw new Error(`no clickable element matching "${text}"`);
}

/** 页面内辅助：按 label 文本填写 input/textarea/select（React 受控组件需要原生 setter）。
 * 首词精确的 label 优先（“密码”不会误撞“确认密码”）；写入后读回校验，select 选项异步渲染时自动重试。 */
const FILL_HELPER = `(labelText, value) => {
  const wanted = labelText.trim();
  const labels = [...document.querySelectorAll("label, legend")].filter(node =>
    (node.textContent ?? "").replace(/\\s+/g, " ").trim().includes(wanted));
  const pick = candidates => {
    const normalized = candidates.map(node => ({ node, text: (node.textContent ?? "").replace(/\\s+/g, " ").trim() }));
    const exact = normalized.filter(entry => entry.text === wanted);
    if (exact.length > 0) return exact[exact.length - 1].node;
    const firstWord = normalized.filter(entry => entry.text.split(" ")[0] === wanted);
    if (firstWord.length > 0) return firstWord[firstWord.length - 1].node;
    const contains = normalized.filter(entry => entry.text.includes(wanted));
    if (contains.length === 0) return null;
    return contains.reduce((best, entry) => (entry.text.length < best.text.length ? entry : best), contains[0]).node;
  };
  const label = labels.length > 0 ? pick(labels) : null;
  let control = null;
  if (label) {
    const id = label.getAttribute("for") ?? label.htmlFor;
    control = (id && document.getElementById(id)) || label.querySelector("input, textarea, select");
  }
  if (!control) return "missing-label";
  const proto = control.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype
    : control.tagName === "SELECT" ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, "value").set;
  setter.call(control, value);
  control.dispatchEvent(new Event("input", { bubbles: true }));
  control.dispatchEvent(new Event("change", { bubbles: true }));
  // 读回校验：select 选项尚未渲染（异步列表）时 value 会是空串，调用方据此重试。
  return control.value === value ? "ok" : "not-stuck";
}`;

export async function fillField(session, labelText, value, { timeoutMs = 8_000 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let last = null;
  while (Date.now() < deadline) {
    last = await evaluate(session, `(${FILL_HELPER})(${JSON.stringify(labelText)}, ${JSON.stringify(value)})`);
    if (last === "ok") return;
    await sleep(300);
  }
  throw new Error(`field "${labelText}" not filled (${last})`);
}/** 页面内辅助：勾选（或取消）包含指定文本的 label 内或表格行内的 checkbox，返回是否发生变更。
 * 弹窗打开时优先匹配弹窗内的候选；首词精确的候选优先（避免 groups.write 撞上 servicegroups.write）。 */
const CHECK_HELPER = `(labelText, checked) => {
  const wanted = labelText.trim();
  const visible = node => {
    const box = node.getBoundingClientRect();
    return box.width > 0 && box.height > 0 && getComputedStyle(node).visibility !== "hidden";
  };
  const boxed = node => {
    if (!visible(node)) return false;
    const box = node.getBoundingClientRect();
    return box.top < window.innerHeight && box.bottom > 0;
  };
  const loose = node => visible(node);
  const pick = (${MATCH_HELPER});
  const collect = withinDialog => {
    // 1) label 包裹的 checkbox（表单场景）
    const labels = [...document.querySelectorAll("label")].filter(node => {
      if (!(withinDialog ? loose(node) : boxed(node))) return false;
      if (withinDialog !== Boolean(node.closest("[role='dialog']"))) return false;
      if (!node.querySelector("input[type='checkbox']")) return false;
      const text = (node.textContent ?? "").replace(/\\s+/g, " ").trim();
      return text.split(" ")[0] === wanted || text.includes(wanted);
    });
    const fromLabel = labels.length > 0 ? pick(labels, wanted)?.querySelector("input[type='checkbox']") : null;
    if (fromLabel) return fromLabel;
    // 2) 表格行内的 checkbox（成员表场景）：按最近行容器文本匹配
    const boxes = [...document.querySelectorAll("input[type='checkbox']")].filter(box => {
      if (!(withinDialog ? loose(box) : boxed(box))) return false;
      if (withinDialog !== Boolean(box.closest("[role='dialog']"))) return false;
      const row = box.closest("tr, li, [role='row']");
      if (!row) return false;
      const text = (row.textContent ?? "").replace(/\\s+/g, " ").trim();
      return text.includes(wanted);
    });
    return boxes.length > 0 ? boxes[boxes.length - 1] : null;
  };
  const target = collect(true) ?? collect(false);
  if (!target) return "missing-label";
  if (target.checked === checked) return "already";
  target.scrollIntoView({ block: "center" });
  target.click();
  return "ok";
}`;

export async function setCheckbox(session, labelText, checked = true, { timeoutMs = 8_000 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let last = null;
  while (Date.now() < deadline) {
    last = await evaluate(session, `(${CHECK_HELPER})(${JSON.stringify(labelText)}, ${checked ? "true" : "false"})`);
    if (last === "ok" || last === "already") return last;
    await sleep(300);
  }
  throw new Error(`checkbox "${labelText}" not set (${last})`);
}

/** 读取“label 文本匹配的 checkbox”当前勾选状态；找不到时返回 null。匹配规则与 setCheckbox 一致。 */
export async function checkboxState(session, labelText) {
  return evaluate(session, `(() => {
    const wanted = ${JSON.stringify(labelText)}.trim();
    const visible = node => {
      const box = node.getBoundingClientRect();
      return box.width > 0 && box.height > 0 && getComputedStyle(node).visibility !== "hidden";
    };
    const labels = [...document.querySelectorAll("label")].filter(node => {
      if (!visible(node)) return false;
      if (!node.querySelector("input[type='checkbox']")) return false;
      const text = (node.textContent ?? "").replace(/\\s+/g, " ").trim();
      return text.split(" ")[0] === wanted || text.includes(wanted);
    });
    const pick = candidates => {
      const normalized = candidates.map(node => ({ node, text: (node.textContent ?? "").replace(/\\s+/g, " ").trim() }));
      const exact = normalized.filter(entry => entry.text === wanted);
      if (exact.length > 0) return exact[exact.length - 1].node;
      const firstWord = normalized.filter(entry => entry.text.split(" ")[0] === wanted);
      if (firstWord.length > 0) return firstWord[firstWord.length - 1].node;
      const contains = normalized.filter(entry => entry.text.includes(wanted));
      if (contains.length === 0) return null;
      return contains.reduce((best, entry) => (entry.text.length < best.text.length ? entry : best), contains[0]).node;
    };
    const label = labels.length > 0 ? pick(labels) : null;
    const box = label?.querySelector("input[type='checkbox']");
    return box ? box.checked : null;
  })()`);
}

/** 读取整页可见文本，供断言与证据输出。 */
export async function bodyText(session) {
  return evaluate(session, "document.body.innerText");
}
