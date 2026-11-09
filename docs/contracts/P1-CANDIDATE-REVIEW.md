# P1 候选契约审查与冲突登记

2026-11-08主控R11候选：用户上传三策略/真实回执以及显式bindingVersion=2；新增ds/0043（29项），旧28语义与历史SQL不改。详见[兼容差异](revisions/v09-r11-upload-policies.md)，尚不冻结G2、不关闭P1-C10/发布准入。

2026-11-04主控增量：AM-02首审A4的独立新增候选位于`api/openapi/revisions/v09-r9/file-distributions-candidate.yaml`，覆盖5条冻结多目标分发路由、202/task绑定、逐目标投影、取消与400/404/409/422/503等错误语义。新候选由主控协调随#11返修提出，Redocly独立登记；现有3原候选、R8、Proto/gen及兼容基线不变。它不批准完整持久续传、不冻结G2，P1-C10/资料与发布缺口仍OPEN。

日期：2026-10-29；状态：**G2-CANDIDATE-NOT-FROZEN**。本文件记录工程诊断，不替代产品/安全 ADR，不宣告 G2 Gate 通过。

## 原始来源与工作入口

`SOURCE-INDEX.json` 列出完整来源。69 项原始清单和三份 OpenAPI 工作副本由 `verify-source.mjs` 每次验证字节一致；资料原文和历史 ADR 未改。

| 工件 | 原始候选 | 工程候选/状态 |
|---|---|---|
| G2 Core OpenAPI | 现行基线 `g2/G2-Core-OpenAPI-candidate.yaml` | `api/openapi/g2-core-candidate.yaml`；29 路径/34 操作，字节不变 |
| G2 Merged OpenAPI | 现行基线 `g2/ServerFleet-API-Contract-v0.9-G2-merged-candidate.yaml` | `api/openapi/g2-merged-candidate.yaml`；147 路径/185 操作，字节不变 |
| Xray OpenAPI | 现行基线 `Plugin/xray-manager/Xray-Manager-OpenAPI.yaml` | `api/openapi/xray-candidate.yaml`；19 路径/23 操作，字节不变 |
| Agent Protobuf | 现行基线 `g2/proto/serverfleet/agent/v1/agent.proto` | `proto/serverfleet/agent/v1/agent.proto`；独立 **M0-P1-r1** 修订候选 |

## Proto 实际诊断

可重复诊断命令：`node scripts/diagnose-original-proto.mjs`（Go 工具先安装）。原 g2 目录执行：

- `buf build`：PASS，原协议能够编译；不能把它写成 Proto 语法失败。
- `buf lint`：FAIL，原 buf.yaml 未声明模块路径 `proto`，规则提示 package `serverfleet.agent.v1` 应位于 `serverfleet/agent/v1`，实际相对根为 `proto/serverfleet/agent/v1`。
- `buf generate --template <本地 protoc-gen-go 配置>`：FAIL，关键错误 `unable to determine Go import path for "proto/serverfleet/agent/v1/agent.proto"`，缺少 `go_package` 或生成器 M 映射。

**独立修订内容**：根 buf.yaml 显式 `modules: [{path: proto}]`；M0-P1-r1 仅增加 `go_package = "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1;agentv1"` 并运行 `buf format`。原始字段、编号、oneof、enum、WSS/mTLS 语义均未改。逐行差异保存在 `proto-M0-P1-r1.patch`。本地包路径只服务当前无远程工程；未来 module 改名需要显式迁移。

修订候选已实测：STANDARD lint、格式、编译、Go 生成、生成一致性、FILE 兼容基线、Go 序列化往返与损坏帧拒绝。另与原候选 descriptor 进行 WIRE breaking 检查通过；这只证明本次修订未改线格式。测试基线位于 `tests/contracts/proto-baseline`，不代表产品冻结；禁止为消除错误直接更新基线。

负向工具验证：把字段 1 类型改成 string，Buf 报 `changed type from uint32 to string`；复用字段号 2，编译报 `field number 2 used more than once`。这些是**预期拒绝**，不属于修订候选失败。

## 现行资料冲突和待评审

