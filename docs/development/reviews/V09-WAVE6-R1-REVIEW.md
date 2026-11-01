# Wave6-R1 · 三线续作审查与接收

审查/集成codex-review-02；2026-11-01；起点main `379d5e0e4818844eb44d8ac13010f251d215d8c0`。**三位增量经审查、必要返修与共享组合后接收。完整v0.9仍未完成，正向套件20 PASS / 6 BLOCKED / 0 FAIL，PARTIAL；完整验收开关正确退出1。** 不推送远端，不自动启动Agent或代发消息。

## 来源与被测版本

| 来源 | 精确提交 / 接收范围 |
|---|---|
| CX-02 / codex-06 | `d58020c`、`8bbe64e`、`8336e56`、HEAD `17017a6a1aba0e9af5c03fdbb89894bc52496be8`；真实WSS文件会话与私有读写、持久预算/typed审计、本地TLS、自己拥有的更新子进程/回滚与恢复新代次约束 |
| DS-02 / deepseek-06 | 代码`bf3ec59`、HEAD `361098283b3ed0e987c70e2292839acf42d6b341`；文件帧/当前授权/原子claim/有界spool、完整CA信任与callback供应商、发布目标/补偿增量 |
| GM-02 / gemini-06 | `fa112f2`→`7d52dbb`→`b7dae0a`→HEAD `27c92be56b40cab725cca8a260c2ebe67fef078b`；模型设置/显式生成界面、真实reload/失败任务/撤权及双Agent验收结构 |
| 主控返修 | `c4a95ec` ACME清理与AI冲突状态；`39305ca807f865836dc7193d99c4d520140e4301`共享组合/迁移/验收门控；`1c34f6d202247481f81f3264f1ca912347d83c5b`Linux私有spool夹具 |
| 最终技术代码 | `9ddd5c64a5887e4fb05a85fb362fd2c71e6bcea9`；上传owner写权限修复；最终根/安全、13组Linux及70步回归均在此代码验证。后续仅文档/证据 |

原作者提交与第一次失败日志均保留。最终main及三位原分支的干净快进结果记录在主目录忽略文件`reports/v09-wave6-r1-review-handoff.json`和用户交接中，不改写提交历史。

## 主要发现与返修

| 编号 | 问题与实际结果 |
|---|---|
| R1-01 | ACME仅成功签发后处理cleanup错误，challenge/finalize等失败路径吞错误并丢record IDs。新两条子用例先FAIL再PASS；改为所有退出路径清理一次，同时保留签发/清理错误及记录ID，不交出未验证证书。持久清理队列另续作 |
| R1-02 | AI设置页所有保存失败都显示409；真实冲突读到新revision后key重建又丢提示。新两个UI用例先FAIL再PASS；仅真实409进入冲突状态，提示移至revision外层，身份世代仍隔离 |
| R1-03 | DS新的私有spool检查正确拒绝Linux测试夹具0755，首次15个协调器/任务测试FAIL。只将新建测试目录明确0700，生产安全检查保留；Linux完整files重新通过 |
| R1-04 | GM再次把403/500、文件失败和60s超时判BLOCKED；恢复预演读错status字段；真实夹具路径与请求路径不符、请求不支持的overwrite，且未配置Agent file-root。收紧精确状态/代码，恢复用passed布尔值，错误shape FAIL；修正路径/skip，默认B01明确HARNESS_INCOMPLETE，配置完整宿主后--attempt-file-flow才尝试并要求终态/字节/SHA。B03/B05仍是探针，不能称完整流程 |
| R1-05 | 新GetUpload读权限预检查使只有files.write的合法上传者PUT内容就403，70步回归首次68/70（上传与后续审计失败）。新HTTP测试先实际403再PASS；上传状态属于owner绑定写流程，开始/content/complete仍检查当前files.write。下载未获read仍拒绝，撤销write后状态拒绝；重跑70/70，缺通道503+失败审计保留 |
| OPEN-GM-403 | 作者报告Operator间歇性任务403。主控URL-safe ID/精确授权/撤销向量及两个实际Operator浏览器流程通过，未复现、未证明根因；不升为管理员解决，保留追踪。持续403必须FAIL |

编写新回归时的两次测试API返回值编译错误也保留，修正测试后另取得实际业务FAIL/PASS，不把编译错误当业务复现。首次固定工具安装因离线缓存缺项失败，保留后改用正常下载源安装成功，未改固定版本或根锁。

## 已完成的共享装配

