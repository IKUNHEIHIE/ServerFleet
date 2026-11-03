# R2 中央装配精确 diff（真实符号，可直接审查）

本文件只给中央/主控接收的接线差异。`apps/controller/server/gateway_*.go`
与 `apps/agent/gateway_*.go` 的实现已在 PR 中，本文件不重复伪代码。

## 1. 迁移（apps/controller/server/server.go `Migrations()`）

R2 在候选表上新增 `site_path`、`state`、`result`、`updated_at` 四列。主控分配正式版本号后加入：

```go
{
    Version:    "cx/00XX_gateway_artifact_bindings", // 主控分配，禁止复用
    Name:       "durable gateway artifact child executions and bindings",
    Statements: []string{gatewayArtifactBindingsSchema},
},
```

`gatewayArtifactBindingsSchema` 在 `gateway_binding.go`，已包含全部列；测试用
`EnsureGatewayArtifactBindings(ctx, db)` 建表。

## 2. Agent 子事件路由（apps/controller/server/agent_links.go）

`agentLinkBridge` 增加字段：

```go
type agentLinkBridge struct {
    // ...existing...
    gatewayChildren *GatewayArtifactObserver
}
```

`event` 开头（第 132-143 行，`fileWorker.Observe` 之前）插入：

```go
if b.gatewayChildren != nil {
    owned, err := b.gatewayChildren.Observe(ctx, observation, envelope)
    if err != nil {
        return err
    }
    if owned {
        return nil
    }
}
```

语义：只有 `gateway_artifact_bindings.execution_id` 命中时才 owned；未命中返回
`false, nil`，继续交给 `FileExecutionWorker.Observe` 与任务路由。子 Ack/Started/Finished
只更新绑定状态，**不调用 `tasks.RecordResult`**，因此不会提前完成父发布任务。

## 3. 构造与赋值（apps/controller/server/server.go）

在 `NewProductionPublishingBridge` 拿到 `dispatcher` 后：

```go
gatewayChildObserver, err := NewGatewayArtifactObserver(db, dispatcher)
if err != nil {
    return err
}
bridge.gatewayChildren = gatewayChildObserver // 或 server 层持有并赋给 agentLinkBridge
```

`agentLinkBridge` 的创建处（`server.New` 内）赋值：

```go
bridge.gatewayChildren = gatewayChildObserver
```

顺序必须是：先建 `dispatcher`，再建 observer/gate/uploader，最后赋回 bridge，
不能反向依赖。

## 4. 文件 Gate 组合（server.New `AgentFileGate`）

```go
gatewayGate, err := NewGatewayArtifactGate(db, dispatcher)
if err != nil {
    return err
}
fileGate := cfg.AgentFileGate
cfg.AgentFileGate = func(ctx context.Context, o transport.LinkObservation,
    offer *agentv1.TaskOffer, e *agentv1.Envelope) error {
    if offer != nil && offer.ActionId == transport.FileCapability {
        owned, matchErr := gatewayGate.Matches(ctx, offer.ExecutionId)
        if matchErr != nil {
            return matchErr
        }
        if owned {
            return gatewayGate.Authorize(ctx, o, offer, e)
        }
    }
    if fileGate == nil {
        return files.ErrTaskBindingRequired
    }
    return fileGate(ctx, o, offer, e)
}
```

## 5. Uploader 与 dispatcher（`ProductionPublishingConfig`）

```go
Channel      files.AgentFileChannel // NewGatewayWSSChannel(s.AgentFiles)
GatewayRoots map[string]string      // nodeID -> Agent -file-root 绝对路径
```

```go
dispatcher, err := newGatewayTaskDispatcher(... Artifacts: gatewayUnboundUploader{}, ...)
uploader, err := NewGatewayChannelUploader(config.Channel, config.GatewayRoots, config.DB, dispatcher)
dispatcher.artifacts = uploader
```

`gatewayUnboundUploader.Upload` 返回 `files.ErrTaskBindingRequired`，仅占位，确保
构造期间也 fail closed。

## 6. Agent 入口（同 R1）

`-gateway-prefix`（绝对私有 0700 根）、`-nginx-bin`；`-file-root` 必须包含 gateway prefix。
注册：

```go
actions, err := newGatewayRegistration(gatewayRegistrationConfig{
    Prefix: request.GatewayPrefix, NginxBinary: request.GatewayNginxBinary,
})
for name, action := range actions {
    executor.actions[name] = action
    semantic.AllowedActions[name] = gatewayActionSchemaVersion
}
```

## 7. 组合验证（应用上述接线后）

1. 迁移后确认 `gateway_artifact_bindings` 存在且含 `site_path/state/result/updated_at`。
2. 启动 Agent：`-file-root=/srv/gateway -gateway-prefix=/srv/gateway -nginx-bin=...`。
3. Controller：`GatewayRoots{"node-one":"/srv/gateway"}`。
4. 提交 plan/任务并完成独立审批，触发 `publish`。
5. 期望：工件子执行使用 `gwa-...` execution（与 `exec-...` Nginx 动作不同）；
   Agent ledger 接受两个 execution；`nginx -t`/reload 成功；公开 HTTP 返回站点字节；
   子 Finished 不改变父任务状态；父任务只在 Nginx 结果后进入终态。
6. 负向：上传中撤权/取消停止后续块；子 Finished 早于 Accepted 记 UNKNOWN；
   旧绑定在任务 Revise + 新审批后仍被 `CheckArtifactStage` 拒绝。

## 8. 当前未完成（保持 OPEN）

- 上述接线属中央文件，本任务未修改；因此**跨进程完整组合尚未在本环境启动**。
  当前证据为：真实 `files.ChannelTransport` 绑定/负向、observer 生命周期、
  Agent 侧真实 WSS+Nginx+HTTP 用例（`TestActualWSSGatewayApplyReloadAndHTTPBytes`）。
- `check:web` 与 `npm run check` 全量未跑。
- ACME/cleanup 产品组合未完成。