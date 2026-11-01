# V09 Wave6 · 三线最终审查与交接

> 本文保留上一轮交接快照。三位续作的接收、返修、最新验证和任务以[Wave6-R1审查](V09-WAVE6-R1-REVIEW.md)为准。

审查/集成：codex-review-02；2026-11-01；起始main `73d5dd46fb29b3cf4a445b43b48186d5938fe781`。**接收三位作者的增量代码及部分证据，并补总控组合修复。完整v0.9未完成、未获发布准入；正向成功套件为12 PASS / 6 BLOCKED / 0 FAIL，整体PARTIAL。** 原提交和首次失败证据均保留。

## 接收范围与精确版本

| 来源 | 本轮最终SHA / 结论 |
|---|---|
| Codex / codex-06 | `4a3e31edad46051c2f9de3c9f54a16c843a3f93c`；5个独立增量接收，文件真实通道/AI持久配额/平台适配继续 |
| DeepSeek / deepseek-06 | `fd2e015b6de99349a34f8061bbb39485f119ab7c`；增量接收，文件一致性/完整CA信任/真实发布继续 |
| Gemini / gemini-06 | `9a04c87`、`1e16ba5`、最终`e88e9b034eeafce6680fc61e1f2351b0d8703098`；UI、部分验收及进度材料接收，六个完整成功流程未验收 |
| 初次CX/DS组合 | `de38c5fab036287d9a12f814e06a8b9e44db44ff`；awskr九组隔离验证的代码版本 |
| 新文件候选及兼容验证 | `275544b4e619c14fa4f75123d0ada29669fdf04e`、`eb1c9812ddb7a3c99618ab8f84669337b1f53c30`；可编译但默认未开放File*能力 |
| GM验收门控及真实UI修复 | `e7f15f7e624a8eb7760a356d619a7c7d34a2091d`、`cc36d594efe6478e4e2820c17b1b02ce7d7ed106`；正向浏览器复验使用cc36d59 |
| 最终技术检查点 | `9d3aa2a45bc3d5d45a788e708e1717aab08f83d5`；上传完成授权/失败审计及异步测试修复；最终根检查、安全、70步回归和Linux files补验所用版本 |

后续提交仅本报告、任务/共享说明及证据，不再改变被测业务代码。最终main及三作者工作目录快进结果，记录在主目录忽略文件`reports/v09-wave6-review-handoff.json`和用户交接中。旧错误GM SHA `639eff2`保留为误交历史，不冒充Wave6成果。

## 审查发现与处理

CX/DS初次接收的完整记录见[先行审查快照](V09-WAVE6-REVIEW.md)。总控已经移除默认固定密钥、接入随机持久密钥/Windows私有权限与原子发布；修复撤销后重启、每项文件读写前后授权、ACME公钥/域名/有效期绑定和AI候选YAML错误。主控配置/显式生成/平台注入接缝已接入，未配置的能力继续拒绝。

默认文件服务不再用Controller本机目录冒充远端节点。旧本地字节成功测试保留为显式fixture。FileOpen/Chunk protobuf字段33—36固定工具生成，原13类消息与旧兼容基线保持，新增4类往返且默认双向拒绝；还需真实任务、协商与逐帧校验才能开放。

| 编号 | 实际问题 | 本轮处理与剩余范围 |
|---|---|---|
| GM-R01 | 正向脚本把用户消息入队当AI生成、发行源检查当安装、202当发布完成、preview当灾备；同一已退出Agent被当跨节点文件端点 | 删除这些错误成功判据。已知不可用代码才可判阻塞，400/500/认证或无关503判失败；新增3条判定测试。B01/B04明确HARNESS_INCOMPLETE，其余仍是探针，需要完整流程 |
| GM-R02 | P02仅导航却声称刷新并从me响应恢复CSRF | 改为实际可证明的导航保持会话；完整reload、me响应与写锁恢复仍待GM补验 |
| GM-R03 | 实际任务DTO含jobId，界面显示“任务undefined”；成功执行说明显示在“错误”列 | 截图发现后补真实DTO回归，先FAIL再修复。归一jobId/taskId，列名改“执行说明”，保留实际失败文本；最终截图与真实任务标题断言通过 |
| GM-R04 | 清理脚本目录/PID归属核对不足 | 校验普通用户运行根目录、Windows临时目录父路径、运行前缀及进程命令归属；本轮执行使用--keep，不递归清理测试数据 |
| W6-R05 | 缺真实文件通道时上传完成返回503但缺失败审计；撤销写授权后仍可进入推送 | 新增两条测试先FAIL再PASS；开始及推送前检查当前写授权，缺通道记录有界失败审计。Linux files补验45顶层PASS；Append/GetUpload、逐帧和推送后UNKNOWN另续作 |
| QA-R01 | 旧工作流测试等到标题后立即断言异步错误，整体检查偶发FAIL | 保留相同错误断言，改为等待该错误出现；未删用例、放宽错误要求或增加超时 |

