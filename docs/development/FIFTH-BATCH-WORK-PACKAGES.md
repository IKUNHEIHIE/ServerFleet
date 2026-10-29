# 第五批：连接对账与序列、节点授权返修、键盘焦点

日期：2026-10-29；总控codex-review-01。依据第四批独立审查：T01-B/W03接收，N02因N02-R01退回；G2仍未冻结。任务单是分配，不自动启动Agent，三项可人工并行开展。

## 共同约束

读AGENTS、README、架构索引、MODULE-OWNERSHIP、DEPENDENCY-RULES、TASK-BOARD、本单及BATCH-04-REVIEW。开始前记录实际目录/分支/HEAD。除N02-R1在原任务追加返修外，其余必须使用下列新工作目录，起始为第四批集成后的main。

S为受保护集合：根/模块AGENTS、现有ports.go、依赖清单/锁、API/Proto/gen、Buf/Redocly、CI/scripts、依赖规则、数据库公共结构、原始资料/批准ADR。不可修改；不加依赖。需要跨模块差异先交总控。N02-R1延续第四批两个测试适配文件的明确授权；W04仅额外允许本工作目录下忽略的reports/_preview、reports/_review用于临时组件验收，不改共享配置或生产入口。

仅合成离线测试/本机隔离组件预览；不连接真实节点/Controller/Agent/数据库/云，不使用真实凭据，不实现真实执行、WSS/mTLS、登录或生产mock，不批准运行时profile/协议冻结。每项交接后停止，不合并main、不推送、不启动其他Agent。

## T01-C · 单连接对账与入站序列守卫候选

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Codex codex-01 / 总控独立安全与通信审查 |
| 分支 / 目录 | feature/connection-guard-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--connection-guard-cx01` |
| 允许写入 | internal/transport内新增连接守卫/测试/说明；复用T01-A/B入口，原测试保留，不改ports/Proto/gen |
| 目标 | 在有界解码后，按调用方显式提供的可信连接绑定核对node_id/connection_id，并为单方向的单连接记录入站sequence高水位；在原始/语义/绑定/序列全部通过后才原子提交高水位 |
| 身份边界 | 绑定来自未来经验证的TLS/节点记录组合根，本轮仅显式合成输入；不能从消息自报node_id/connection_id创建或自动切换可信绑定。类型/非空字符串不是mTLS验证证明。绑定缺失或无效时默认拒绝 |
| 候选规则 | 单连接单入站方向严格递增，允许跳号；相等/降低/0拒绝。此为局部保守候选约定，不锁定重连恢复协议。方向、期望版本、阶段/时钟和限制由可信调用方显式提供；本轮不自动推进握手或做能力协商 |
| 状态边界 | 零值/nil未绑定守卫不能接收；字段私有、不自动按输入ID重置；失败不推进状态。相同sequence并发最多一个成功，比较/提交在同一同步边界。关闭/失效为终止状态，不能靠下一条消息重开；新连接由可信调用方显式建新实例，旧connection_id不得混用，不新增全局无界连接表 |
| 输出边界 | 拒绝不返回可用消息，错误不回显帧/ID。只验证这个内存实例的入站序列；不声称跨重启持久防重放、execution_id去重或业务权限/审批通过，不接Sender、网络、Controller/Agent或执行 |
| 验收 | 正常递增/跳号、重复/乱序/最大uint64边界、错node/connection、错误方向/阶段、缺绑定/关闭、畸形帧和语义错误不消费水位、纠正后同序号可用、32并发同序号仅1成功；验证实际解码入口而非测试自写判定器。未知扩展兼容及既有13种消息测试保留 |
| 命令 | go test -count=1 -cover ./internal/transport/...；npm run check:go；npm run check:policy；npm run check:contracts；npm run check:security |

来源：现行G2通信候选的node_id只作对账、错误node ID/重放拒绝，以及已接收T01-A/B。真实证书映射/吊销与恢复隔离仍未实现；不得虚构缺失安全ADR或把守卫接收映射为执行许可。

## N02-R1 · 不让拒绝原因掩盖其他授权错误

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | DeepSeek deepseek-01 / 总控安全与节点审查 |
| 原分支 / 目录 | feature/nodes-auth-errors-ds01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--nodes-auth-errors-ds01` |
| 精确起点 | ea7ed5c99c85c28dc0797fcaeaa2ba1da9bef9f0；保留原提交，追加返修，不rebase/reset或合并main来覆盖历史 |
| 允许写入 | 原N02范围：internal/nodes可见性/测试/说明，以及tests/testkit/auth.go/auth_test.go兼容；不修改auth实现或公共端口 |
| 必修问题 | BATCH-04-REVIEW的N02-R01，当前visibility.go:52先命中ErrDenied，导致errors.Join(ErrDenied,fatal)被忽略。15个独立断言复现，原探针/输出在审查evidence内 |
| 通过条件 | 仅纯拒绝可过滤；同时含策略不可用、非法请求、取消、期限或未知原因必须中止且结果nil。保留errors.Is/As链及普通单链包装ErrDenied过滤。可保守拒绝所有多分支错误，不能仅依赖“当前读取器不会Join” |
| 回归矩阵 | ErrDenied与5种fatal正/反顺序、外层包装/嵌套join；在第一次和已允许一个节点后的最后一次检查触发；Visible/VisibleFilter/VisibleClassify均不得输出半截成功。保留真实NodePolicyAuthorizer组合、撤权、主体不trim、ctx和testkit兼容测试 |
| 交接要求 | 展示修复前独立失败与修复后通过；HANDOFF删除把本bug推给组合根的限制说明，改成实际分类边界；不要删失败覆盖、降检查或改auth契约规避 |
| 命令 | go test -count=1 -cover ./internal/nodes/... ./tests/testkit/...；npm run check:go；npm run check:policy；npm run check:security |

