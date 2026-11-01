# V09 Wave6 · CX/DS增量审查与主控接线

审查/集成：codex-review-02；日期2026-11-01；起点`73d5dd46fb29b3cf4a445b43b48186d5938fe781`。**接收CX/DS模块增量及本轮集成修复；Gemini待新SHA，完整v0.9未验收、未获发布准入。** 原作者提交与失败证据保留，没有改写历史或改动作者未提交文件。

## 精确范围

| 项目 | SHA / 结论 |
|---|---|
| CX | `6331a56`→`9d63de0`→`86a7247`→`c2b2b85`→`4a3e31edad46051c2f9de3c9f54a16c843a3f93c`；增量接收，不能称全部端到端完成 |
| DS | 最终`fd2e015b6de99349a34f8061bbb39485f119ab7c`；增量接收，真实文件/供应商/发布组合仍有缺口 |
| GM | 所给`639eff20526a12acee8900abcac62a5d4376b732`已在旧main；用户确认任务中断后继续，新SHA未交；不合并旧提交、不移动其活动目录 |
| 业务接线与修复 | `de38c5fab036287d9a12f814e06a8b9e44db44ff`；awskr全部本轮测试所用源代码 |
| 文件编译候选 | `275544b4e619c14fa4f75123d0ada29669fdf04e`；新增33—36和生成代码，未开放默认能力 |
| 最终技术检查点 | `eb1c9812ddb7a3c99618ab8f84669337b1f53c30`；根组合/安全及WSL传输回归所用SHA；之后仅本报告/任务板/证据 |

共享配置、候选兼容性及保守默认见[WAVE6-SHARED](../sprints/v09-five-day/WAVE6-SHARED.md)。CX/DS可在接收最终main后执行[连续续作包](../sprints/v09-five-day/WAVE6-FOLLOWUP.md)，无需等GM整包完成。

## 审查发现与修复

| 编号 | 可复现问题 | 本轮结果 |
|---|---|---|
| W6-R01 | 凭据撤销合法擦除密文后，新持久密钥服务将其当损坏数据，导致Controller重启失败 | 新增撤销→重启→仍拒绝借用测试，修复；Linux awskr PASS |
| W6-R02 | 文件第一项完成后撤权，协调器仍写第二项 | 新增真实协调器回归，补每项读前/写前/写后授权；PASS。分块中撤权仍须后续真实Agent门控 |
| W6-R03 | ACME客户端把其他公钥、其他域名、过期证书记为签发成功 | 三个负向先FAIL，补CSR公钥/域名/有效期/非CA叶子绑定；PASS。完整信任链另列后续任务 |
| W6-R04 | 新AI候选3个flow-map描述含未引用逗号，Redocly报6个struct错误 | 引用描述后纳入根门禁PASS，未放宽规则 |

主控默认固定密钥已移除，默认数据库旁保存随机持久密钥；Linux owner/mode、Windows owner/DACL、重启沿用、轮换、缺钥不替换均有测试。模型owner CAS与显式生成路由、AI当前操作借用门及更新/恢复/Xray/通知注入端口已组合；配额未配置仍拒绝，未假接真实供应商/Runtime。

默认文件传输不再把Controller本机暂存目录当作远端节点。旧字节成功集成测试保留为显式注入LocalStagingTransport的fixture；真实WSS FileOpen/Chunk尚未接通，不能用该fixture关闭跨机验收。

## 独立验证

| 验证 | 实际结果 |
|---|---|
| npm ci；npm run tools:install | PASS；固定工具，无新npm依赖/锁变更 |
| npm run check | 最终eb1c981 PASS：policy/contracts/go/web/git；Go **714顶层+1173子测试PASS，18 SKIP，0 FAIL**；Web **329 PASS** |
| npm run check:security | 最终eb1c981 PASS：npm audit 0、govulncheck无已知漏洞；不代表Xray候选工件通过 |
| 原始资料/兼容 | 69原文、三候选、Proto兼容基线、r03 provenance不变；固定Buf重生一致、breaking检查PASS |
| 新文件消息默认关闭 | 四种候选往返；默认Validator在两个方向均拒绝，原13个允许消息保留；WSL Linux amd64 **56顶层PASS/0 SKIP**，SHA eb1c981 |
| awskr Linux amd64 | 普通用户UID1000、独立私有目录/临时loopback端口，9组 **283顶层PASS**；Windows专用名称用例1 SKIP，不混计PASS |

awskr源SHA de38c5f，测试总耗时178.17s；新File*候选/生成代码在后续275544b加入，默认继续拒绝，最终eb1c981另有根检查与WSL实测。de38c5f→eb1c981的apps/internal业务运行时代码没有变化；差异是生成候选、测试/契约和文档，不把旧实机结果声称为新文件通道成功。

awskr的5个专项：旧单次CLI完整闭环106.16s；**常驻Agent持续心跳/指标并连续完成两次固定自检20.13s**；主控随机密钥/重启/owner模型HTTPS配置0.72s；**独立Nginx进程真实代理0.10s**；真实SQLite隔离恢复保持quarantine及撤销0.04s。其余8组为auth、agent、files、dns、notifications、gateway、publishing、certificates的模块测试；供应商使用本地TLS协议端，不是真实外部账号。

测试目录`/home/admin/.local/share/serverfleet-qa/wave6-review-20261101-cx02-6419888e6b`，只使用新建合成数据与自建测试进程。既有Controller、Agent、nginx、MySQL前后均active，没有重启/改配置/改库/改防火墙；没有云API、新主机或真实外部秘密。最大单个子进程RSS487276KiB包含编译，**不是整组总内存峰值或1C1G验收**。未做arm64实测或物理双机文件传输。

## 失败保留及后续边界

- 三项业务回归的初始FAIL、OpenAPI格式FAIL均保留。首次WSL调用参数被Windows拆分、测试尚未执行；引用参数后得到真实撤销重启FAIL，再修复，不把调用错误算通过。
- 新增Envelope候选先触发旧“仅13类/只能格式化”的库存断言。未改兼容基线；保留P1基线与原文精确比较，另加当前前缀只允许4项声明差异、17类往返和4类默认拒绝。最终全部PASS，没有移除负向或忽略失败。
- 尚待：实际文件通道与任务绑定/路径映射/冲突/逐帧撤权和断线去重；AI持久配额/typed审计/浏览器接入；真实发布与供应商借用；已准入Xray Runtime/安全工件；更新真实supervisor与恢复fence/释放；GM正向浏览器和AC-01—24全层证据。G2及缺失正式ADR不假关闭，11月3日完整交付目标仍有风险。
- 原始输出/哈希/平台和SKIP名单见[机器结果与证据索引](V09-WAVE6-REVIEW-RESULTS.json)。压缩工件全部往返核验；二进制/测试数据库/私钥不入Git。最终main SHA及CX/DS干净目录快进回执在主目录忽略文件reports/v09-wave6-review-handoff.json和用户交接中。
