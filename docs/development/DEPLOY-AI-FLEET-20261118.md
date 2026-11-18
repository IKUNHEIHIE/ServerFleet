# AI 对话、授权与节点通信测试站回执

任务 AI-FLEET-STREAM-20261118，会话 codex-ai-fleet-stream-20261118-01。
北京时间 2026-11-18 12:08:14 按用户要求部署至 https://agent.wentian.org:8443。
上线版本 0.9.3-ai-fleet-20261118，Controller/Web 源码 96d98b3cd241b527e3d6c667512fbb93b8f7b31a。
二进制构建标记 vcs.modified=false，SHA256 20b20a9887a65d9b842799528a705de9c349c38f24227d8ac30b6edc72d78c5a。
未创建备份。启动参数摘要未变，五项 ready。所有开发分支、提交和工件留在 awskr，本次未上传 GitHub。

## 测试站

- index.html，HTTP 200，SHA256 1599bc7356dbbf454720e332f2c460be5f7bb08b66b760f900efd20f3eaf40f6。
- assets/serverfleet-mobile-0.1.0-preview.apk，HTTP 200，SHA256 2fe28ffbde9ee8ee8a3be356da12be3ddee9dc470063e520253a6686e55442ff。
- assets/index-BVtuIaMC.js，HTTP 200，SHA256 2b3a51c84938c7377c6a062e699f16072bf7ab74b24ae0028e9a4ac586689c16。
- assets/index-BEknkhiT.css，HTTP 200，SHA256 0afca39bd16980e6537f011819095e9e321e25e6342e298e46c4ccb613711a36。

浏览器真实公网访问已加载新版入口和登录表单。隔离浏览器样例复用线上同一构建，用受控的实时 SSE 验证
先显示「开」，随后显示「开始」，最后保存完整答复。授权动作输入不存在，聊天上下文估算可见。
该样例使用合成供应商/账号数据，实际供应商输出粒度尚未在线验证。

## Agent 工件与四节点验收

新版 Agent 工件放在 /home/admin/sf-ai-fleet-stream-20261118/reports，构建源码均为 96d98b3cd241b527e3d6c667512fbb93b8f7b31a。

- Linux amd64，serverfleet-agent-linux-amd64，SHA256 23dfbefa80b744984606ffabc31f80e84428ed70f168e3ea974e6668b8131e95。
- Linux arm64，serverfleet-agent-linux-arm64，SHA256 1dd9e9379949d81762dc7a72c7ca1dcd1d9ce389d5c6491846193af27325c831。

aliyun-hk-1b、azure-20-215、node-142、wentian-host-01 均在 Controller 重启后重新握手并在线。
四台均未报告 system.updates.agent_apply.v1。原有签名更新入口属于系统更新，
其监督器预置策略与更新能力是远程安装所需条件。当前 AI 工具目录也没有签名更新操作。
因此真实现有节点的升级指令下发与批量升级验收为 BLOCKED，完成 0/4。没有修改节点二进制或部署配置。
mTLS 回归确认命令初始与启动校验共用同一条 TLS 连接，仍能及时拒绝撤销后的活动命令。真实节点命令延时尚未实测。

## 已通过的检查

AI、transport、Agent、Controller server Go 包回归通过。
小 SSE 增量、跨增量密钥遮蔽、重复授予、完全访问当前权限与 mTLS/WSS 相关用例的竞态检查通过。
Web 专项、类型检查与部署生产构建通过；policy、contracts 与启动预检通过。

Web 完整回归 PASS，56 文件 / 493 用例，部署构建摘要一致。
平台集成初次全包 FAIL：旧授权测试仍期望重复行，签名回滚测试首次出现 UNKNOWN，另有独立 Gateway 测试的 nginx 不在 PATH。
按新授权契约更新测试期望后，定向集成的授权、真实本地 WSS 命令、TLS 供应商与实际签名 Agent 安装/回滚四项均通过。
没有重跑独立 Gateway 验收。ARM64 首次构建受 /tmp 空间限制，切到任务专用 GOTMPDIR 后通过。
