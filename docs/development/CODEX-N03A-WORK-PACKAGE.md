# Codex后续：N03-A · 预检先行的离线节点读取用例

日期：2026-10-29；任务总控codex-review-01。AUTH-01B通过独立审查后，本项只在nodes增加隔离的读取组合，复用已验收auth读取范围。D01–D07及P1-C10仍OPEN，G2未冻结，不授权HTTP/身份/存储或运行时profile接线。

| 项目 | 分配 |
|---|---|
| 会话 / 审查 | Codex codex-01 / 总控授权、安全与节点审查 |
| 分支 / Worktree | feature/nodes-read-usecase-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--nodes-read-usecase-cx01` |
| 起点 | 本次AUTH-01B复审交接后的main；开始记录实际HEAD。旧AUTH-01B目录停止保留 |
| 允许新增 | internal/nodes下独立读取用例、局部函数型库存来源接缝、测试及NODE-READ-USECASE说明；这是总控为本任务分配的nodes写权限，与DeepSeek当前internal/tasks不重叠 |
| 禁止修改 | 全部现有auth/nodes文件及测试、ports.go、tasks/Web/Controller/Agent、API/Proto/gen、r03候选/来源/限制探针、共享脚本/配置/依赖/锁/数据库结构、原始资料/批准ADR；不加依赖 |

## 目标和最小组合

新增一个以ctx、可信subjectID、auth.PolicyReader和显式库存读取函数为输入的离线用例。命名由作者选最小清楚形状，例如ReadVisibleInventory；局部函数接缝只返回现有[]Node，不设计通用仓储框架或替换现有Reader端口。内部每次调用先PrepareNodeReadScope，然后才读取库存，再用同一范围逐节点CheckNode。调用方不能传一个历史scope代替当轮预检。

新用例用于隔离证明“空库存也预检”和“本次授权过滤只用一次策略快照”；既有Visible/VisibleFilter/VisibleClassify及r03限制探针保持原样。新入口不接任何生产组合根，不代表整个系统的D07已经解决。

## 必须成立

1. **先预检再读库存**：主体/策略失败时库存来源调用数为0；不知道库存是否为空也不能跳过策略。正常有效调用读取策略恰好一次、库存恰好一次；CheckNode不得再次读取策略。已知主体零授权可以成功返回空集合，未知主体不能得到成功空集合。nil库存来源失败关闭，不能替换成生产mock/空成功。
2. **共同策略视图**：库存来源在预检后修改/替换原策略map，也不能让同次列表前后节点使用不同策略。新一次调用须重新预检并看见撤权；读取失败不回退到上一次范围或列表。不缓存reader、scope或结果，不造scopeRevision。
3. **仅明确拒绝可过滤**：复用已验收错误分类边界或明确等价的最小处理，非拒绝的错误一律中止且返回nil结果；取消/期限在入口、两个来源之后、遍历/复制及最终返回处不能变成成功/空列表/部分列表。不得将errors.Join中的故障按拒绝吞掉。
4. **库存来源失败**：来源即使同时给出部分数据和错误也不能输出。未知错误转换成固定安全的本地来源不可用错误，不展示或保留原文/unwrap因果链；标准或包装的context错误保留errors.Is分类而去掉包装秘密。nil ctx、取消、nil来源等明确拒绝，不制造后台goroutine终止不合作来源；来源需配合ctx。
5. **集合质量与复制**：保留可见节点的库存输入顺序；重复或空/非法节点ID等无效库存不能静默去重/跳过并产生成功计数。返回节点slice、Tags slice、Metrics指针均与来源脱离，修改来源或输出不能改写另一方；来源须保证本次读取/复制期间快照不被并发写入。只复制/过滤现有Node，不新增或伪造DTO字段、revision、状态或指标采样时间。
6. **授权后再筛选/计数**：后续消费者只能对用例的成功输出调用现有Filter/Classify；测试用隐藏节点的独有名称/标签/区域证明它们不进入输出后的筛选和分类计数。错误路径不能返回可供计数的部分集合。本轮无需新增查询/统计API。

## 接受范围及未解决问题

主体仍须未来可信认证适配绑定；PolicyReader/库存来源必须来自可信后端，并各自提供当前、一致且调用期间只读的快照。两个函数顺序调用**不等于**策略与库存拥有同一存储事务，也不保证撤权与输出序列化原子绑定。旧范围的时点语义继续有效，不能在返回前简单再读一次就声称没有TOCTOU。说明这些限制，D07保持OPEN。

不得实现登录/会话、Cookie/Bearer解析、全局列表权限、公开DTO迁移/HTTP错误映射、缓存头、数据库/锁/事务、审计持久写入、WSS/mTLS、审批/任务/执行。真实HTTP须等G2和身份/字段/错误决定；本项不改变D01–D07状态，不读写DeepSeek/Gemini在编目录。

## 验收和停止

直接测试真实新增入口及真实PrepareNodeReadScope：空库存下未知主体/坏策略/已知零授权；来源调用次序与次数；预检失败绝不读库存；来源变更不改变同次共同范围，新调用看见撤权；库存错误/部分数据+错误/重复坏ID；入口/最后来源/遍历及返回前取消/期限；敏感源错误不回显；双向深复制；先授权再筛选/计数。合成数据和可控回调，不用真实网络或sleep制造竞态，不用测试内自写用例实现替代生产函数。

命令：go test -count=1 -cover ./internal/nodes/... ./internal/auth/...；npm run check:go；npm run check:policy；npm run check:contracts（含r03来源/原限制测试）；npm run check:security。Go race/Linux/生产性能未执行则明确UNTESTED。

先读AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、本单及reviews/AUTH-01B-REVIEW.md。交付目录/分支、起始和最终HEAD、文件、命令/真实输出、负向场景、兼容边界与风险；任何FAIL/BLOCKED/UNTESTED如实保留。仅本地提交后停止，不合并main、不推送、不启动其他Agent或擅自扩到真实接口。
