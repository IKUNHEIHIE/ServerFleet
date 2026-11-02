# 接手提示词：V09-R6-CX-04

你接手当前 CX-04 开发工作。用户已明确要求原 Agent 暂停以交接；不要与原 Agent 并行写这个目录。本文件是暂停时的事实快照，不代表整包完成或当前未提交代码通过验证。

## 身份、目录与规则

- 任务：V09-R6-CX-04；原唯一会话 codex-06；审查人 codex-review-02。沿用原分支/工作目录，不创建新会话并擅自复用同一写目录。若需变更登记，由总控协调。
- 工作目录：`D:/admin/desktop/codex/myproject/ks/.worktrees/feature--v09-r6-platform-cx06`。
- 分支：`feature/v09-r6-platform-cx06`。
- 本轮同步基线：`db3cf87d44022be79bbec55eb4012b95f307dac7`。
- 当前 HEAD：`0bad5461fdfa1cd2a2dc38795e8cd47718e24495`。
- **工作区有大量未提交、未跟踪的产品更新实现，必须保留。不要 reset/clean/stash 或覆盖。未提交不表示可丢弃。**

先读取本工作目录 AGENTS.md，以及主目录只读任务资料：

1. `D:/admin/desktop/codex/myproject/ks/docs/development/sprints/v09-five-day/WAVE6-R3.md`
2. 同目录 `WAVE6-R2-SHARED.md`
3. `D:/admin/desktop/codex/myproject/ks/docs/development/reviews/V09-WAVE6-R2-REVIEW.md`
4. README、TASK-BOARD、MODULE-OWNERSHIP、DEPENDENCY-RULES。
5. 本 progress/wave6-r3 的 STATUS.md、SHARED-CHANGES.md，再结合本交接文件；这两个已提交文档目前主要记第一检查点，尚未补录第二增量。

授权沿用 CX 的 internal/auth/tasks/transport/files/ai/updates/recovery/plugins、Agent 与已获授权 Controller 包内文件、tests/integration/v09/platform、tests/poc/v09/platform、progress/codex/wave6-r3。不要写 DS gateway*.go、GM Web/宿主。共享中央 bootstrap/server/main、Proto/gen、DDL公共序列、根配置/锁/CI只交具体 patch，由总控接收。只总控合 main/操作 awskr；作者不推送、不 SSH、不用真实外部秘密、不操作云、不自动启动其他 Agent/代发消息。

**上轮五份 CX-03 补丁及 24 项迁移已经正式接收。不要运行 R2 的旧 prepare/overlay/补丁应用脚本。** R2正式组合 runner 已被总控改为无需 overlay，原历史资料不应再次应用。

## 已提交、已验证：文件可信 CLI 装配

提交 `0bad5461fdfa1cd2a2dc38795e8cd47718e24495`：`feat(files): load private deployment roots through explicit CLI assembly`。

主要代码：

- `internal/files/deployment.go`、`deployment_linux.go`、`deployment_other.go`、`deployment_test.go`：启动时读取私有 JSON；明确 nodeID→逻辑根和 spoolDir，拒绝重复字段/重复nodeID/未知字段/不合法根；Linux要求当前UID、父目录0700、配置0600/单硬链接/非symlink，核对打开文件身份。非Linux缺ACL实现时拒绝此加载入口。
- `internal/files/spool.go`：增加Linux spool当前UID检查。
- `apps/controller/server/file_deployment.go`：`server.LoadFileExecutionConfig(filename)`，不提供默认 AI/plugin ScopeGate。
- `controller-file-cli.patch`：仅新增中央 bootstrap 的 `-file-deployment` 与配置加载；需要管理TLS模式，失败在打开PKI/DB前终止，不能失败后静默关闭worker继续启动。
- `tests/integration/v09/platform/file_cli_r3_test.go`：实际 Controller CLI、两实际 Agent CLI；初始化管理员是明确的受信本地 installer 夹具，其后入网/审批/授权/transfer全走真实HTTPS。

干净 HEAD `0bad5461` 的第一检查点复验通过：65,000目标字节、SHA `1c306fab543a4a90a516da64c02f4dc66cbe7d2b84ffefbb83fb602bbd14dcd3`、两节点父任务/transfer终态、重复skip、未登记路径拒绝与不安全配置启动失败。Windows files/server/架构检查、Linux files模块、相关staticcheck通过。

原始证据：`reports/wave6-r3/checkpoint1-result.json`、`checkpoint1-native.log`、`files-native.log`、`file-config-tests.log`、`checkpoint-staticcheck.log`。前两份是复制保存的第一检查点固定干净SHA结果。最早两次404为我写错夹具节点路由（先详情、再state）；最终核对正式 `/api/v1/nodes/{id}/diagnostics` 后通过，失败日志 `cli-first-route-fixture-fail.log`、`cli-second-route-fixture-fail.log` 保留。

宿主例子和私有配置格式已在 SHARED-CHANGES.md。CLI补丁只待总控接收，不能因此停止其他独立工作。

## 正在进行、尚未提交：实际 Agent 产品更新装配

