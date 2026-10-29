# 增量交接 / codex-02

## 01 / CX-01

SHA `2fc8dab314a906c0e48543d4735ee5509d08e355`。仅三份候选/进度 Markdown。`git diff --check` PASS。没有运行时接口、依赖、公共 schema 或生成变更。审查入口 CHECKPOINT-01.md、SHARED-CHANGES.md。

## 02 / CX-02 认证组件（本记录随代码提交，SHA见后续记录）

文件 internal/auth/identity.go、browser_guard.go、identity_test.go。实现受控 setup/单用初始化、PBKDF2-SHA256（随机32字节盐、600000轮，候选参数）、身份与秘密管理ID分离、8小时绝对/30分钟空闲期限、每请求来源查询、双维度持久限速、CSRF/Origin/Cookie约束、单会话撤销、改密epoch全撤、提交前世代闸门、时钟回拨拒绝、事务审计出箱。

实际命令及输出：

- 固定本地 Node `npm ci`：PASS，added 127 packages in 17s；npm提示buf安装脚本未单独批准，未改allowScripts。
- 固定 Go `go test ./internal/auth -count=1`：PASS 9.578s（首轮）。
- `go test ./internal/auth ./tests/architecture -count=1`：PASS auth 17.392s / architecture 2.051s（补充过期回拨/结构校验后）。
- Context7 resolve Go→/golang/go/go1.27.3，查询KDF/HTTP/AEAD；本地go doc核对PBKDF2.Key/CookiesNamed等确切签名。

复现：使用任务目录 .tools/go/bin/go.exe，GOCACHE=.cache/go-build，GOPATH=.cache/gopath。测试仓储只位于 *_test.go；新建service复用测试状态验证撤销不复活，不等同SQLite进程重启或真实浏览器验收。调用顺序：NewIdentityService(经审持久IdentityStore) → 本机PrepareSetup → Bootstrap → Login → BrowserGuard.Check/SessionCredential → Authenticate → CommitGate。PrepareSetup不得提供远程路由，LoginSession.Secret禁止JSON输出。

候选端口新增在auth自有文件（未改ports.go）：IdentityStore、IdentityService、Identity、BrowserGuard/Cookie helper；尚无跨owner生产消费。请总控审查这些精确文件后与DS适配事务、审计出箱及HTTP契约。无schema编号/锁/生成变更；没有默认管理员节点授权，Bearer保持关闭。缺完整PRIV/MFA政策、受控安装交付、持久事务/审计出箱消费、配置与真实HTTP/浏览器验收，**CX-02未完成**。16MiB状态/10000会话与审计出箱为有界候选，性能未测；出箱满拒绝，不能长期运行而不接审计消费。Tgate后网络输出窗口仍存在。
