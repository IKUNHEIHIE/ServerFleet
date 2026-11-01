# V09-R6-CX · codex-06 最终交接

审查：codex-review-02。独立工作目录/分支按 WAVE6；起点 73d5dd46fb29b3cf4a445b43b48186d5938fe781，固定被测代码 **c2b2b85d6bbb369484d93460868d9678efba9328**。本次交付为可独立实施的代码增量及共享候选，**不是完整 v0.9 或各子项端到端验收完成**。已到共享/准入/实机依赖边界，证据收口后停止；没有主线合并、启动其他 Agent、发送消息、SSH/awskr/云/外部秘密操作。

## 提交顺序

| SHA | 文件/结果 |
|---|---|
| 6331a5691350adeac907d86a90315af248cbca00 | internal/auth/credential_keyring*、credential_permissions*；随机私有持久密钥、并发发布、重启校验、离线轮换与 owner 元数据；专用 WSL runner；STATUS/共享密钥补丁 |
| 9d63de015a2411df67801da60c71bcd80b1a931e | apps/agent：常驻、有界重连、持续指标、信号取消、持久 UNKNOWN 复用；Linux 私有文件分块、哈希、续写、拒绝覆盖；Linux 启停候选与具体 Proto patch |
| 86a7247b3ecdcc91b37c6f5d21d44f7b39765a9c | internal/ai：BYOK/持久模型配置、owner CAS HTTP、显式生成 HTTP、真实上游 SSE 消费；正常/配额/撤权/断流测试；Agent worker UNKNOWN 包装修复；组合主控补丁 |
| c2b2b85d6bbb369484d93460868d9678efba9328 | internal/updates：签名目录源/实际 SlotInstaller 与 HTTP 接缝；internal/recovery：新目录隔离 SQLite 恢复执行；auth 恢复身份失效变换；plugins Xray 统一任务 Host；负向/真实 SQLite 测试与平台/API补丁 |

所有源码只在 apps/agent、internal/auth/ai/plugins/updates/recovery；测试只在 tests/integration/v09/platform、tests/poc/v09/platform；文档只在本 wave6 progress。共享 apps/controller、API/Proto/gen、根锁/迁移/CI/其他作者目录无语义改动。最终证据文件 EVIDENCE.json 含固定代码 SHA、运行二进制/日志 SHA256、SKIP 完整名单和限制。

## 实际验证

工具：固定 Go1.27.3、Node24.22.1、npm11。合成身份/新临时目录/回环 TLS/本机已有 WSL Ubuntu 非 root UID1000。

| 命令 | 最终输出/含义 |
|---|---|
| npm ci；npm run tools:install | PASS；未改依赖或锁；npm 提示 buf postinstall 尚无 allowScripts 记录，后续固定 Buf 各项实际运行成功 |
| npm run check | PASS（固定 c2b2b85）；原始69文件/3候选、模块边界、契约/固定生成、build/test/vet/staticcheck、Web/构建、Git检查 |
| npm run check:security | PASS（固定 c2b2b85）；npm 0漏洞、govulncheck No vulnerabilities found；不覆盖未准入 Xray 发行物安全 |
| 根 Go JSON | 645 顶层 + 1138 子测试 PASS，0 FAIL；15 SKIP 单列，不计 PASS |
| Web | 37文件 / 329测试 PASS；Vite 大块体积警告保留，无前端源码改动 |
| node tests/poc/v09/platform/run-wave6-linux.mjs auth | 71 PASS / 0 SKIP，含私有权限、并发创建、重启/轮换和恢复身份失效 |
| 同 runner agent / ai | 38 / 33 PASS，各0 SKIP；Agent 现有会话/取消/对账、文件字节及有界重连；AI 真实回环TLS/SSE、正常和负向 |
| 同 runner plugins / updates / recovery / restore | 12 / 14 / 5 / 1 PASS，各0 SKIP；restore 为真实 SQLite 隔离复制/哈希/台账验证专项 |
| GOOS=linux GOARCH=amd64/arm64 go build ./apps/agent | 两架构编译 PASS；只有 amd64 WSL 有执行证据，arm64 UNTESTED |
| git apply --check 四份补丁 | controller-ai-keyring、controller-platform、agent-file-proto、ai-openapi 均 PASS |
| Go overlay 对两份主控补丁组合编译 | PASS；不执行新配置，不能等同主控接线/运行验收 |

