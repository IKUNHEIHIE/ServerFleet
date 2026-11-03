# V09-CLOUD-AM-02 · agentmore-02 · 第一检查点（多目标分发）

日期：2026-11-03（Asia/Shanghai）。会话 agentmore-02；分支 `feature/cloud-platform-am02`；起点 main `51e0ee57e66493892eb5f5168b9e0690b53f3661`。

## 交付内容

### 多目标文件分发（冻结计划 + 每目标持久化 + 真实双 Agent）

统一 worker 新增 `kind="distribute"` 任务形态：一个已验证的 Controller staging 文件按冻结计划分发到 N 个目标节点。

- **冻结计划**：`SubmitDistribution` 提交统一任务，params 冻结目标身份集合、每目标映射写路径（各自私有 file root）、尺寸与摘要；`decodeDistributeTask` 严格校验 task 目标集合与 params 完全一致（条目数、去重、逐节点 write_path、上限 16）。
- **每目标持久化**：新表 `file_distributions` + `file_distribution_targets`（候选迁移 `am/0001_file_distribution`，最终编号由主控分配）持久化每目标身份/写路径/状态/字节/摘要/错误；per-target 终态更新带恰好一次守卫（pending/dispatched 才收敛，terminal 幂等），成功结果不可被后续失败覆盖。
- **逐帧当前权限复查**：`liveDistribute` 接收帧级节点身份——Gate/Observe 以执行绑定节点（b.NodeID / 观察身份）调用；对当前帧节点复查在线状态与 files.write 授权。某个目标撤权/离线只失败该目标，不连坐其他目标。
- **部分成功语义**：`DistributionPartial` 终态=至少一目标成功且至少一目标失败/取消/未知；全部成功=completed；无成功按错误类型 failed/cancelled；unknown 保持 dispatching 交由对账。
- **成功目标不重放**：executeDistribute 对已 completed 的目标行直接跳过（记录 `already completed; not replayed`）；帧级守卫拒绝向已完成目标重推帧。
- **重启对账**：`ReconcileDistributionTasks` 挂载既有 `RecoverTransfers` 启动钩子（零中央改动）+ `GetDistribution` 懒对账（`TaskOutcomeReader` 按 task.Targets 的每目标 Code 映射收敛）。
- **HTTP**：POST /api/v1/files/distributions（201）、GET …/{id}（每目标视图）、PUT …/content?offset=（断点续传语义与 upload 相同）、POST …/complete（202+taskId，明确 dispatching≠远端完成）、POST …/cancel（open→abort 走既有直停；dispatching→统一任务取消）。

## 验证记录

- `go build ./...`、`go vet ./...` PASS；internal/files、internal/tasks、internal/transport、apps/controller/server 全部既有测试 PASS（0 FAIL）。
- 真实 e2e `TestAM02DistributionMultiTargetRealAgents`（tests/integration/v09/platform/）：PASS 20.48s。非 root Linux + 真实构建二进制（go build -trimpath，controller/agent SHA-256 记录于测试日志），两台常驻真实 Agent 进程 + 一个已登记未启动的离线节点：
  1. 双目标正常分发：两台落盘字节与 SHA 一致，per-target 行 completed+bytes+sha256；
  2. 离线+取消：离线目标使任务诚实排队（15 分钟 TTL 语义，未到期），任务接口取消后每目标行收敛 cancelled，无目标文件；
  3. 撤权+部分成功：A（24MB 大文件传输中）completed、B failed(denied_now) 无写帧、整体 partial、A 的结果不被覆盖；
  4. 在途取消（202 后毫秒级取消）：每目标行收敛 cancelled，无目标文件。
- 逐帧隔离修复：`CheckExecution` 的任务级授权原为全目标一票否决，撤权任一目标会拒绝其余目标全部在途帧；新增 `CheckExecutionForNode`（帧级节点作用域 task_execute 授权，验证节点属于冻结目标集），分发帧改用之，撤权只影响被撤目标自身。internal/tasks 既有行为不变（单目标计划语义等价）。
- 受迁移计数影响的共享测试实测：TestR1MigrationPreserves… PASS、TestWave2ControllerEndToEndHTTPSComposition PASS（计数 27）。
- 全量 `CI=1 npm run check` / `check:security`：UNTESTED（本检查点未在本地完成全量套件，CI 结果以 PR 评论补充，不以此冒充全绿）。

## 剩余风险与候选依赖

- 候选迁移编号待主控分配；五处共享迁移计数断言机械更新（26→27 / 14→15），语义未改，详见 COORDINATION-AM-R2.md。
- 分发为顺序逐目标执行（无并发扇出）；并发分发留待后续检查点。
- 64MiB 单文件通道上限（transport.FileMaxBytes）沿用，未扩容。
- 续传（两段式持久 offset）、更新可靠性、恢复信任、AI/工作流负向为本包后续连续项。
