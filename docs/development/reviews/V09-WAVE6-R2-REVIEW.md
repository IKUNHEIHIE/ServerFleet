# Wave6-R2 · 三线 -03 审查与共享接收

审查/集成 codex-review-02；2026-11-02；起点 main `c9013f4231c5f0a586eb0af26386f6e265996c0c`。**三位增量经必要返修、正式共享组合和复验后接收；实际发布执行器与供应商 cleanup 尚未获准默认启用。完整 v0.9 未完成：正向浏览器 21 PASS / 5 BLOCKED / 0 FAIL，严格验收退出 1。**

## 来源与被测版本

| 来源 | 精确提交与范围 |
|---|---|
| CX-03 / codex-06 | HEAD `26f7cf6d247183f50b9c16d1beee4616cc4b7b98`，作者被测代码 `22bbe91799ab11fbf0a0035296055a110eae4c40`；HTTP/tasks/WSS 双 Agent 多文件 worker、持久 binding/取消/重启 UNKNOWN、AI 预算 HTTP、实际进程更新和恢复运行门 |
| DS-03 / deepseek-06 | HEAD `3efebe862add104975d6b15d322eaf78b55ab40e`，代码 cde4a21/3c3c4ef；真实发布候选桥、typed Nginx 能力、供应商秘密回调、cleanup 持久记录/HTTP 候选 |
| GM-03 / gemini-06 | d6e58d9 → 4135e31 → 8efa9ec → HEAD `b37636cc61da9479cbe2f099a936ae97b81fcbe6`；独立 AI TLS 验收宿主、真实生成/模型 CAS、异步单测稳定与 Operator 403 原始证据 |
| 总控组合/返修 | `95adeb53174b54172dcff372ef568c6c3440c477`：正式中央补丁、24 项迁移、候选/权限登记、发布副作用前置检查、同名 nodeID 和宿主认证修复 |
| 最终技术版本 | `23e56d55098a4fa7655ac4c1c03874384b64de5a`：仅补删已废弃的 fixture 类型及改正注释；根/安全、13 组 Linux、浏览器均在此代码验证。后续仅本轮文档/证据 |

联合 Linux 6 组在干净 95adeb5 执行；之后 23e56d5 没改后端/Agent，二进制摘要与版本区别记录在机器结果。保留原作者历史和首次失败，不改写提交。最终 main/三位原分支 SHA 与干净状态写入主目录忽略文件 `reports/v09-wave6-r2-review-handoff.json`。

## 发现与处理

| 编号 | 发现、修正与状态 |
|---|---|
| R2-01 | **发布适配提前副作用/假终态**：gatewayTaskHost 在 Submit/授权/审批前上传工件，同时 node_ids 附 TargetResourceID 导致任务输入无效；排队 taskID 被当发布成功，remove 排队就可能删除 route。用原文件 overlay + 真实 SQLite/tasks/审计复现两种零授权/待审批场景均发生上传。改为先统一提交、当前执行检查再允许上传；排队/待审批不能成功，终态需 completed 且各节点 succeeded。修正后回归通过。完整工件 worker/Agent 注册仍 OPEN，不默认启用生产桥 |
| R2-02 | **两个 enrollment-token 同名目标混淆**：旧通用 checkbox 选择器取最后一个同名标签，而 Operator 仅有 node1 权限。新增逆序同名 UI 回归先 FAIL，再以稳定 nodeID 的 value/可访问名 PASS；正向 P13 显式选择 nodeID，并读取真实任务 targetNodeIdsSnapshot 后再进行 Operator 审批，实际成功进入 Agent 拒绝的预期失败终态。未放宽授权。该目标混淆有确定回归覆盖；未声称所有未来 403 都由同一原因造成 |
| R2-03 | **测试宿主认证旁路**：原无认证明文 GET /fixture/configure 接受 owner/profile/secret，按传入 owner 作为 actor 配置预算。测试用途也不能视为已认证。删除该路由，遥测改为强制 loopback/只读；合成上游密钥通过私有 0600 文件启动注入，浏览器预算配置走正式带 Cookie/Origin/CSRF 的 PUT。B02 实际调用 TLS 上游一次、验证 bearer/model 并出现真实 assistant 终态 |
| R2-04 | **共享组合已接收**：应用全部五份 CX 补丁，注册三个新迁移和三份 R7 候选，cleanup HTTP 走实际会话且四动作管理员限定。新的 files/AI/update/recovery 集成用例解除候选 build tag；combined runner 改用正式源码/迁移，不再先生成或应用 overlay |
| R2-05 | 初次综合检查 staticcheck U1000：旧 configureRequest 已无调用；删除后完整根/安全复验通过。不是删测试或放宽 lint |
| R2-06 | **OPEN，阻止真实 cleanup 启用**：execute/setStateAt 尚无原子持久执行 claim 和 attempt 预留，cancel 可能与正在进行的 provider I/O 终态竞争；单线程限次测试不能证明并发安全。当前 cleaner 默认 nil，retry unavailable，未启自动 RetryDue；DS-04 必须补 claim/取消/重启/当前授权负向后再接实际供应商 |
| R2-07 | **OPEN，阻止默认生产发布**：需要真实 typed worker/工件/注册/等待终态/逆序补偿完整接线；Agent 私有根 owner/权限及回滚失败 UNKNOWN 也待补验。独立 Nginx 实验通过不能代替统一发布成功 |

## 正式接收的接口与迁移