| 编号 | 问题与证据 | P1 处理 / 后续建议 |
|---|---|---|
| P1-C01 | 原 Buf 根路径和 Go 包路径缺口（见上） | 独立工具链/Proto 修订候选；须契约 owner 审查，尚未冻结 |
| P1-C02 | 原 API Bearer/JWT 描述与 G2 Cookie+CSRF 候选混用 | 原文不改；先由 Codex 写认证迁移、token 生命周期与 CSRF 兼容方案，再实现真实登录 |
| P1-C03 | 旧 BatchTaskJob.state 与 G2 TaskSummary/状态机的 unknown/paused 等不一致 | 不造第二状态源；统一迁移和完整状态枚举后才能做真实调度 |
| P1-C04 | 旧 Error/成功响应/分页与 RFC9457、202、page/ETag 未统一 | 逐操作兼容评审；lint PASS 不等于语义统一 |
| P1-C05 | 旧 ADR 示例 `/jobs` 与当前 `/tasks` | 依据 G2 统一规则用 `/tasks` 作为候选公开入口；保留旧 ADR，禁止双队列 |
| P1-C06 | Release-History 文件标题及开头仍称 v0.7；PRD 局部仍有“待 ADR 修订”的旧语句 | 不用历史覆盖 v0.9 和 G1 Accepted；新索引说明正式替代关系，原文哈希不变 |
| P1-C07 | 部分 PRIV/WC/PKI-OPS/UPDATE 原始 ADR 未单独收录 | 本阶段不实现相关业务；后续安全实现前从正式归档补齐核验，不能猜测 |
| P1-C08 | Protobuf 帧/心跳/缓存/版本协商、mTLS、持有证明、去重和恢复无真实 PoC | 参数仍候选；不得从编译成功推断通信安全或 1C1G 性能 |
| P1-C09 | WASM Host ABI、签名、配额、Xray/Nginx/灾备 PoC 未完成 | 不锁定运行时，不实现插件业务；按 G2 E01～E08/G1 PoC 门槛推进 |
| P1-C10 | 节点只读适配的资料范围不一致：Core候选只有单节点详情，但Merged候选已有GET /api/v1/nodes（listNodesForTransfer、NodeSummary，含online布尔及可空CPU/内存），不同于NodeDetail的status/revision模型；旧指标没有采样时间 | OPEN：G2-R02A/r03已完成来源/字段/错误矩阵并接收为评审工件；总控完成D08检查接线及两处Web范围注释修正。D01–D07的字段/认证/错误/采样/空库存预检与列表原子视图仍待批准和真实验证。不替换原GET、不凭空补状态/版本/时间；旧候选/r02/原始资料不变，运行时profile和G2冻结均未批准。详见G2-R02A-REVIEW |

P1-C10补记（FOLLOWUP03复审）：AUTH-01B/N03-A离线预检/共同范围读取和W05隔离加载容器已接收，不代表真实身份/HTTP/库存事务或浏览器接线完成。READ-PLAN-01草案d0709fc把已有Status/ObservedAt与缺失字段混淆，并遗漏原列表onlineOnly请求兼容，已登记RP01-R01/R02并退回，未进入main批准包；来源事实、可选元数据与r03提案的批准级别须分别核对。先列表后详情、Cookie等仍只是待批建议，D01–D07/P1-C10保持OPEN，详见docs/development/reviews/FOLLOWUP-03-REVIEW.md。

P1-C10补记（FOLLOWUP04复审）：READ-PLAN-01-R1的4da769c修正上述两项，RP01-R01/R02关闭；总控澄清公开可选metadata string缺失省略、局部VM可null，不改schema。方案接收为PROPOSED，安全来源包READ-SEC-01亦已接收；W06虽有19项浏览器PASS记录，但同ID重复激活/关闭与迟到失败/卸载覆盖尚缺，待W06-R1，未批准真实HTTP/身份/字段迁移。原r03哈希不变，D01–D07/P1-C10仍OPEN；详见FOLLOWUP-04-REVIEW.md。

