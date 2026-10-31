# AUTH-01B · 离线读取预检与不可变范围独立复审

日期：2026-10-29（Asia/Shanghai）；任务AUTH01B-REVIEW；总控会话codex-review-01；起始main `a652696671e718643ab8d80dba58f13e691a521c`。负责人本轮提交Codex精确HEAD `14b9eacdaa6fb0ea6c0921e1f2fdf8bc912e82fd`，作者codex-01，分支feature/auth-read-preflight-cx01，原基线1f8f8cb。作者工作区跟踪文件干净，未修改其源码/历史。

## 接收决定

**ACCEPT OFFLINE RULES**：仅接收新增的node_read_scope.go、node_read_scope_test.go和NODE-READ-SCOPE.md，三个文件均在允许的internal/auth内。没有发现阻止本轮接收的新问题。既有auth文件/测试、Authorizer端口、nodes、API/Proto/gen、r03候选/矩阵/限制探针和依赖配置均未改。

PrepareNodeReadScope不需要库存或假节点；先校验可信主体格式，再通过显式PolicyReader读取一次策略，校验整份策略，确认主体存在并复制其精确nodes.read集合。已知主体无授权可形成有效空范围；未知主体即使没有库存也拒绝。nil读取器/策略、读取失败、坏策略和悬空授权均失败关闭，不保留来源错误文字/链，也不回退旧允许。

NodeReadScope只有私有map，唯一公共方法CheckNode只收ctx和精确node ID，不允许覆盖主体/动作，也不实现Authorizer。nil/零值范围拒绝；无授权和未知目标拒绝；坏目标拒绝。没有成员访问器、源策略、读取器、时钟、后台任务、身份/授权缓存或生产接线。复制完成后源map/外层map变化不能扩展或收缩旧范围；新Prepare重新读取当前策略，撤权后新范围拒绝原节点。

取消/期限检查覆盖入口、读取后、策略遍历、授权复制和返回；准备命名返回值的defer会移除可用范围，CheckNode也不能让已观察到的取消变成允许。包装context错误被规范化，errors.Is分类保留，敏感包装文字去除。来源仍需配合ctx，没有用后台goroutine强杀读取器。同步返回检查之后才发生的取消无法预知，作者说明已明确。

## 边界与尚未关闭事项

捕获范围只代表一个显式时点，旧范围仍允许旧集合是约定语义，不是实时撤权接口。调用方必须每次请求重新准备；Go类型不能强制身份绑定、来源当前性或禁止跨请求错误复用。来源在准备期间必须保持只读；捕获后可变更不等于准备期间并发写map安全。

旧Visible及r03的空库存/逐节点策略限制测试保持原样；本项没有让旧入口自动预检，也没有实现库存与策略同一存储事务、输出前撤权与序列化原子性、Cookie/Bearer认证、HTTP错误/缓存或真实身份。**D07与D01–D06/P1-C10仍OPEN，G2未冻结**。通过本模块不能声明首条真实管理API已完成。

DeepSeek继续TASK-01C，Gemini继续原分支W05-R1，W05-R01/R02仍OPEN且W05仍未合入。没有读取两位未提交内容、切换其分支或发送消息；本轮只审查Codex交付。

## 总控实际复验

独立integration/auth01b-review目录中，先登记任务再按共同祖先合入精确作者提交；组合技术HEAD为`35084a8128a91c01b75886a8550e5120e90752d2`。之前main的TASK-01B等已验收代码保留；之后只增加审查/任务说明。固定Go1.27.3、Node24.22.1/npm11.19.1；合成数据，无真实节点或云。

| 检查 | 结果 |
|---|---|
| 作者精确HEAD：go test -count=1 '-coverprofile=reports/auth01b-review-cover.out' ./internal/auth/... | PASS，包98.6%；PrepareNodeReadScope/CheckNode均100.0% |
| 总控真实函数独立探针 | PASS，36断言：每次策略读取次数、跨主体拒绝、源集合修改隔离、新准备撤权、来源失败无旧允许、空库存已知零授权/未知主体、坏策略/坏输入、nil/零范围、敏感错误与取消规范化 |
| 组合npm ci / npm run tools:install | PASS，127包/4个固定Go工具；复用本地file模块缓存，未关闭模块校验或修改许可/依赖 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go189顶层/677子测试、0失败，auth28顶层/233子测试；Web151项 |
| 组合npm run check:security | PASS，npm0漏洞，govulncheck无漏洞 |
| 原资料/契约/生成一致性 | PASS，69原文件与3原候选字节校验、OpenAPI/Buf及r02/r03显式检查；r03候选哈希未变 |

摘要见AUTH-01B-RESULTS.json，原始输出/探针/覆盖率见evidence/auth01b-review-*。作者原Go176/648包含其1f8f8cb基线，不包含随后main接收的TASK-01B；本次组合实际为189/677，没有用旧计数替代。组合中没有W05或其他未接收工作。

作者曾有PowerShell未引用coverprofile参数导致的失败，其说明已保留。本次总控使用正确引用实跑模块与覆盖率，没有修改检查或删除用例。本轮调用工具时一段编排JS语法错误在任何命令执行前被拒绝，修正后才启动组合check/security；该次不能当作检查已运行。

未验证真实认证/策略存储、跨请求生命周期强制、库存/策略/输出原子性、HTTP/缓存、WSS/mTLS、任务/审批/执行、Linux race、GitHub CI及1C1G性能。只读并发测试PASS不冒充race检测；r03仍是候选工件，不是已批准运行方案。

## 后续与停止

只给Codex分配N03-A：在新目录新增预检先行的离线节点读取用例，复用真实PrepareNodeReadScope及同次共同范围，先预检再调用库存来源、只对可见集合后续筛选/计数。总控明确分配internal/nodes新增文件权限，不与DeepSeek当前internal/tasks任务重叠，现有auth/nodes文件及r03限制探针保持不改。见CODEX-N03A-WORK-PACKAGE.md。

该组合仍不解决真实身份、存储原子性或撤权/输出绑定，不接Controller/HTTP，不批准G2。旧分支、报告和工件保留；完成本轮验证、证据、main接收及Codex新目录交接后停止，不启动Agent、不推送远程。
