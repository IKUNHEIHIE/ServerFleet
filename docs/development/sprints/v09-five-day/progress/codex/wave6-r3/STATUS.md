# V09-R6-CX-04 / codex-06

审查 codex-review-02；原分支/目录已核对，干净起点 db3cf87d44022be79bbec55eb4012b95f307dac7。已读 R3、R2-SHARED 和本轮总控审查；24项迁移/五份旧patch均已正式接收，不再重放。沿用已授权目录，不写 DS gateway 或 GM Web/宿主；无 main合并/推送/SSH/awskr/云/真实外部秘密/其他Agent/消息。

## 第一增量（已提交 0bad5461）

internal/files/deployment*、spool owner检查、新增server/file_deployment.go和实际产品CLI组合验证。启动文件严格version/schema/重复字段及重复nodeID拒绝；Linux配置0600/单硬链接、父目录0700/当前UID、非symlink/打开句柄身份核对，私有spool owner/权限校验。非Linux缺等效ACL证据明确拒绝此加载入口。映射只读一次，非HTTP配置，不提供默认AI/plugin ScopeGate。

中央bootstrap.go只交controller-file-cli.patch：新增-file-deployment，管理TLS模式必需，配置失败在PKI/DB打开前终止，不能退回关闭worker后继续启动。新脚本prepare-wave6-r3-cli只生成这个新候选；不执行R2旧prepare。run-wave6-r3.mjs --final要求干净且不变HEAD。

开发组合PASS（不是固定clean SHA最终证据）：本机WSL UID1000实际Controller CLI +两个实际Agent CLI，HTTP入网/审批/资源授权后重启加载已登记nodeID，65,000目标字节及SHA、父task两节点/transfer完成、第二次skip、未登记路径400和不安全配置启动失败。初始fixture把节点详情路由写成不存在的路径，后又误用state；两次404原始输出已保留，最后核对正式diagnostics路径后PASS。未修改产品路由/放宽断言。files/架构/server模块Windows检查PASS；Linux模块和固定提交复验随后执行。

第一检查点：干净 HEAD 0bad5461 复验 65,000目标字节、SHA 1c306fab543a4a90a516da64c02f4dc66cbe7d2b84ffefbb83fb602bbd14dcd3、两节点父task/transfer终态、重复skip、未登记路径拒绝、不安全配置启动失败；证据 reports/wave6-r3/checkpoint1-*。前两次404为夹具路由错误，原日志保留。

## 第二增量（本提交）：实际 Agent 产品更新装配

目标是替换 R2 仅由测试 helper 输出版本的更新证据：可信发布源、owner计划、原始安装来源、统一审批、安装、真实版本健康、兼容回滚组合为同源实际产品。

- `apps/agent/release_health*.go` + `connect.go`：真实 WSS 启动成功后写私有健康文件（编译变量 `main.agentBuildVersion`、PID、supervisor nonce、nodeID、connectionID、stateFormat、trustGeneration、时间）；没有 `SF_AGENT_HEALTH_FILE` 时不写。版本不是 `-agent-version` 可伪造标签。写临时文件后 rename，启动前清理残留 tmp。
- `internal/updates/owned_process.go`：`OwnedProcessObservation` 移到公共文件，保持非Linux编译；`supervisor_linux.go` 移除原定义。
- `internal/updates/agent_ready.go` + `health_private_{linux,other}.go`：真实PID/nonce/新鲜度/编译版本/存储格式/当前WSS会话复核；报告文件用 Lstat→Open→SameFile→LimitReader 有界读取，目录必须是当前UID 0700、非root、非symlink；supervisor 独立核 `/proc/PID/exe` SHA。非Linux拒绝。
- `internal/transport/current_connection.go`：只观察当前真实、已验证连接，不要求文件能力，不授予任务权限。
- `internal/updates/schema_fingerprint.go`：`SQLiteSchemaFingerprint` 哈希 user_version + 排序后的 sqlite_master 定义，不返回业务数据/秘密；DDL 变化但 user_version 不变也能发现。
- `internal/updates/product_deployment{,_linux,_other}.go`：可信程序配置、私有签名inbox/stager/slot、初始签名工件与实际active一致后同事务审计建立provenance；持久owner计划/一致SQLite备份；审批后worker、真实owned Agent启动停止、版本不符时兼容回滚、SQLInstallJournal原子来源/floor/审计；启动遇update-lock/supervisor-reservation拒绝，不接管外部PID。指纹同时钉入可信配置、运行期schema门与冻结计划（进入审批摘要）；撤权/审计失败清理未签发计划与备份，其余失败 UNKNOWN。
- `apps/controller/server/file_update_deployment.go`：`NewOwnedProductUpdates` 接入 RuntimeGate、真实 Tasks.CheckExecution、当前管理员、实际连接身份；缺Link/缺少runtimeGate拒绝装配。
- `tests/poc/v09/platform/prepare-wave6-r3-product.mjs`：只生成新中央 `controller-product-updates.patch`（Config.UpdateProduct、worker/HTTP resolver/plan/check、启动/关闭生命周期），不直接改中央源码；会先运行 R3 CLI 准备器，与 R2 历史 prepare 无关。
- `tests/integration/v09/platform/update_product_r3_test.go`：build tag `cx_r3_product`（引用尚未接收的 Config.UpdateProduct），必须用 product-overlay 编译。

