# CX-03 中央接收候选

controller-files.patch针对c9013f4，中央server.go/agent_links.go最小接线与cx/0008_file_execution_bindings登记。新Controller file_worker.go在授权范围直接提交；依赖图无新增边，files不导入transport，唯一tasks调度器程序注册Worker负责组合执行。

总控配置`Config.FileExecution=&FileExecutionConfig{Paths:明确files.PathMapper,SpoolDir:专用私有目录}`，必须同时有正式EnrollmentIssuer/authority；不能再叠加任意AgentFileGate。默认nil不启用文件worker，不能加放行门替代映射/权威。新增表只存每node/item唯一ID、父任务/plan/完整offer/原连接观察及结果元数据，无文件字节或秘密；旧迁移不改。总控注册新候选后方可启用。

HTTP创建transfer由文件submitter将transfer ID、source/dest、每项绝对/相对路径及期望checksum冻结为唯一task的string-map参数（每项<=1024字节/最多16项）。当前只准skip，原有普通上传/下载服务和写权限负向保留，不用Controller本地I/O冒充Agent文件通道。父任务持久reserve两节点之后，worker单独向每项源发送offer→真实Agent持久Ack→Controller同事务记accepted→Open→冻结stat/SHA→有界spool/实际SHA→源子终态；然后才冻结目标write offer并重复接受顺序。子事件由新桥接观察器消费，绝不把源Finished交通用RecordResult提前完成父任务；各节点父结果仅在所有items结束后由worker提交。

同一子执行不支持重连接管/重开/非零resume；UNKNOWN不重试。每帧核同连接/身份、完整offer、当前task/审批/取消、两端read/write/transfer权限、节点online、当前逻辑路径映射及ScopeGate；缺scope gate的非Web任务拒绝。ScopeGate是额外当前AI/plugin范围端口，不豁免基础门，当前产品AI无文件工具，不自动给予权限。参数、transfer row或映射变化均拒绝。

重现：`node tests/poc/v09/platform/run-wave6-r2-linux.mjs`。使用固定Windows Go交叉编译的实际Agent CLI及带cx_r2_controller标签的正式Controller测试，非root WSL执行；准备器仅写reports拟议.txt与本progress具体patch，不改共享源码。候选overlay正常/负向通过不等同主线接收、物理双机、awskr、1C1G或生产准入。
# Additional candidates / apply order

1. `controller-files.patch`: base c9013f4, new cx/0008 binding migration and sole file worker.
2. `controller-ai-budget.patch`: independent owner budget route registration. `redocly-ai-budget.patch`: independent r7 candidate lint registration.
3. `controller-platform.patch`: applies AFTER 1 + 2. Proposed cx/0009_update_journal is for total-controller approval/number assignment, not a registered author migration. Adds Config.UpdateExecution/UpdateResolvePlan, admin-only system.updates.apply permission, worker registration and Apply-to-unified-task adaptation. Existing UpdateApply cannot be combined with this worker, and absent resolver is rejected. ResolvePlan must load an existing immutable, owner-bound reviewed plan; it cannot construct trust/path/backup evidence from the HTTP body. Default CLI still has no release feed/worker/managed slot.

The installer requires a dedicated owned slot/source/supervisor and actual version/storage readiness; SQLInstallJournal requires current task+plan+subject recheck INSIDE its transaction and pre-established real installed provenance/floor. Missing provenance, ambiguous commit, unsafe rollback or existing permanent reservation fails closed. A rollback advances the maximum attempted release floor but retains the verified old current manifest. Do not seed provenance from a restore snapshot or confirmation string. Candidate schema statements are `updates.InstallJournalSchemaStatements()`, and central registration is concrete in the platform patch. The program assembly ports are exported `NewFileExecutionWorker` / `RegisterAIBudgetRoutes` so the author branch also passes static analysis before central registration; no ignore annotation is used.

Recovery Config.RecoveryLatch is the exact external latch for adjacent hold.json/restored.db, selected by the offline procedure. Server construction rejects a held database before migration, even if the latch option was omitted. Offline release is explicit and has no management HTTP endpoint. After release, normal RBAC/CSRF still applies; every HTTP route, scheduler execution admission and Agent request/reply rechecks the live gate. Quarantine enforcement does NOT supply current authority or remote old-controller fence; trusted verifier must independently prove fresh epochs/revocations/reconciliation and real ongoing isolation. Those proofs remain unimplemented/unverified, so no production restore release is approved.

Reproduce combination with `node tests/poc/v09/platform/prepare-wave6-r2-platform-overlays.mjs` then Go tags `cx_r2_controller,cx_r2_platform` with `reports/wave6-r2/platform-overlay.json`. The preparation scripts write only reports + this author's review patches. Upgrade test verifies adopted 21 migrations → proposed 23 and idempotent reapply. Proto/gen and existing DDL unchanged; no new module import edges.
