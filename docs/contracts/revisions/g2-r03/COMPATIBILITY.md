# 兼容语义与接入条件（提案）

本文件与 OpenAPI 都是冻结前候选。字段存在与字段可信是两项不同条件；通过 schema 仅证明形状符合候选，不证明身份、授权、时间新鲜度或真实服务行为。

## 来源和范围

1. 原始 v0.9 与 `api/openapi/g2-merged-candidate.yaml` 的 `paths./api/v1/nodes.get`：`listNodesForTransfer`，`onlineOnly:boolean=false`，200 `{items:[NodeSummary]}`，401 `application/json` 的 `Error`，继承根 Bearer。`NodeSummary` 必填 id/name/online，已有 region/provider/countryCode/ip/tags 和 0–100 的可空 CPU/内存，没有 status/revision/平台/版本/lastSeen/采样时间。
2. Core 与 Merged 的 `NodeDetail`：必填 nodeId/displayName/status/revision；五状态为 online/offline/suspended/revoked/unknown；revision 为 integer、minimum=1。lastSeenAt 为可选 date-time **string**，platform/agentVersion 为可选 string；原 DTO 均不接受这些字段为 null。详情无 CPU/内存。`getNodeDetailG2` 的 Cookie/Bearer OR 与 Problem 错误媒体不同于列表，原错误码还包含通用的 409/412/428/429/503。
3. r02 保留两个已有操作和这两种节点成功形状，另提议必需 `X-ServerFleet-Contract: g2-r02`、跨操作认证/错误统一。第五批明确未批准其运行时 profile，本轮不继承协商头、双路由或自动降级。r02 旧说明中临时端口能力的描述是当时审查快照；当前 auth/nodes 新增的离线实现不能据此忽略，也不意味着已有会话/HTTP。
4. Web `candidate-adapter.ts` 的 CandidateNodeDetail 是 Core 子集；局部 CandidateNodeMetrics 与 CandidateNodesSource 没有真实数据来源。`types.ts` 的 revision 必需、五状态、MetricReading 都是本功能模型。其“候选只有详情/没有CPU内存”注释只适用于 Core 范围，泛指整个候选集会失实。P1-C10 继续开放；修订建议仅在本目录交总控。
5. 当前 `internal/nodes.Node` 是内部库存，含 ID/Name/Address/Region/Provider/Tags/Status/LastSeenAt/Metrics，**没有 revision/platform/agentVersion**；不能将它强转 NodeDetail。Metrics.ObservedAt 可能为零值，Usage 要求 online、有限值、有效时间、显式 now/maxAge，但没有 0–100 范围检查；Web 更严格且可展示带标识的 stale 值。两种行为不能声称完全相同。当前模块不提供 HTTP DTO 或授权后的原子读快照。

各来源的精确文件和 SHA-256 在 provenance.json；操作、请求参数、required 和旧属性由实际 YAML 深比较，而非仅信任本说明。

## 字段收口

列表保留原 id/name，详情保留原 nodeId/displayName。新消费者可做精确别名映射，但不增加重复别名以制造两个 ID 权威。ID 不 trim、不从名称/IP 推导；Web 的显示名回退仅是显示策略，不能回写 DTO。

列表可选增加 status/revision/platform/agentVersion/lastSeenAt，仅在可信来源确有证据时输出。status 存在时提议 `online == (status == "online")`；该优先级仍待 D02 批准。status 不存在时仍接受任何合法旧 online 布尔，**尤其 online=false 不能确定是 offline、suspended 或 revoked**。本候选不靠布尔值填五状态，也不把未知状态映射为在线。新旧来源冲突时拒绝发布矛盾的富化记录，交服务端核对，不能以 UI 选择一方掩盖。

revision 的可选/必需差异明确保留：列表可以没有，详情仍必须 >=1；不得填 0、1、时间戳或数组序号。它是节点元数据并发版本提案，不等于授权 scopeRevision、列表版本或指标样本序号。采样刷新不应随意改变它。版本来源、递增边界、存储原子性和 JS 大整数精度尚无实现/批准；未额外增加 maximum，也没把 GET 变成必须 If-Match 的写前置条件。

