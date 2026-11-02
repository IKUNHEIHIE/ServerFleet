# Wave6-R3 · 完整 v0.9 三线连续收口包

2026-11-02；负责人/审查人 codex-review-02。负责人要求完整 v0.9、11 月 3 日目标不变；当前仍有五个正向阻塞，不承诺届时必然完成，不降低授权/审批/工件安全门槛。

先读根 AGENTS、README、TASK-BOARD、MODULE-OWNERSHIP、DEPENDENCY-RULES、[共享接收说明](WAVE6-R2-SHARED.md)和[本轮审查报告](../../reviews/V09-WAVE6-R2-REVIEW.md)。旧 WAVE6/CODEX/DEEPSEEK/GEMINI 的整包目标继续有效，下面明确本轮优先级、共享装配和所有权。本包授权连续完成多个可独立增量，每个检查点交 SHA 后继续其他独立项，不等待小项重复派单。

起点为总控最终交接 SHA；三个原目录必须干净且已由总控快进至同一起点，若不符先报告具体差异，禁止覆盖、stash/reset 他人修改。保持原作者会话和分支，不自动启动其他 Agent，不合 main、不推送、不 SSH、不使用真实外部秘密。

## V09-R6-CX-04 / codex-06

分支 `feature/v09-r6-platform-cx06`，目录 `.worktrees/feature--v09-r6-platform-cx06`；审查 codex-review-02 + 安全/通信。沿用 CX 所有权：internal/auth、tasks、transport、files、ai、updates、recovery、plugins 及已获授权的 Agent/Controller 包内文件；新增进度 `progress/codex/wave6-r3`。公共 Proto/gen/根配置/共享数据库序列及中央注册仍交 patch；不写 DS gateway*.go 或 GM Web/宿主。

1. **先交可部署文件装配**。FileExecution worker/24 项迁移已正式接收，无需再次等待中央补丁。提供可信程序配置入口的具体中央 patch：明确 nodeID→逻辑根注册表、私有 spool、Agent file-root 与生命周期、启动失败时关闭；不能开放任意 HTTP 路径配置或默认允许 AI/plugin。用实际普通用户 CLI 和两个 Agent 证明配置能生效，而不仅在 Go 测试里 new(worker)。给 GM 可直接调用的稳定宿主配置例子和错误码。
2. **完成需求内剩余文件行为**。确认上传到远端/多目标分发、skip/overwrite/rename、断连续传/冲突和部分结果的需求差距；对当前尚未实现的策略显式拒绝并记录。按冻结计划、当前权限、不可重放 UNKNOWN 和私有根边界逐项实现必需能力；不能用根目录猜测、绕开统一任务或无条件重试改写已成功目标。保持取消、撤权、接受持久化失败、已写后丢回执/重启的实际负向。若公开 DTO/Proto 需要扩展，先交精确候选/兼容说明继续独立项。
3. **更新完整产品链**。交正式 update-source/resolver/installed provenance/worker 的可信组合 patch，使用本地合成签名工件和自己拥有的真实产品进程，走当前浏览器/HTTP→统一审批→安装→版本/健康→日志/终态；包括中断重启、兼容回滚和不可逆 schema 的 UNKNOWN。发布来源未提供时继续默认拒绝，不把测试 helper 的版本输出称为产品升级。给 GM 可注入的隔离验收端口。
4. **真实恢复证明**。在两个本地普通用户隔离目录/主控进程实现可验证新权威与旧主控 fence，验证旧信任代次拒绝、恢复启动和活跃 HTTP/WSS/tasks 隔离、撤销/过期任务不复活及显式释放。缺正式 PKI-OPS 等原文时记录具体缺条，不能编造 verifier、用 double 或确认短语宣称完整恢复。
5. **插件/Xray 独立推进**。继续来源、签名、漏洞与包准入、必要 Host/配额/声明式 UI/typed task 的独立工作；未通过安全工件不得运行，不擅定 WASM/Eino Runtime。若有新工件仅静态审查与安全证据先行，未获准不得接入任意 root shell。更新 AC-01—24 的实际证据和未完成项。

停止条件：所有可独立项完成或逐项给出没有独立工作可做的具体依赖；任何一个共享 patch 等待不阻塞整个包。禁止把默认端口缺来源重复登记为全部工作已完成。

## V09-R6-DS-04 / deepseek-06

分支 `feature/v09-r6-services-ds06`，目录 `.worktrees/feature--v09-r6-services-ds06`；审查 codex-review-02 + 安全/数据。沿用 DNS/证书/网关/发布/通知/监控/配额/服务组所有权，允许直接修改 `apps/agent/gateway_capability*.go`、新增/修改 `apps/controller/server/gateway_*.go`；不写 internal/files 或其他 CX 包。进度 `progress/deepseek/wave6-r3`。中央 server/main/Agent capability 注册交精确 patch。

