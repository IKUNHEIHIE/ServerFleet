# V09-R6-CX-02 · codex-06 交接

审查人codex-review-02。工作目录/分支沿用授权；同步起点379d5e0e4818844eb44d8ac13010f251d215d8c0，固定被测代码 **8336e56426d2016707b89f4cd80bd8b46f221687**。本轮独立实现与验证已收口，余项是共享接收、明确未实现的拒绝策略或环境/安全准入；交证据后停止。未合并main、未操作awskr/SSH/云/真实外部秘密、未启动其他Agent或发送消息，不宣称完整v0.9/G2/发布验收完成。

## 可运行提交

| SHA | 文件与行为 |
|---|---|
| d58020c67d9ac798ec502853015e2f392c62cd49 | internal/transport/file_*、controller_link/validation；apps/agent/connect/session/runtime/file_*：已生成33—36类型的真实WSS文件路由、逐帧门、持久TaskAck、任务/执行/transfer/方向/摘要绑定；Linux显式file-root读写、冻结快照、NOFOLLOW、取消/UNKNOWN/重连防重复；controller-files.patch |
| 8bbe64e7f72c42577bd13a4146df9a4d0e34dc07 | internal/ai/budget_sql/generation_budget/model_audit_sql/local_tls_provider等：唯一OperationID持久预算与typed审计同事务、模型审计不丢profile/revision、本地TLS供应商；真实Controller HTTPS→秘密借用→供应商HTTP/SSE→完整消息专项；AI组合/迁移候选 |
| 8336e56426d2016707b89f4cd80bd8b46f221687 | internal/updates/supervisor_linux*：实际受控子进程、镜像/健康验证、持久防重reservation与真实签名更新回滚；apps/agent资源预查/实际EFBIG负向；internal/recovery严格新trust代次；auth只读管理员门和预算事务内复核；取消/未知文件session封闭 |

测试只在apps/agent及对应internal模块、tests/integration/v09/platform、tests/poc/v09/platform；交接只在本wave6-r1目录。EVIDENCE.json列出基线以来全部37个变更文件、原始日志/二进制SHA256、SKIP名单和限制。共享源码、Proto/gen、DDL编号、根依赖/锁/CI及其他作者目录无改动，已接收密钥/Windows/撤销修复未覆盖。

## 固定提交验证

工具：项目固定Go1.27.3、Node24.22.1、npm11；现有本机WSL Ubuntu、UID1000；合成身份/合成凭据、临时私有目录与本地TLS，无真实外部秘密。

| 命令 | 实际结果 |
|---|---|
| npm ci；npm run tools:install | PASS；buf postinstall缺allowScripts记录警告保留，后续固定Buf检查实际执行成功；依赖/锁未变 |
| npm run check | 全部policy/contracts/go/web/git PASS；格式/模块边界、固定生成/兼容、build/test/vet/staticcheck、前端测试与构建通过 |
| npm run check:security | PASS；npm 0漏洞，govulncheck No vulnerabilities found；不覆盖Xray未准入发行物 |
| Linux GOOS=linux GOARCH=amd64 CGO_ENABLED=0 govulncheck ./...；对应改动模块go vet | PASS；包含Windows构建不包含的Linux文件/进程代码 |
| 根Go JSON | 720顶层 +1177子测试PASS，0 FAIL；20 SKIP逐项登记，不计成功 |
| Web | 38文件/333测试PASS；既有Vite大块体积警告保留；本轮未改前端 |
| node tests/poc/v09/platform/run-wave6-r1-linux.mjs agent/transport/ai | 分别42/58/34顶层PASS；Agent 1 helper SKIP，其余0 SKIP |
| 同runner budget/controller/updates/recovery/restore | 分别1/1/15/5/1顶层PASS；updates 1 helper SKIP，其余0 SKIP；controller显式使用尚未接收的组合overlay |
| GOOS=linux GOARCH=amd64/arm64 CGO_ENABLED=0 go build -mod=readonly ./apps/agent | 两架构编译PASS；arm64运行UNTESTED，未做物理跨机/1C1G/部署推断 |
| git apply --check 三份共享patch | PASS；files+AI组合overlay真实HTTP专项PASS；迁移helper额外overlay编译PASS，未登记/执行新正式迁移 |