目标是把实际产品的可信发布源/owner计划/原始安装来源/统一审批/安装/真实版本健康/兼容回滚接成组合，替换上轮仅用测试 helper 输出版本的证据。

已写在工作区的内容：

- `apps/agent/release_health.go`、`release_health_linux.go`、`release_health_other.go`，及 `connect.go` 增量：由真实 WSS 启动成功后写私有健康文件；编译变量 `main.agentBuildVersion`，PID、supervisor nonce、nodeID、connectionID、stateFormat、trustGeneration、时间。默认没有 `SF_AGENT_HEALTH_FILE` 时不写报告。报告不是 `-agent-version` 可伪造标签。
- `internal/updates/owned_process.go`：把 `OwnedProcessObservation` 类型从 linux-only supervisor 移到公共文件，保持其他平台编译；`supervisor_linux.go` 对应移除原类型定义。
- `internal/updates/agent_ready.go`：真实PID/nonce/新鲜度/编译版本/存储格式及当前Controller WSS会话复核；supervisor独立核 `/proc/PID/exe` SHA。
- `internal/transport/current_connection.go`：观察当前真实、已验证连接，不要求文件能力，不授予任何任务权限。
- `internal/updates/product_deployment{,_linux,_other}.go`：可信程序配置、私有签名inbox/stager/slot、初始签名工件与实际active一致后同事务审计建立provenance；持久owner计划/一致SQLite备份；审批后worker、真实owned Agent启动停止、版本不符时兼容回滚、SQLInstallJournal原子来源/floor/审计；启动遇update-lock/supervisor-reservation拒绝，不接管外部PID。
- `apps/controller/server/file_update_deployment.go`：`NewOwnedProductUpdates`，将RuntimeGate、真实Tasks.CheckExecution、当前管理员、实际连接身份接入，禁止缺Link装配。
- `tests/poc/v09/platform/prepare-wave6-r3-product.mjs`：新中央 `controller-product-updates.patch`，为Config增加UpdateProduct、创建/注册worker和HTTP resolver/plan/check、启动/关闭生命周期；中央源码未直接改。会先运行新的R3 CLI准备器，与R2历史prepare无关。
- `tests/integration/v09/platform/update_product_r3_test.go`：**build tag `cx_r3_product`**，因为其引用尚未接收的Config.UpdateProduct，必须用新product-overlay编译。
- 已修改 `run-wave6-r3.mjs`：生成新CLI+product两个候选overlay，交叉编译实际Controller、普通Agent、实际Agent的1.0.0/1.0.1两份编译版本；运行R3真实组合。`--final`要求起止干净HEAD；开发态可不加但结果会标 DIRTY。

在最后一次加schema fingerprint之前，开发态实际组合 **2 PASS /0 SKIP**：

1. 文件CLI测试继续通过。
2. 实际Agent产品：HTTP申请→awaiting_approval期间PID未变→另一个管理员真实HTTP审批→1.0.1替换成功、父任务completed、真实build/PID/WSS健康、journal floor=2；再给签名manifest 1.0.2但artifact实际build仍1.0.1，检测真实版本不符，安全回滚到1.0.1新PID、父任务failed、floor=3。

这是同源实际产品的本地合成签名/编译版本证据，不是外部正式Release、浏览器或实机准入。记录在 `reports/wave6-r3/native.log`、`runner.log`、`result.json`。**其中result.json标 DIRTY DEVELOPMENT RUN；并且这些PASS早于暂停前最后一次修改，不适用于当前工作区。**

先前真实失败 `product-first-constructor-fail.log`：构造函数未将成功标志置true，defer错误关闭了对象；已修复并通过上述两项组合。相关Windows模块/架构与staticcheck在最后schema变更之前也通过。

## 暂停瞬间刚改、未格式化/未编译/未测试的部分（接手第一步）

我发现只检查PRAGMA user_version不够，DDL可以变化但version不变；正在增加可信程序配置固定的schema指纹与计划审计。最后一次补丁已写入文件，但用户随后要求暂停，故**没有继续修补/运行验证**：

1. 新 `internal/updates/schema_fingerprint.go`：`SQLiteSchemaFingerprint(ctx, db)`，把user_version与排序后的sqlite_master定义哈希，不返回业务数据/秘密。
2. `ProductDeploymentOptions.SchemaFingerprint` 新必需字段；Linux constructor要求合法SHA，schema()同时复核当前指纹。
3. 计划/备份文件改用有界regular-file打开；备份限制256MiB；计划保存后fsync目录，并补plan_prepared审计；删除无证据的 `MigrationReversible:true`。
4. 测试已计算 `schemaFingerprint`，**尚未赋给后面的 `cfg.UpdateProduct.SchemaFingerprint`**。因此当前带tag测试预计会因局部变量未使用而编译失败，即使只消除未使用也会因配置缺指纹被拒绝；应完成字段接线。
5. 上述几个文件尚未gofmt；当前不要直接声称检查通过或提交为可运行检查点。