开发组合 2 PASS / 0 SKIP（`reports/wave6-r3/result.json` 标 DIRTY DEVELOPMENT RUN）：

1. 文件 CLI 测试继续通过。
2. 实际Agent产品：HTTP申请→awaiting_approval期间真实PID未变→另一管理员真实HTTP审批→1.0.1替换成功、父任务completed、真实build/PID/WSS健康、journal floor=2；再给签名manifest 1.0.2但artifact实际build仍1.0.1，检测真实版本不符，安全回滚到1.0.1新PID、父任务failed、floor=3。

同时验证：内部 updates 单测在 Windows 与原生Linux均PASS（含 DDL指纹变化、写事务内读指纹不死锁、旧计划缺指纹拒绝、1024字节参数上限、健康文件symlink/超限/目录拒绝）；`go test ./internal/updates ./internal/transport ./apps/controller/server`、`go test ./tests/architecture`、Windows+Linux staticcheck 均通过，证据 reports/wave6-r3/product-*。

这是同源实际产品的本地合成签名/编译版本证据，不是外部正式Release、浏览器或实机准入；product patch 待总控接收。

## 第三增量：文件冲突策略（skip/overwrite/rename）

此前仅 skip：Agent 打开写会话时无条件跳过已存在的目标，overwrite/rename 虽在 Controller 模型中合法，却会被静默降级为 skip，且线协议在 `ParseFileParameters`/`FileOpenRequest` 校验中硬编码只收 `skip`。

本增量：

- `internal/transport/file_protocol.go`：协议参数与 FileOpenRequest 接受受审的 `skip|overwrite|rename`（字段本来就是 string，无 Proto/gen 变更）；新增 `ValidFileConflictPolicy`。
- `apps/agent/file_capability.go`、`file_session.go`：写会话按计划策略处理；`skip` 保留原目标并回 skipped；`overwrite` 只对非 symlink 常规文件生效，发布前原子替换；`rename` 在打开时选择同目录空闲 `name (N).ext`（最多32次），在 `RelativePath` 回给 Controller；链接/目录一律拒绝。断连续传从私有 partial binding 复用已选路径，丢失回执仍 UNKNOWN、不自动重试。
- `apps/agent/file_nofollow_linux.go`：`Linkat` 保持不覆盖；新增 `Renameat` 原子替换与 `Fstatat(AT_SYMLINK_NOFOLLOW)` 目的地分类；非Linux保持显式拒绝。
- `internal/transport/file_client.go`：只允许 rename 回复同目录内的合法相对路径，其他策略仍须与请求一致。
- Controller 协调器已用 `UploadResult.DestinationPath` 记录实际 rename 路径，无需新扣；非策略型 transport 仍对非 overwrite 显式拒绝。

验证：新增实际双进程 WSS 测试 `TestActualWSSOverwriteAndRenameConflictPolicies`（overwrite 替换、rename 依次得到 `target (1).bin`/`target (2).bin`、原文件保留、skip 不改写）；`internal/files`、`internal/transport`、`apps/agent` 的 Windows 与原生Linux测试 PASS；staticcheck（Windows+Linux）与架构检查PASS；R3 开发组合再次 2 PASS/0 SKIP。证据 reports/wave6-r3/files-conflict-*、agent.test、files.test。

根检查：修改后第一次 `npm run check` 在 `tests/integration/v09/services/file_transport_blackbox_test.go` 失败的原始日志保留为 `reports/wave6-r3/root-check-after-files.log`（该黑盒原先固定要求 overwrite/rename 继续被拒绝）。按该测试注释“until CX implements”更新为接受三种受审策略、其余不安全请求仍拒绝，并把该跨套件期望变更登记在 SHARED-CHANGES 交总控复核；随后 `npm run check` exit=0（Go、契约、policy、Web 341 PASS、git），`npm run check:security` exit=0（npm 0 漏洞、govulncheck 无已知漏洞）。证据 `reports/wave6-r3/root-check-final.log`、`root-security-final.log`。

