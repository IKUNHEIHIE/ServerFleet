# 增量交接 / codex-02

## 01 / CX-01

SHA `2fc8dab314a906c0e48543d4735ee5509d08e355`。仅三份候选/进度 Markdown。`git diff --check` PASS。没有运行时接口、依赖、公共 schema 或生成变更。审查入口 CHECKPOINT-01.md、SHARED-CHANGES.md。

## 02 / CX-02 认证组件

SHA `8ad55cc3a81c702f124a4ee8f2084709157ec7a7`。

文件 internal/auth/identity.go、browser_guard.go、identity_test.go。实现受控 setup/单用初始化、PBKDF2-SHA256（随机32字节盐、600000轮，候选参数）、身份与秘密管理ID分离、8小时绝对/30分钟空闲期限、每请求来源查询、双维度持久限速、CSRF/Origin/Cookie约束、单会话撤销、改密epoch全撤、提交前世代闸门、时钟回拨拒绝、事务审计出箱。

实际命令及输出：

- 固定本地 Node `npm ci`：PASS，added 127 packages in 17s；npm提示buf安装脚本未单独批准，未改allowScripts。
- 固定 Go `go test ./internal/auth -count=1`：PASS 9.578s（首轮）。
- `go test ./internal/auth ./tests/architecture -count=1`：PASS auth 17.392s / architecture 2.051s（补充过期回拨/结构校验后）。
- Context7 resolve Go→/golang/go/go1.27.3，查询KDF/HTTP/AEAD；本地go doc核对PBKDF2.Key/CookiesNamed等确切签名。

复现：使用任务目录 .tools/go/bin/go.exe，GOCACHE=.cache/go-build，GOPATH=.cache/gopath。测试仓储只位于 *_test.go；新建service复用测试状态验证撤销不复活，不等同SQLite进程重启或真实浏览器验收。调用顺序：NewIdentityService(经审持久IdentityStore) → 本机PrepareSetup → Bootstrap → Login → BrowserGuard.Check/SessionCredential → Authenticate → CommitGate。PrepareSetup不得提供远程路由，LoginSession.Secret禁止JSON输出。

候选端口新增在auth自有文件（未改ports.go）：IdentityStore、IdentityService、Identity、BrowserGuard/Cookie helper；尚无跨owner生产消费。请总控审查这些精确文件后与DS适配事务、审计出箱及HTTP契约。无schema编号/锁/生成变更；没有默认管理员节点授权，Bearer保持关闭。缺完整PRIV/MFA政策、受控安装交付、持久事务/审计出箱消费、配置与真实HTTP/浏览器验收，**CX-02未完成**。16MiB状态/10000会话与审计出箱为有界候选，性能未测；出箱满拒绝，不能长期运行而不接审计消费。Tgate后网络输出窗口仍存在。

## 03 / CX-03、CX-04 mTLS与本地执行保留记录

SHA `96b0568297bba7efc3ee2eb74f4c1a04758eec6c`。

文件 internal/transport/peer_identity{,_test}.go、apps/agent/executor{,_test}.go。mTLS TLS1.3标准CA/EKU/域名验证+证书指纹/serial→当前身份映射；事件/派发需重复检查吊销；消息nodeId不能替代证书。Agent白名单回调仅程序注册，限制参数/权限/期限/路径，副作用前持久保留executionId、绑定完整Offer；并发/重启重复不重跑、无完成回执UNKNOWN、输出有界。root/Shell/插件能力未获准入，均拒绝。

实际命令：`go test ./apps/agent ./internal/transport ./tests/architecture -count=1` PASS：2.961s / 3.936s / 1.533s。`go vet ./internal/auth ./internal/transport` PASS。tools:install PASS（四个固定工具安装到本目录bin）。Context7查询Go os.Root，使用固定工具API。

中间失败如实记录：mTLS首轮TestPeerRealLocalTLSAndLiveRevocation FAIL，VerifyConnection在HandshakeComplete前运行；改为回调内部验证CA结果，公开事件Verify仍强制完成握手，重跑PASS。Agent测试首轮编译FAIL（测试err变量作用域），修复后PASS。

真实证据仅为本机回环TCP mTLS握手和本地文件副作用；非WSS/完整注册、Linux进程、双架构或生产链路。Windows不支持本测试所需目录fsync语义，仅验证逻辑去重；Linux代码强制同步文件和目录，断电/真实Linux仍UNTESTED。当前能力Runner是测试注册函数，未编入可远程调用动作或root旁路。Agent主入口尚未联网。候选IdentityLookup/AgentIdentity/PeerVerifier必须由总控审查后接权威仓储。剩余：WSS依赖精确锁差异、注册/签发/重装准入、持久节点状态、DS统一调度/审批/事件接线及真实隔离环境，CX-03/04仍未完成。

## 03a / 全量检查返修

第一次 `npm run check` 的 policy/contracts/web/git PASS，go FAIL：staticcheck报CertPool.Subjects弃用。移除这项无必要的预检查；空可信池本身会在TLS证书验证时拒绝，nil池仍在构造时拒绝。没有用deprecated抑制注释或降门槛。第二次全量检查另记结果。

`npm run check:security` PASS：npm audit 0 vulnerabilities / govulncheck No vulnerabilities found。总控确认S01—S04未接收，SHARED-CHANGES已纠正S01为独立修订候选；受保护原文件未改。

## 04 / CX-05 AI工具策略与工作流计划组件

文件 internal/ai/tool_policy{,_test}.go、internal/workflows/plan{,_test}.go。实际 `go test ./internal/ai ./tests/architecture -count=1` PASS 2.044s/2.132s；`go test ./internal/workflows ./internal/updates ./tests/architecture -count=1` PASS 2.138s/2.176s/2.256s。最终组合平台模块测试也全部PASS（reports/platform-modules.log）。

AI：零资源默认、服务组成员冻结并与当前成员取交集、当前RBAC和工具/插件权限交集、审批绑定会话/主体/目标/参数摘要/世代/期限、自动模式只允许白名单只读、限时full仍强制root Shell真人管理员审批、秘密导出无条件拒绝。工作流：固定触发类型/结构化相等条件、动作白名单、复制目标/参数、按定义修订+事件+步骤产生稳定幂等键；无脚本表达式或自造执行队列。

这些是可单测的**策略/计划组件**，不是持久AI会话/模型对话/SSE、审批服务、工作流调度或POC-06 E2E。Scope、CurrentAuthority、Approval及工具分类只能由受审服务端来源构造，HumanWeb/Administrator不是HTTP布尔输入。批准的原子消费/任务提交、每阶段当前性、BYOK秘密仓储、模型/通知/触发持久来源和完整任务参数端口尚缺；未经接收不向旧Submitter丢弃参数。Eino未选生产runtime。CX-05仍未完成，独立组件不授予任何执行通道。
