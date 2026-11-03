# R1 中央接线精确补丁（Controller / Agent 入口）

本文件给出可直接应用的接线步骤。所有被改文件均属中央/其他作者范围，本任务不直接修改。

## 1. 迁移注册（apps/controller/server/server.go）

在 `Migrations()` 的 `cxMigrations` 列表中分配正式版本号后加入：

```go
{
    Version:    "cx/00XX_gateway_artifact_bindings", // 由主控分配，禁止复用
    Name:       "durable gateway artifact task/execution/transfer/plan bindings",
    Statements: []string{gatewayArtifactBindingsSchema},
},
```

`gatewayArtifactBindingsSchema` 与 `GatewayArtifactMigrations()` 已存在于
`apps/controller/server/gateway_binding.go`；也可调用
`EnsureGatewayArtifactBindings(ctx, db)` 做显式 bootstrap（测试用）。

## 2. Controller 组合（apps/controller/server/gateway_tasks.go + server.go）

`ProductionPublishingConfig` 增加：

```go
Channel       files.AgentFileChannel // NewGatewayWSSChannel(s.AgentFiles)
GatewayRoots  map[string]string      // nodeID -> Agent -file-root 绝对路径
```

`NewProductionPublishingBridge` 内部按此顺序构造，避免循环依赖：

```go
dispatcher, err := newGatewayTaskDispatcher(gatewayExecutionConfig{
    Repo: config.TasksRepo, DB: config.DB, Plans: config.Plans,
    Artifacts: gatewayUnboundUploader{}, // 仅占位，Upload 返回 ErrTaskBindingRequired
    Next: config.NextDispatcher, Authorizer: config.Authorizer,
    Nodes: config.Nodes, Registry: registry, Now: config.Now, ApprovalTTL: config.ApprovalTTL,
})
if err != nil { return publishing.LocalBridge{}, nil, err }

uploader, err := NewGatewayChannelUploader(config.Channel, config.GatewayRoots, config.DB, dispatcher)
if err != nil { return publishing.LocalBridge{}, nil, err }
dispatcher.artifacts = uploader
```

若希望完全避免占位，可将 `newGatewayTaskDispatcher` 的 `Artifacts` 必填校验后移到
`dispatcher.artifacts = uploader` 之后再断言一次；两种方式都必须保持“无真实 uploader 即拒绝”。

`gatewayUnboundUploader`：

```go
type gatewayUnboundUploader struct{}
func (gatewayUnboundUploader) Upload(context.Context, string, string, string, []byte, string) error {
    return files.ErrTaskBindingRequired
}
```

`server.New` 传入：

```go
Channel:      gatewayChannel,        // NewGatewayWSSChannel(s.AgentFiles)
GatewayRoots: cfg.GatewayFileRoots,
```

并在 `Config` 增加 `GatewayFileRoots map[string]string`（operator 配置，必须与 Agent
`-file-root` 完全一致；缺失该节点即拒绝上传，不回退本地目录）。

## 3. 文件帧 Gate 组合（server.New 中 AgentFileGate 处）

```go
gatewayGate, err := NewGatewayArtifactGate(db, pubDispatcher)
if err != nil { return cleanup(err) }
fileGate := cfg.AgentFileGate
cfg.AgentFileGate = func(ctx context.Context, o transport.LinkObservation,
    offer *agentv1.TaskOffer, e *agentv1.Envelope) error {
    if offer != nil && offer.ActionId == transport.FileCapability {
        owned, matchErr := gatewayGate.Matches(ctx, offer.ExecutionId)
        if matchErr != nil { return matchErr }
        if owned { return gatewayGate.Authorize(ctx, o, offer, e) }
    }
    if fileGate == nil { return files.ErrTaskBindingRequired }
    return fileGate(ctx, o, offer, e)
}
```

要求：先判定网关持久绑定，命中即走网关 Gate；未命中才交给 CX 文件 Gate；两者都为空时拒绝。

## 4. Agent 入口（apps/agent/runtime/runtime.go + connect.go）

`ConnectRequest` 增加：

```go
GatewayPrefix      string
GatewayNginxBinary string
```

CLI：

```go
gatewayPrefix := flags.String("gateway-prefix", "", "absolute private gateway nginx prefix (0700)")
gatewayBin := flags.String("nginx-bin", "", "optional absolute nginx binary; defaults to PATH nginx")
```

`connect.go` 中注册（仅在两个字段均有效时）：

```go
if request.GatewayPrefix != "" {
    actions, err := newGatewayRegistration(gatewayRegistrationConfig{
        Prefix: request.GatewayPrefix, NginxBinary: request.GatewayNginxBinary,
    })
    if err != nil { return errAgentSession }
    for name, action := range actions {
        executor.actions[name] = action
        semantic.AllowedActions[name] = gatewayActionSchemaVersion
    }
    minor = 1 // 与文件能力相同的协议 minor
}
```

`-file-root` 必须等于或包含 `-gateway-prefix`，使 `artifacts/` 相对路径可由文件能力写入；
Controller 的 `GatewayRoots[nodeID]` 必须与该 `-file-root` 一致。

## 5. 组合验证步骤（R1 验收）

1. 应用迁移；启动 Controller，确认 `gateway_artifact_bindings` 表存在。
2. 启动真实 Agent：`-file-root=/srv/gateway -gateway-prefix=/srv/gateway -nginx-bin=/usr/sbin/nginx`。
3. 创建 gateway、route 与 publishing plan，提交统一任务并完成独立审批。
4. 期望顺序：Dispatcher 在 `queued + 当前审批 + plan target revision + 节点授权` 下写绑定 →
   `files.transfer.v1` offer → 每块重跑 stage gate → Agent 写入 `artifacts/site.conf` →
   `nginx -t` → `nginx -s reload` → 公开 HTTP 返回站点字节 → 任务进入持久终态。
5. 失败/取消：上传中撤销审批或撤权必须停止后续块；UNKNOWN 不重放、不当作成功。
6. 回滚逆序：先 `site_remove`（校验 digest 与 revision），再仅删除本计划创建的 route；
   任何一步失败保留部分状态供显式补偿，不自动重试。

## 6. 尚未覆盖（保持 OPEN）

- Agent 侧 `files.transfer.v1` 能力 root 与 `gateway-prefix` 的目录包含关系需要入口校验，
  本补丁只要求 operator 配置一致；建议在 runtime 参数校验中增加“gateway prefix 必须位于
  file-root 之下”的显式检查。
- 完整 Controller↔Agent 进程级组合测试尚未在本环境跑通（缺中央接线后的可启动组合）。