P1-C10补记（W06-R1收口）：2c1c8bc报告及作者键盘/迟到结果证据接收，总控补齐真实同key旧拒绝与两次独立卸载3项浏览器证据，W06-R01/R02关闭；作者25个断言不等于其C24前提正确，报告已区分。方案/安全/隔离浏览器材料齐备，OWNER-CHOICES.md等待负责人选首版范围与凭据设计方向；尚未授权新实现、冻结G2或关闭D01–D07/P1-C10，原r03哈希不变。

P1-C10补记（A1+B1设计派发）：负责人在会话明确回复“按推荐 A1+B1”，确认先legacy兼容列表、后详情与浏览器服务端会话Cookie设计方向。只授权READ-AUTH/HTTP/WEB-DESIGN-01的具体设计、兼容差异和验收计划；原Bearer列表的Cookie迁移、D06错误映射/D07来源与撤权窗口等仍需组合评审，不能据此改schema或启动实现。D01–D07/P1-C10仍OPEN，r03/原候选/已批准ADR不改，见OWNER-CHOICES和READ-DESIGN-01-WORK-PACKAGES。

P1-C10补记（三设计首次复审）：认证48409bac文档接收为PROPOSED，非__Host名/TTL/opaque Token/Tgate等具体提案批准；HTTP ece5efd缺身份源不可用分支及最小可信来源方案，Web 63b0feb误将403清会话且未限制非在线无时间百分比，各2P2退回。完整原文保留归档，未接收项不参与main，D01–D07/P1-C10仍OPEN；组合差异见node-read-01/DESIGN-INTEGRATION-STATUS.md，返修见READ-DESIGN-01-REPAIR，r03哈希不变。

P1-C10补记（2026-10-30设计返修复审）：HTTP 4fbdcf5补身份源503与最小可信来源，Web 39b2b7c区分403/会话并禁止非在线历史百分比，原四P2关闭；两份原设计及R1接收为PROPOSED。总控补权威查询前不预判有状态秘密/撤销、viewEpoch参与key且终止错误关闭自动加载、onlineOnly请求错误等文义。具体Cookie/Bearer迁移、状态映射/错误码、来源/Tgate、跨标签恢复仍待准入，真实身份/HTTP/存储未实现；W07纯展示组件仅准备待批提案。D01–D07/P1-C10保持OPEN，r03/原始资料/已批准ADR不改，见READ-DESIGN-R1-REVIEW及DESIGN-INTEGRATION-STATUS。

YAML 输入有大量正常锚点重用，默认库别名上限曾触发拒绝；工程只把上限设为有限 10000，并限制单文件 2 MiB、检测重复 key/避免重复遍历，未关闭保护或改写候选。Redocly `spec` 与补充规则对三份文件均通过；另有损坏 ref、重复 ID、路径参数和重复 YAML key 负向样例。

P1-C10补记（完整v0.9五日冲刺）：负责人明确“五天内完整v0.9”，本地实施授权扩为CX/DS/GM三个复合包，旧W07待批并入GM-03；不再受旧三设计仅文档限制。具体兼容/安全/来源/状态/错误/撤权仍需第一批接缝及后续增量证据审查，完整产品范围不自动批准D01–D07/G2、WASM/Eino或外部真实执行。五日目标风险和24项验收/PoC责任见sprints/v09-five-day/PLAN.md、ACCEPTANCE.json；原始资料/候选/历史ADR不改，不以mock或缺环境替代通过。

P1-C10补记（2026-11-01 Wave6增量组合）：独立AI设置/显式生成候选位于`api/openapi/revisions/v09-r6/ai-candidate.yaml`，原三候选/69原文不改。当前管理HTTP采用`__Host-sf_session`，旧候选`sf_session`仍是未关闭的迁移/冻结差异；新增候选明确真实Cookie名，不视为D01—D07批准。DS的rollback-all路由保持实现候选；文件能力/minor、TaskOffer绑定及路径/ID兼容仍待运行时审查，编译候选接收情况见下一条。已登记主控持久密钥/AI与平台注入接缝，默认远端文件缺通道失败关闭；详见development/sprints/v09-five-day/WAVE6-SHARED.md。本地与指定awskr隔离测试获当前授权，真实高权限、生产与外部账号仍未准入。