建议立即：先阅读git diff与所有未跟踪文件，补齐指纹配置接线、gofmt；检查这批新安全逻辑，再跑开发组合。考虑将兼容指纹明确纳入冻结计划/审批摘要，测试新旧计划兼容与1024字节参数上限；目前只是运行期可信配置固定指纹，没有新增冻结计划字段。

还需审查/补验的具体问题：

- 健康报告读取目前Lstat后os.ReadFile，虽检查初始size<4096，但打开期间身份/大小变化未严格有界；应考虑打开句柄+SameFile+LimitReader与私有目录/所有权复核。不要把本地同UID文件伪造当真实权威。
- Plan创建备份/写文件与新增审计的失败/撤权行为、同计划重放/并发、修改类任务串行、关闭时仅终止自己拥有的进程都需负向。现有binding使用atomic.Pointer，Close只在supervisor-stop允许自身生命周期停止，不能放开其他安装gate。
- 新schema()可能在journal事务内读指纹，要验证查询/事务组合不会死锁，不要通过关闭门来解决。
- `Check`只宣告可信配置CandidateVersion并重新验证签名；没有凭空发现在线feed。不要把candidate不存在时的503改成假更新可用。
- UpdateSource/计划/备份/初始provenance仅来自可信程序文件和当前角色，不能加HTTP配置、任意URL/命令/密钥或直接种入伪造installed记录。
- 更新中断/主控重启、不可逆schema变化UNKNOWN、真实产品状态格式兼容仍未在这条新产品组合中补齐。上轮Linux module有实际SQLite user_version变化UNKNOWN测试，但不能冒称是本轮完整产品HTTP链。
- 第二增量尚未更新STATUS/SHARED-CHANGES、没有检查点commit或clean SHA证据。

## 后续整包未完成项（继续独立推进，不能只修更新就停止）

- 文件：远端上传/多目标分发、overwrite/rename、断连续传/冲突/部分结果等需求差距。当前产品仍仅skip/最多16项/每文件64MiB，其他策略显式拒绝。必须冻结路径/计划、当前授权和审批、UNKNOWN不重放；已成功目标不能被无条件重试改写。先读已接收file_worker/Agent帧参数约束，公共DTO/Proto变更只交精确候选。
- 恢复：本轮**还未开始实现**两目录/双Controller真实新权威和旧Controller fence。已有RuntimeGuard/QuarantineLatch入口门，但上轮verifier含double。必须证明实际新CA/信任世代、旧信任拒绝、旧进程/活跃HTTP/WSS/tasks隔离、撤销/过期任务不复活、显式释放；缺正式PKI-OPS来源应具体登记，不伪造verifier/确认短语。
- Xray/插件：本轮**尚未新推进**。历史固定26.10.29工件获取失败、安全FAIL/Runtime准入仍有效；只能先做静态来源/签名/漏洞/包与typed能力独立工作，未准入不得运行或自行改选WASM/Eino。
- 真实ENOSPC/inode容量、ARM运行、物理双机、1C1G、外部账号、root/产品发布准入仍无新证据。不要混淆实际EFBIG与ENOSPC。
- 本轮**尚未执行完整 npm ci/tools:install/check/check:security**。当前只有前述模块/静态/本机Linux专项。最终必须根检查/安全/模块负向、保留首次失败和SKIP，再写固定SHA证据与HANDOFF。旧轮根PASS不能替代本轮检查。
- 第一与第二新中央patch待总控接收，不自己改中央源码/合main；不等待一个共享patch而停止其余独立项。

## 工具与重现提示

在工作目录运行 `. ./scripts/env.ps1` 使用固定Go1.27.3、Node24.22.1、npm11。当前已有本机WSL Ubuntu，普通用户UID1000；通过Windows固定Go交叉编译后在WSL执行原生Linux二进制，不用SSH。没有安装新的本机/远端服务。

```powershell
. ./scripts/env.ps1
node tests/poc/v09/platform/prepare-wave6-r3-product.mjs
# 当前未提交进行中，修完以后先跑开发态：
node tests/poc/v09/platform/run-wave6-r3.mjs
# 可运行增量提交、目录干净后再运行 --final
```

产品测试的Go显式参数要整体引用：`'-tags=cx_r3_product'`、`'-overlay=reports/wave6-r3/product-overlay.json'`。PowerShell直接调用WSL测试二进制时 `'-test.v'`、`'-test.timeout=180s'` 也整体引用。gofmt不要把Windows通配路径当literal传入；显式列文件或用rg搜出文件再传参数。

如固定tools安装外部代理再次失败，可参考上一轮保留的相同固定版本模块缓存方式，将GOPROXY设为当前worktree `.cache/gopath/pkg/mod/cache/download` 的file URL再运行原安装器；不改pins、不关校验、不清理他人数据。

Context7已核对本轮Go1.27.3 os.Root/Lstat/SameFile。业务逻辑审查不必反复查库文档；需要新库/API语法时按AGENTS先resolve再query。当前未新增依赖。

完成每个可运行增量提交SHA/测试/剩余缺口后继续独立项。用户这次只是暂停并交接，没有取消CX-04任务，也没有授权放宽安全门或直接提交未验证工作。
