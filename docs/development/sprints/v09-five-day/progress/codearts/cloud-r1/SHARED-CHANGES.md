# V09-CLOUD-HW-01 · 共享差异登记

本文件只登记需要主控协调的中央文件/共享契约差异。未登记前不直接修改共享文件。

## 当前需要主控接收的差异

检查点 1、2 均未修改共享文件：只改 `apps/agent/gateway_capability_test.go` 并新增 `apps/agent/gateway_registration*.go`、`apps/controller/server/gateway_artifact*.go`，均在任务独占范围。

## 中央接线请求（本任务不直接改）

1. **Controller 默认组合**（`apps/controller/server/server.go`）：当前 `server.New` 仍用 `LocalBridge`。需要主控在可信程序配置齐全时调用 `NewProductionPublishingBridge`，并用 `NewGatewayArtifactUploader(<真实 files.UploadTransport>)` 注入 `Artifacts`。缺任一依赖必须保持拒绝，不得回退到测试/本地传输。
2. **Agent 入口**（`apps/agent/runtime/runtime.go`、`apps/agent/connect.go`）：新增 `-gateway-prefix`（绝对私有根，0700，当前用户）与可选 `-nginx-bin` 参数；仅当两者可信时调用 `newGatewayRegistration` 并把返回 actions 注册到 `agentExecutor.actions`。缺配置时不注册、不报告能力。
3. **文件通道生产实现**：`files.AgentFileChannel` 目前只有测试 fake。若要用真实 WSS 文件帧承载网关工件，需要可信程序提供基于 `transport.FileClient` 的适配器和按节点 artifact 根的 `files.PathMapper`；本任务的 Uploader 已按 `files.UploadTransport` 端口解耦，等待该实现注入。
4. **公共迁移/契约**：本检查点未新增表、未改 Proto/gen/OpenAPI；如后续需要 gateway 工件持久绑定表，只提交候选文件与编号请求，不自行占用正式编号。

## 已识别的候选接缝（待实现后给出精确 diff）

1. `apps/controller/server` 中央注册：Agent gateway 能力配置与 GatewayArtifactUploader 需要接入 `server.New`/Agent 启动组合，属中央文件，届时提交精确 patch。
2. 公共迁移编号：若 gateway 工件链需要新表，只提交候选文件与差异，不自行占用正式编号或改历史 checksum。
3. OpenAPI/Proto/gen：本任务不手改生成物；若需契约变化，先交精确候选差异。