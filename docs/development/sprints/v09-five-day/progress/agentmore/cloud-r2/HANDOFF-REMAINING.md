# AM-02 剩余项交接（第一检查点后）

状态日期：2026-11-04。原会话agentmore-02；分支`feature/cloud-platform-am02`。当前PR为#11；#10已关闭未合并，39a8660是旧记录。第一检查点仅已提交、首审未通过，不能称已接收。用户授权ChatGPT只接管本轮返修，见[REPAIR-R1.md](REPAIR-R1.md)；下列四项保持未完成，不自动启动。下一检查点须另行派发，从届时已接收main新建分支。

## 已完成的基建（复用清单）

- 分发全链：internal/files/distribution.go（模型/服务/对账）、repository.go（分发 CRUD+终态守卫）、migrations.go（候选 am/0001）、http.go（五路由）、apps/controller/server/file_distribution.go（worker 执行）、file_worker.go（liveForNode 帧级接线）
- CheckExecutionForNode（internal/tasks/worker.go）：帧级节点作用域 task_execute 授权——多目标计划必备
- e2e 模板：tests/integration/v09/platform/distribution_am02_test.go（三节点注册/双真实 Agent/门控通知钩子/任务转储），新测试直接复制其 harness 段
- 调试经验：保留实际帧/执行证据；测试同步须有明确事件及取消/超时边界，不持有DB事务等待其他请求。只通过夹具直接拥有且已记录的Process/PID停止自身进程，不按名称或全局匹配终止。

## 2. 持久续传（下一检查点优先）

需求：两段式（浏览器→Controller、Controller→Agent）断点续传，实际进程终止重启证据，持久 offset/绑定/校验和，仅正向证据恢复；负向：计划过期、主体/目标/路径变化、摘要不匹配、重复 complete、staging 损坏。
入口：
- 浏览器段：file_uploads/file_distributions 的 bytes_received+offset 语义已有（PUT content?offset= 断点追加已实现）；补：CompleteUpload/CompleteDistribution 之后 dispatching 状态下的重试/恢复语义
- Controller→Agent 段：cx_file_execution_bindings 已有 Offset/Complete/State 字段（file_worker.go Gate 帧序检查：r.Offset/r.NextOffset/b.Offset）；worker executeUpload/executeDistribute 目前从 0 重推（成功目标跳过已做，但部分传输目标无 offset 续传）
- 设计建议：执行绑定行已有 per-frame offset 持久；worker 重入时读绑定 Offset 从该 offset 续传（需 FileOpen 支持 resume offset>0——检查 transport.FileParameters 与 agent 端 file_session 的 resume 语义，FileOpenRequest 有 ResumeOffset 字段线索）
- e2e：仿 upload_task_am01_r1_test.go 的 crash 场景（pause 冻结 worker→关控制器→重组→RunOnce 对账），断言不重放已收字节

## 3. 更新可靠性与 Controller 可信装配

需求：并发 apply 幂等、变更串行化、撤销/取消/审计失败处理、Close 只停自己进程；Controller 可信更新启动器（配置候选/备份/安装/健康检查/失败恢复）放 apps/controller/updater/**（写权限已授权）；可信在线发布源须可验证，无正式材料/发布凭据则逐项 BLOCKED。
入口：internal/updates/（executor/task_worker/install/staging/supervise linux/forked process）；AM-01 已验证实际 Agent 本地签名更新/回滚（产品更新 2 组 PASS）
- 注意：真实发布源/发布凭据不在授权范围→该项预期 BLOCKED，如实记录即可

## 4. 实际恢复与信任证明

需求：两个普通用户私有目录+实际 Controller 进程，快照 verify/restore、会话/审批失效、未完成任务隔离；新权威建立、旧信任纪元拒绝、旧主控不能继续。
入口：internal/recovery/（runtime_queue/reststore_executor/evidence）；平台测试 tests/integration/v09/platform/recovery_guard_r2_test.go（27 迁移计数）；信任纪元=transport enrollment（peer_identity/enrollment_sha）
- 双进程证据用 httptest + 实际 exec 控制器二进制（productbuild_test.go 有构建器）

## 5. AI/工作流可靠性负向回归

需求：资源快照、预算并发/409、授权变化、取消、续期不重放、失败审计回归；保持默认零资源。
入口：internal/ai/（budget_http/budget_sha/generation_budget + 测试完备）；internal/workflows/（engine/plan/sha）。
- 此项多为负向测试补齐（相对最轻），可先扫现有覆盖找缺口再补

## 检查点纪律（每轮必做）

- Conventional Commits、Draft PR base=main、先最终代码提交后用 PR 评论补 CI 结果
- 主控未合入的共享文件改动只交精确候选 diff（COORDINATION-AM-R2.md 模式）
- FAIL/BLOCKED/SKIP/UNTESTED 原样保留，不删测试不伪造
