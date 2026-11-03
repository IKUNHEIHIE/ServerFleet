# R2/R3 中央装配候选 diff（真实符号与完整上下文）

所有片段使用本 PR 已存在的真实类型/函数，不含 `gatewayUnboundUploader` 或省略参数。
共享文件由主控独立接收；本任务不直接修改。

## 1. 迁移（apps/controller/server/server.go）

`GatewayArtifactMigrations()` 在 `gateway_binding.go` 返回完整 migration。主控分配版本号后：

```go
// 修改 gateway_binding.go 中 GatewayArtifactMigrations() 的 Version 为分配的
// "cx/<number>_gateway_artifact_bindings"（唯一、不可复用）。

// server.go:204 之前（Migrations() 内，return 前）加入：
cxMigrations = append(cxMigrations, GatewayArtifactMigrations()...)
return [][]storage.Migration{
    cxMigrations,
    // ...existing...
}
```

`gatewayArtifactBindingsSchema` 已含 `execution_id,task_id,node_id,transfer_id,plan_digest,
gateway_id,plan_id,target_revision,absolute_path,relative_path,site_path,size,sha256,
state,result,created_at,updated_at`。

## 2. `ProductionPublishingConfig`（gateway_tasks.go:51-72）

在现有字段后增加：

```go
Channel      files.AgentFileChannel // 由 NewGatewayWSSChannel(s.AgentFiles) 构造
GatewayRoots map[string]string      // nodeID -> Agent -file-root 绝对路径
```

## 3. `NewProductionPublishingBridge`（gateway_tasks.go:78）

替换 dispatcher 构造段（当前 86-95 行）：

```go
registry := newGatewayArtifactRegistry()
authority := publicationPlanAuthority{repo: config.Plans}
stageSlot := &GatewayArtifactStageSlot{}
uploader, err := NewGatewayChannelUploader(config.Channel, config.GatewayRoots, config.DB, stageSlot)
if err != nil {
    return publishing.LocalBridge{}, nil, err
}
dispatcher, err := newGatewayTaskDispatcher(gatewayExecutionConfig{
    Repo: config.TasksRepo, DB: config.DB, Plans: config.Plans,
    Artifacts: uploader, Next: config.NextDispatcher, Authorizer: config.Authorizer,
    Nodes: config.Nodes, Registry: registry, Now: config.Now, ApprovalTTL: config.ApprovalTTL,
})
if err != nil {
    return publishing.LocalBridge{}, nil, err
}
if err := stageSlot.Bind(dispatcher); err != nil {
    return publishing.LocalBridge{}, nil, err
}
```

`config.Artifacts` 可保留旧字段供测试，但生产必须传 `Channel` 并使
`NewGatewayChannelUploader` 成功；`StageSlot` 未 Bind 前所有检查返回
`files.ErrTaskBindingRequired`，不存在占位放行。

## 4. `agentLinkBridge`（agent_links.go:50-62）

```go
type agentLinkBridge struct {
    // ...existing...
    fileWorker      *FileExecutionWorker
    gatewayChildren *GatewayArtifactObserver
    now             func() time.Time
    // ...
}
```

`event`（agent_links.go:132，函数体第一行之前）：

```go
func (b *agentLinkBridge) event(ctx context.Context, observation transport.LinkObservation, envelope *agentv1.Envelope) error {
    if b == nil || envelope == nil || envelope.GetNodeId() != observation.Identity.NodeID {
        return transport.ErrLinkRejected
    }
    if b.gatewayChildren != nil {
        owned, err := b.gatewayChildren.Observe(ctx, observation, envelope)
        if err != nil {
            return err
        }
        if owned {
            return nil
        }
    }
    fileChild := false
    // ...existing fileWorker/files/task routing unchanged...
```

## 5. server.New 赋值（server.go:669 与 741 附近）

```go
agentBridge.fileWorker = s.FileWorker
```