CPU/内存保留旧列表字段名、可选性、number/null 和 0–100 边界，提议在详情增加同形可选字段；真实 0/100 合法，null/缺失不是 0，越界不能夹取。旧列表原来允许离线节点带历史数值，候选保留这种可读形状；UI 不得显示成当前在线读数。

sampledAt 为两种 DTO 的**可选** date-time string/null，表示 CPU/内存确来自同一次真实采样。来源每指标各有时间且不同，就不能拼成同一时间的快照；省略/null 并标为缺时间，或另提逐指标结构，不能取最大时间/响应时间/lastSeenAt/now 补齐。旧无时间的数值继续允许读取，但不能标为新鲜。

lastSeenAt 是最近联系时间，sampledAt 是资源观测时间，revision 是并发版本，三者不可互代。Web 新鲜度必须用调用方显式 now/maxSampleAgeMs：未来时间无效、超阈值标陈旧、缺时间不展示新鲜数值。schema 只校验 date-time 格式，无法比较真实 now；独立正例故意允许格式正确的未来时间，记录这种限制。没有在公开契约锁定生产采样周期。

可选扩展并非对所有任意旧扩展都无损：NodeSummary 原来未禁止额外属性，若旧消费者/生产者私自占用了 status/revision/sampledAt 等名字且类型不同，新约束会拒绝它。原规范字段的类型/required 和老样例均保留；保留扩展名、online/status 一致性、详情新增字段及响应头都要批准/探测。严格拒绝未知字段的旧客户端也需要升级。不能宣称“所有旧消费者无条件兼容”。

## 旧消费者和 Web 的迁移顺序

1. 契约 owner 确认本投影与原 Merged 同一操作，不另建重复 GET、不改 listNodesForTransfer，不把本文件当成第二套运行时 profile。旧互传消费者继续读 items/id/name/online 及旧可选属性，原 Bearer/401 Error 规则不静默改变。
2. 先盘点真实客户端是否拒绝未知属性、是否占用扩展名，以及响应库能否处理新增 400/403/503；当前仓库只有离线 Web 模型，不能冒充真实客户端兼容验收。若最终选择统一 canonical DTO/认证/错误媒体，要另批显式破坏性迁移和能力探测，本提交不执行。
3. Web 未来来源须解开 `{items}`，精确映射 id/name；只有权威 status/revision 均存在才可构造现有必填 CandidateNodeDetail。缺少时先按当前身份读取既有详情；若仍缺失则给不可用/错误状态，不能用假 revision、猜状态或直接类型断言。降低 N+1 开销、改局部 VM 允许未知 revision，都是后续经 owner 批准的选择，本轮没有实现。
4. 将 CPU/内存与真实共同 sampledAt 交给当前局部指标适配；缺时间的旧样本仍是缺数据。lastSeenAt 缺失归一为空显示，不抄作 sampledAt。保留授权后的顺序，原接口未定义稳定排序/分页，本轮不加 limit/cursor/total 或本地模拟服务端分页。
5. 任一策略/身份全局故障必须进入错误态并丢弃之前主体的节点/详情/计数，不能把已富化的一部分冒充成功完整列表。被移除/不可见节点如何刷新页面须由真实服务集成验证，不使用旧授权缓存继续展示。

## 授权、错误和缓存

身份必须由未来 Controller 认证适配从有效会话/受限 Bearer 可信绑定，不能读取客户端自报 subjectId/角色/授权集合就放行。NodePolicy 的 ReadGrants 来源必须是可信后端且当前有效；SubjectID 参数只是字符串，不是认证证据。原列表只声明 Bearer，而详情允许 Cookie 或 Bearer；本候选原样保留这项差异，因此 **当前 Web 不能据本提案直接用 Cookie 调列表**。认证方案、双凭据处理和撤销验证是接线阻塞项 D05。

