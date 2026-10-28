# 第三批任务：授权策略、审计基础、节点列表交互

日期：2026-10-29；总控：codex-review-01。接续已复审的第二批交付。任务单是具体分配，**不会自动启动Agent或向会话发送消息**。每个任务完成后提交本地结果并停止，统一交总控审查/合并。

## 共同约束

基线均为第二批复审集成完成后的main，开始时记录实际HEAD。新建独立Worktree，不在已完成任务目录继续叠加无关工作。读根AGENTS、模块归属/依赖规则和本任务；仅改允许目录。

共享保护集合S：根/模块AGENTS、现有ports.go、依赖清单/锁文件、API/Proto/gen、Buf/Redocly、CI/scripts、依赖规则、数据库公共结构、原始资料及已批准ADR。模块作者不得改S；需要公共接口调整时先交具体兼容差异给总控。测试只能用合成数据和临时内存/文件，不连接真实Controller/Agent/数据库/云，不加入新依赖。

## AUTH-01A · 节点作用域的默认拒绝策略与错误语义

| 项目 | 分配 |
|---|---|
| Agent | Codex，唯一会话codex-01 |
| 目标 | 在现有Check/Authorizer边界内，为节点作用域操作提供可直接测试的默认拒绝纯策略；区分拒绝、无效输入、策略读取不可用和ctx取消/期限。不是登录系统或完整RBAC |
| 分支 / Worktree | `feature/auth-policy-cx01` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--auth-policy-cx01` |
| 允许修改 | `internal/auth/`内新增策略/错误类型、测试、局部说明；现有ports.go保持不变 |
| 禁止修改 | S、nodes.Visible、Controller/Agent、存储、任务和审计实现；不设计JWT/密码/会话存储、不增加API |
| 已有接口 | auth.Check{SubjectID,Action,NodeIDs}、auth.Authorizer；当前nodes只读使用固定nodes.read，但策略不得反向import nodes |
| 具体边界 | 可信的当次策略输入必须显式提供；空/未知主体、未知动作、空目标或任何未授权目标默认拒绝；不接受通配目标或客户端自报授权。策略只评估本轮明确的节点作用域操作，不把空NodeIDs解释成全局管理员权限 |
| 撤权 | 纯函数可基于每次显式策略快照评估；如提供Authorizer适配，必须每次读取可信当前策略，不能缓存旧授权。测试用可控读取器模拟授权被撤销。缺策略/读取失败不得放行；错误不含敏感输入 |
| 交付 | 最小实现、可errors.Is/As判断的错误语义、真实函数正反测试、调用者迁移建议及未覆盖权限域说明。给nodes的拒绝/不可用分类接线建议交总控，不修改nodes来扩围 |
| 自动验收 | 默认拒绝、跨节点/混合目标、通配/未知动作、策略失败、撤权、ctx取消/期限、正确授权通过；测试调用实现而非自写测试判定器。`go test -count=1 -cover ./internal/auth/...`、`npm run check:go`、`npm run check:policy`、`npm run check:security` |
| 并行 / 审查 | 可与A01/W02并行；只读使用audit/storage边界但本轮不接真实存储。总控独立安全审查后合并，不形成完整授权闭环或G2冻结 |

不得根据普通节点权限单独批准AI/root操作；会话授权、强制人工审批、插件细分能力、任务实时复核仍属后续独立门槛。此纯策略没有用户认证能力，策略数据必须由可信后端提供。

## A01 · 审计事件校验、受限视图与测试适配

| 项目 | 分配 |
|---|---|
| Agent | DeepSeek，唯一会话deepseek-01 |
| 目标 | 为现有audit.Event建立字段控制/校验和受限只读投影；提供显式测试内存Recorder及查询样例，不接数据库或HTTP |
| 分支 / Worktree | `feature/audit-domain-ds01` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--audit-domain-ds01` |
| 允许修改 | `internal/audit/`内新实现、测试、局部testdata/testkit和说明；ports.go不变 |
| 禁止修改 | S、auth/nodes/tasks/storage等其他模块；不加公共审计表、保留期任务、备份、防篡改或真实读写接口 |
| 已有接口 | audit.Event{SubjectID,Action,Result,RequestID,At}、Recorder.Append；原始G2审计DTO只读参考，不把它直接扩充进端口 |
| 字段/安全 | 不增加raw payload/message/details等自由秘密容器；Action/Result等代码域通过明确允许列表验证，长度/UTF-8/控制字符/时间边界可注入。视图只输出明确字段；不把上游异常直接写入日志/错误。不能宣称能识别任意字符串里的全部秘密 |
| 测试适配 | 内存Recorder只放testkit或测试文件，禁止接入生产组合根；写入快照/并发/ctx取消、不覆盖旧事件、只读过滤/排序/边界分页由真实测试调用验证。相同RequestID可关联多条事件，不得当作全局唯一事件ID而丢事件 |
| 交付 | 校验/投影代码、测试适配、失败/秘密字段拒绝样例、分页/顺序/多事件样例、局部说明。调用方过滤权限和真实审计完整性仍须后续实现，不用内存测试冒充真实安全日志 |
| 自动验收 | 正常事件、非法代码/时间/控制字符、取消不写入、重复请求多事件不丢、查询不修改原记录、仅允许字段输出；`go test -count=1 -cover ./internal/audit/...`、`npm run check:go`、`npm run check:policy` |
| 并行 / 审查 | 可与AUTH-01A/W02并行；不要等待或依赖未提交auth代码。总控安全/数据审查后合并 |

## W02 · 节点列表本地搜索与状态筛选

| 项目 | 分配 |
|---|---|
| Agent | Gemini，唯一会话gemini-01 |
| 目标 | 在已验收NodeList之上添加独立可测试的NodeListPanel（或同等小型包装），搜索当前传入节点的名称/ID，按已有状态筛选，不新增后端能力 |
| 分支 / Worktree | `feature/web-node-filters-gm01` / `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--web-node-filters-gm01` |
| 允许修改 | `apps/web/src/features/nodes/`，含组件、纯过滤、局部ViewModel适配、测试和README；不破坏已验收指标不变量 |
| 禁止修改 | S、App/main、全局CSS、components/ui、后端、真实API；不加依赖，不开发批量执行/成员编辑/所有页面 |
| 已有接口 | NodeList/NodeListItemView/NodeListState、局部质量模型、Badge/cn；不假设缺失的地区/IP字段已在公开DTO冻结 |
| 交互边界 | 只过滤调用方已提供的可见节点集合，不能假定本地筛选替代权限；大小写/首尾空白处理明确，源数据和顺序不变；区分源列表空与无匹配结果，计数只针对当前已加载集合 |
| 数据质量 | 加载/失败不能显示成功节点或伪造计数；保留offline/null、invalid、stale可见时间、有效0/100；时间/新鲜度继续由调用方注入，不读取机器日期自动改策略 |
| 交付 | 包装组件/过滤函数、带可访问名称的搜索与筛选/清空控件、完整正反测试与局部说明；继续不接App或真实Controller |
| 自动验收 | 搜索、组合筛选、无匹配、清空、数据更新、键盘操作、加载/失败与指标回归；测试不得访问网络。`npm run check:web`、`npm run check:policy` |
| 并行 / 审查 | 可与AUTH-01A/A01并行，互不改目录；总控检查行为/可访问性/质量回归后合并 |

## 交付与停止

提交当前目录、分支、起始HEAD、最终commit、文件清单、实际命令和结果、至少一个失败场景、未验证边界；保留本地历史。遇到共享文件需求先交差异，不通过降低测试或改原始资料“解决”。总控复审前不合并main、不推远程、不开始下一项。
