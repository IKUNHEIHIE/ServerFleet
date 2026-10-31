/**
 * 最小 CDP 驱动，只用于 GM-09 本地浏览器验收（Chrome headless，127.0.0.1:9223）。
 * 不是产品代码。用法：
 *   node cdp-driver.mjs goto <url>
 *   node cdp-driver.mjs eval <expression>
 *   node cdp-driver.mjs shot <absolute-or-relative.png>
 *   node cdp-driver.mjs viewport <width> <height>
 *   node cdp-driver.mjs wait <ms>
 */
const [, , command, ...rest] = process.argv;

async function pageTarget() {
  const response = await fetch("http://127.0.0.1:9223/json");
  const targets = await response.json();
  const page = targets.find(target => target.type === "page");
  if (!page) throw new Error("no page target; launch chrome with --remote-debugging-port=9223");
  return page.webSocketDebuggerUrl;
}

function connect(url) {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(url);
    const pending = new Map();
    let id = 0;
    socket.addEventListener("open", () => resolve({
      send(method, params = {}) {
        const messageId = ++id;
        socket.send(JSON.stringify({ id: messageId, method, params }));
        return new Promise((res, rej) => {
          pending.set(messageId, { res, rej });
        });
      },
      close() {
        socket.close();
      },
    }));
    socket.addEventListener("error", reject);
    socket.addEventListener("message", event => {
      const message = JSON.parse(event.data);
      if (!message.id || !pending.has(message.id)) return;
      const { res, rej } = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) rej(new Error(message.error.message));
      else res(message.result);
    });
  });
}

const client = await connect(await pageTarget());

if (command === "goto") {
  await client.send("Page.enable");
  await client.send("Page.navigate", { url: rest[0] });
  await new Promise(done => setTimeout(done, 2500));
  const result = await client.send("Runtime.evaluate", { expression: "document.title + ' | ' + location.hash", returnByValue: true });
  console.log(result.result.value);
} else if (command === "eval") {
  let expression = rest.join(" ");
  const sizeIndex = rest.indexOf("--at");
  if (sizeIndex >= 0) {
    await client.send("Emulation.setDeviceMetricsOverride", { width: Number(rest[sizeIndex + 1]), height: Number(rest[sizeIndex + 2]), deviceScaleFactor: 1, mobile: Number(rest[sizeIndex + 1]) < 600 });
    await new Promise(done => setTimeout(done, 900));
    expression = rest.slice(0, sizeIndex).join(" ");
  }
  const result = await client.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
  if (result.exceptionDetails) {
    console.error(JSON.stringify(result.exceptionDetails.exception?.description ?? result.exceptionDetails));
    process.exitCode = 1;
  } else {
    console.log(typeof result.result.value === "string" ? result.result.value : JSON.stringify(result.result.value));
  }
} else if (command === "shot") {
  if (rest[1] && rest[2]) {
    await client.send("Emulation.setDeviceMetricsOverride", { width: Number(rest[1]), height: Number(rest[2]), deviceScaleFactor: 1, mobile: Number(rest[1]) < 600 });
    await new Promise(done => setTimeout(done, 900));
  }
  const { data } = await client.send("Page.captureScreenshot", { format: "png" });
  const { writeFile } = await import("node:fs/promises");
  await writeFile(rest[0], Buffer.from(data, "base64"));
  console.log(`saved ${rest[0]}`);
} else if (command === "viewport") {
  await client.send("Emulation.setDeviceMetricsOverride", {
    width: Number(rest[0]),
    height: Number(rest[1]),
    deviceScaleFactor: 1,
    mobile: Number(rest[0]) < 600,
  });
  console.log(`viewport ${rest[0]}x${rest[1]}`);
} else if (command === "wait") {
  await new Promise(done => setTimeout(done, Number(rest[0] ?? "1000")));
  console.log("ok");
} else {
  console.error("unknown command");
  process.exitCode = 2;
}

client.close();
