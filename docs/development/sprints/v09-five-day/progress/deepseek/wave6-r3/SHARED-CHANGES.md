# Wave6-R3 · DS 中心共享变更（候选）

基线 `db3cf87`。以下差异均**未由 deepseek-06 应用到中央文件**；由集成负责人审查、应用并跑根检查。应用顺序建议：迁移 → Agent capability → Controller 装配 → 契约注册。

## 1. `ds/0063_cleanup_claim` 迁移注册

文件：`apps/controller/server/server.go`

```diff
 		certificates.Migrations(),
 		certificates.CleanupMigrations(),
+		certificates.CleanupClaimMigrations(),
 		gateway.Migrations(),
```

同步共享测试：

```diff
--- a/tests/integration/v09/controller_wave2_test.go
+++ b/tests/integration/v09/controller_wave2_test.go
@@
-	if len(versions) != 24 {
-		t.Fatalf("applied migrations = %d (%v), want 24", len(versions), versions)
+	if len(versions) != 25 {
+		t.Fatalf("applied migrations = %d (%v), want 25", len(versions), versions)
```

```diff
--- a/tests/integration/v09/wave6_r1_migration_test.go
+++ b/tests/integration/v09/wave6_r1_migration_test.go
@@
-			if migration.Version != "cx/0007_ai_budgets_and_model_audit" && migration.Version != "ds/0081_publishing_targets" && migration.Version != "cx/0008_file_execution_bindings" && migration.Version != "cx/0009_update_journal" && migration.Version != "ds/0062_acme_cleanup" {
+			if migration.Version != "cx/0007_ai_budgets_and_model_audit" && migration.Version != "ds/0081_publishing_targets" && migration.Version != "cx/0008_file_execution_bindings" && migration.Version != "cx/0009_update_journal" && migration.Version != "ds/0062_acme_cleanup" && migration.Version != "ds/0063_cleanup_claim" {
@@
-	if err != nil || len(versions) != 24 {
+	if err != nil || len(versions) != 25 {
```

兼容性/回退：

- `ds/0063` 重建 `certificate_dns_cleanup`，复制全部旧列并新增 `claim_token`、`lease_until`，状态约束新增 `unknown`；索引/唯一键保留。
- 应用后旧二进制（无 claim 列代码）仍可读旧列，但新二进制在未应用迁移时会因缺列失败；禁止把已应用 ds/0063 的库降级给 DS-03 代码。
- 回退只能从备份恢复；不要删除/修改已应用迁移历史。
- 未要求默认启用真实 cleaner；`CertificateCleanupCleaner` 仍可保持 nil。

## 2. Agent capability 注册

文件：`apps/agent/runtime/runtime.go`（program config，非 HTTP）

```diff
 type ConnectRequest struct {
 	...
 	FileRoot        string
+	GatewayPrefix   string
+	GatewaySiteDir  string
+	GatewayArtifactDir string
+	GatewayMainConfig  string
+	GatewayNginxBinary string
 }
```

文件：`apps/agent/connect.go`，在 `semantic, frames := agentSessionLimits()` 之后、构造 `admit` 之前：

```go
var gatewayActions map[string]agentAction
if request.GatewayPrefix != "" {
	capability, err := newGatewayCapability(gatewayCapabilityConfig{
		Prefix: request.GatewayPrefix, SiteDir: request.GatewaySiteDir,
		ArtifactDir: request.GatewayArtifactDir, MainConfig: request.GatewayMainConfig,
		NginxBinary: request.GatewayNginxBinary,
	}, execGatewayRunner{})
	if err != nil {
		return errAgentConnect
	}
	gatewayActions = capability.Actions()
	for id, action := range gatewayActions {
		executor.actions[id] = action
		semantic.AllowedActions[id] = gatewayActionSchemaVersion
	}
}
```

修改 `admit`：

```go
admit := func(ctx context.Context, offer *agentv1.TaskOffer) error {
	if ctx.Err() != nil {
		return errAgentSession
	}
	if offer != nil {
		if action, ok := gatewayActions[offer.ActionId]; ok {
			if offer.GetPrivilege() != agentv1.Privilege_PRIVILEGE_NORMAL ||
				offer.GetPluginId() != "" || offer.GetActionSchemaVersion() != gatewayActionSchemaVersion ||
				len(offer.GetCanonicalParametersJson()) > 16<<10 {
				return errExecutionRejected
			}
			return action.Validate(offer.GetCanonicalParametersJson())
		}
	}
	if offer != nil && offer.ActionId == transport.FileCapability && request.FileRoot != "" {
		_, err := transport.FileOffer(offer)
		return err
	}
	return admitControlledOffer(offer)
}
```

CLI/宿主需从**可信程序配置**填充上述五项并保证：
- `GatewayPrefix` 为 Agent 普通用户私有目录；`SiteDir`/`ArtifactDir`/`MainConfig` 位于 prefix 内；main config 已存在且为非 symlink 常规文件。
- `GatewayNginxBinary` 为绝对路径（或 PATH 中可解析）；不接管系统 nginx/80/443。
- 未配置时 capability 不注册、不授予，行为与现在一致（closed）。

## 3. Controller 任务 dispatcher 与发布 bridge 装配

文件：`apps/controller/server/agent_links.go`

```diff
 func agentAllowedActions() map[string]string {
 	return map[string]string{
 		"tasks.command":          "1",
 		transport.FileCapability: "1",
+		gateway.NginxActionApply:  "1",
+		gateway.NginxActionRemove: "1",
 		"host.patch":             "1",
 		"host.root":              "1",
 	}
 }
```

