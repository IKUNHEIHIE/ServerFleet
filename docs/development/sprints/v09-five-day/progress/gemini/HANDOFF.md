# 交接说明（gemini-02）

## 运行

```sh
npm ci                       # 仅在干净环境需要；当前目录已有依赖
npm run check:web            # typecheck + vitest + vite build
node scripts/web-boundaries.mjs
npm run dev --workspace @serverfleet/web   # 127.0.0.1:5173
```

真实 Controller 当前只有 `/healthz`。按契约启动管理 API 之前，登录页会明确显示“无法验证管理会话”，不会伪造登录或节点数据。

## 实际验证（累计，截至 GM-08）

- `npm run typecheck --workspace @serverfleet/web`：通过（`tsc --noEmit` 无错误）。
- `npm test --workspace @serverfleet/web`：34 个文件、282 个测试通过（GM-01 207 → GM-02/03 235 → GM-04 253 → GM-05 259 → GM-06 266 → GM-07 275 → GM-08 282）。
- `npm run build --workspace @serverfleet/web`：通过（`dist/assets/index-*.js` 约 612 kB，gzip 170 kB；未做代码分割）。
- `node scripts/web-boundaries.mjs`：PASS（无测试/fixture 生产导入、无 Agent WebSocket）。
- 测试中的 HTTP 全部来自 `apps/web/src/test/http.ts` 的 fetch 桩，用于在缺少真实管理 API 时验证前端行为；**不是生产回退数据**，生产代码失败时只显示错误分类。
- 未做：真实管理 API 联调（后端只有 `/healthz`）、浏览器端到端证据（GM-09）。

## 已知限制

- 刷新后写操作因 CSRF 契约缺口而锁定；见 `SHARED-CHANGES.md` §1。
- `/updates`、`/backups` 已接线；`PendingPage` 占位组件已删除，导航中不再有“实施中”页面。
- 工作流、公网证书、文件上传字节通道/下载、资源历史指标、AI 会话列表等为契约缺口，页面显示 BLOCKED 而不伪造功能；逐项见 `SHARED-CHANGES.md` §2–§10。
- 插件/Xray、DNS、证书、网关、发布等页面的真实供应商与节点行为均 UNTESTED，需后端与真实环境。
