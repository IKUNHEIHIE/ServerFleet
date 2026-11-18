# AI 运维修复测试站部署回执

任务 AI-OPS-DEPLOY-20261118。会话 codex-ai-ops-deploy-20261118-01。
用户明确要求部署测试站。执行范围为 awskr 上的测试站 Controller 与 Web 工件。
检查启动版本、启动参数、数据库迁移差异、公开工件和站点健康后结束本次部署。

- 站点 https://agent.wentian.org:8443
- 完成时间北京时间 2026-11-18 11:41:30，UTC 2026-11-18 03:41:30。
- PR #62 合并提交 894a395920357b48c0480d06bad52bce684cae0a。
- 工件版本 0.9.3-ai-ops-20261118。构建源码 c50dec2993a7bdb080f9f7bd1e08eee2e5dd719b，vcs.modified=false，源码树与该合并提交一致。
- Controller SHA256 b06da0035c977ca23060aa33f14ba2962122ce3cedfbf233d3a983f71b9c0367。
- 按用户要求未新增备份。Android APK 沿用原工件。此次源码无迁移差异。

## 上线检查

systemd 服务 active/running，NRestarts=0。启动参数 SHA256 aacbfab1e627a6b002e655ac055a1866acf653cfdc308562d7e65c6d6a368627，与更新前一致。
公网 readyz 的 agentTransport、authority、database、pki、scheduler 均 ready。
远程 Chromium 打开公网 AI 入口，匿名登录表单正常渲染，新版 JS 已加载，无页面脚本错误。

- index.html，HTTP 200，SHA256 08cf826ad391dbdcd9b3871a8ed075ed385ff8c6eac03948516bd9451a1964fa。
- assets/index-tyEx3qUb.js，HTTP 200，SHA256 1d5487fce28a9ccc889ea8c11719e1eaa55bc5fbff2b18e444b0fc600b053225。
- assets/serverfleet-mobile-0.1.0-preview.apk，HTTP 200，SHA256 2fe28ffbde9ee8ee8a3be356da12be3ddee9dc470063e520253a6686e55442ff。
- assets/index-BEknkhiT.css，HTTP 200，SHA256 0afca39bd16980e6537f011819095e9e321e25e6342e298e46c4ccb613711a36。

部署前有一条超过 5 分钟未更新的 running 任务与两条待审批。重启后未见 running，仍有两条待审批。
保留任务账本与调度器恢复行为，部署脚本未手动重放或编辑任务。

## 检查记录

PR #62 的 CI 44220325416 中，foundation、postgres-maintenance 均 PASS。
dependencies FAIL 对应已记录的旧构建工具 picomatch 通告 GHSA-fc5q-39xc-vjpf，锁文件未变。
在 awskr 上完成 AI、auth、tasks、Controller server 竞态回归及 17 项 Web 专项和生产构建。
本轮验证覆盖任务唤醒、运行结果语义、默认模型与删除密钥。线上真实节点/供应商耗时未实测。
完整核心流程验收按用户要求跳过。
远程构建及部署摘要保存在 /home/admin/sf-ai-ops-feedback-20261118/reports。
