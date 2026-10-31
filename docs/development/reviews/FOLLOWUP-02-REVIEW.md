# TASK-01C / W05-R1独立复审

日期：2026-10-29（Asia/Shanghai）；任务FOLLOWUP02-REVIEW；总控会话codex-review-01；起始main `8b2d8aa5b7d249b032d2c64abbbc2f758c739a4a`。只审查两位本次任务的已提交代码，Codex继续N03-A原目录/8b2d8aa基线，本轮未读取其未提交内容或改变范围。

## 提交核对与决定

| 任务 | 负责人所报 / 实际审查提交 | 结论 |
|---|---|---|
| TASK-01C / deepseek-01 | 所报a652696671e718643ab8d80dba58f13e691a521c是分配起点；任务原分支干净HEAD实际为`e169afa9e1b8e1277b8b63119c3d7acee6c2851c`，仅领先该起点1个范围内提交 | ACCEPT：按实际任务交付审查接收，明确记录提交号更正，没有把基线当作新实现 |
| W05-R1 / gemini-01 | `54df33ec4f0bc34e0bce5734d0a769bb0b543baa`，保留原0ebe812 | CHANGES_REQUESTED：W05-R02 CLOSED；W05-R01仍OPEN，暂不合入原W05或本次返修 |

DeepSeek新增4文件均在internal/tasks，现有规则/测试/HANDOFF/ports未改。Gemini相对原交付仅修改加载容器、对应测试和README三个文件；没有改既有展示组件、App/契约/依赖。两位工作区跟踪文件干净；总控只在忽略reports/_review写入外置验证，不改作者生产源码或历史，不检查其他任务未提交代码。

独立integration/followup02-review先登记任务，按共同祖先合入实际DeepSeek提交，代码HEAD为`c03cdd62bc120b21ff9f70aa99fedf6df3bb89fd`。保留main已有AUTH-01B及此前代码，之后只新增审查/任务文档；W05未参与组合。原分支、原失败与输出全部保留，无远程推送、真实节点或云操作。

## TASK-01C接收依据

StartBatchProgress从有效非空BatchPlan深复制批次，只能从首批waiting开始。所有状态/索引/计划字段私有，目标访问器给副本；零值不有效，ApplyBatch拒绝。ApplyBatch复用真实EvaluateBatch，incomplete原地等待，advance仅加一，末批成功complete、失败/取消/超时blocked；终结值不能再次提交或重开。非法结果返回原进度和空outcome，原值、结果输入和计划不被修改。

没有额外时钟、锁、队列、I/O、执行许可或完整TaskState；按值复制共享的计划只通过私有只读入口访问。调用者不能传一个索引跳批，不能把前批结果套到不同目标的下一批。**旧waiting副本仍可重新计算同样推进**，这符合明确的纯值范围，不是防重放或持久权威进度；不能宣称解决调度并发、执行ID绑定或重启恢复。作者说明已清楚区分，未把unknown变成自动重试。

模块测试100.0%语句覆盖，独立真实新API探针165断言PASS：普通/root/修改各13目标、多批序列、等待/失败终结/终结拒绝、旧批结果拒绝、输出复制与旧值不变，以及零计划/零进度拒绝。它只验证纯进度类型，不是测试内自写调度器，不证明真实派发门禁已落实。本轮没有阻止接收的新问题。

## W05-R1结论

### W05-R02 · CLOSED

key以精确字符串作为身份，带首尾空白来源直接安全失败，避免trim后等价；原总控第二个用例未改源码重跑PASS。原A成功→null→恢复A的首提交补测也PASS，说明key变化的恢复路径已修复。作者补上真实仍pending旧A在B失败重试期间迟到成功/失败，原已reject Promise再resolve的说明测试保留但不再作为竞态证明；常规前端177项通过，StrictMode/固定安全提示/指标/键盘回归保留。

### W05-R01 · P2 · 仍OPEN：同key来源可用性恢复复活旧ready

位置：`apps/web/src/features/nodes/node-loader.tsx:62–67`。identity只保存key/epoch，更新条件只看key变化；但usable还取决于load是否函数。

真实组件复现：有效A(key=scope-a)成功显示8节点并打开详情→传入同key但load=undefined的无效来源→恢复有效A，第二次加载仍pending。无效阶段安全失败且无表格；恢复时key和epoch都未变，首个布局提交再次显示旧节点`aws-tokyo-01`、`共 8 个节点`及旧详情，然后effect才设loading。这次补测还实际观察到旧详情复活，不能以最终DOM无旧内容代替首提交约束。

这条路径属于原W05-R1任务明列的“无效load恢复”，当前组件也主动用typeof识别该无效输入；测试用类型断言构造运行时坏接缝，不是在声称正常TypeScript调用方必然传错。修复应使来源可用性转变同样使旧世代失效，不只处理key变化。不要求每次有效内联对象引用变化都重新请求，也不扩大到通用缓存/请求框架。

总控原两例+新增三例共5个实际组件用例：**4PASS/1FAIL**。原null恢复及空白key用例PASS；同key无效load时pending请求会abort，旧成功/旧错误均被忽略，另两例PASS；只有ready恢复首提交用例FAIL。布局阶段输出、源码/config、JUnit归档为evidence/followup02-loader-probe*，原FOLLOWUP01的FAIL仍保留。没有真实浏览器/跨用户攻击或HTTP验收。

## 实际检查与边界

固定Go1.27.3、Node24.22.1/npm11.19.1，合成数据/可控Promise，无真实网络和sleep竞态。最终组合仅含已接收TASK-01C及原main，不含W05。

| 检查 | 结果 |
|---|---|
| TASK-01C模块go test -count=1 -cover ./internal/tasks/... | PASS，100.0% |
| 总控真实进度API探针 | PASS，165断言 |
| W05-R1 npm run check:web / check:policy | PASS，177项及类型/构建、规则检查 |
| W05独立首提交/迟到响应补测 | 4PASS/1FAIL，保留实际失败，W05-R01未关闭 |
| 组合npm ci / npm run tools:install | PASS，127包/4个固定工具；本地file模块缓存，未关闭校验或修改许可/依赖 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go200顶层/700子测试、0失败；Web151项 |
| 组合npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 来源/契约/生成一致性 | PASS，69原文件/3原候选字节校验、OpenAPI/Buf及r02/r03显式检查；r03候选hash未变 |

摘要和原始输出/探针见FOLLOWUP-02-RESULTS.json及evidence/followup02-*。主线PASS不包含未接收的W05，也不覆盖其独立FAIL。

G2及D01–D07/P1-C10仍OPEN；没有新批准运行时profile、真实身份/数据库/HTTP/WSS/mTLS、任务审批/执行、Linux race/GitHub CI或1C1G性能验收。App仍是开发骨架，PureProgress和UI测试通过不能替代真实闭环。

## 后续与停止

DeepSeek转入READ-PLAN-01，新增可批准的首条只读闭环方案/验收包：将现有7个待决项转为有依据的推荐、依赖与最多3个负责人选择，明确哪些事实/建议/缺失依据，不再重复矩阵或自启实现。这里只授权新目录中的文档提案，不批准认证、契约冻结或真实接线。

Gemini原分支追加W05-R2，补同key无效load恢复的世代隔离，保持177项与5个总控用例。范围/路径/停止条件见PARALLEL-FOLLOWUP-03.md。Codex继续N03-A原任务，不同步main、不干预其目录。总控完成证据、主线接收和两位任务交接后停止，不启动或代发其他Agent消息。
