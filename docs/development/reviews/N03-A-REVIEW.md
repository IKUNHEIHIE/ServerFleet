# N03-A · 预检先行离线库存读取独立复审

日期：2026-10-29（Asia/Shanghai）；任务N03A-REVIEW；总控会话codex-review-01；起始main `98a1f6b2971d87adbb88c74bbc8ec1484c0e9de2`。作者codex-01，原分支feature/nodes-read-usecase-cx01，起始8b2d8aa，精确HEAD `f57fe0dd3c77691518d4e5b5a30bda714c13bc81`。作者工作区跟踪文件干净；未修改作者源码/历史。

## 接收决定

**ACCEPT OFFLINE USECASE**：接收ReadVisibleInventory、测试和NODE-READ-USECASE说明，仅3个新增文件位于授权的internal/nodes。没有发现阻止本轮接收的新问题。现有auth/nodes文件/测试、Reader/Authorizer端口、API/Proto/gen、r03来源矩阵/旧限制探针、依赖/共享配置均未改。

ReadVisibleInventory每次真实调用PrepareNodeReadScope，成功后才调用显式InventorySource，随后用同一个范围检查所有节点。不能由调用者传历史scope替代准备；主体/策略失败不读库存，空库存也不绕过预检。正常调用策略/库存各一次；已知零授权得到非nil空集合，未知主体拒绝。nil库存来源在成功预检后返回来源不可用，没有mock/空成功兜底。

库存非context错误一律转为固定ErrInventoryUnavailable，丢弃同返部分数据，不保留原文或unwrap链；标准/包装context错误规范化并保留分类。CheckNode复用既有保守错误分类，仅明确纯拒绝可过滤。空/坏ID或重复ID（包括隐藏节点）中止，nil结果不暴露已积累前缀；没有静默去重后生成错误计数。

返回slice从可见节点增长，保留库存输入顺序；Tags和Metrics逐节点复制，即使源节点共享同一对象，输出节点也彼此脱离。Node其余值原样保留，不伪造revision/状态/采样时间或DTO。后续Filter/Classify只对成功授权输出执行；隐藏名称/标签/区域不能进入该输出后的搜索/计数。该约定仍须未来可信调用方落实，原Filter/Classify没有认证能力。

取消/期限在预检、来源后、遍历/标签复制及最终defer检查，已观察取消不能返回允许/空成功/部分数据。nil ctx由真实预检拒绝，不会在此后解引用；没有隐藏goroutine、队列、锁、重试、执行或生产接线。

## 保留的边界

主体必须由未来可信认证绑定，两个来源须提供可信当前快照并在相应读取/复制期间只读。库存回调在准备后修改策略不会改变同次捕获范围；新调用重新准备才看到撤权。这是时点语义，不是即时撤权，也不等于策略/库存同一存储事务或与输出序列化原子绑定；不以再读一次或伪scopeRevision声称消除TOCTOU。

坏库存ID的auth.ErrInvalidCheck来自后端库存，不证明客户端输入错误，不能直接推出HTTP400。库存来源故障/主体拒绝等仍只是内部分类，公开错误媒体/HTTP码/防枚举依然待D06批准。新用例没有更改旧Visible；r03空库存/逐节点策略限制测试仍针对旧入口，保留原样PASS不表示整个系统已关闭D07。

DeepSeek继续READ-PLAN-01，Gemini继续W05-R2；未读取其未提交内容或改变目录/基线/任务。W05仍未合入，W05-R01仍OPEN、W05-R02已关闭。**G2与D01–D07/P1-C10仍未关闭**；本项不批准真实身份、HTTP、数据库、运行时profile或首条真实管理API。

## 总控实际复验

独立integration/n03a-review先登记任务，按共同祖先合入精确作者提交；组合代码HEAD `0ec62214d206af71c4dffb81bdc033fde4eec062`，保留main已有TASK-01C等已验收代码。之后只新增审查/任务文档。固定Go1.27.3、Node24.22.1/npm11.19.1；合成输入/显式时间，无真实节点、云或凭据。

| 检查 | 结果 |
|---|---|
| 作者精确HEAD：go test -count=1 -cover '-coverprofile=reports/n03a-review-cover.out' ./internal/nodes/... ./internal/auth/... | PASS，nodes96.3%、testkit100%、auth98.6%；新增ReadVisibleInventory及两个auth范围函数均100.0% |
| 总控实际用例独立探针 | PASS，33断言：来源顺序/次数、预检失败零库存调用、空库存/零授权、共同范围/新调用撤权、错误无前缀/敏感链、坏/隐藏重复ID、双向及节点间复制隔离、授权后筛选/计数、最后来源取消、nil来源 |
| 组合npm ci / npm run tools:install | PASS，127包/4个固定工具；复用本地file模块缓存，未改许可/依赖或关闭校验 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试、0失败，Web151项 |
| 组合npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 原资料/契约/生成一致性 | PASS，69原文件/3原候选字节校验、OpenAPI/Buf及r02/r03显式检查，r03候选哈希未变 |

摘要/原始输出/探针/覆盖率见N03-A-RESULTS.json及evidence/n03a-review-*。作者基于8b2d8aa的201顶层/782子测试不含后续TASK-01C，组合实际为212/805；没有使用旧基线计数代替本次执行结果，也不包含未接收的W05。

没有真实浏览器/HTTP认证/库存存储、跨请求生命周期强制、撤权与输出原子性、缓存、WSS/mTLS、审批/任务执行、Linux race、GitHub CI或1C1G性能验收。离线共享范围与复制通过不代表这些能力已实现；原始资料/候选/生成与旧限制探针保持不变。

## 后续与停止

只给Codex分配READ-SEC-01：在独立新文档目录核验安全条款来源、批准状态、现有实现/负向测试证据和批准前缺口。重点D05/D06/D07，明确未收录安全ADR及来源坏ID/内部错误不能直接映射HTTP的事实。DeepSeek负责总体方案/负责人选择，Codex此项提供独立安全事实，不新增产品选项或自启认证实现。

范围/目录/停止条件见CODEX-READ-SEC01-WORK-PACKAGE.md。不改原始资料或既有候选，不关闭G2或D01–D07；旧提交/目录/报告保留。完成验证、证据、main接收和Codex新目录交接后停止，不启动/代发其他Agent消息、不推送远程。
