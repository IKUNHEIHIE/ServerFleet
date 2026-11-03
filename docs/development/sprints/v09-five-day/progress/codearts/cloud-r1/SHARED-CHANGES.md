# V09-CLOUD-HW-01 · 共享差异登记

本文件只登记需要主控协调的中央文件/共享契约差异。未登记前不直接修改共享文件。

## 当前需要主控接收的差异

检查点 1-3 均未修改中央共享文件。R1 新增的 `gateway_wss.go`、`gateway_binding.go` 与
`gateway_worker.go` 改动均在 `apps/controller/server/gateway_*` 独占范围。

## 中央接线（精确补丁见 `CENTRAL-PATCH-R1.md`）

1. **迁移**：把 `gatewayArtifactBindingsSchema` 以主控分配的正式 `cx/....` 版本加入
   `Migrations()`；本任务的候选版本 `cx/CANDIDATE_gateway_artifact_bindings` 未注册。
2. **Controller**：`Config` 增加 `GatewayFileRoots map[nodeID]Agent 的 -file-root 绝对路径`；
   `NewProductionPublishingBridge` 增加 `Channel files.AgentFileChannel`。先建 dispatcher，
   再 `NewGatewayChannelUploader(channel, roots, db, dispatcher)`，最后赋回
   `dispatcher.artifacts`，避免 stage/uploader 循环依赖。
3. **文件 Gate**：`NewGatewayArtifactGate(db, dispatcher)`；在 `AgentFileGate` 组合中先用
   `Matches(executionID)` 判定归属，命中走网关 Gate，否则回落 CX 文件 Gate；两者都为空拒绝。
4. **Agent 入口**：新增 `-gateway-prefix` / `-nginx-bin` 并调用 `newGatewayRegistration`；
   `-file-root` 必须包含 gateway prefix，Controller 的 roots 必须与之一致。
5. 本任务**不再等待** AgentMore 提供 ArtifactUploader：真实 WSS 通道适配、scope/逐块授权、
   持久绑定与 gate 已在本任务实现。

## 已识别的候选接缝（待实现后给出精确 diff）

1. `apps/controller/server` 中央注册：Agent gateway 能力配置与 GatewayArtifactUploader 需要接入 `server.New`/Agent 启动组合，属中央文件，届时提交精确 patch。
2. 公共迁移编号：若 gateway 工件链需要新表，只提交候选文件与差异，不自行占用正式编号或改历史 checksum。
3. OpenAPI/Proto/gen：本任务不手改生成物；若需契约变化，先交精确候选差异。