Wave6后续共享检查点：Proto FileChunk33/34、FileOpen35/36已按固定工具接收为可编译候选（非G2冻结），chunk另加cancel=8，open携带plan digest且必须与已接收TaskOffer核对。默认Validator/CLI仍拒绝文件动作；不改旧字段号/基线/r03 provenance，不授权运行时。旧未知字段负向改用仍未知的100并新增四种文件消息往返及两个方向默认拒绝，具体字段与后续门控见WAVE6-SHARED。

正式允许真实节点/高权限任务前，需要 G2 C01～C07 审查和 BOOT/PRIV/TASK 必需安全验证。当前已有本地实现与隔离实机证据，完整功能和发布准入须分别验收。

Wave6-R1补记：接收File*显式minor1/capability/当前trusted gate及Controller注入接缝，默认CLI无完整文件worker/路径映射，不能以模块WSS通过批准默认运行。AI预算为同DB持久事务+typed/通用审计，只有程序注册管理端口，无新预算HTTP；模型设置沿用独立R6候选。新增cx/0007与ds/0081迁移，发布目标/targetRevision/targetsPersisted为实现候选，与原OpenAPI差异交下一轮独立v09-r7候选，旧三候选/69原文/Proto基线及r03不变。真实更新/灾备权威、Xray工件/Runtime及G2发布准入未关闭，详见development/sprints/v09-five-day/WAVE6-R1-SHARED.md。

Wave6-R2补记（2026-11-02）：正式组合统一文件worker、owner预算GET/管理员CAS PUT及更新/恢复运行门；新登记cx/0008、cx/0009、ds/0062后共24项迁移，旧checksum保留。独立v09-r7的ai-budget/services/updates候选已登记lint；更新实现id/taskId/status/targetVersion与旧jobId/state/toVersion存在显式DTO差异，不以新增候选宣称原接口全兼容。cleanup管理员HTTP默认无真实cleaner，执行claim/取消竞争未关闭；生产发布bridge/网关typed worker待完整装配，不能用排队任务当终态。默认CLI可信路径/来源配置及恢复新权威/fence仍待交付。原三OpenAPI/69原文、Proto/gen/旧兼容基线/r03未改；D01—D07/P1-C10/G2仍OPEN。详见development/reviews/V09-WAVE6-R2-REVIEW.md与WAVE6-R2-SHARED。

Wave6-R3补记（2026-11-02）：已接收文件可信CLI、skip/overwrite/rename和实际Agent产品UpdateProduct程序装配；策略沿用既有string参数，不改Proto/gen。ds/0063_cleanup_claim新增token/lease/unknown并重建保留旧数据，正式25项迁移，旧checksum不变；降级须备份恢复，不能把新库直接交旧状态机。R7 services候选更新实际502/unknown/409语义，原候选不改。queued取消已补原子终态与claim互斥。发布工件queued阶段与CheckExecution要求running仍需统一；预算界面/文件页面/本地签名Agent更新证据不等于完整产品发布、Controller升级或新权威灾备。原资料/兼容基线/r03不变，D01—D07/P1-C10/G2与正式资料缺项保持OPEN，见development/reviews/V09-WAVE6-R3-REVIEW.md。

集中代码完成包R12候选（UNTESTED）：Controller本地统一任务/加密备份/离线隔离恢复、32项追加迁移与多网关结构回执差异见[审查登记](revisions/v09-r12-backups-controller-tasks.md)。主控单写，旧候选/Proto/gen/原始资料/已接收SQL/hash保持；未冻结、未获实际fence/Runtime/发布认证。

集中代码包R13（UNTESTED）：可信HTTPS Agent工件拉取经Controller本地统一任务，独立获取/安装，追加第33项迁移，见[差异](revisions/v09-r13-release-source.md)。固定地址/签名/实际字节和UNKNOWN不重放有效；没有默认外部源/秘密/新Runtime或发布认证。

R14集中编码候选（UNTESTED）：节点本地网站私钥/公开CSR统一任务、精确待派发复权和公共回执事务；新增第34项迁移。见[差异](revisions/v09-r14-node-csr.md)。旧候选/Proto/gen/已接收SQL保持；CSR不是ACME签发或HTTPS成功，不关闭B04/G2。

