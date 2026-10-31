# 第六批部分验收：TASK-01A / W04-R1

日期：2026-10-29（Asia/Shanghai）；任务BATCH06-PARTIAL-REVIEW；审查人codex-review-01；起始0fef8ba。负责人要求先审核DeepSeek/Gemini并给两位安排后续任务，明确Codex的G2-R02A仍在工作。

## 决定与范围

| 任务 | 精确HEAD | 结论 |
|---|---|---|
| TASK-01A / deepseek-01 | `0204d299ca2aadfc48c2649ebeaadedded609431` | ACCEPT：目标快照/未启动等待窗口；5个新增文件均在internal/tasks，ports.go不变 |
| W04-R1 / gemini-01 | `b3150464b78967dd36c33a00bade6faa4119001f` | ACCEPT：W04-R01关闭；接收原8e95c3c与追加返修，保留历史 |
| G2-R02A / codex-01 | 本轮没有交付提交 | 负责人确认进行中，未审查/未接收；不检查其未提交内容，不改原任务、分支、基线或目录 |

两位作者工作区无未提交跟踪文件变更；总控只在忽略的reports/_review保存探针/配置，不改作者源码。独立integration/batch06-partial-review组合代码HEAD为`169591d0f44013f05fb8ec490ef3685fa19bada5`；通过全量检查后才接收main。旧Gemini分支按共同祖先合入，main已有节点授权/通信规则保留，没有把后续主线文件误删。原提交/分支/报告均保留，无推送或真实节点操作。

## TASK-01A审查

TargetSnapshot字段私有，构造拒绝空/坏/重复ID，按输入顺序复制；IDs返回新slice，零值只表示空，不解释为全节点。错误仅含固定分类和索引，不回显ID。后续成员变化不能改写旧快照；快照不证明节点存在、授权或审批有效。

WaitWindow使用显式创建时间/正时长，默认15分钟；字段私有，Evaluate拒绝零时间、无效窗口与未知起始状态。截止前pending、恰好/之后expired；已启动返回not_applicable，不由等待TTL重新入队或推断成功。没有隐藏时钟、Submitter/调度器、存储、发送或执行。是否离线及阶段一致性仍由未来可信任务服务判断；pending不是可执行许可。

本轮没有发现阻止这项纯规则接收的新问题。模块100%语句覆盖不代表真实任务/权限闭环或并发状态一致性已经实现。

## W04-R01关闭证据

同ID键盘激活现在直接聚焦已挂载详情，不依赖同值setState产生提交；关闭、指针替代及失效路径清理待执行意图，effect消费前先清标志，避免请求漂到后台刷新。原搜索/选择/来源失效隐藏/非模态焦点和指标行为保留。

总控重跑第五批两个失败用例，测试源码不变，仅外置配置中的报告目录从batch05改为batch06：重复激活立即聚焦、搜索后刷新不抢焦点均PASS，W04-R01 CLOSED。作者另加入Enter、Space、刷新和关闭回归，常规前端151项通过。

作者真实浏览器CDP脚本/JSON保留，并补充鼠标选择→重复Enter→详情聚焦→搜索→数据刷新：refresh计数0→1后activeElement仍为INPUT；原Tab/离开/Escape返回及420/1280视口记录仍在。本轮只读核对该记录，未由总控重启作者预览或重跑浏览器；独立复验是上述真实组件Vitest。不要把作者浏览器资料表述成总控现场浏览器验收。

## 总控实际复验

固定Go1.27.3、Node24.22.1/npm11.19.1；仅使用合成数据/固定时间。

| 检查 | 结果 |
|---|---|
| TASK-01A：go test -count=1 -cover ./internal/tasks/... | PASS，100.0% |
| 总控真实任务规则探针 | PASS，14断言：目标复制隔离/顺序/后增成员、坏输入、15分钟默认、截止纳秒边界、已启动不适用、未知/零窗口拒绝 |
| W04-R1：npm run check:web | PASS，151项、类型检查/构建通过 |
| 第五批原两个独立焦点用例 | PASS，2/2；原FAIL仍保留在第五批证据 |
| 组合npm ci / npm run tools:install | PASS，127包/固定4个Go工具；复用已验证的本地file模块缓存，未改锁或关闭校验 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go158顶层/595子测试、0失败；Web151项 |
| 组合npm run check:security | PASS，npm0漏洞，govulncheck无漏洞 |
| 原始资料/现有契约 | PASS，69原始文件与3原候选字节校验、OpenAPI/Buf兼容/生成一致性及r02显式检查。没有包含Codex在编r03，不能当作其交付已验证 |

摘要见BATCH-06-PARTIAL-RESULTS.json，原始输出/探针文本/作者浏览器JSON见evidence/batch06-*。探针位于reports/_review，源码以文本归档，不混入常规Go/Web测试包。

## 后续与停止

只给DeepSeek分配TASK-01B默认分批/后批推进纯规则，给Gemini分配W05注入只读数据源的节点加载容器；均用新分支/目录，范围见PARALLEL-FOLLOWUP-01.md。两个任务不依赖或读写G2-R02A的未提交契约；Codex继续原0fef8ba基线、原分支和第六批范围，无自动启动/消息派发。

G2未冻结；真实身份/策略存储、数据库/HTTP/WSS/mTLS、任务调度/审批/执行、Linux race/GitHub CI及1C1G性能尚未验收，App仍为开发骨架。此次只验收两项，不标记整个第六批完成。交付审查和两个新任务目录后停止，不干预正在工作的Codex。