原N02尚未进入main。返修完成后交原提交与新HEAD，由总控复审完整差异；本轮不实施HTTP/审计写入/授权缓存或其他功能。

## W04 · 非模态详情的键盘焦点与隔离预览验收

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Gemini gemini-01 / 总控UI、安全与契约审查 |
| 分支 / 目录 | feature/web-node-accessibility-gm01 / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-accessibility-gm01` |
| 允许写入 | apps/web/src/features/nodes内最小焦点/语义调整、测试/说明；可在本工作目录忽略的reports/_preview、reports/_review临时建验收入口/截图，不提交临时工具或fixture到生产 |
| 目标 | 补足W03已说明的焦点局限：键盘激活行后可明确到达并理解详情；关闭/Escape返回当前仍存在的来源行，若该行已被本地筛选隐藏则回到可用搜索入口/稳定区域，不能指向旧DOM |
| 动态状态 | 来源移除、loading/error或空列表隐藏详情时，如焦点原在被移除详情中，应落到当前可用的稳定位置；不得为保焦点保留旧节点内容。仅本轮输入解析，原选择/数据质量不变量保留 |
| 非模态与语义 | 不做焦点陷阱；原生Tab可离开，控件均有明确名称。必要的详情关联/公告须简洁，不逐格播报指标或在后台更新时反复抢焦点；关闭无效回调不能产生假按钮 |
| 验收 | 真实组件的选择/关闭/Esc/筛选后关闭/来源更新与移除/loading/error焦点测试；既有129测试和指标回归保留。用本机隔离预览做键盘Tab与窄/宽屏检查，记录实际结果及截图路径；不把fireEvent直接focus当作真实Tab证据。浏览器工具不可用则明确该项UNTESTED，不伪称通过 |
| 预览限制 | 只用合成数据、固定注入时间和既有依赖；不改App/main/全局CSS/共享UI/Vite配置/锁文件。临时页面位于reports，测试fixture不进入生产bundle；仅监听127.0.0.1，结束时关闭本任务启动的服务，不终止其他会话进程 |
| 命令 | npm run check:web；npm run check:policy；记录真实浏览器步骤/输出，新增库/CLI用法按Context7规则查文档 |

这项不是W03阻断返修，而是独立交互完善；不开发真实API、编辑/删除/终端/执行或新业务字段。保持简单，避免为非模态区引入弹窗框架或全局状态。

## 交付与停止

报告实际目录/分支、起始与最终HEAD、文件、实际命令/输出、至少一个失败场景、兼容变化、候选依赖与剩余风险；FAIL/BLOCKED/UNTESTED如实保留。完成本地提交后停止，交总控独立复审；不自行合并main/推送/启动其他Agent。原作者目录及审查证据保留，不自动清理他人文件。