仍未完成：远端上传/多目标分发、断连续传之外的冲突/部分结果矩阵、每文件64MiB以上及>16项的产品限制策略，真实ENOSPC/inode专项。协议不再只收 skip，但也没有放宽到任意字符串。

## 第四增量：更新前置拒绝与 schema 漂移 UNKNOWN

- 产品真实 HTTP 链新增负向：先正常 plan 1.0.3，再在审批前把实际 SQLite `PRAGMA user_version` 改为 1，随后另一管理员审批；`/api/v1/system/updates/jobs/{id}` 终态为 `unknown`，实际产品 PID 不变、journal floor 仍为 3、无 provenance 提交，最后恢复 user_version=0。证明审批不能绕过当前 schema 门，且 UNKNOWN 不自动重试。
- 新增 Linux 单测 `TestProductDeploymentRefusesLeftoverUpdateEffects`：残留 `update-lock.json` 或 `supervisor-reservation.json` 时 `NewProductDeployment` 返回 `ErrUpdateBusy`；干净私有目录可创建并关闭，防止重启自动接管中断更新。
- 首次把期望写成 `failed`、实测 `unknown` 的失败日志保留为 `reports/wave6-r3/product-schema-expectation-fail.log`，随后按真实终态改为 UNKNOWN，未放宽断言。
- 证据：R3 开发组合 **2 PASS / 0 SKIP**（HEAD `8af6f57` 加本增量，DIRTY DEVELOPMENT）；`internal/updates` 原生 Linux 全 PASS。仍未覆盖真实进程崩溃后的全新进程恢复与不可逆迁移的实际回滚，只有当前 schema 漂移拒绝与残留标记拒启动。

## 第五增量：真实统一任务链的 overwrite/rename

此前 Agent 已实现三种策略，但 Controller 的 `FileExecutionWorker` 仍硬编码只接受 `skip`（plan/params/decode/live/Gate/open 五处），因此真实产品 API 无法使用 overwrite/rename。本增量：

- `apps/controller/server/file_worker.go`：`fileTaskPlan` 携带并冻结 `ConflictPolicy`；plan/decode/live/Gate/open 全部按受审策略校验；Agent 回传的 rename 路径必须与计划路径同目录（经显式 PathMapper 转绝对路径比较），否则拒绝；`copyItem` 把实际发布路径写回 transfer item 的 `DestinationPath`。
- `tests/integration/v09/platform/file_cli_r3_test.go`：在真实 Controller CLI + 两个实际 Agent CLI 的既有 skip 流程后，再通过 `/api/v1/transfers` 统一任务链执行 overwrite（原地替换、字节校验）与 rename（得到 `/target/out/source (1).bin`、原文件保留、API item 路径更新）。
- R3 开发组合仍 **2 PASS / 0 SKIP**（HEAD `5ceadcc` 加本增量，DIRTY DEVELOPMENT）；`apps/controller/server` 与架构检查 Windows PASS。

仍缺：>16 项/多目标分发、断连续传跨主控重启、真实 ENOSPC/inode 专项；rename 冲突耗尽（32 次）只返回 UNKNOWN，没有专门产品级测试。

## 剩余（继续独立推进）

- 本增量新CLI+product两个中央patch待总控接收；不等待共享patch而停止其他独立项。
- 文件：远端上传/多目标分发、overwrite/rename、断连续传/冲突/部分结果差距。当前产品仅skip/最多16项/每文件64MiB，其他策略显式拒绝；须冻结路径/计划、当前授权和审批、UNKNOWN不重放，已成功目标不被无条件重试改写。
- 恢复：实现两目录/双Controller真实新权威与旧Controller fence；已有RuntimeGuard/QuarantineLatch入口门，上轮verifier含double。需证明实际新CA/信任世代、旧信任拒绝、旧进程/活跃HTTP/WSS/tasks隔离、撤销/过期任务不复活、显式释放；缺正式PKI-OPS来源登记，不伪造verifier/确认短语。
- 更新：更新中断/主控重启、不可逆schema变UNKNOWN、真实产品状态格式兼容在本轮产品HTTP链仍缺；上轮Linux module的user_version变化UNKNOWN不能冒称完整产品链。
- Xray/插件：本轮尚未新推进。历史固定26.10.29工件获取失败、安全FAIL/Runtime准入仍有效；只能先做静态来源/签名/漏洞/包与typed能力独立工作，未准入不运行、不擅定WASM/Eino。
- 真实ENOSPC/inode容量、ARM运行、物理双机、1C1G、外部账号、root/产品发布准入无新证据；不混淆EFBIG与ENOSPC。
- 本轮尚未执行完整 npm ci/tools:install/check/check:security；最终必须根检查/安全/模块负向、保留首次失败和SKIP，再写固定SHA证据与HANDOFF。旧轮根PASS不替代本轮。