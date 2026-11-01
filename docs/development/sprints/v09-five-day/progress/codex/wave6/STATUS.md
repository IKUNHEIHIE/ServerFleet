# V09-R6-CX · codex-06

审查人 codex-review-02；独立目录 feature--v09-r6-platform-cx06；基线 73d5dd46fb29b3cf4a445b43b48186d5938fe781。仅本包允许目录，无其他 Agent、远程连接或 main 合并。

## 增量 1：私有随机密钥持久化

- auth.NewPersistentCredentialService：Linux 当前用户 0700 目录、0600 常规文件；随机 256 位主密钥；完整文件先同步再原子发布；并发首次创建不可覆盖；不支持的 OS 权限适配拒绝启动。
- 已初始化身份库缺密钥、未知格式、不可读密文、宽松权限、符号链接均失败关闭。没有读取旧库固定 local-default 或自动迁移。
- 离线 RotateCredentialMasterKey：停止所有写进程后添加新活动密钥，原子替换文件、保留旧密钥。没有删除密钥、自动重加密或在线轮换承诺。
- ServiceMetadata 只提供当前 owner 的单个未撤销引用；借用仍经 UseForService 的当前门控、持久意图、禁止重放、借后复核和缓冲清零。
- Windows `go test ./internal/auth` PASS（新增 Linux 权限测试 SKIP）；本地非 root WSL `node tests/poc/v09/platform/run-wave6-linux.mjs auth` PASS：70 顶层，0 SKIP。日志/二进制摘要位于忽略的 reports/wave6-linux/auth.json 与 auth-native.log。

剩余：Controller 固定密钥位于总控独占目录，作者未修改；具体补丁 controller-keyring.patch 待接收。总控必须配置私有路径、在新隔离数据库验证；awskr 已有数据库不允许用新密钥替换。Windows ACL 适配未准入。

增量 1 SHA：6331a5691350adeac907d86a90315af248cbca00。

## 增量 2：常驻连接与受限文件字节组件

Agent `-serve`、有界重连/退避、持续心跳指标、信号取消及 worker UNKNOWN 停止重连；复用原持久执行/对账。Linux 非 root 私有文件根，分块读写、完整 SHA、不可变绑定、重启续写、当前权限前后复核、原子拒绝覆盖；字节不进入任务输出。

Windows `go test ./apps/agent/...` 首次 FAIL：文件最终目录同步在 Windows 不受支持；修正为 Linux 专属能力并明确 SKIP 后 PASS。不是放宽 Linux 持久化门。WSL 初验 38 顶层 PASS、0 SKIP，后续最终复验另记。`AGENT-LINUX.md` 含启动/停止/安装候选、Proto 接收/兼容/DS 接缝与未测项目。共享 Proto 只交 patch，未生成/接线，不声称跨机功能完成。

增量 2 SHA：9d63de015a2411df67801da60c71bcd80b1a931e。

## 增量 3：AI BYOK、配置及真实 HTTP SSE 消费

NewBYOKRuntime 绑定已有持久 ModelSettings、owner 精确凭据元数据、Conversation 和 OpenAI 文本适配；强制注入配额适配，无默认无限配额；凭据独立 gate 仍要求私有 generating 观察。新增 owner 配置 GET/PUT 与显式 generate 候选 HTTP 入口，CAS、会话/作用域/密钥版本、取消和 UNKNOWN 不重试不变。上游 stream=true，消费有界 SSE，严格序号/终端文本一致性，拒绝工具/失败/断流；终端及借用后复核前不披露部分文本，因此**不是浏览器逐 token 流式展示**。

Windows AI 模块 PASS；WSL 初验 32 顶层 PASS、0 SKIP。新增受控 TLS 成功/配额零 I/O/撤权/断流负向及 owner 切换、CAS HTTP 用例。测试供应商不是外部模型服务。Context7 OpenAI resolve 超时，按 OpenAI Docs 使用官方 Responses streaming-events 页面核对事件；Go 文件安全及 HTTP 取消文档由 Context7 返回。

controller-ai-keyring.patch **取代** controller-keyring.patch，proposed.txt 是它的完整拟议文件供 overlay 编译；共享目录未改。`go test -overlay reports/wave6-linux/controller-overlay.json ./apps/controller/server -run '^$'` 编译 PASS。此项仅编译，不是补丁已接收或主控集成成功。总控需注册配额、模型审计 sink、UI 路由/API 审查及新私有密钥配置，真实外部模型 UNTESTED。

代码复核同时修正 Agent 身份借用包装会隐藏 worker UNKNOWN 的问题，避免未停止 worker 后重连。

增量 3 SHA：86a7247b3ecdcc91b37c6f5d21d44f7b39765a9c。

## 增量 4：更新、隔离恢复与 Xray 任务 Host

- 更新：DirectoryReleaseSource 读取固定私有离线 inbox，签名/版本/架构/序列下限/大小/哈希由既有 Stager 验证；NewTaskInstallExecutor 将已批准持久计划接入真实 SlotInstaller；HTTP Apply 可注入实际执行，重复安装拒绝。服务 Stop/Start/Health、存储兼容、审计提交仍是必需适配，测试中明确为 double，没有伪装进程健康。
- 恢复：RestoreIsolated 在新私有任务目录内先落 quarantine，再复制/哈希/验证真实 SQLite 台账、用外部权威合并撤销和 UNKNOWN；同任务目录不可重跑。成功仍 restored_quarantined，没有自动放开旧主控 fence。新增 auth 纯变换提升身份/策略 epoch、撤销旧会话、使旧 Token/审批失效、合并当前凭据撤销/主体禁用；保留密钥计数与借用防重放台账，去除旧暂存轮换。由总控/DS 在一笔恢复事务应用，不直接修改运行库。
- Xray：NewXrayHTTPWithTaskHost 仅给明确 mutation 白名单接入统一任务 Submit；不可变动作/目标/参数摘要、当前准入前后检查、生命周期版本检查、幂等键、拒绝内联秘密/Shell。返回 queued taskId，**不声称 Xray 已执行**；预演/配置验证/导出仍需已准入业务 Runtime/Host，不能靠启用 flag 绕过。
- 权限/worker 安全复核：启动还验证暂存凭据与 Xray 密文；Agent 文件绑定/目录/提交回执增加目录同步。Windows 不开放这些 Linux 权限能力。

模块及平台 Windows 测试 PASS；WSL 初验 plugins 12、updates 14、真实 SQLite 恢复专项 1，均 PASS/0 SKIP；最终固定提交复验待 HANDOFF。恢复首次缺测试台账 FAIL 已修为真实空迁移台账后 PASS，未放宽生产验证。根 check PASS；security 首次 FAIL 由自建 overlay 临时 .go 被扫描为独立包导致，改用 .txt 后 check:go 与 check:security PASS（0已知漏洞）。新增 auth 变换后另做最终检查。

共享补丁 controller-platform.patch 提供更新源/执行、恢复入队、已准入插件 Runtime 与 Xray TaskHost 的 Config 注入；与组合 AI/密钥补丁无重叠。组合 Go overlay 编译 PASS。所有共享补丁初次 git apply 检查因作者生成 CRLF 失败，现统一 LF，git apply --check PASS；没有应用到共享文件。

当前硬依赖：总控/DS 接收 WSS 文件/Proto、统一任务动作与恢复事务适配；总控配置私有密钥、配额与供应商；Xray 安全工件/运行时/Host 准入；真实 supervisor/旧主控 fence/新密钥与吊销权威及实机。未解决前不能关闭完整 v0.9 验收。完成证据收口后停止，不合并 main。
