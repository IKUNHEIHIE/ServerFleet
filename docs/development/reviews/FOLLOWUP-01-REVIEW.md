# TASK-01B / W05独立审查

日期：2026-10-29（Asia/Shanghai）；任务FOLLOWUP01-REVIEW；会话codex-review-01；起始main `1f8f8cbcb9655484d391a287865b38eee9c6885b`。本轮只审核负责人提交的DeepSeek/Gemini精确HEAD，Codex继续AUTH-01B原任务/目录/基线；未检查其未提交内容，未启动或代发其他会话消息。

## 接收决定

| 任务 | 精确HEAD | 结论 |
|---|---|---|
| TASK-01B / deepseek-01 | `e23a4c543e539cd6398077400bef5dab7caa64e8` | ACCEPT：默认分批计划与当前批结果纯判定；5个文件均在internal/tasks，现有ports/快照/等待规则未改 |
| W05 / gemini-01 | `0ebe81224e63065d40a608e9eccb15123d9c8d60` | CHANGES_REQUESTED：W05-R01、W05-R02两个P2；4个文件在features/nodes，未合入main |

两位工作区均无未提交跟踪文件变更；只在忽略的reports/_review写入外置探针/配置，并按固定工具安装流程补齐Gemini本地忽略工具，不改作者生产源码。两项作者基线均为adce532，未要求重放到最新main。总控独立目录为`.worktrees/integration--followup01-review`，接收TASK-01B后的实际代码HEAD为`4e6c1f28aafb14ae7bc42d4bb5866833f246c481`；全量验证针对该代码，之后仅增加审查/任务文档。原提交/分支/工件保留，无远程推送或真实节点操作。

## TASK-01B

NewDefaultBatchPlan从已接收TargetSnapshot复制目标，未知/零分类与空快照拒绝。普通每批最多5；root或修改首批1、后续最多5；保持目标输入顺序且不丢失/重复。计划字段私有，Batches深拷贝，零计划没有批次且EvaluateBatch拒绝索引。分类来源明确为未来可信动作目录，不以AI/客户端自报作为安全依据。

EvaluateBatch检查当前批目标关联、代码、重复/批外证据。仅全部可靠succeeded才advance或末批complete；failed/canceled/timed_out阻止后批；running/unknown/缺结果不推进。非法输入的结果为空并返回分类错误，不回显node ID。没有ACK/exitCode到成功的推断，也没有取消、重试、调度、I/O或执行。

这个函数无状态，不保存前批失败、执行ID或证据新鲜度。调用者必须遵循顺序，并在派发前实时授权/审批/TTL校验；测试内顺序driver只证明遵循调用协议时会停，不代表已经实现调度器或持久化失败屏障。这是本轮明确范围内的剩余边界，未作为已实现能力。后续TASK-01C仅增加不可变顺序进度纯值，仍不能解决真实执行、防旧副本重放或崩溃恢复。

本轮模块测试PASS，100.0%语句覆盖；总控直接调用真实构造/判定函数的69个独立断言PASS，覆盖18组普通/root/修改数量与复制隔离、逐批成功、失败/取消/超时/running/unknown/缺证据，以及重复/批外/ACK/零值拒绝。没有发现阻止接收的新问题。

## W05返修事项

### W05-R01 · P2 · 无效来源恢复后首帧重新显示旧数据

位置：`apps/web/src/features/nodes/node-loader.tsx:58–60`及后面的snapshot.key比较。

真实组件复现：来源A成功并显示8节点→source=null→恢复相同A，第二个load保持pending。null阶段effect提前return，没有使旧ready快照失效；恢复时snapshot.key再次等于sourceKey，布局阶段记录的首个提交已显示`aws-tokyo-01`及`共 8 个节点`，之后被动effect才切loading。即使最终DOM没有旧表格，也不满足首提交隔离要求。此处复现证明的是节点/计数旧数据曝光；工作区因key变化重建，未据此声称旧详情也已复现。

要求：来源失效应使先前加载世代失效；恢复时首个提交就不能使用前一世代ready，即使原key回来。保留无效来源安全失败、abort、旧响应丢弃以及同一有效来源稳定重渲染行为。不要只把清空移到另一个被动effect后宣称没有短暂提交。

### W05-R02 · P2 · trim把不同来源身份合并

