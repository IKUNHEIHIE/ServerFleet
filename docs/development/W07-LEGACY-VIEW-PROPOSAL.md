# W07 · 旧列表展示组件实施准入提案

日期：2026-10-30；状态 **PROPOSED / NOT_AUTHORIZED**。负责人已选择 A1+B1，本文件不重问该方向。已授权的三份设计现已具备评审输入；以下是下一步可独立实施的具体范围，尚不构成派发或开工许可。

## 建议先做这一小步

将 legacy `{items}` 转为独立展示模型，完成只读列表与本地搜索组件，让旧列表形状有可验证的展示入口。数据由组件参数传入；合成数据只在测试/忽略预览中使用。此步不需要假造真实用户、节点、策略来源或采样时间，也不改变现有富化模型。

| 登记项 | 批准后采用的具体范围 |
|---|---|
| 任务 / 会话 | W07-LEGACY-VIEW / gemini-02；旧 gemini-01 设计任务已结束 |
| 审查人 | codex-review-01，总控+UI/契约/安全 |
| 拟分支 / 独立目录 | `feature/web-legacy-view-gm02` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-legacy-view-gm02`；当前未创建 |
| 起点 | 批准后由总控从本轮已接收的干净 main 创建，并登记精确 SHA；没有登记起点不得开工 |
| 允许新增文件 | 仅 `apps/web/src/features/nodes/` 下 `legacy-types.ts`、`legacy-adapter.ts`、`legacy-adapter.test.ts`、`legacy-node-list.tsx`、`legacy-node-list.test.tsx`、`legacy-node-panel.tsx`、`legacy-node-panel.test.tsx`、`LEGACY-VIEW.md` |
| 允许临时工件 | 自己目录内忽略的 `reports/_review/w07-legacy-view/`，仅离线测试/127.0.0.1预览及浏览器证据；不提交预览到产品入口 |
| 不包含 | App/fetch/HTTP/登录/凭据/存储、加载器或泛型 hook 抽取、旧组件/导出修改、公共契约/端口/生成/依赖配置/根锁文件、真实节点/远程/云、详情/新字段 |
| 停止条件 | 精确本地提交、文件清单、命令/实际输出、适用候选依赖及风险交总控；不合并 main/推送、不启动下一任务或其他 Agent |

## 本任务采用的局部展示规则

这些规则只限定 W07 的本地消费者，不批准 D 项或公共 schema：

1. 输入按 `unknown` 校验，保留服务端顺序；缺 items/非数组、非对象行、坏 id/online 或重复 id 整次失败，显示固定格式错误且无部分表格/计数。id 不 trim 合并；非法 name 回退 id 并固定提示；region/provider 非法显示缺失。只投影白名单，不回显未知字段、原始错误或秘密。
2. `online=false` 不展示任何历史百分比；`online=true` 的有限 0–100（包括 0/100）可显示，并标“无采样时间”。缺失与非法分别提示，不补 0、不夹取、不补造 sampledAt/status/revision/platform/agentVersion。
3. 列表行没有详情动作且不可聚焦；本地搜索仅对已加载的 name/id 生效。计数仅是当前参数传入的可见行数与搜索命中数，不推断全局/隐藏节点数；服务端空集与本地无命中文案不同。
4. 组件输入改变或格式错误时同步推导当前展示，不缓存上一份行/计数，不把合成 fixture 作为生产默认值。本任务没有异步加载或会话处理；迟到响应、viewEpoch 和终止错误门控仍留给之后单独批准的加载接缝任务，不声称 WSES/WHTTP 已通过。

设计依据：`docs/contracts/decisions/node-read-web-01/DESIGN.md` §3/§5/§7、ACCEPTANCE 的 WDTO/WCOUNT/WKB/WRESP；与 HTTP 提案的必填失败/可选降级及非在线指标方向一致。后端 Status→online、公共错误码、Cookie/Bearer 与来源建立仍待共审，不由本任务决定。

## 实际验收要求

- 适配器和真实组件覆盖：online 两值×0/100/普通值/null/缺失/非有限/越界；重复/坏 id 与坏 online 整次失败；名称等展示字段降级；服务端顺序、扩展字段忽略、搜索/清空和计数；输入切换与错误首提交不得出现旧行/计数。将缺失、0 和非法分开断言。
- 真实浏览器检查键盘搜索/清空、非交互行与可见焦点、420×900 卡片内横滚和1280×900布局，保留截图与实际观察；临时服务仅回环地址，结束后停止自己启动的服务。未做的辅助技术/真实 HTTP 如实 UNTESTED。
- 保留并运行现有 Web 全套测试，模块检查及合并门槛按 AGENTS 执行；测试不得以删除/改弱旧用例通过。文档说明本次只是隔离展示组件，App 仍未接线。

## 其他两位与后续准入

Codex 的认证设计及 DeepSeek 的 HTTP 设计当前任务完成停止；不为并行数量而新增无授权代码任务。认证实现需明确 D05/适用完整 PRIV、可信身份/会话及显式 grants 的持久来源、Cookie/CSRF与受限 Bearer迁移；HTTP 接线需明确 D01/D02/D06/D07、可信库存、Tgate/世代及残余窗口，并有具体范围和验收。跨标签/BFCache恢复须在真实 Web 会话接线前组合确定。它们不会因批准本项展示组件而自动获批。

负责人只需明确“批准 W07 旧列表展示组件”即可授权上述局部实施；总控届时登记精确起点并准备目录/可复制任务，随后由负责人派发。当前没有自动运行或代发消息。