目标顺序是：先验证可信主体及策略可用（包括空库存），再逐资源授权，再过滤 onlineOnly/分类/计数，再序列化有权字段。所有总数、桶计数和分页信息只能来自可见集合；本候选不新增全局 total。纯拒绝可过滤某节点，策略不可用/未知故障/混合错误/取消/期限不得转换成成功空/部分列表。节点离线不等于禁止读其库存详情，也不能自动映射 409。

| 场景 | 来源事实 | r03 声明/待批映射 |
|---|---|---|
| 缺失/无效/已撤销认证 | 两个旧操作均声明 401，但目前无认证/会话实现 | 401；列表 Error、详情 Problem，保留媒体差异。谁发凭证、如何即时撤销及认证头仍 D05 |
| 列表操作被整体拒绝 | 旧列表摘要已说仅返回有权节点；未显式声明 403 | 提案新增 403 Error；与逐节点纯拒绝后的空可见集合区分 |
| 详情拒绝或节点不存在 | 原详情同时声明 403/404；内部策略对未知目标和无权目标均拒绝 | 提案对二者统一 404 防枚举，403 保留给非资源级整体拒绝；查找/鉴权顺序与文案统一待 D06，不输出差别细节 |
| 策略读取失败/不可用，未知或混合授权故障 | NodePolicyAuthorizer 返回不可用；N02-R1 对非纯拒绝返回 nil 结果和错误 | 列表提案新增 503 Error，详情保留 503 Problem；不能 200 items=[]，错误不能携带部分节点 |
| 非法请求 / 内部 ErrInvalidCheck | 原详情 400，列表缺 400；内部错误可能是接线错误 | 列表提案新增 400，仅证据确为客户端非法参数时使用；内部故障不能伪归咎客户端，暂拒绝服务/最终 500 规则待 D06 |
| 取消/期限 | Go 原样传播；尚无 HTTP 入口 | 不能成功或补部分列表；断连是否无响应、期限用 503/504 的最终策略待 D06，不把静态错误表当作已实现 |
| 409/412/428/429 | 原详情批量声明的通用错误 | 原样保留比较证据，本轮未赋予 GET 写前提、离线失败或真实限流语义；适用性待 D06 |

本轮不统一旧 Error 和 Problem。详情错误响应在原 Problem 上增加 `status == HTTP status` 的约束提案，缺少请求 ID、状态不一致等坏响应不能伪装成功；消息只用脱敏模板，不复制原异常/策略/节点 ID/凭据。这里的机器码示例仅为提案，不是已有后端码目录。

提案为全部成功/错误响应声明 Cache-Control: no-store；不允许代理、服务 Worker、Web 内存或预取结果跨主体复用。未来若做私有缓存，键需包含可信主体/会话、授权版本和请求范围并处理撤权失效，必须另审；单加 Vary、资源 revision 或 no-store 注释不证明缓存隔离。此处没有真实响应头或浏览器缓存测试。

## 当前离线实现已证明和仍缺失的部分

独立 Go 探针直接调用现有 NodePolicyAuthorizer 和 Visible：普通单链 ErrDenied 过滤单节点，任意多分支/混合授权故障返回错误且 nil 列表，N02-R1 已关闭的错误吞并没有被本轮恢复。

同时实测并保留两个限制：空库存时 Visible 不调用 Authorizer，未知主体/不可用策略因而不会被它单独验证；逐节点检查会分别读取当前策略，两次读取可来自不同快照，不能保证一份原子列表政策视图。后续可信入口必须补空集合预检，并决定整次读取的策略版本/撤权与输出原子性。本轮仅记录，不改 nodes/auth 代码、不重新宣称旧审查失败、不把探针 PASS 说成这些接线缺口关闭。

没有验证 HTTP、Cookie/Bearer、数据存储、授权后计数/字段过滤、真实撤权、跨主体缓存、全链路时钟或实时指标；它们仍是接入首条真实只读链路前的工作。
