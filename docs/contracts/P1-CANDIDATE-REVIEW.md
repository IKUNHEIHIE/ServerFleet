# P1 候选契约审查与冲突登记

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
