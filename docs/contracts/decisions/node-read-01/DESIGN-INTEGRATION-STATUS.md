# A1+B1具体设计 · 组合评审状态

日期：2026-10-29；总控codex-review-01；状态 **DESIGN_REVIEW_OPEN**。A1+B1方向已确认，详细D01–D07/P1-C10仍OPEN，G2未冻结。本文件登记精确设计交付与组合依赖，不替代负责人批准、不启动实现。

| 设计 | 精确提交 | 本轮决定 |
|---|---|---|
| 身份/会话 | 48409bacf4e582f8ab4f196982a5677fa3a21d18 | 文档接收为PROPOSED，22条来源/34项未来验收已核；Codex完成停止 |
| HTTP | ece5efd77e8c99fb9a9d1f19eb31db5bd78b3734 | CHANGES_REQUESTED，HD01-R01/R02；原文未合入main，按原3文件返修 |
| Web | 63b0feb8dffad7dc88252dd7dd786b80c7e54e7c | CHANGES_REQUESTED，WD01-R01/R02；原文未合入main，按原3文件返修 |

返修范围见[任务单](../../../development/READ-DESIGN-01-REPAIR.md)，证据/理由见[复审](../../../development/reviews/READ-DESIGN-01-REVIEW.md)。未接收两份设计的原文已作为历史证据归档，不能因有归档就声称已获接收或批准。

## 接线前必须组合明确的事项

| 项 | 现有提议/差异 | 当前状态与处理 |
|---|---|---|
| 认证失效、策略拒绝、临时故障 | 身份设计分credential.invalid/identity.unavailable/policy.subject_denied；HTTP没有完整身份来源失败映射；Web拟对401/403都清会话 | HTTP/Web按R01返修，分别保留身份与授权语义；清节点展示不等于服务器会话撤销，错误码/媒体仍提案 |
| A1真实来源 | 已有离线PolicyReader/InventorySource约定，缺少最小来源持有/建立/更新/读取模型；初始化管理员也未自动授全量节点 | HTTP按R02补可信来源方案，不使用假节点/默认allow；主体目录、显式grants与库存来源要可追溯，存储/初始化细节仍待共审 |
| Cookie/CSRF及部署 | 认证建议__Host-sf_session、HTTPS同源/受信代理、Secure/HttpOnly/Lax/Path=/与同步CSRF；旧候选sf_session、列表Bearer-only | 都是具体待批差异；A1+B1没有批准新名称/参数、Cookie已可调用旧列表或跨源支持。真实浏览器/代理兼容UNTESTED |
| 非浏览器Bearer | 认证建议有状态opaque Token、当前权限∩签发精确上限；原候选标JWT，HTTP称旧Bearer保持 | 保留Bearer传输不等于保留全部旧凭据值；限额适配/源策略完整验证/发行与迁移需具体对齐，在安全/契约批准前不启用新机制 |
| 撤权与响应边界 | 认证建议Tgate前复核身份/授权世代，不再Prepare；HTTP只有T0/T1捕获与在途残余窗口 | HTTP返修明确推荐及差异；还须确定权威世代来源/读取所有权与Tgate到提交的残余窗口，不能把重读或10秒超时当原子性/SLA |
| legacy前端指标/数据质量 | Web拟对合法百分比一律显示无时间值，并保留首次重复ID、坏行dropped；HTTP拟坏必填整请求失败、非在线指标null | Web按R02补online判定，数据质量/重复ID/部分展示的取舍另明确；不能因服务端计划输出null就跳过旧合法历史响应的客户端处理 |
| 页面/标签恢复 | 认证建议跨标签失效提示、pageshow/BFCache清旧再验证；Web把跨标签排除在首版 | 作为待对齐范围记录，不自动认定任何一方已同意；来源key不是登录证明，no-store不擦除已显示内容 |
| 泛型加载接缝 | Web建议抽useLoadState<T>，原富化NodeLoader仍对外保持 | 仅设计建议；修改前须定范围并重跑旧行为和新legacy测试，W06历史证据不能直接证明抽取后行为；不在本轮实现新hook |
| 安全原文与实际验证 | 完整PRIV缺失且直接适用，WC/PKI-OPS/UPDATE依具体设计适用补证；全部运行验收UNTESTED | 不虚构旧ADR，不把四套完整实现机械列为A1前置；后续安全/契约准入前明确补证责任和实际证据 |

本轮不将上述任一未定项静默选定为最终技术方案；先完成两份返修并组合审查，再整理具体待批差异及实施前条件。当前没有新实现任务、生产凭据、公共契约或运行时profile批准。