GM的3条新测试加总控真实DTO回归使Web由329增至333。React质量检查覆盖组件/hook顺序、响应世代与旧响应隔离、文本转义及表格可访问性；没有新增npm依赖。页面中的BYOK秘密未回显或存入证据。

## 独立验证与证据边界

| 命令 / 环境 | 实际结果与范围 |
|---|---|
| npm ci；npm run tools:install | PASS；固定工具，无新增npm依赖/根锁变更 |
| npm run check（9d3aa2） | policy/contracts/go/web/git全部PASS；Go **716顶层+1173子测试PASS，18 SKIP，0 FAIL**；Web **38文件、333 PASS** |
| npm run check:security（9d3aa2） | PASS，npm audit 0、govulncheck无已知漏洞；不替代可选Xray工件准入 |
| Node probe-status测试 | **3 PASS**；区分已知依赖阻塞与真正失败 |
| 浏览器Stub套件 | **16/16 PASS**；候选契约fixture，不是完整后端功能 |
| 既有真实Controller浏览器回归（9d3aa2） | 首次69/70暴露上传失败审计缺口；修复后 **70/70 PASS**。包含正常与拒绝/降级回归，缺文件通道503+失败审计不算远端字节成功 |
| 正向成功套件（cc36d59） | Windows真实无头浏览器→WSL Linux Controller+Agent；**12 PASS / 6 BLOCKED / 0 FAIL，PARTIAL，complete=false**。实际HTTPS入网、UI批准/授权、WSS+mTLS和固定自检TaskOffer→TaskFinished；没有伪造预种节点。P02不是完整reload验收 |
| WSL文件候选/传输测试（eb1c981） | Linux amd64 **56顶层PASS / 0 SKIP**；默认File*仍拒绝 |
| awskr九组隔离测试（de38c5f） | 普通用户UID1000，**283顶层PASS、1条Windows专用SKIP**，耗时178.17s；本地TLS供应商协议夹具，不是真实外部账号 |
| awskr最终files补验（9d3aa2） | **45顶层PASS、1条Windows专用SKIP**，0.91s；覆盖新增上传失败审计与当前写授权。与上述43条原files用例重叠，不能直接相加为独立覆盖 |

awskr五个专项为：单次双进程CLI 106.16s；常驻Agent连续心跳/指标及两次固定自检20.13s；随机密钥重启/owner模型配置0.72s；独立Nginx渲染与真实代理0.10s；真实SQLite隔离恢复保持quarantine/撤销0.04s。均使用`/home/admin/.local/share/serverfleet-qa/wave6-review-20261101-cx02-6419888e6b`中新建合成数据、自建进程与loopback端口，没有修改原服务/数据库/站点/防火墙。

九组实机验证的源版本de38c5f早于新File*候选；后续候选由eb1c981的根/WSL检查覆盖，上传修复由9d3aa2的Linux补验覆盖，不能声称整个最终源码重新跑过九组实机测试。最大单子进程RSS487276KiB包括编译，不是总内存峰值或1C1G验收；未做arm64实测或物理双机文件成功传输。

69份原始资料、三份旧OpenAPI候选、Proto旧兼容基线及r03来源不变，固定Buf重生/breaking检查PASS。正式PRIV/WC/PKI-OPS/UPDATE原文缺失与G2/D01—D07/P1-C10仍OPEN，不用本轮实现自行补批。

## 环境问题ENV-W6-01

最终补验时，只读观测awskr原有`serverfleet-agent.service`为`activating/auto-restart`，MainPID=0、退出码1，间隔3秒NRestarts由3060增至3062；Controller/nginx/MySQL保持active。旧先行报告“均active”是当时观察，不能推导当前环境健康。没有读取私钥/环境秘密、重启服务或诊断根因。

本问题与新建普通用户隔离测试分开登记，部署前需总控另行排查并形成具体修复方案；作者不得自行SSH或修改既有服务。本轮没有部署新版本，环境与完整产品均不能标记就绪。

## 后续交接

继续原三个Wave6会话和原分支，执行[三线连续实施包](../sprints/v09-five-day/WAVE6-FOLLOWUP.md)：CX真实文件会话/AI配额审计/平台适配，DS文件一致性/完整CA信任/真实发布，GM六个完整正向流程/会话回归/AC矩阵。共享入口、Proto/gen、DDL及awskr只由总控接收。每2—4小时或可运行增量交SHA后继续独立项，不因一个共享依赖停止整包。

完整v0.9目标及11月3日时限未改变，但真实跨节点文件、AI完整生成、Xray安装、发布及补偿、签名更新、第二Controller灾备六项仍未由正向套件关闭；不得用70步回归或模块PASS替代。三份任务已登记READY，没有自动启动Agent、代发消息或推送远端。

原始输出、首次失败、SKIP、精确版本和逐工件SHA-256见[最终机器结果与证据索引](V09-WAVE6-COMBINED-RESULTS.json)，先行283项实机/兼容证据见[CX/DS索引](V09-WAVE6-REVIEW-RESULTS.json)。压缩文本按base64→gzip解码，归档时逐项往返核验；修复前后任务截图一并保留。测试数据库、私钥和上传二进制不入Git。
