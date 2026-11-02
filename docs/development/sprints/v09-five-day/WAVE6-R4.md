# Wave6-R4 · 三线连续交付包

2026-11-02；总控/审查codex-review-02；11月3日完整v0.9目标不变，当前不能保证按时全量完成。原需求/安全约束保持，不以测试通过数替代产品验收。

先读根AGENTS、README、架构说明、TASK-BOARD、MODULE-OWNERSHIP、DEPENDENCY-RULES、[共享接收](WAVE6-R3-SHARED.md)与[本轮报告](../../reviews/V09-WAVE6-R3-REVIEW.md)。下面是对既有整包的继续授权，已接收补丁不能重复作为阻塞。精确起点为总控最终交接SHA；只有目录干净、原HEAD未变时由总控快进，各自核对后在原目录/分支工作。

## V09-R6-CX-05 / codex-06

分支`feature/v09-r6-platform-cx06`；目录`.worktrees/feature--v09-r6-platform-cx06`；审查codex-review-02+安全/通信。沿用CX包，独占internal/files/tasks/transport/AI/updates/recovery/plugins及已授权Agent和Controller文件；不写DS gateway*.go或CA Web/宿主。进度`progress/codex/wave6-r4`。

1. **首个可运行检查点：上传统一任务化。** CompleteUpload当前校验后直接调用transport的模式尚未完成统一任务/审批。补上传计划/任务绑定/worker、当前owner/目标权限、逐帧授权、任务/上传实际终态与取消，未知结果不重放；通过真实HTTP上传字节→统一任务/必要审批→实际Agent目标文件/SHA验证。新增Controller upload_*.go可写，已有server/bootstrap中央注册交patch。不得另造执行队列或仅把直接传输包一层“任务成功”记录。
2. **剩余文件需求连续完成。** 多目标分发冻结每目标身份/权限/计划/子结果，不能因分组变化扩权；部分结果保留已完成节点，不整批重试。跨重启续传需要持久offset/绑定/校验和明确恢复语义，不以重新从头写替代。可新增`api/openapi/revisions/v09-r8/files-candidate.yaml`和新迁移候选（建议cx/0010 namespace，最终编号由总控登记），不改历史DDL/旧候选/Proto号。中央契约/生成/公共schema仍交具体差异并继续其他独立项。
3. **产品更新剩余项。** UpdateProduct/CLI文件配置已接收，不再等待。补同计划并发apply、修改类串行、Close仅停止自有PID及失败审计负向；明确Controller组件更新、可部署配置/发行源和Agent组件已有实现之间的差距，按需求补可信装配。不要把Agent签名本地inbox测试叫成Controller升级/在线发布准入。把稳定程序配置样例给CA，不要求其再等待已存在接口。
4. **恢复/信任证明。** 继续两隔离目录/实际Controller进程的恢复执行和旧主控隔离、新信任代次拒绝、撤销/过期任务不复活证据；先交可运行进程/证据绑定/验证端口与负向。正式PKI-OPS/PRIV等缺条逐条列清，不能用double/确认短语/现有健康状态证明真正新权威，不因此停止文件和更新独立项。
5. **Xray/插件与发布差距。** 继续可复核来源、签名/漏洞、Host/配额/声明式UI和typed任务的独立证据；未通过工件不运行，不擅定WASM/Eino。公共发行材料获取失败如实保留。更新完整需求差距与实际证据，不把“本轮未做”列为已完成。

DS-05现在负责完整网关工件链，CX继续用户文件上传/分发；两者都消费同一统一任务/transport安全边界，不互改目录。

## V09-R6-DS-05 / deepseek-06

分支`feature/v09-r6-services-ds06`；目录`.worktrees/feature--v09-r6-services-ds06`；审查codex-review-02+安全/数据。沿用DS领域、`apps/agent/gateway_capability*.go`、`apps/controller/server/gateway_*.go`；另允许新增`apps/agent/gateway_registration.go`及对应测试、`apps/controller/server/gateway_artifact.go`/`gateway_execution.go`等gateway_*.go；可调用既有transport文件端口，不写internal/files。进度`progress/deepseek/wave6-r4`。已有connect/runtime/main/server中央文件交精确patch。