WSL 七组共174顶层PASS，和根Windows结果是不同平台复验，不能重复相加宣称产品覆盖。根15跳过包括 Linux权限/文件/恢复（上述专项补验）、Windows符号链接、真实 Cloudflare、3项真实Xray和Linux CLI闭环。没有重跑70步浏览器：本次没有改主线入口或前端，旧负向通过不能替代新增功能成功。

原始输出保存在本目录工作树的忽略 reports：wave6-final-check.log、wave6-final-security.log、go-tests.log、web-junit.xml、wave6-linux/*.json / *-native.log。EVIDENCE.json 保存摘要和哈希，实机二进制也只在忽略 reports/wave6-linux。

首次失败如实保留：Windows文件目录同步不支持（改为明确Linux能力）；恢复夹具缺迁移台账（修夹具，未弱化校验）；overlay临时.go被安全扫描当独立包（改.txt并重跑全套）；补丁CRLF上下文（统一LF，适用性复验）。详见 STATUS/EVIDENCE。

## 需要总控接收的具体差异与剩余阻塞

1. **密钥与AI**：controller-ai-keyring.patch 取代 controller-keyring.patch；两个 proposed.txt 是完整拟议文件。新隔离 Linux 实例先创建当前用户0700密钥目录并配置 CredentialKeyDir；旧初始化库缺钥必须停止，不能向 awskr 旧库自动生成/迁移。Windows ACL 未准入。配额适配必须持久绑定唯一生成操作，补 typed model audit sink；模型所有者仅借自己的当前凭据。ai-openapi.patch 新增独立候选，三原件保持不变；真实 Cookie `__Host-sf_session` 与旧候选 sf_session 差异待总控登记。外部供应商与浏览器逐token展示仍 UNTESTED/未实现，不使用上游流式测试替代。
2. **Agent/DS文件**：agent-file-proto.patch 只是提议 oneof33/34，不是已冻结字段号。按 AGENT-LINUX.md，双方协商 capability+minor、接源读/目标写分别授权和任务ID/摘要、更新校验与固定生成。当前文件组件未接 CLI/WSS，不能称两端互传；空间不足/真实断线/两机恢复、正常运维动作 schema/真实降权沙箱仍待接收；任意Shell/root继续关闭。
3. **Xray**：controller-platform.patch 可注入已准入 Runtime/TaskHost，默认仍拒绝。Host测试仅证明白名单 mutation 提交统一任务及当前门控，不是安装/用户/流量成功。两份历史候选工件安全FAIL、运行时/Host ABI/隔离及双架构仍BLOCKED；安装预演/配置验证/材料导出需要准入适配。没有自行选WASM/Eino、重编官方包或在核心导入可选业务。
4. **更新**：controller-platform.patch 注入 Check/Plan/Apply。DirectoryReleaseSource+TaskInstallExecutor 已能将验证工件装入私有slot；计划解析器须从唯一任务/审批和真实备份解析，提供实际 Stop/Start/进程身份/健康/兼容/原子审计。测试服务钩子是double；job查询/回滚要接统一任务视图，不能把历史receipt当当前健康。缺正式UPDATE准入/真实supervisor时不开放。
5. **恢复**：RecoveryExecute 只能入队统一任务；worker可调 RestoreIsolated，再由总控/DS在同一事务应用任务对账与 InvalidateRestoredIdentity、最新撤销和审计。外部权威/旧主控fence、新密钥/epoch、release及各入口latch.Check均未完成真实环境验收；当前复制成功只得到 restored_quarantined，不能调度或复活旧凭据。无需也不能在作者目录越权实现总控/DS接线。

总控可按固定SHA接收上述增量，先做新私有目录下的配置/组合负向，再由总控自行在awskr按隔离授权测试。G2、D01–D07/P1-C10、完整AC及发布准入仍未关闭。本任务不继续扩展其他模块或部署。
