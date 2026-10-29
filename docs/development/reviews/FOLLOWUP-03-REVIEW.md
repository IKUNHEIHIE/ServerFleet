# READ-PLAN-01 / W05-R2独立复审

日期：2026-10-29（Asia/Shanghai）；任务FOLLOWUP03-REVIEW；总控会话codex-review-01；起始main `7ef78771da410a06d62a8605058e67a2f066ceb5`。只审查本次两位交付，Codex继续READ-SEC-01原任务/目录；未读取其未提交内容，不要求同步main，不启动/代发其他Agent消息。

## 决定

| 任务 | 精确HEAD | 结论 |
|---|---|---|
| READ-PLAN-01 / deepseek-01 | `d0709fc1fe0a5518000bdb7b81f8c1f8f82334e1` | CHANGES_REQUESTED：RP01-R01/R02两个P2；方案3个新增文档在允许目录，尚未合入/提交负责人批准 |
| W05-R2 / gemini-01 | `fcb9382315b71e80d1340c4d15419bfcb194cd43` | ACCEPT：W05-R01关闭；接收原0ebe812及54df33e、fcb9382两次返修，W05-R02保持CLOSED |

两位作者跟踪工作区干净；总控未改作者源码或历史，仅在忽略reports目录增加/读取外置验证。Gemini原分支从adce532起累计4个范围内文件，按共同祖先合入，没有把旧基线缺少的r03/新Go文件当成删除；现有candidate-adapter的两处来源注释及来源哈希保留。独立integration/followup03-review先登记任务，接收Gemini后组合代码HEAD为`b44352fba27a462442a97d7f7748f526bd22e2e6`，之后只增加审查/任务文档。DeepSeek方案没有参与main组合。

## W05-R01关闭与接收范围

来源世代现由精确key和可用性共同决定；同key的load由函数变为非函数再恢复也会在渲染期使旧snapshot失效。恢复首提交不能重新展示旧节点/计数/详情，不依赖被动effect事后清空。原null来源恢复、空白key安全失败、迟到成功/失败、忽略abort、重试/卸载及稳定有效同key不重复请求等边界保留。

总控重跑原5个真实组件用例，源码与FOLLOWUP02归档逐字节SHA-256一致，只更换外置配置的报告输出目录。结果**5/5 PASS**，其中原同key ready失效恢复的FAIL现在PASS；两个原最初用例及pending旧成功/错误仍PASS。源码完整性记录、原样测试文本、输出/JUnit见evidence/followup03-loader-probe*及followup03-probe-integrity.json，旧FOLLOWUP01/02失败仍保留。

作者新增undefined加载来源的布局阶段首提交回归，原177测试保留，常规前端现179项、类型/构建通过。接收的是可注入局部NodeListItemView来源的隔离加载容器，不是公开DTO适配、真实fetch或App接线；key仍不是认证凭证。身份/授权来源变化需可信调用方更换key，容器不能证明RBAC或撤权。此轮总控仍仅做jsdom组件复验，未做真实浏览器Tab/截图或真实HTTP/跨用户攻击验收；后续W06补实际浏览器证据。

## READ-PLAN-01返修

方案明确PROPOSED，分阶段先旧兼容列表、后富化/详情，以及Cookie方向均只是待批建议。没有擅改代码/候选/决定状态。总控按其精确98a1f6b引用基线独立核查61处引用、24个文件，路径与行区间均存在；这与作者机械检查结果一致，**不代表语义正确或方案已批准**。

### RP01-R01 · P2 · 混淆缺少字段与权威来源待确认

位置：PROPOSAL.md第12行；相关第30、70行与Q3/依赖拆分。

推荐阶段把revision/status/platform/agentVersion来源、共同采样语义概括为“这些是当前内部Node没有的字段”。实际同一基线的internal/nodes/node.go已有Status以及Metrics.ObservedAt，方案第30行也正确承认这一点。真正缺revision/platform/agentVersion；Status/ObservedAt存在但权威来源、状态优先级、共同样本与公开字段语义尚待确认。把它们统称结构缺失会误导前置任务和负责人对范围/工期的判断。