之后增加（需要 publishing dispatcher 已构造；若 server.New 未构造发布桥，则暂不赋值，
网关子事件保持未启用，能力关闭）：

```go
agentBridge.gatewayChildren, err = NewGatewayArtifactObserver(db, gatewayDispatcher)
if err != nil {
    return cleanup(err)
}
```

`gatewayDispatcher` 是 `NewProductionPublishingBridge` 返回的 `tasks.Dispatcher` 具体值
（`*gatewayTaskDispatcher`）；生产组合应由 server 持有该返回值，避免第二份 dispatcher。

## 6. 文件 Gate 组合（server.go:629-638）

替换：

```go
if cfg.FileExecution != nil {
    if cfg.AgentFileGate != nil || cfg.EnrollmentIssuer == nil || cfg.AgentAuthority == "" {
        return cleanup(files.ErrTransferExecution)
    }
    s.FileWorker, err = NewFileExecutionWorker(s, filesRepo, authorizer, *cfg.FileExecution)
    if err != nil {
        return cleanup(err)
    }
    cfg.AgentFileGate = s.FileWorker.Gate
}
```

为（gatewayGate 需在 dispatcher 构造后创建）：

```go
if cfg.FileExecution != nil {
    if cfg.AgentFileGate != nil || cfg.EnrollmentIssuer == nil || cfg.AgentAuthority == "" {
        return cleanup(files.ErrTransferExecution)
    }
    s.FileWorker, err = NewFileExecutionWorker(s, filesRepo, authorizer, *cfg.FileExecution)
    if err != nil {
        return cleanup(err)
    }
    gatewayGate, gateErr := NewGatewayArtifactGate(db, gatewayDispatcher)
    if gateErr != nil {
        return cleanup(gateErr)
    }
    cxGate := s.FileWorker.Gate
    cfg.AgentFileGate = func(ctx context.Context, o transport.LinkObservation, offer *agentv1.TaskOffer, e *agentv1.Envelope) error {
        if offer != nil && offer.ActionId == transport.FileCapability {
            owned, matchErr := gatewayGate.Matches(ctx, offer.ExecutionId)
            if matchErr != nil {
                return matchErr
            }
            if owned {
                return gatewayGate.Authorize(ctx, o, offer, e)
            }
        }
        return cxGate(ctx, o, offer, e)
    }
}
```

## 7. Agent 入口（apps/agent/runtime/runtime.go:63-73 与 connect.go）

`ConnectRequest` 增加：

```go
GatewayPrefix      string
GatewayNginxBinary string
```

`connect.go`（`newAgentExecutor` 返回之后）：

```go
if request.GatewayPrefix != "" {
    actions, regErr := newGatewayRegistration(gatewayRegistrationConfig{
        Prefix: request.GatewayPrefix, NginxBinary: request.GatewayNginxBinary,
    })
    if regErr != nil {
        return regErr
    }
    for name, action := range actions {
        executor.actions[name] = action
        semantic.AllowedActions[name] = gatewayActionSchemaVersion
    }
}
```

`-file-root` 必须包含 `-gateway-prefix`；Controller 的 `GatewayRoots` 必须等于该 `-file-root`。

## 8. 验证（应用后）

1. 迁移存在含 `site_path/state/result/updated_at`。
2. `gwa-` 子执行与 `exec-` apply 在 Agent ledger 均被接受（负向：同 ID 不同动作拒绝）。
3. 子 Ack/Started/Finished 只更新 `gateway_artifact_bindings`，父任务状态不变。
4. 迟到/并发/重复事件后 `state/result` 保持 finished/终态；UNKNOWN 不升级。
5. `nginx -t`/reload 成功，公开 HTTP 返回站点字节，父任务只在 Nginx 结果后终态。
6. 取消/撤权/Revise+新审批：旧绑定拒绝。

## 9. 未完成

完整跨进程组合需应用本文件后启动；本任务未伪造该证据，保持 UNTESTED。