# V09-CLOUD-HW-01 · 共享差异登记

本文件只登记需要主控协调的中央文件/共享契约差异。未登记前不直接修改共享文件。

## 当前需要主控接收的差异

检查点 1-3 均未修改中央共享文件。R1 新增的 `gateway_wss.go`、`gateway_binding.go` 与
`gateway_worker.go` 改动均在 `apps/controller/server/gateway_*` 独占范围。

## 中央接线（精确补丁见 `CENTRAL-PATCH-R2.md`）

1. **迁移**：R2 表新增 `site_path/state/result/updated_at`；`gatewayArtifactBindingsSchema`
   以主控正式版本加入 `Migrations()`。
2. **Agent 子事件路由**（`agent_links.go`）：`agentLinkBridge` 增加
   `gatewayChildren *GatewayArtifactObserver`；在 `event` 的 `fileWorker.Observe` 前调用
   `Observe`，owned 即返回，未命中继续原路由。子事件不调用 `tasks.RecordResult`。
3. **Controller**：`Config` 增加 `GatewayFileRoots` 与 `Channel`；先建 dispatcher，再建
   observer/gate/uploader，最后赋回 bridge/dispatcher。
4. **文件 Gate**：`NewGatewayArtifactGate(db, dispatcher)` 先 `Matches` 再 `Authorize`，
   未命中回落 CX 文件 Gate，两者都空拒绝。
5. **Agent 入口**：`-gateway-prefix`/`-nginx-bin`，`-file-root` 必须包含 gateway prefix。
6. 本任务不修改中央文件；上述 patch 由主控独立接收并组合验证。

## 已识别的候选接缝（待实现后给出精确 diff）

1. `apps/controller/server` 中央注册：Agent gateway 能力配置与 GatewayArtifactUploader 需要接入 `server.New`/Agent 启动组合，属中央文件，届时提交精确 patch。
2. 公共迁移编号：若 gateway 工件链需要新表，只提交候选文件与差异，不自行占用正式编号或改历史 checksum。
3. OpenAPI/Proto/gen：本任务不手改生成物；若需契约变化，先交精确候选差异。