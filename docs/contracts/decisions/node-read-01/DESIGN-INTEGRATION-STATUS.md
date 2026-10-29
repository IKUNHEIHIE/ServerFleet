# A1+B1具体设计 · 组合评审状态

日期：2026-10-30；总控codex-review-01；状态 **DESIGN_DOCUMENTS_RECEIVED / IMPLEMENTATION_NOT_AUTHORIZED**。A1+B1方向已确认，三份设计文档均接收为PROPOSED；原四个P2关闭。详细D01–D07/P1-C10仍OPEN，G2未冻结；文档接收不等于接口/参数批准或运行验证。

| 设计 | 精确提交 | 本轮决定 |
|---|---|---|
| 身份/会话 | 48409bacf4e582f8ab4f196982a5677fa3a21d18 | 文档接收为PROPOSED，22条来源/34项未来验收已核；Codex完成停止 |
| HTTP | 原ece5efd；R1 4fbdcf5126b02981f8f3fcf5cb403641693172fb | 接收原设计及R1为PROPOSED，HD01-R01/R02关闭；当前任务完成停止 |
| Web | 原63b0feb；R1 39b2b7cd1bf047c9691206c08bc489ff4ef112c9 | 接收原设计及R1为PROPOSED，WD01-R01/R02关闭；当前任务完成停止 |

本轮见[返修复审](../../../development/reviews/READ-DESIGN-R1-REVIEW.md)。总控在独立集成目录补充：有状态凭据的秘密/到期/撤销需查权威来源，不能先断言有效性；Web的viewEpoch实际参与key，终止错误先关闭加载许可，防止错误换key自动重复请求，并修正onlineOnly请求错误的表述。作者精确提交/旧报告保留，未改作者目录。[原返修任务](../../../development/READ-DESIGN-01-REPAIR.md)和[首次复审](../../../development/reviews/READ-DESIGN-01-REVIEW.md)保留历史状态，不代表本轮又退回。

## 接线前必须组合明确的事项

| 项 | 现有提议/差异 | 当前状态与处理 |
|---|---|---|
| 认证失效、策略拒绝、临时故障 | 已区分本地结构拒绝/权威来源确认无效401、来源故障503、策略拒绝403；503无法先确认凭据有效性；Web403不清会话，终止错误清旧行/计数且关闭自动加载 | R01均关闭；具体状态/机器码/媒体仍提案，实际认证/HTTP UNTESTED |
| A1真实来源 | 已补auth持主体/凭据、策略域持显式grants/世代、nodes持可信库存、storage管事务的受控建立/更新和一致快照模型；区分已初始化空库/已知零授权与未配置，无默认管理员全节点或假库存 | R02关闭；持久结构、安装/恢复、可信更新入口、真实库存接入和事务/世代具体机制仍待共审与实现 |
| Cookie/CSRF及部署 | 认证建议__Host-sf_session、HTTPS同源/受信代理、Secure/HttpOnly/Lax/Path=/与同步CSRF；旧候选sf_session、列表Bearer-only | 都是具体待批差异；A1+B1没有批准新名称/参数、Cookie已可调用旧列表或跨源支持。真实浏览器/代理兼容UNTESTED |
| 非浏览器Bearer | 认证建议有状态opaque Token、当前权限∩签发精确上限；HTTP已明确保留传输不保证旧JWT值兼容 | 旧发行方/客户端盘点、受限Token适配/完整策略验证/签发与迁移尚未批准或实现 |
| 撤权与响应边界 | HTTP已对齐Tauth/Tpolicy/Tgate/Tcommit并推荐提交前查身份/授权世代，不再Prepare/重读完整策略，失败丢弃缓冲 | 权威世代来源/一致读取、gate接口与Tgate到首字节残余窗口仍待共审；10秒预算不是原子性或撤权SLA |
| legacy前端指标/数据质量 | 非在线不显示无更新时间的历史百分比；在线合法0/100保留并标无采样时间；坏权威字段/重复ID整次失败，展示字段局部降级；计数不泄露隐藏集合 | R02关闭；后端online来源/映射与真实客户端兼容D01/D02/D04仍待批，组件/HTTP未实现 |
| 页面/标签恢复 | 认证建议跨标签失效提示、pageshow/BFCache清旧再验证；Web把跨标签排除在首版 | 作为待对齐范围记录，不自动认定任何一方已同意；来源key不是登录证明，no-store不擦除已显示内容 |
| 泛型加载接缝 | Web建议抽useLoadState<T>，原富化NodeLoader仍对外保持 | 仅设计建议；修改前须定范围并重跑旧行为和新legacy测试，W06历史证据不能直接证明抽取后行为；不在本轮实现新hook |
| 安全原文与实际验证 | 完整PRIV缺失且直接适用，WC/PKI-OPS/UPDATE依具体设计适用补证；全部运行验收UNTESTED | 不虚构旧ADR，不把四套完整实现机械列为A1前置；后续安全/契约准入前明确补证责任和实际证据 |

本轮不将上述未定项静默选定为最终技术方案。建议下一步只批准[W07旧列表展示组件](../../../development/W07-LEGACY-VIEW-PROPOSAL.md)，范围已列清，不改App/fetch/旧加载器/凭据/公共契约。本轮只准备提案，不创建新作者目录、不派发或自动启动实现；三位当前任务均完成停止。当前没有新实现任务、生产凭据、公共契约或运行时profile批准。
