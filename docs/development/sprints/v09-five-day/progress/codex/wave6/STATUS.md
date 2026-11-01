# V09-R6-CX · codex-06

审查人 codex-review-02；独立目录 feature--v09-r6-platform-cx06；基线 73d5dd46fb29b3cf4a445b43b48186d5938fe781。仅本包允许目录，无其他 Agent、远程连接或 main 合并。

## 增量 1：私有随机密钥持久化

- auth.NewPersistentCredentialService：Linux 当前用户 0700 目录、0600 常规文件；随机 256 位主密钥；完整文件先同步再原子发布；并发首次创建不可覆盖；不支持的 OS 权限适配拒绝启动。
- 已初始化身份库缺密钥、未知格式、不可读密文、宽松权限、符号链接均失败关闭。没有读取旧库固定 local-default 或自动迁移。
- 离线 RotateCredentialMasterKey：停止所有写进程后添加新活动密钥，原子替换文件、保留旧密钥。没有删除密钥、自动重加密或在线轮换承诺。
- ServiceMetadata 只提供当前 owner 的单个未撤销引用；借用仍经 UseForService 的当前门控、持久意图、禁止重放、借后复核和缓冲清零。
- Windows `go test ./internal/auth` PASS（新增 Linux 权限测试 SKIP）；本地非 root WSL `node tests/poc/v09/platform/run-wave6-linux.mjs auth` PASS：70 顶层，0 SKIP。日志/二进制摘要位于忽略的 reports/wave6-linux/auth.json 与 auth-native.log。

剩余：Controller 固定密钥位于总控独占目录，作者未修改；具体补丁 controller-keyring.patch 待接收。总控必须配置私有路径、在新隔离数据库验证；awskr 已有数据库不允许用新密钥替换。Windows ACL 适配未准入。

Agent / 文件 / AI / Xray / 更新 / 恢复继续包内推进。G2 和发布准入不变。