Controller 迁移总数 **24**：原 21 项加 cx/0008_file_execution_bindings、cx/0009_update_journal、ds/0062_acme_cleanup。已有 19→24、21→24 和重复应用检查，旧 checksum 不变。作者只组合 CX 的 overlay 为 23 项，不是正式数量。

AI owner 预算 GET/管理员 CAS PUT `/api/v1/ai/model-settings/{profileId}/budget`、更新 job 真实任务状态与读路由、恢复运行期门及 cleanup 管理员路由已组合。路径 mapper/spool、更新可信来源/installed provenance、恢复权威等仍为显式程序配置；默认 CLI 并未自动提供产品部署装配。

根 lint 已接三份 `api/openapi/revisions/v09-r7/*-candidate.yaml`。旧更新 `jobId/state/toVersion` 与实现 `id/taskId/status/targetVersion` 的差异显式登记。69 原文、三份旧 OpenAPI、Proto/gen/兼容基线与 r03 未改；不宣称 G2 冻结或旧 DTO 全兼容。详细消费方式及停止使用旧 prepare/patch 的说明见[共享接收](../sprints/v09-five-day/WAVE6-R2-SHARED.md)。

## 独立验证结果

| 检查 | 结果与边界 |
|---|---|
| npm ci；npm run tools:install | PASS；首次外部 Go 代理失败保留，重用本机相同固定版本的模块缓存后安装 PASS，未改依赖/锁 |
| npm run check | policy/contracts/go/web/git 全 PASS；Go **796 顶层 + 1199 子测试 PASS，23 顶层 SKIP，0 FAIL**；40 包；Web **341 PASS** |
| npm run check:security | PASS；npm 0 漏洞，govulncheck 无已知漏洞；另以 Linux amd64 构建条件 govulncheck PASS |
| 正式组合原生 Linux | **6 顶层 PASS / 0 SKIP**，UID1000，无 overlay：实际 HTTPS Controller + 两个常驻 Agent、多文件 99,000/48,000/0 字节/SHA、六个唯一子执行/父终态、skip 与拒绝 overwrite、实际 HTTP cancel/撤权、持久化失败、已写后丢回执/Controller 重启与 Agent 重连不重写；并含预算、更新/恢复运行门与 SQLite restore 专项。旧 fence/new authority 仍含明确 double |
| 13 组原生 Linux | **364 顶层 PASS / 4 SKIP**：Agent50、transport58、AI34、budget1、Controller1、updates17、recovery9、restore1、files60、DNS25、notifications31、certificates34、publishing43。两个仅由父用例调用的子进程 helper、Windows 专用路径、Linux 未装 Nginx 的独立用例跳过均保留；真实 Nginx 另在 Windows 普通用户隔离目录通过 |
| 实际 Nginx | 私有 prefix/高位 loopback，现有便携 nginx；`NGINX_BIN=… go test ./apps/agent -run TestGatewayCapabilityRealNginxIsolated -count=1 -v` PASS，真实配置验证/reload/代理/幂等/无效配置恢复；未触碰系统实例或 80/443 |
| 真实 Controller 浏览器回归 | **70/70 PASS**；含预期拒绝/失败关闭，不把拒绝测试当功能成功 |
| 契约桩浏览器 | 最终 **16/16 PASS**。首次 15/16，503 步截图为静态 “not found”，与并行根检查重建 dist 时间重叠；保留原日志/图。构建结束后未改任何断言/代码单独复验通过；该环境竞争解释不冒称产品故障已定位 |
| 正向浏览器 --ai-host --require-complete | **21 PASS / 5 BLOCKED / 0 FAIL**，PARTIAL，实际退出 **1**；B02 真实 TLS assistant 新通过，P13 scoped Operator 正常；同 WSL 双进程不是物理双机 |

Windows、Linux 和联合测试存在重叠，不能相加宣传独立覆盖；联合套件的 restore 与 13 组 restore 也重叠。跳过名称、二进制摘要、所有原始输出与版本矩阵见[机器结果及 57 项证据](V09-WAVE6-R2-RESULTS.json)。base64(gzip) 工件均解码往返/SHA-256 验证，不提交测试数据库/私钥/可执行二进制。源代理、PowerShell 参数/缓存设置及最初回归夹具输入错误日志也保留，与代码实际 FAIL 区分。

## 剩余范围与续派

五个正向阻塞分别是：**B01 文件页面真实闭环宿主未配置，B03 Xray 安全工件/Runtime，B04 实际发布/补偿，B05 正式产品更新来源/装配，B06 第二主控与真实 fence/新权威恢复。** 后端已通过的文件联合测试不等于页面与全部策略已完成；真实子进程更新和恢复门也不等于正式产品更新/灾备验收。

本轮未使用 SSH、真实外部秘密或云 API，未重新观察/修改 awskr 原服务；ENV-W6-01 沿用上轮未关闭，不声称当前远端进程状态。ARM 运行、硬 1C1G、物理双机和真实外部供应商验收未完成。正式 PRIV/WC/PKI-OPS/UPDATE 原文缺项、D01—D07/P1-C10/G2 和 Xray 历史工件安全问题保持未关闭，11 月 3 日全量交付有实质风险。

下一轮[三线连续包](../sprints/v09-five-day/WAVE6-R3.md)：CX-04 完成可信文件/更新部署装配及真实恢复/平台独立项；DS-04 先补实际发布执行器和 cleanup 并发，再接供应商与网关安全；GM-04 首先完成 B01 和 AI 预算界面，再扩完整发布/更新/灾备验收。原三会话各自连续推进整包，不自动启动或代发消息。