1. **优先交真实发布执行器**。承接总控对 gatewayTaskHost 的修正：授权/审批前零上传，TargetType=node_ids 不附 TargetResourceID，排队/awaiting_approval 不算发布成功。补完整 typed task worker、受控工件生命周期/摘要/路径映射、Agent capability 注册、等待真实执行终态与同计划幂等；审批/target revision 在上传前、每阶段与补偿前当前复核。可以用测试宿主注入完成本地闭环，不等待生产默认启用。
2. **发布成功与失败恢复**。普通用户私有 Nginx prefix、高位 loopback、本地 TLS DNS 协议端或受控预置证书，实际统一 HTTP/tasks/WSS/Agent→nginx -t/reload→HTTP 代理字节及终态；再注入 apply/reload/回执丢失和补偿失败。仅删除本计划创建的 DNS/route；部分 DNS 清理错误必须持久保留，不能吞掉。并发 run/cancel/rollback、权限撤销和 UNKNOWN 不盲重试。未有终态之前不得删除 route、返回发布成功或补偿完成。
3. **先返修 cleanup 并发，再启用真实删除**。当前 journal 已迁移并注册管理员 HTTP，默认 cleaner=nil。现 execute 先读取再无条件 setStateAt，尚无持久执行 claim：补原子 CAS/attempt 预留、并行 Retry/RetryDue 不重复删除/超额、cancel 不能被后续 resolved 覆盖、重启不盲重放未知 provider 结果、I/O 前后当前授权。补可重复的竞争负向和失败审计；RecordFailure 接所有 ACME 失败路径，精确绑定订单/FQDN/记录ID/value。新增 DDL 交候选，不改历史迁移。通过前不要求总控默认注册 cleaner/后台轮询。
4. **Agent 网关安全**。私有根/owner/权限、逐段 symlink 与路径替换竞态、固定 argv/可执行来源、artifact SHA/当前审批，失败回滚自身失败或结果不明必须 UNKNOWN。不能信任 HTTP 的 takeover_approved 字符串证明真实接管批准，不接管现有系统 Nginx/80/443。
5. **供应商与契约继续完成**。秘密借用 callback 保持当前 owner/ref revision/用途/operation/plan 绑定和 I/O 前后复权，补 Controller 组合 patch 和本地 TLS 正常/失败证据；不得给通用明文读取 API。使 services-candidate 与实际 plan/任务/清理状态和错误一致，Web 集成样例交 GM。真实外部账号仍未获准。其他通知、配额、监控、证书生命周期独立差距继续收口。

停止条件：整包可独立项完成并列明剩余依赖，不以 port/函数存在、202、排队任务或独立 Nginx 测试代替完整统一发布。

## V09-R6-GM-04 / gemini-06

分支 `feature/v09-r6-console-gm06`，目录 `.worktrees/feature--v09-r6-console-gm06`；审查 codex-review-02 + UI/安全。沿用全部 Web、tests/browser/v09，允许测试专用 fixturehost；不改生产 Go/公共契约/Proto/根锁/CI。进度 `progress/gemini/wave6-r3`。

1. **先把 B01 做成真实页面闭环**。后端 worker/迁移/路径 mapper 已在统一基线，解除旧中央接线等待。扩展 fixturehost 用可信程序端口注入 FileExecution、私有 spool 和按已入网 nodeID 的明确逻辑根表；两个 Agent 都传独立私有 -file-root。动态登记只允许测试程序内部受控来源，不能暴露无认证 HTTP 配置。实际页面创建/统一任务/如需则审批/真实源目标字节与 SHA/父 transfer 和双方终态；再覆盖 cancel/撤权/回执丢失。至少先交现已支持 skip 的可运行检查点，不能把后端已接收仍叫未接收。
2. **AI 预算正式界面**。接 GET/PUT `/ai/model-settings/{profileId}/budget`，显示 owner 当前窗口/限额/已用/剩余、缺策略拒绝、管理员 CAS 配置/409、普通用户只读、撤权/身份切换和旧响应隔离。不能从消息表或本地 state 造计数。保留本轮真实 B02：密钥私有文件、遥测 loopback/只读、预算配置走正式 Cookie/CSRF API，不恢复 /fixture/configure。
3. **把 B04/B05/B06 从探针扩为完整流程**。先搭独立隔离宿主/合成签名工件和第二主控进程的测试结构，按 DS/CX 稳定端口接真正发布/补偿、更新实际版本/健康/回滚、恢复实际执行与 fence；没有真实权威证明必须精确 BLOCKED，不把 preview/check/202 叫成功。对每项列清楚还缺哪些端口/代码/原文而不是一句“待接线”。Xray 安全工件与运行时未准入则 B03 保持关闭，同时完成不依赖工件的 UI/错误/权限工作。
4. **稳定身份与证据**。保持 checkbox 的 nodeID value/可访问名称和同名逆序回归；P13 先断言服务端目标快照，Operator 仅对该节点授权，不以改角色绕过 403。补当次 target/subject/grant 与响应的脱敏证据，保留原 403。浏览器服务使用已完成的 dist，不能与同目录 Vite 重建并行运行（本轮桩截图曾出现静态 not found）；保留原失败并说明环境原因，不降低断言。
5. **完整验收矩阵**。回归70/70、桩16/16和严格正向套件分别报告；500/401/403/格式错/超时为 FAIL，明确预期拒绝仅在相应负向步骤通过。当前21 PASS/5 BLOCKED不是完整验收；必须 --require-complete，剩余BLOCKED退出1。AC-01—24分源码/模块/页面正常与负向/真实外部/发布准入，关联精确 clean SHA，不只写截图数量。

停止条件：全部可独立前端/宿主项完成，后端依赖给最小可运行失败用例；不得因为一个后端端口等待而停止 AI 界面、文件宿主或其他独立项。

## 三位共同交接

每个可运行增量（或 2—4 小时）交 SHA、文件、命令/原始输出、真实正常与负向结果、已知依赖，然后继续独立工作。根要求 npm ci、tools:install、check、check:security 与相关模块/负向检查；受环境阻塞时保留精确失败，不删测试/伪造通过。中央共享文件交可应用差异和兼容/回退说明，DDL 交新候选并由总控登记；不要重放已接收补丁。

最终交接列全部提交、干净 HEAD、证据/输出/候选依赖/剩余风险。仅总控合 main 和在指定 awskr 私有目录操作隔离测试；没有自动消息派发或自动启动。主控本次完成接收与派单后停止；三位开始由负责人把对应提示发回各原对话。