R15集中编码（UNTESTED）：公开证书链批准安装/节点私钥留存/typed终态、第35项迁移和nodecert发布/TLS叶指纹绑定，见[差异](revisions/v09-r15-node-certificate-chain.md)。通用任务参数1024字节限制不放宽；仅严格公开链专用载荷有界例外。跨节点证书槽拒绝，不据此关闭完整ACME/多节点HTTPS/B04/G2。

R16集中代码候选（UNTESTED）：不可变多节点独立证书集合及按实际gateway节点选择路径/叶证书，新增第36项迁移，见[差异](revisions/v09-r16-node-certificate-sets.md)。不复制网站私钥、不跨节点套用单槽、不把集合或任务登记称作HTTPS成功；完整真实组合与G2/发布仍OPEN。

R17 集中编码候选（UNTESTED）：实际节点 CSR 的批准公开签发、私有 ACME account signer 与回调式固定版本 Cloudflare 凭据、Controller DNS 检查/已有 Zone 导入任务、cleanup 当前 claim/lease，以及 DNS 意图/实际 ID/未知结果持久证据；新增第 37 项迁移。见[差异](revisions/v09-r17-public-certificate-tasks.md)。不导出网站私钥、不复用 Root/Issuer、不放行旧直接 HTTP provider 执行、不自动重放未知 CA/DNS 副作用；尚未编译或验收，G2/发布保持 OPEN。

R18 集中编码（UNTESTED）：正式 DNS 记录 create/update/delete 强制统一 modifying 人工审批任务，当前 Zone/凭据与记录快照复核、有界公开 record 片段、正式 Web 任务回执与 UNKNOWN 分类。见[差异](revisions/v09-r18-dns-record-tasks.md)。不扩大通用参数限额、旧直接 HTTP 无 provider scope、不新增迁移；发布父计划 DNS 接线仍须继续，G2/发布不关闭。

R19 集中代码（UNTESTED）：发布 DNS 父计划/网关 IP/服务组与凭据绑定、完整 batch 人工审批任务、逐记录持久意图与 UNKNOWN、202 同任务继续、独立逆序补偿和正式观察页面。新增 ds/0082（预计 38 项），见[差异](revisions/v09-r19-publication-dns-tasks.md)。Cloudflare 同 ID 操作前读取核对不是原子 CAS；无外部并发/多主 fence 承诺。旧契约/SQL/Proto/gen 保留，实际整链、G2/B03—B06/发布仍 OPEN。

R20 五项核心包首个代码检查点（UNTESTED）：普通程序 profile、独立 root-owned helper/mTLS 当前真人审批、固定程序字节与 ledger、持续复权、正式命令表单，见[差异](revisions/v09-r20-command-profiles.md)。无 Proto/gen/迁移/锁/CI 变更，普通能力不默认提权，AI/plugin 不开放新旁路，未执行 sudo 或实际命令。其余四项及集中验收仍进行中。

### R21 — Agent file execution resume (candidate / UNTESTED)

New files.resume permission/task and files.resume.v1 capability are explicit, Web-only human-approved modifications. Existing file capability unchanged; optional resumeFrom is accepted only by the new capability. No generated wire changes: existing resume_offset carries the new verified offset. Original executions are immutable; commit-intent/unknown final publication cannot be resumed. Private confirmed-prefix adoption and retained source require real restart/permission/negative verification in the final consolidated acceptance.

### R22 — supervised Controller/Agent update (candidate / UNTESTED)

Typed Agent apply is explicitly enabled by node-owned mailbox configuration and remains WSS-dispatched. Read-only/current mTLS admission and commit are Controller-owned, not a new inbound execution endpoint on an Agent. Existing unified modifying task batches supply ordered canary then five, independent current human administrator approval, stop on failure/UNKNOWN, and per-node results. Controller handoff uses the external owned supervisor and separate historical install receipt; original interrupted tasks remain UNKNOWN. New updates→transport edge consumes only the typed message. No generated protocol or historical schema changes. Standalone Linux supervisor, actual signed builds, independent health/storage and restart/negative evidence await consolidated verification.