WSL八组共157顶层PASS、2 helper SKIP；两个helper均由父测试另起实际子进程执行，不能与Windows数量相加宣传覆盖。根20 SKIP包括Linux权限/文件/恢复、Windows符号链接、真实Cloudflare/Xray、CLI/隔离Nginx等。当前WSL无可调用的native Go工具链，完整Controller/Agent独立CLI测试未在本提交重跑（环境探查exit127，linux-cli-toolchain.log）；八组WSL为固定Windows Go交叉编译后直接执行原生Linux测试二进制，真实WSS/子进程/目录证据不受此限制。未重跑浏览器70步或将历史正确拒绝当新增功能成功。

原始输出在本worktree忽略reports/wave6-r1、reports/wave6-r1-linux、reports/go-tests.log、reports/web-junit.xml；EVIDENCE.json保存可核摘要。首次WSS取消fixture时序FAIL、TLS取消fixture诊断timeout、主控fixture权限/HTTP状态误判、恢复旧代次回归FAIL均保留经过，详见STATUS；修复未弱化授权/审计门。

## 总控接收与剩余缺口

1. **文件共享组合BLOCKED**：接收controller-files.patch；唯一任务worker调用FileClient，不与现有dispatcher重复发TaskOffer。总控/DS实现冻结路径映射、每项稳定ID、当前任务/审批/执行/取消/身份/AI scope门及源读/目标写分别授权、真实任务终态。两实际Agent会话已经传87,500字节并验证完成回执丢失/重连UNKNOWN不重写，但任务权威回调为明确fixture，不能等同主线HTTP+DS完整流程。只支持skip/new write；rename/overwrite/createParents/nonzero resume明确拒绝。Fstatfs预查与实际RLIMIT_FSIZE部分写失败已验证；真实ENOSPC UNTESTED，不填满共享磁盘伪造证据。
2. **AI共享DDL/配置BLOCKED**：接收controller-ai.patch、controller-ai-schema.patch，由总控分配新不可变迁移号并注册，之后才启用AITypedAudit；同一DB事务配置持久预算与typed audit，策略缺失拒绝。程序注册管理员门必须只读且当前，事务内不可嵌套Authenticate writer。8192输出token是实际请求上界预留，不是实测消费或金额。真实外部模型、浏览器逐token UNTESTED，本地真实HTTP/SSE不替代它们。
3. **更新生产准入BLOCKED**：OwnedProcessSupervisor已提供实际Start/Stop/Health，可绑定既有InstallHooks；只拥有本对象启动的进程句柄。真实产品版本/存储兼容Ready、RollbackSafe、冻结计划/当前审批和原子Commit仍由总控/DS接入。测试真实签名工件是可控测试二进制，任务/存储兼容/审计hook为double；不接管既有systemd/外部PID，未做后代进程/cgroup/正式沙箱或root动作准入。
4. **恢复真实权威/fence BLOCKED**：严格新trust代次、隔离SQLite/撤销并集/UNKNOWN与quarantine已验证；新密钥/当前离线权威、旧远端Controller可靠fence、各入口latch及显式release仍需总控完成。成功状态保持restored_quarantined，不能启动调度或复活旧凭据。Supervisor本机reservation只防同目录重复启动，不替代远端fence。
5. **Xray BLOCKED**：固定26.10.29工件重试在下载阶段失败，尚未进行本轮新扫描；历史两候选安全FAIL仍有效，三项实际Xray功能SKIP。没有执行该工件、安装插件或自行选择WASM/Eino/Host ABI来绕过准入。

具体接入参数与安全约束见SHARED-CHANGES.md。上述共享补丁均为候选；仅codex-review-02接收/合并/操作实机，作者停止本包工作。
