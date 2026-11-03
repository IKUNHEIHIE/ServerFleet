# 中央接缝精确候选（workbuddy-02 / WB-02，未自行落改公共文件）

本文件是本包对**公共/他人域文件**的精确请求。作者没有修改下列文件；实现只在自己的独占范围内完成。
消费顺序：主控审查 → 分配编号/契约 → 本包在合并后的 main 上接真实实现。

## C-01 `internal/tasks`：插件受控调用动作类型（阻塞 Xray 安装/部署/重启）

**为什么必须由主控/AM 落地**：Xray 所有变更动作必须经唯一任务链（权限/审批/审计/终态）。现有 `internal/plugins.XrayTaskHost.Submit` 只有回调位置，没有可提交的动作类型；`internal/tasks` 由 AM-02 独占，本包不得改。

请求的最小面（示意签名，非最终契约）：

```go
// internal/tasks（请求方：AM 负责人；编号由主控分配）
const ActionPluginHostInvoke = "plugin.host.invoke"

type PluginHostInvocation struct {
    PluginID   string   // "xray-manager"
    Operation  string   // xray.install / xray.deploy / xray.rollback / xray.user.create ...
    NodeIDs    []string // 已冻结目标快照
    Parameters json.RawMessage // 已校验、无秘密、无命令
    PlanDigest string   // 与 preview 返回一致
}
// 需要的能力：与既有任务相同的 approval/expiry/concurrency/TTL 语义；
// 终态必须可投影为 {state, perNode results, errorCode}，不得以 202 代替成功。
```

同时需要 Controller 侧 `cfg.XrayTaskHost` 的实现（本包独占的 `apps/controller/server/plugins.go` + 新增 `xray_*.go` 会在 C-01 接收后填 `Admit`/`Submit`，`Admit` 复核当前身份/CSRF/节点授权/审批/插件 revision，`Submit` 只调用上述唯一任务服务）。

**未接收时的行为（当前已实现且必须保持）**：`POST …/instance/actions|deployments|users|…` 返回 `501 XRAY_RUNTIME_UNAVAILABLE` 或 `503 XRAY_TASK_OUTCOME_UNKNOWN`，不落地、不重试、不伪造 taskId。

## C-02 受控 Host / Runtime 准入

需要主控明确：进程所有权、私有目录、清理、typed 动作白名单、来源可信（见 `ARTIFACT-ADMISSION.md` 的 BLOCKED 项）、以及“准入未被接收前 `AdmittedRuntime` 保持 nil”的书面结论。
本包已遵守：`plugins.NewLifecycleManager(..., runtime=nil)` 时只能保存 `installed_disabled` 工件，`Enable` 直接拒绝。

## C-03 `internal/auth` 权限目录

请求新增节点范围权限：`xray.read` / `xray.config` / `xray.users` / `xray.users.export` / `xray.traffic` / `xray.install`。
现状：`internal/auth` 已有 `xray_materials*.go`（本包可写）但权限目录不含上述条目；Web 端只能按“管理员 + 现有 file/node 权限”降级展示，不能自行发明权限。

## C-04 `api/openapi`（Xray 声明式 UI 契约）

仓库已有 `api/openapi/xray-candidate.yaml`（未接收）。请求主控确认其与已实现 handler 的差异并冻结：
`GET /status|capabilities|nodes|templates|users|traffic|nodes/{id}/configurations`，
`POST /installations/preview|installations|configurations/validate|deployments/preview|deployments|nodes/{id}/configurations/rollback|users|users/{id}/client-export|traffic/settings`。
本包按“设备返回 200 才展示真实数据，501/503 显示不可用”实现，不先造 stub 对外宣称支持。

## C-05 `apps/web` 声明式 UI schema（与 C-04 同步）

需要契约冻结字段（模板参数、批量策略、配额方向/窗口、导出格式）；未冻结前 Web 只用已实现字段，不做乐观假设。

## 已声明但不请求

- 不请求 `WASM`/`Eino` 任何准入。
- 不请求改 `go.mod`/`package-lock`/`buf*.yaml`/CI/迁移编号。
- 不请求“仅校验和即可发布”的安全门槛放宽（见 ARTIFACT-ADMISSION §4 的三条路，需主控决策）。