需修正开头、Q3/依赖和HANDOFF的相应概括，区分数据结构、数据当前性/权威与公共形状。公开NodeDetail的平台/Agent版本等是可选字段，Web局部对应值可以为null；不要把它们必须拥有非空来源与revision/status的真正前置条件混为一谈。D02栏的五状态/online一致性来自未批准r03提案，应明确提案身份，不能以“当前事实/约束”让它看似已获批准。此项不否定暂缓详情的建议，也不要求伪造缺失字段。

### RP01-R02 · P2 · legacy兼容计划遗漏onlineOnly请求行为

位置：PROPOSAL.md第45–51行；ACCEPTANCE.md的COMPAT-01/Controller后续拆分。

既有原Merged及r03同一GET都有onlineOnly布尔query、默认false（r03候选第24–29行，原Merged第94行）。三份方案文档均未出现onlineOnly，只列路径/operationId/items/字段；若按这一“兼容”任务实现，会缺失原有筛选请求行为。只验证响应旧字段不足以证明请求兼容。

须把参数与默认语义纳入R1-A/D01/任务/验收：先完成身份/策略预检和授权可见集合，再执行onlineOnly过滤；true/false/省略、隐藏在线节点不进计数、过滤成空仍不绕过预检、策略故障不能变200空列表均有未来验收。坏query解析与HTTP映射按批准方案明确，不能在文档里擅定未批准细节或另增路由。

### 其他交接澄清

Q1与Q3的阶段选择重叠；Q3标题是“现在还是R1-B补”，替代却为“永久legacy-only”，请合并重复选择或使标题/选项对齐，最多3个真正独立问题。不能让阶段顺序选择隐含删除已确认产品能力。

N03-A/W05未接收的描述在作者98a1f6b历史基线下属实，**不作为作者误报或另一个退回问题**。如今主线已经接收两项，返修可追加明确的当前接收记录/精确来源，保留历史基线语境，不要求作者合并main或读取其他会话未提交内容。真实认证/HTTP仍未实现，不能因更新接收状态写成闭环PASS。

## 实际验证与边界

固定Go1.27.3、Node24.22.1/npm11.19.1；固定合成数据与受控Promise，没有真实网络或凭据。主线组合只含W05及返修，不含未接收的方案。原资料69文件/3候选与r02/r03/生成一致性继续检查，不改变规则来掩盖问题。

| 检查 | 结果 |
|---|---|
| W05-R2原5个独立组件用例 | PASS，5/5；两份测试源码SHA-256与原归档一致，原FAIL保留 |
| 作者精确HEAD npm run check:web | PASS，179项、类型/构建通过 |
| READ-PLAN引用/原文事实核验 | 61处引用/24文件路径及行区间有效；来源事实/onlineOnly两项内容问题仍FAIL，不能以引用机械检查替代语义审查 |
| 组合npm ci / npm run tools:install | PASS，127包/4个固定Go工具；本地file模块缓存，未关闭校验或改许可/依赖 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go212顶层/805子测试、0失败；Web179项 |
| 组合npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 原资料/契约/生成一致性 | PASS，69原文件/3原候选、OpenAPI/Buf及r02/r03显式检查；r03候选哈希不变 |

摘要和源码/输出见FOLLOWUP-03-RESULTS.json及evidence/followup03-*。来源冲突补记到P1-C10，明确未接收草案/未批准提案，原始资料与候选不改。

G2与D01–D07/P1-C10保持OPEN；没有批准R1-A/R1-B、Cookie/会话、legacy VM、字段迁移或真实接线。真实身份/HTTP/数据库/策略库存输出原子性、WSS/mTLS、审批/执行、Linux race、GitHub CI与1C1G性能均未验收。没有把文档核对或组件PASS当作这些能力。

## 后续与停止

DeepSeek原分支追加READ-PLAN-01-R1，只修原三份方案文档的事实/参数/验收/选择题与状态交接；Gemini转新W06目录做隔离真实浏览器/原生键盘验收，提交报告，临时预览/截图/trace仅忽略reports，不接生产App/真实API。范围和停止条件见PARALLEL-FOLLOWUP-04.md。

Codex继续READ-SEC-01，方案与安全证据均完成复审后再向负责人提交可批准的选择包，不提前索取抽象批准。完成本轮证据、主线接收与任务交接后停止，保留全部历史/目录/工件，不推送、不启动Agent。