- 接收CX文件client/bridge配置patch，只有显式trusted gate才协商minor1与能力，默认CLI仍关闭；**尚无统一文件worker/显式路径映射**，DS协调器与CX单文件TaskOffer的接受顺序/多item生命周期仍需统一。
- `Config.LocalAIProvider`已可显式注入loopback TLS协议端；默认预算由`Server.AIBudget`持久化，缺策略拒绝，当前管理员/owner/启用profile校验不信任浏览器自报。暂未提供预算HTTP管理，不能说浏览器AI生成已通过。
- 登记新不可变迁移`cx/0007_ai_budgets_and_model_audit`和`ds/0081_publishing_targets`，19→21；升级及二次Apply保留旧发布记录/迁移历史。预算reservation/typed及通用审计同事务，模型typed审计默认写入，不再需要旧patch的AITypedAudit开关。
- CX主控HTTP/TLS测试已去overlay/build tag/手工建表，使用正式server.New、正式迁移及默认持久预算，实测一次真实供应商HTTP/SSE成功、后续预算拒绝无额外HTTP、typed审计失败回滚和owner/profile拒绝。
- 供应商callback秘密协议、CA信任根/authorization/origin校验与真实发布目标持久已接收；默认真实供应商借用组合、生产发布器及Agent Nginx动作/审批仍未补齐。没有使用真实外部账号。

## 最终独立验证

| 检查 | 结果与实际范围 |
|---|---|
| npm ci；npm run tools:install | PASS；固定版本，依赖/锁未变；第一次离线缓存失败保留 |
| npm run check（9ddd5c6） | policy/contracts/go/web/git全部PASS；Go **753顶层+1186子测试PASS，20 SKIP，0 FAIL**；Web **38文件、340 PASS** |
| npm run check:security（9ddd5c6） | PASS，npm 0漏洞、govulncheck无已知漏洞；另以Linux amd64构建条件govulncheck PASS |
| 13组WSL原生Linux（9ddd5c6） | UID1000、合成数据/私有目录；**331顶层PASS / 3 SKIP**。Agent42、transport58、AI34、budget1、主控HTTPS/TLS1、updates15、recovery5、隔离SQLite restore1、files58、DNS25、notifications31、certificates28、publishing32。两个helper由父用例实际子进程执行，另一个Windows专用路径用例SKIP |
| 浏览器既有真实Controller回归（9ddd5c6） | 修复后 **70/70 PASS**；之前68/70失败与截图保留。正确拒绝/降级不算远端字节、发布成功或外部能力完成 |
| 浏览器契约桩 | **16/16 PASS**；只算候选fixture |
| 正向浏览器 | source39305ca **20 PASS / 6 BLOCKED / 0 FAIL**；实际reload+me/CSRF、Operator异人审批→Agent拒绝→失败页面、撤权403不清认证、两实际Agent在线、模型设置CAS。双进程同WSL，不是物理双机 |
| 严格正向开关 | source1c34f6d再次 **20/6/0**，`--require-complete`实际退出**1**，正确阻止全量通过。之后9ddd5c6只修上传状态授权，无正向脚本/UI/Agent/AI代码变化，最终上传另由70步覆盖 |
| 验收判定单测 | **4 PASS**，含500/认证/未知错误拒绝与实际recovery passed布尔DTO/空形状检查 |
| 原始资料/契约 | 69原文、三份旧候选、Proto及生成代码、旧兼容基线/r03不改；原根契约/固定生成与breaking门禁PASS。发布新目标字段仍须独立候选说明，不偷偷修改旧OpenAPI |

Windows与Linux数量存在重叠，不相加为独立覆盖。Linux8个平台组中budget/Controller/restore分别只选择指定测试；未在当前SHA重跑需要WSL native Go的独立CLI专项。没有ARM运行、硬1C1G、物理双机文件、真实外部供应商或生产部署证据。Xray历史安全FAIL没有被测试替代。

## 环境与未完成项

awskr仅只读复查原有Agent：User=root、auto-restart、MainPID0、退出码1、NRestarts10461。ENV-W6-01仍OPEN，根因未诊断，既有服务/站点/数据库/防火墙未改；本轮Linux执行在本机WSL隔离区，没有新增awskr部署。

六个完整正向流程仍未关闭：跨节点文件、AI浏览器生成、Xray安装/业务、真实发布/补偿、签名更新及真实健康回滚、第二Controller灾备及fence/撤销不复活。已有本地TLS主控生成成功与真实子进程更新测试不替代整项产品/发布验收。G2/D01—D07/P1-C10与正式PRIV/WC/PKI-OPS/UPDATE资料缺口保留，11月3日完整交付目标仍有风险。

## 下一轮

见[共享接收](../sprints/v09-five-day/WAVE6-R1-SHARED.md)及[三线连续包](../sprints/v09-five-day/WAVE6-R2.md)：CX-03接管完整文件执行路径和AI预算/平台独立项，DS-03负责实际发布/网关/供应商/清理持久化，GM-03可写测试专用Go宿主，先补不依赖新HTTP的真实AI浏览器成功。原三个会话/分支继续，每个可运行增量交SHA后继续独立项。

原始日志、第一次失败、SKIP、Linux二进制哈希、严格退出码及截图见[机器结果与59项证据索引](V09-WAVE6-R1-RESULTS.json)。压缩工件往返和SHA-256均已验证；测试数据库、私钥、二进制不入Git。
