# V09-CLOUD-AM-01-R1 · agentmore-01 · 返修检查点

日期：2026-11-03（Asia/Shanghai）。锚定审查 head `383fba190364d911754731e09c7cd8745f7c6f71`（PR #3 两条 COMMENTED 审查）。

## 五项必改点处置

- **AM-R1（未启动取消不收敛）**：`CancelUpload` 在 CancelTask 后按持久绑定证据收敛——
  绑定缺失（查询成功无行）或仍处 reserved/offered/accepted（代理未打开目标文件）时，
  经恰好一次守卫落 `cancelled`；绑定查询基础设施错误不收敛（保持 dispatching，交由 worker/重启对账）。
  任务接口取消（POST /tasks/{id}/cancel，含 queued 任务直接终结路径）经 `GetUpload` 懒对账收敛
  （真实 e2e：离线排队任务取消→上传 cancelled，无写帧、无目标文件）。过期/拒绝/worker 提前退出
  的收敛由统一映射 `uploadTerminalFromTask` 覆盖（canceled→cancelled；expired→failed(task_expired)；
  failed/partial→failed(task_failed)；unknown→failed(agent_unknown)；completed→completed）。
- **AM-R2（崩溃对账）**：`ReconcileUploadTasks` 挂载于 `RecoverTransfers`（既有中央启动钩子内扩展，
  零中央改动）+ `GetUpload` 懒对账。真实 e2e 两例：worker 流式执行中冻结 Controller（首个组合不应用
  任何终态）→重组 Controller→Tasks.Recover 将任务置 UNKNOWN、上传经真实对账收敛 failed/agent_unknown，
  RunOnce×3 无重放（绑定数与目标文件 mtime 不变）；任务提交与上传绑定之间中断→孤儿任务经 5 秒
  linkage 超时置 UNKNOWN，重试 complete 幂等绑定同一任务并收敛 agent_unknown，全程零写帧。
- **AM-R3（证据原则）**：`classifyUploadFailure` 纯函数化（证据注入可测）。绑定查询仅
  `storage.IsNotFound`（查询成功无行）构成正面缺席证据；超时/数据库故障保持 UNKNOWN。
  任务读取失败时取消与拒绝证据同样失效（保持 UNKNOWN）；授权仅 `auth.ErrDenied` 纯拒绝
  构成 denied 证据（`deniedNow`）。11 例故障注入表测试（含查询超时+取消/拒绝同时存在仍 UNKNOWN）。
- **AM-R4（摘要丢失）**：`BindUploadTask` 在派发事务中持久化验证摘要为 `sha256_actual`；
  完成终态使用持久值；`CompleteUpload` 返回与 HTTP DTO 均携带；liveUpload 交叉校验持久摘要。
  审查者回归探针（无客户端 SHA）原样集成并 PASS；真实 e2e 断言落盘文件摘要与记录一致。
- **AM-R5（精确协调差异）**：见 `COORDINATION-AM-R1.md`（迁移装配、四处共享断言机械更新 diff、
  HTTP 202/taskId/GET 状态端点候选合同说明）。未扩大共享文件修改范围。

## 验证记录

- 审查者两条 Go 回归（`TestCloudReviewUploadWithoutClientDigestKeepsActualSHA`、
  `TestCloudReviewQueuedUploadCancelTerminates`）+ 3 个对账/取消证据负向：PASS。
- `classifyUploadFailure` 11 例注入表：PASS。internal/files 与 apps/controller/server 全包：PASS。
- 真实 e2e `TestAM01R1UploadTaskConvergenceAndCrashRecovery`：PASS 15.80s（四场景）；
  原 `TestAM01UploadHTTPUnifiedTaskActualAgentShaVerified` 回归：PASS 23.21s。
- 全量 `CI=1 npm run check` / `check:security`：见 PR 描述（本轮全绿后提交）。

## 剩余风险

- 懒对账依赖 `TaskOutcomeReader`（由 FileExecutionWorker 实现，经 submitter 类型断言注入）；
  无受控通道的组合（直推路径）不参与对账，行为不变。
- crashed-Controller 场景中首个组合保持冻结（模拟进程死亡）；其 worker 在测试 ctx 结束后
  解冻时终态写已被恰好一次守卫拦截，无重放风险。