文件：`apps/controller/server/server.go`（在 `tasks.NewService` 之前）

```go
type lateTaskDispatcher struct {
	mu    sync.RWMutex
	inner tasks.Dispatcher
}

func (d *lateTaskDispatcher) Dispatch(ctx context.Context, c tasks.Command) (tasks.DispatchResult, error) {
	d.mu.RLock()
	inner := d.inner
	d.mu.RUnlock()
	if inner == nil {
		return tasks.DispatchResult{Accepted: false, Reason: "dispatcher_unavailable"}, nil
	}
	return inner.Dispatch(ctx, c)
}

func (d *lateTaskDispatcher) Cancel(ctx context.Context, c tasks.CancelCommand) error {
	d.mu.RLock()
	inner := d.inner
	d.mu.RUnlock()
	if inner == nil {
		return tasks.ErrDispatcherAbsent
	}
	return inner.Cancel(ctx, c)
}

func (d *lateTaskDispatcher) set(inner tasks.Dispatcher) {
	d.mu.Lock()
	d.inner = inner
	d.mu.Unlock()
}
```

装配位置：`tasksService` 创建前：

```go
lateDispatcher := &lateTaskDispatcher{}
if cfg.Dispatcher != nil {
	lateDispatcher.inner = cfg.Dispatcher
} else if agentBridge != nil {
	lateDispatcher.inner = agentBridge
}
```

`tasks.NewService(tasks.Options{... Dispatcher: lateDispatcher ...})`。

`tasksService` 创建后、`publishing.NewService` 之前：

```go
var pubBridge publishing.LocalBridge
if cfg.ProductionPublishing != nil {
	bridge, gatewayDispatcher, err := NewProductionPublishingBridge(ProductionPublishingConfig{
		Plans: publishingRepo, DNS: dnsService, Certificates: certificatesService,
		Gateways: gatewayService, Nodes: nodesRepo, ServiceGroups: serviceGroupsRepo,
		Tasks: tasksService, TasksRepo: tasksRepo, DB: db,
		Artifacts: cfg.GatewayArtifacts, NextDispatcher: agentBridge,
		Authorizer: authorizer, CertificatePath: cfg.GatewayCertificatePaths,
		ApprovalWait: cfg.GatewayApprovalWait, ExecutionWait: cfg.GatewayExecutionWait,
		Poll: cfg.GatewayPoll, OnTaskSubmitted: cfg.GatewayTaskSubmitted,
	})
	if err != nil {
		return cleanup(err)
	}
	lateDispatcher.set(gatewayDispatcher)
	pubBridge = bridge
} else {
	pubBridge = publishing.NewLocalBridge(cfg.PublishingBridge)
}
```

说明：

- `GatewayArtifacts` 必须由 CX/中央实现；**接口已含 taskID**：
  ```go
  type GatewayArtifactUploader interface {
      Upload(ctx context.Context, taskID, nodeID, absolutePath string, content []byte, sha256Hex string) error
  }
  ```
  实现必须：先 `tasks.CheckExecution(taskID, planHash)` 取得当前任务/审批/授权；用现有 Agent file channel 写入 `absolutePath` 对应的显式逻辑根（大小/SHA 精确）；不得写入任意 HTTP 路径，不得开 SSH/第二个执行队列，不得在失败后自动重试 UNKNOWN。
- `NextDispatcher` 必须是原始 `agentBridge`，不能是 `lateDispatcher`（否则递归）。`lateDispatcher` 只供 `tasks.Service` 使用。
- `cfg.ProductionPublishing == nil` 时保持现有 `cfg.PublishingBridge` 注入（测试/本地），不得把测试 bridge 当生产成功证据。
- 未装配生产 bridge 时，网关任务仍会走 agentBridge 直接 dispatch：没有工件上传门；必须把它视为未启用发布执行器，不能对外宣称成功。

文件：`apps/controller/server/server.go` `ExtraPermissions()`：

```diff
 		{Key: gateway.ActionProvision, Description: "Provision gateway runtime on granted nodes", ResourceType: "node"},
+		{Key: gateway.NginxActionApply, Description: "Apply controlled Nginx site files on granted nodes", ResourceType: "node", AdminOnly: true},
+		{Key: gateway.NginxActionRemove, Description: "Remove controlled Nginx site files on granted nodes", ResourceType: "node", AdminOnly: true},
```

原因：统一任务的 `ActionID` 就是这两个值，`tasks.Service` 的授权检查按 `ActionID` 授权；缺权限目录会导致真实主体无法执行。

权限/后台：

- `cleanup-service` 需要 `certificates.cleanup.retry`（`RetryDue` 先调用 `RecoverStaleClaims`）。
- 仍不要求默认启用真实 `CertificateCleanupCleaner` 或后台轮询；启用后应先跑 `RecoverStaleClaims`，不要把 `unknown` 当可重试。

## 4. OpenAPI candidate 注册

文件：根 Redocly 配置（中央）

```yaml
apis:
  services-candidate:
    root: api/openapi/revisions/v09-r7/services-candidate.yaml
```

已本地执行：

```text
npx --no-install redocly lint api/openapi/revisions/v09-r7/services-candidate.yaml
→ valid
```

候选与本轮代码对齐：run 阶段失败返回 **502**（不是 422）；cleanup state 增加 `unknown`；retry 409 覆盖 resolved/canceled/unknown/busy/exhausted。

## 5. 应用后必须重跑

```text
.tools/go/bin/go.exe test ./... -count=1
npm run check
npm run check:security
```

若共享迁移计数测试出现差异，以 `server.Migrations()` 的实际 version 列表为准，不允许删除测试或改历史迁移。
