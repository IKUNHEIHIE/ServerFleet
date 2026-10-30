# 构建、运行与接线边界 / codex-02

当前不是可安装完整v0.9发行物。Controller仍只开放health；Agent主入口仍离线。新组件以单元/局部集成实验验证，未经接收的仓储/任务/契约不接主入口，不以测试仓储提供登录或任务假成功。

## 复现

工作目录为 `D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`。使用只读链接的固定Node/Go工具；缓存/输出保留本目录。

```powershell
& .tools/node-v24.22.1-win-x64/npm.cmd ci
& .tools/node-v24.22.1-win-x64/npm.cmd run tools:install
& .tools/node-v24.22.1-win-x64/npm.cmd run check
& .tools/node-v24.22.1-win-x64/npm.cmd run check:security
& .tools/node-v24.22.1-win-x64/node.exe --test tests/integration/v09/platform/auth-candidate.test.mjs
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run.mjs
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run-eino.mjs
& .tools/node-v24.22.1-win-x64/node.exe tests/poc/v09/platform/run-identity.mjs
& .tools/node-v24.22.1-win-x64/node.exe tests/integration/v09/platform/build.mjs
```

最后命令仅生成 `reports/_review/v09-platform/build/{controller,agent}-scaffold-linux-{amd64,arm64}` 四个编译证据，不运行、不安装、不签发证书、不创建系统服务、不包含可选Xray。真实Linux安装/资源压测均UNTESTED；不存在可供发布的完整安装器/签名发行包。

如需直接运行模块测试，先设置当前命令GOCACHE=.cache/go-build、GOPATH=.cache/gopath，再用.tools/go/bin/go.exe test；所有路径基于本worktree。隔离PoC的依赖/源码及局限见tests/poc/v09/platform/README.md。

## 审查后接线顺序

1. 总控接收独立auth-session.patch.json及安全新决策候选，协调GM的HTTP DTO（保持userId/displayName/roles，不直接JSON编码服务内Identity）；受保护原OpenAPI保持不变。
2. DS/总控提供并接收持久事务/迁移、auth namespace及审计出箱消费的精确接口；取消/提交失败/并发初始化/真实重启撤销须先测。不自动创建编号、猜RecordTx或只用内存实现替代。
3. 总控接收统一tasks完整参数/摘要/审批/幂等事务端口；AI/工作流/插件/更新均通过该入口，未知结果不重跑，当前授权逐阶段检查。当前Request字段不足，禁止丢参数接线。
4. S04依赖patch与WSS适配经审后才能进入根工程；组合PeerVerifier、现有ConnectionGuard/DecodeEnvelopeFrame、受审持久节点回调和执行器。隔离实验不是已接收生产接线。Agent安装/签发/替换/吊销需PKI适用条款和安全准入，不复用setup或用户会话Token。
5. 可选Xray与Host独立发行，完整生命周期/限额PoC、实际Xray目标版本预检及双架构环境接收后再激活动作；核心不得import可选包。Eino/WASM均未冻结。
6. 更新仅对已验证私有staging对象执行受控替换，并验证真实健康/迁移；缺UPDATE原文/替代决定不宣称可回退。恢复组件只产隔离计划，离线密钥验证、信任连续、最新撤销、旧主控fencing及DS原子恢复完成前不得开调度。

本包没有真实安装/升级/灾备成功步骤，因为相应实现/准入/材料尚缺；给出假命令会掩盖风险。上述BLOCKED项需要总控提供接收SHA/隔离测试条件，不能由本会话越权改共享文件或上线替代。

R14—26新增实际SQLite恢复隔离、AI消息HTTP/SSE/推理凭据闸、Agent连接/日志和私有更新切换组件；步骤与局限分别见RECOVERY-QUARANTINE、AI-MESSAGES-HTTP、AI-GENERATION-ADMISSION、AGENT-SESSION-WIRING、AGENT-OUTPUT-WIRING、UPDATE-INSTALL。更新的服务/health/迁移与审计适配仍须接收；Controller不能内联停止自己的进程后假设还能继续升级，必须由独立受控监管进程实际承接。

可选Xray26.6.1历史功能实验证据保留，但额外二进制扫描55项FAIL；新正式26.10.29三项FAIL且未启动。复查`node tests/poc/v09/platform/audit-xray-release.mjs 26.10.29`应如实返回非零并写静态证据；不能因根安全扫描PASS而发布。run-xray.mjs现在先扫旧工件，FAIL直接拦截，不提供跳扫描开关。更多来源/pin见XRAY-ARTIFACT-SECURITY。没有可发布Xray安装命令。
