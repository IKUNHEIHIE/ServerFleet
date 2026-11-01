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

Xray / 更新 / 恢复继续包内推进。G2 和发布准入不变。
