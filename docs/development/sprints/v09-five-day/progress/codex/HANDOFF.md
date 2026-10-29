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

SHA `fb0245add5e0e3021598fcc874f51cb2fb43e0fd`；03a返修SHA `51714c4e6348850084a4f9b3c340df8240fadd0a`。

文件 internal/ai/tool_policy{,_test}.go、internal/workflows/plan{,_test}.go。实际 `go test ./internal/ai ./tests/architecture -count=1` PASS 2.044s/2.132s；`go test ./internal/workflows ./internal/updates ./tests/architecture -count=1` PASS 2.138s/2.176s/2.256s。最终组合平台模块测试也全部PASS（reports/platform-modules.log）。

AI：零资源默认、服务组成员冻结并与当前成员取交集、当前RBAC和工具/插件权限交集、审批绑定会话/主体/目标/参数摘要/世代/期限、自动模式只允许白名单只读、限时full仍强制root Shell真人管理员审批、秘密导出无条件拒绝。工作流：固定触发类型/结构化相等条件、动作白名单、复制目标/参数、按定义修订+事件+步骤产生稳定幂等键；无脚本表达式或自造执行队列。

这些是可单测的**策略/计划组件**，不是持久AI会话/模型对话/SSE、审批服务、工作流调度或POC-06 E2E。Scope、CurrentAuthority、Approval及工具分类只能由受审服务端来源构造，HumanWeb/Administrator不是HTTP布尔输入。批准的原子消费/任务提交、每阶段当前性、BYOK秘密仓储、模型/通知/触发持久来源和完整任务参数端口尚缺；未经接收不向旧Submitter丢弃参数。Eino未选生产runtime。CX-05仍未完成，独立组件不授予任何执行通道。

## 05 / CX-06 插件包校验与Xray计数组件

SHA `52ea2b9a6b1ba21ee5b85cd25b84ac8d6937bbc2`。

文件 internal/plugins/package_verify{,_test}.go、plugins/xray-manager/traffic{,_test}.go。新签名元数据只是独立候选，不替代现有Plugin manifest/Host ABI。实现Ed25519域分离签名、精确源pin、载荷摘要/长度、规范JSON（拒绝重复/未知字段/非规范字节）、唯一xray-manager ID、权限白名单、无脚本声明UI、当前启用/安装修订检查。没有WASM加载、安装成功或任意Host能力。

Xray累计组件：稳定用户/实例/维度key，计数段、首样本基线、重复去重、重启/重置、乱序拒绝、缺口不充0、历史缺口保留，不将用户/入站/实例相加或当VPS账单。核心没有import可选包。

实际 `go test ./internal/ai ./internal/plugins ./internal/transport ./tests/architecture -count=1` PASS 1.989s/2.067s/3.713s/1.439s；`go test ./internal/recovery ./plugins/xray-manager ./tests/architecture -count=1` PASS 1.389s/1.371s/1.794s。最后平台组合测试10包全部PASS。测试包载荷明确为非WASM合成数据，只证明密码学/规则层，不算POC-01/02/03/04或真实StatsService。缺Host/WASM依赖审查、插件持久生命周期/统一任务、目标Xray版本与Linux双架构、配置模板/实际预检、用户持久化/受控导出/采集器/页面，CX-06未完成。

## 06 / CX-02/07 凭据加密、更新验签与恢复隔离组件

SHA `7303301d936d9a1ff1aa509265979d4d081c5696`。

文件 internal/auth/credential_cipher{,_test}.go、internal/updates/verify{,_test}.go、internal/recovery/reconcile{,_test}.go。AES-256-GCM随机nonce+对象/用途/owner/修订/keyID的AAD绑定、过期/撤销拒绝、新key重加密/旧key退出；仅auth私有函数，无普通API明文GET，不接受CA/恢复密钥用途。全局key使用量/存储/当前权限仍需适配，不把该组件称为凭据管理完成。

更新校验：固定版本/组件/平台/架构/单调发布序列、Ed25519域分离签名+摘要、限量流式读取、取消/短包/尾随/篡改拒绝。没有替换二进制、启动服务、健康/迁移回退或伪造installed。签名格式是新候选，缺UPDATE原文，不默认采用生产。

恢复组件：信任根/世代与撤销序列不能回退，撤销并集保留；running/offered/acknowledged归UNKNOWN，未启动过期归expired，全部旧session/approval待作废；输出恒为quarantine计划，不能开调度。不接触恢复私钥，不因可解码就声称可恢复；实际离线解密/备份完整性/旧主控fencing与DS原子写入仍缺。

组合命令 `go test ./internal/auth ./internal/ai ./internal/plugins ./internal/workflows ./internal/updates ./internal/recovery ./plugins/xray-manager ./apps/agent ./internal/transport ./tests/architecture -count=1`：10包PASS，详见reports/platform-modules.log（auth 6.752s；其余1.345–3.341s）。单元测试用合成key/字节，不是真实更新或POC-08。第二次 `npm run check` 五套全部PASS；`npm run check:security` PASS。这些检查不能关闭AC或缺失E2E。

## 06a / 负向加固

SHA `0ce9268d6089cc900de98ec81cf129fc46291424`。

Agent保留目录Linux模式要求无group/other访问；读取保留/结果文件按1024字节/128KiB限量，损坏记录不触发执行；落盘后再次检查未启动期限。补测真实两个主体会话，跨主体撤销拒绝且另一会话保持有效。插件即使可信capability配置误列shell.root，也拒绝非xray命名空间；工作流ID拒绝Unicode空白/控制符。

`go test ./apps/agent ./internal/auth -count=1` PASS 2.291s/6.371s；`go test ./internal/plugins ./internal/workflows -count=1` PASS 1.718s/1.647s。没有扩展root权限或开放未审路由。

## 07 / 独立认证契约修订与安全新决策候选

文件auth-session.patch.json、SECURITY-DECISIONS-PROPOSED.md及tests/integration/v09/platform/auth-candidate.test.mjs。仅对内存副本应用RFC6902；Cookie/opaqueBearer/初始化204/me-CSRF有test前置条件；不修改原始字节受保护OpenAPI；不污染通用Identity；保持既有nodes DTO/operationId/参数/响应和全部147路径185操作；拒绝旧基线与重复应用。

`node --test tests/integration/v09/platform/auth-candidate.test.mjs`：2 tests PASS（最终运行771.3874ms）；旧候选SHA256仍c779a4366d5fe0d3088d1c682b231dfe9d157c3d2ae562e1440cba7ccc3f6726。安全文档明确是新PROPOSED输入，未补写/替代历史已批准原文。独立修订仍待总控接收，不能因契约静态检查通过开启HTTP。
