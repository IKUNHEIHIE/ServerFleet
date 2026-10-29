# 交接说明（gemini-02）

## 运行

```sh
npm ci                       # 仅在干净环境需要；当前目录已有依赖
npm run check:web            # typecheck + vitest + vite build
node scripts/web-boundaries.mjs
npm run dev --workspace @serverfleet/web   # 127.0.0.1:5173
```

真实 Controller 当前只有 `/healthz`。按契约启动管理 API 之前，登录页会明确显示“无法验证管理会话”，不会伪造登录或节点数据。

## GM-01 实际验证

- `npm test --workspace @serverfleet/web`：13 个文件、207 个测试通过（原 179 + 新增 28）。
- `npm run build --workspace @serverfleet/web`：通过。
- `node scripts/web-boundaries.mjs`：PASS（无测试/fixture 生产导入、无 Agent WebSocket）。
- 未做：真实管理 API 联调（后端未提供）、浏览器截图（下一步与 GM-09 合并验收）。

## 已知限制

- 刷新后写操作因 CSRF 契约缺口而锁定；见 `SHARED-CHANGES.md` §1。
- 其余功能页面仍为显式“实施中”状态，不是交付完成；后续检查点逐个替换。