1. **独占首要整链：实际网关工件→执行→代理→补偿。** 交可运行Controller程序组合与Agent能力配置patch，把NewProductionPublishingBridge、typed执行、实际文件通道上传/摘要/私有路径、当前任务/审批/target revision和真实终态接成一条链。不得继续把“CX尚无ArtifactUploader”当整包硬阻塞；新增gateway_artifact/registration已获授权。queued上传与CheckExecution只接受running的矛盾要统一解决，可调整为统一worker生命周期或提供等价受审阶段门，不能放宽为任意queued任务即可上传。
2. **实际组合证据。** 在一个Controller+实际Agent WSS+普通用户私有Nginx prefix/loopback端口下，从公开HTTP发布、真人异人审批、工件到达、真实nginx -t/reload、HTTP代理字节、持久阶段/任务终态；再失败/取消/撤权/目标变化/回执丢失和逆序补偿。模块假dispatcher与独立WSS capability两组成功不能合称端到端。提供一次可复现完整命令与最小可信程序配置，默认缺配置拒绝，不能接管系统站点/80/443。
3. **ACME与cleanup产品组合。** ds/0063已正式登记共25项；权限/候选登记也已完成。把PublicationACMEIssuer/RecordFailure接实际证书签发/失败生命周期，实际cleaner只允许精确订单/FQDN/recordID/value和当前凭据作用域，默认nil不擅自启用外部删除。把可安全重试与副作用未知的provider错误区分，未知网络结果不能仅按普通error自动重试；补取消/进程死亡/lease过期/审计失败和重启不重放。用本地TLS协议端完成，不用真实账号。
4. **保持发布和供应商安全。** 保留同计划幂等、taskID工件绑定、每网关显式私有根、失败补偿摘要和DNS部分清理保留。callback借用限制owner/ref revision/purpose/operation/plan及I/O前后当前授权，不增加明文秘密读取。为CA给出已经可运行的注入样例与DTO，不只交接口名。
5. **其余DS独立项**按原全量包持续补通知/配额/监控/证书生命周期的真实正常和负向证据；真实外部账号/生产接管仍未获准。

## V09-R6-GM-05 / codex-arts-01（Codex Arts）

分支`feature/v09-r6-console-ca01`；目录`.worktrees/feature--v09-r6-console-ca01`；审查codex-review-02+UI/安全。沿用Web、tests/browser/v09和测试专用fixturehost；进度`progress/codex-arts/wave6-r4`。不改生产Go/公共契约/根锁/CI，不回旧Gemini目录。

1. **首个检查点：B05真实Agent产品更新浏览器宿主。** 直接消费已接收`server.Config.UpdateProduct`、`updates.ProductDeploymentOptions`及`update_product_r3_test.go`的实际签名inbox/owner配置样例，准备独立私有目录与自己拥有的Agent进程，避免与已有双Agent复用同一身份进程。浏览器check→plan→异人审批→apply→真实版本/健康→失败回滚及重启日志/终态。可先交独立专项，再合正向总套件。端口已存在，不以“等CX给签名”停止；缺具体可信项给最小配置差异，不能用测试helper版本输出或伪造installed状态换通过。
2. **B04实际发布与B06实际恢复。** 继续可独立宿主/页面/脱敏证据结构，按DS完整组合与CX实际恢复检查点接真正执行/补偿和新权威/fence。第二Controller仅健康/独立DB/PKI仍只是隔离基础，不代表恢复。禁止把探针或202/preview当完成。对后端依赖给精确请求/响应/代码接缝和已完成的独立工作。
3. **保持B01与预算成果，补其余负向。** 总控已修queued取消；本轮正式接收overwrite/rename，浏览器已改为未知策略拒绝并新增真实字节/实际路径/两端终态检查，不恢复旧“overwrite必须400”。继续symlink、混合partial、断连/大小限制与权限页面负向；续传未提供则精确说明范围。预算UI/409/只读/403隐藏计数保留，不重做技术栈/整体设计。
4. **证据和全量矩阵。** 按最终组合SHA更新AC-01—24和正常/负向/外部/发布准入五层证据；根全量、Live70、Stub16、严格正向分别报告。500/401/403/错误shape/超时为FAIL，只有明确依赖或已知未配置条件BLOCKED，严格模式有BLOCKED必须退出1。保持nodeID选择和真实目标快照；保留原失败，不降门槛。
5. **完整Web独立项**按旧GM整包继续；Xray工件/Runtime未准入时关闭相应执行，继续可独立UI/权限/错误工作。测试程序不进入产品，私有配置文件/合成秘密不落URL/明文日志，不恢复fixture配置旁路。

## 共同交接和停止条件

每个可运行增量或2—4小时交SHA、文件、正常/负向结果、命令/原始输出、候选依赖/风险后继续其他独立项。根要求npm ci、tools:install、check、check:security及模块负向；库/框架/SDK/CLI具体文档按Context7。不得并行重建浏览器使用中的同一dist。中央共享差异交精确可应用patch，历史prepare不得重放。

整包全部可独立项完成或逐项证明硬依赖、确无独立工作后停止，交干净HEAD及全部证据等总控审查。仅总控合main/操作既有授权awskr隔离目录；不推送、不SSH、不用真实外部秘密/云API、不自动启动Agent或代发消息。主控完成本次接收与续派后停止；负责人将对应任务交回原三对话。