位置：`apps/web/src/features/nodes/node-loader.tsx:48–49`及各处active.key.trim比较。

真实组件复现：A的key为`scope-a`且已成功，换成key为` scope-a `的新来源。trim后身份和effect依赖没有变化，旧8节点/计数持续展示，新来源load也不会调用。局部接口没有规定身份按trim等价，不能静默把新作用域映射成旧作用域。

要求：保留精确标识用于身份比较，或明确拒绝首尾空白标识并安全失败。第二种修复不要求调用新load，但两种都必须从首提交隐藏旧数据。空/全空白来源仍应拒绝；key仍非授权凭证，身份/授权范围变化须由调用者更换key。

总控外置测试用布局effect观察真实DOM首提交，两个用例均实际FAIL（2/2），未修改作者组件。源码/config文本、原始输出和JUnit见evidence/followup01-loader-probe*。这不是生产跨用户攻击验证；它证明当前局部容器的隔离约定被破坏。

### 验证缺口与已通过范围

作者常规前端169项（原151+新增18）及类型/构建均PASS；稳定来源、常规不同key切换、旧成功/错误、忽略abort、卸载、安全提示和既有指标/键盘路径有回归。本轮未做真实浏览器操作或真实HTTP，不能把jsdom当作浏览器现场验收。

原“重试后旧请求成功”测试在同一个Promise已reject后再resolve，不会产生迟到成功，不能单独证明该竞态。返修须补仍pending的旧A→B失败→重试B→旧A迟到成功/失败等真实交错，验证loading和新结果均不被覆盖。

交付目录原check:policy为FAIL（spawnSync go ENOENT）；总控加载固定环境后重跑，Go模块边界通过，但因本地缺actionlint仍FAIL。补齐固定Go工具后再重跑PASS，输出见evidence/followup01-web-policy-after-tools.log。原FAIL均保留，不把工具缺失误报成业务代码缺陷，也不据最终policy通过关闭两个实际组件问题。

## 总控实际验证

固定Go1.27.3、Node24.22.1/npm11.19.1，合成数据/显式时间；没有新增依赖或降低检查。组合只含已接收TASK-01B及原main，不含W05。

| 检查 | 结果 |
|---|---|
| TASK-01B模块go test -count=1 -cover ./internal/tasks/... | PASS，100.0% |
| 总控任务纯函数独立探针 | PASS，69断言 |
| W05 npm run check:web | PASS，169项，类型/构建通过 |
| W05 npm run check:policy | 最终PASS；原Go路径/工具缺失两次FAIL均归档，固定环境/工具补齐后重跑 |
| W05首提交/来源身份独立负向补测 | FAIL，2/2；作为返修证据保留 |
| 组合npm ci / npm run tools:install | PASS，127包/4个固定Go工具；复用本地file模块缓存，未关闭模块校验 |
| 组合npm run check | PASS，policy/contracts/go/web/git；Go175顶层/629子测试，0失败；Web151项 |
| 组合npm run check:security | PASS，npm0漏洞、govulncheck无漏洞 |
| 现有候选/原始资料 | PASS，69原始文件及3字节不变候选、OpenAPI/Buf/生成一致性、r02/r03显式检查；r03候选哈希未变 |

新Worktree最初缺reports目录，两条准备命令在日志重定向处未执行；创建目录后完整重跑成功。NOT_RUN原因见evidence/followup01-preparation-retry.json，不能把初次PowerShell进程的退出状态当作npm成功。主线全量检查与W05外置失败分开记录，主线PASS不表示W05通过。

摘要见FOLLOWUP-01-RESULTS.json，命令/输出/测试源码见evidence/followup01-*。没有真实身份/策略存储、数据库/HTTP/WSS/mTLS、审批/执行、Linux race、GitHub CI或1C1G性能验收；G2、D01–D07/P1-C10仍未关闭，App仍为开发骨架。

## 后续与停止

DeepSeek转入新TASK-01C目录，补最小顺序进度纯值；Gemini在原W05目录保留0ebe812并追加W05-R1。范围和停止条件见PARALLEL-FOLLOWUP-02.md。Codex继续原AUTH-01B，不同步/修改其任务或目录。只准备人工可复制任务，不自动启动或发送其他会话消息。完成审查、证据、主线接收和下一步目录交接后停止。
