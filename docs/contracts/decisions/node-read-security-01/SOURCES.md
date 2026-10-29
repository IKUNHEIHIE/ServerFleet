# READ-SEC-01 · 安全来源与效力核验

会话 codex-01；审查人 codex-review-01；2026-10-29。核验基线为 `7ef78771da410a06d62a8605058e67a2f066ceb5`。只使用本分支已提交内容、现行原始资料及其索引；未读取其他作者在编目录、外部私人归档或凭据。本文不改变批准状态。

**标记含义**：产品约束＝G0现行需求；已批准＝有明确Accepted记录的适用条款；候选＝尚待批准的契约/建议；事实＝本基线源码可见行为；历史证据＝已提交复审/输出，非本轮重新执行。代码注释中的可信调用约定不等于可信来源已经实现。

## 原始依据、适用条件与替代关系

下表短句是核对定位用的原文片段，不是另立规范。链接含本基线的一基行号；章节、适用范围和批准效力以对应原文为准。

| ID | 原始文件、章节/行 | 短句原文 | 效力及本任务可得结论 |
|---|---|---|---|
| S01 | [功能需求 §2.2.3](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md#L85-L94) | `分类计数和所有节点列表均须使用当前用户的可见节点范围` | 产品约束。普通节点列表/分类不可泄露无权节点；不是已实现HTTP过滤的证据。 |
| S02 | [功能需求 §10.2–10.3](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/ServerFleet-Functional-Requirements-v0.9.md#L373-L398) | `客户端传入资源 ID 不构成授权凭证` | 产品约束，具体段落适用于AI工具/会话。默认拒绝、资源目录读取独立、后台统一校验及真实负向验收不能由前端模拟替代；不把AI审批期限移作浏览器会话TTL。 |
| S03 | [G1批准记录 §六项正式决定](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/G1-DECISIONS-Accepted.md#L3-L24) | `架构治理方向获批 ≠ PoC 通过 ≠ G2 API 冻结 ≠ 功能开发完成` | 已批准架构方向；D1为Controller/Agent/Web方向，D4为AI权限硬限制，D5继承不冲突旧条款。未批准Cookie/Bearer选择、HTTP映射或存储原子方案。 |
| S04 | [来源与替代索引](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/SOURCE-AND-SUPERSESSION-INDEX.md#L3-L14) | `不得虚构为已内置本包` | 效力索引。PLUGIN v0.3仅替代旧D2，DEPLOY v0.3扩展旧D2，未替代旧条款继续有效；PRIV/WC/PKI-OPS/UPDATE原文未收录。 |
| S05 | [AI/PRIV解释性附录 §1–2](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/ADR-AI-PRIV-001-Approval-Interpretation-Accepted.md#L3-L26) | `解释性补充，不替代既有权限模型` | Accepted。当前RBAC与会话范围/能力/审批取交集；前端隐藏不能代替后端鉴权；撤权阻止后续动作；API/日志/模型上下文不得显示所列私钥/Token明文。适用AI/受控能力，不等于普通只读API必须实现AI任务审批。 |
| S06 | [BOOT §12已批准决定](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/history/05-ADR-BOOT-001-Agent一键安装与安全注册-已批准.md#L289-L300) | `批准不代表强制冻结某种 HTTP 字段名` | BOOT-D1～D6有批准记录；D2首次信任、D4重装身份约束属于Agent引导。不能拿Agent注册令牌期限/证书机制充当Web登录、Bearer签发或Cookie策略。草案接口段不因文件名含已批准就全部冻结。 |
| S07 | [历史ADR登记开头及R03/R04/R06/R07](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/docs/ADR-REVIEW-REGISTER-v0.9.md#L1-L15) | `不得以此历史表覆盖新的批准记录` | G0时间快照及间接引用，不是新批准。不能用旧待批准列否定S03/S05，也不能把它对缺失ADR的概述当成完整原文。 |
| S08 | [G2核心规范 §1–3](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-CORE-CONTRACT-RULES.md#L3-L27) | `不接受客户端自报角色` | 明确为候选：Cookie/CSRF、受限Bearer、注销/停用失效、Problem、可统一404等均有来源，但具体实现仍待G2/D05/D06审查；JWT标注不等于已选JWT。Web身份、Agent mTLS、注册Token和插件凭据不是同一授权。 |
| S09 | [G2冻结/缺口表](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/g2/G2-FREEZE-AND-GAP-REGISTER.md#L3-L22) | `冻结尚未批准` | 候选门槛：C01错误、C02浏览器/会话、C03 RBAC及E08旧接口迁移都尚需证据，不能把缺口表本身当验收通过。 |

S04及S03给出的替代顺序有效：较新明确批准的替代条款优先；未替代的老条款保留；需求、实现许可和接口候选分层。原始架构基线[§当前决策/并行边界](../../../reference/ServerFleet-ALL-v0.9-G2/01-当前开发基线/architecture/ARCHITECTURE-BASELINE.md#L3-L33)进一步说明技术方向批准与运行验证的区别。没有用历史PLUGIN/DEPLOY段落或旧登记推翻新批准，也没有重写历史材料。

## 未收录安全ADR：只记录证据缺口

本分支 `docs` 下按文件名检索 `*PRIV*/*WC*/*PKI*/*UPDATE*`，仅找到S05解释性附录；S04明确列出四份原文未收录。这里的“未收录”不等于“从未批准”或“已失效”，也不补写不存在的原文。

| 被引用原文 | 当前可用证据 | 对D05/D06/D07的影响与应补证据 |
|---|---|---|
| ADR-PRIV-001 | S03 D5继承、S04缺失说明、S05正式解释、S07间接概述 | 权限/身份/凭据硬边界的完整适用条款尚不能逐字核验。负责人须从授权的原项目归档补充原文、版本/批准/替代关系及适用于只读链路的条款；本任务不自行搜私人归档。 |
| ADR-WC-001 | S03/S04及S07 R07引用 | 若方案涉及网站/网关TLS私钥或证书凭据，须核验密钥持有/披露边界。不能从引用推导Web会话/缓存完整规则。 |
| ADR-PKI-OPS-001 | S03/S04及S07 R04引用 | 若依赖CA、恢复、信任/撤销传播，须补原文及恢复后的权限/身份核对证据；不能凭历史参数摘要声明已实现。 |
| ADR-UPDATE-001 | S03/S04列名，当前无完整原文 | 若首条只读部署依赖更新/恢复流程，须核验相关安全条款；不能自行写出其会话/Token失效策略。 |

适用性本身须负责人核验。缺原文不授权绕过现有安全约束，也不据此额外要求首条只读实现所有证书/恢复/更新功能；条件外的事项不被本任务扩成新的产品前置选择。

## 候选来源与当前决定状态

| ID | 文件/定位 | 短句原文 | 核验结论 |
|---|---|---|---|
| S10 | [Merged既有节点列表](../../../../api/openapi/g2-merged-candidate.yaml#L86-L113) | `operationId: listNodesForTransfer` | 继承[根Bearer](../../../../api/openapi/g2-merged-candidate.yaml#L15-L16)，既有GET并非缺失；401引用旧Error。不能直接新增重复路径或静默改成Cookie。 |
| S11 | [Merged既有详情](../../../../api/openapi/g2-merged-candidate.yaml#L5578-L5617) | `operationId: getNodeDetailG2` | 详情显式Cookie OR Bearer；401/403/404等引用Problem响应。这里只是候选/兼容事实，不选最终认证、防枚举或具体HTTP码。 |
| S12 | [r03 DECISIONS](../../revisions/g2-r03/DECISIONS.md#L3-L14) | `D01–D07仍OPEN / 未批准` | D05身份/凭据、D06错误/枚举、D07策略/库存/输出/缓存仍OPEN；D08只完成检查接线/注释协调。 |
| S13 | [r03候选开头](../../../../api/openapi/revisions/g2-r03/nodes-read-candidate.yaml#L1-L20) | `PROPOSED-NOT-FROZEN` | [no-store响应头](../../../../api/openapi/revisions/g2-r03/nodes-read-candidate.yaml#L99-L104)和新增错误码是提案。静态schema/示例通过不证明真实响应头、HTTP认证或缓存隔离。 |

旧列表错误为 [application/json + Error](../../../../api/openapi/g2-merged-candidate.yaml#L10357-L10362)，[Error](../../../../api/openapi/g2-merged-candidate.yaml#L6865-L6883)必需code/message、requestId可选；详情的[Error401/403/404](../../../../api/openapi/g2-merged-candidate.yaml#L10429-L10446)使用application/problem+json，[Problem](../../../../api/openapi/g2-merged-candidate.yaml#L9650-L9678)必需type/title/status/code/requestId，detail可选。`bearerFormat: JWT`仅见[安全scheme标注](../../../../api/openapi/g2-merged-candidate.yaml#L6205-L6213)，S08明确说不能视为实现选型。

[r02 README](../../revisions/g2-r02/README.md#L26-L36)的显式profile、协商头和统一错误为另一份未批准运行提案，不能自动接线。S12与[G2-R02A复审](../../../development/reviews/G2-R02A-REVIEW.md#L7-L28)只接收工件及D08技术协调；本任务不重新应用旧patch、不更新任何候选/来源矩阵或批准状态。

## 当前实现事实：可信来源在哪里仍只是约定

| ID | 本基线代码/测试 | 短句原文 | 能证明的范围与不能推出的结论 |
|---|---|---|---|
| S14 | [NodePolicy定义/求值](../../../../internal/auth/node_policy.go#L8-L74) | `This type does not authenticate SubjectID` | 只判断显式可信map中的精确节点读取权；KnownNodes/ReadGrants由调用者提供，不是身份库或多角色RBAC实现。 |
| S15 | [PrepareNodeReadScope/CheckNode](../../../../internal/auth/node_read_scope.go#L8-L102) | `a client claim is not sufficient` | 格式/整份策略/主体存在校验，一次读取并复制私有集合；零授权有效、未知主体拒绝。无认证、存储读取器、时钟、请求绑定或自动旧scope失效机制。 |
| S16 | [ReadVisibleInventory](../../../../internal/nodes/read_usecase.go#L15-L96) | `scope, err := auth.PrepareNodeReadScope` | 每次真实预检后才读库存，用同一范围；失败nil结果、深复制、坏ID/重复中止。两个来源都是显式函数，没有生产绑定或存储事务。 |
| S17 | [旧Visible及错误分类](../../../../internal/nodes/visibility.go#L26-L124) | `authorizer.Authorize(ctx, check)` | 仍逐节点调用旧Authorizer，空集合不调用；纯单链拒绝可过滤，多分支/混合故障不能吞掉。新入口没有替换旧入口。 |
| S18 | [Controller health handler](../../../../apps/controller/health/handler.go#L6-L13) | `mux.HandleFunc("GET /healthz"` | 仅进程存活，不是管理权限例外。 |
| S19 | [Controller main](../../../../apps/controller/main.go#L17-L38) | `management API unavailable` | 当前主入口只绑定health.Handler；未把预检/用例接到/api/v1/nodes或认证middleware。不能把源码/端口字符串当成已运行认证。 |
| S20 | [主线Web App](../../../../apps/web/src/App.tsx#L3-L17) | `当前为开发骨架` | 没有首条真实读取/登录链路。静态壳或组件局部模型不能证明跨主体缓存隔离。 |
| S21 | [auth范围直接测试](../../../../internal/auth/node_read_scope_test.go#L25-L382) | `TestReadScopeCopiesSourceAndNewPreparationObservesRevocation` | 有空库存预检、零授权、未知/坏主体与策略、源复制、新准备撤权、旧范围仍为快照、取消和敏感错误测试；不是Cookie/Token或HTTP测试。 |
| S22 | [用例直接测试](../../../../internal/nodes/read_usecase_test.go#L53-L369) | `TestReadInventoryMalformedIDsNeverProducePartialCounts` | 直接调用真实新入口/预检，证明顺序、失败不读库存、共同范围、新请求撤权、错误无部分列表、复制和授权后计数；坏ID确实可返回内部InvalidCheck。 |
| S23 | [r03旧入口限制探针](../../../../tests/contracts/g2r03/read_boundary_test.go#L42-L71) | `LIMITATION: empty inventory bypasses policy reads` | 两项限制复现仍保留：旧Visible空库存不读策略、每节点策略非共同快照。其PASS是局限的成功复现，不是D07关闭。 |
| S28 | [PolicyError](../../../../internal/auth/policy_errors.go#L5-L45) | `never request values` | 固定Kind/Field及sentinel；内部错误类别不是HTTP来源信息，也不证明全链路日志脱敏。 |

仓库内生产Go引用核验：Controller仅有health入口；PrepareNodeReadScope由ReadVisibleInventory调用，后者未在Controller绑定。两种reader、可信主体如何从会话/后端仓库得来仍是调用约定。无scope参数的用例减少了错误复用入口，但其PolicyReader本身仍可能由错误调用者缓存旧策略；Go类型不能证明当前性或身份。返回集合的复制隔离也不能提供撤权与序列化原子性。

## 历史执行证据与本轮核验分开

| ID | 已提交证据/定位 | 短句原文 | 证据范围 |
|---|---|---|---|
| S24 | [N03-A复审](../../../development/reviews/N03-A-REVIEW.md#L7-L40) | `ACCEPT OFFLINE USECASE` | 作者f57fe0d；组合代码0ec62214d206af71c4dffb81bdc033fde4eec062。复审仅接收离线用例，明确坏库存ID不能推出HTTP400，真实身份/HTTP/缓存/存储均未验收。 |
| S25 | [AUTH-01B复审](../../../development/reviews/AUTH-01B-REVIEW.md#L7-L41) | `ACCEPT OFFLINE RULES` | 作者14b9eac；组合代码35084a8128a91c01b75886a8550e5120e90752d2。旧范围时点语义、调用方每请求准备义务仍在，不是跨请求授权票据。 |
| S26 | [r03复审](../../../development/reviews/G2-R02A-REVIEW.md#L7-L28) | `ACCEPT REVIEW ARTIFACTS` | 只接收来源/候选/检查工件，D05–D07与G2未批准。 |
| S27 | [FOLLOWUP-02组件复审](../../../development/reviews/FOLLOWUP-02-REVIEW.md#L27-L40) | `4PASS/1FAIL` | 已提交的历史组件探针：W05-R01同key无效load恢复首提交旧数据仍OPEN，W05未合入。不是本轮新FAIL、主线漏洞复现或真实浏览器跨用户验收；未看作者后续在编代码。 |
| S29 | [AUTH独立探针JSON](../../../development/reviews/evidence/auth01b-review-probe.json#L1) | `"assertions":36` | 历史PASS，明确scope只到actual auth functions；本轮只读核对JSON/原探针文本，未重新运行。 |
| S30 | [N03独立探针JSON](../../../development/reviews/evidence/n03a-review-probe.json#L1) | `"assertions":33` | 历史PASS，仅real offline read usecase；不是identity或storage/output atomicity保证。 |
| S31 | [N03组合摘要](../../../development/reviews/N03-A-RESULTS.json#L1-L20) | `"task":"N03A-REVIEW"` | 历史Go212顶层/805子测试、Web151、npm/govulncheck无漏洞；不能混用作者旧基线201/782或写成本轮新执行。 |
| S32 | [该组合r03结果](../../../development/reviews/evidence/n03a-review-g2r03-results.json#L1-L12) | `"runtimeHTTP": "UNTESTED"` | 结构、lint/examples、真实负向工具及离线边界PASS；runtimeHTTP/runtimeAuthentication明确UNTESTED。 |

已核对原探针文本[auth](../../../development/reviews/evidence/auth01b-review-probe.go.txt)、[nodes](../../../development/reviews/evidence/n03a-review-probe.go.txt)及JSON范围说明；原始[模块输出](../../../development/reviews/evidence/n03a-review-module.log)、[函数覆盖率](../../../development/reviews/evidence/n03a-review-coverage-functions.log)均为复审时的历史记录。历史通过率不证明未接线部分安全；本轮新跑的只有文档/来源/政策检查，见[HANDOFF](HANDOFF.md)。
