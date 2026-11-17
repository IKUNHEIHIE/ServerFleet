# AI 推理流与 Eino 测试站更新回执

任务 AI-AGENT-SDK-20261118；会话 codex-ai-agent-sdk-20261118-01。
本轮是此前 AI 交互和测试站部署的继续修正，沿用用户“不新增备份”的要求。

- 时间：北京时间 2026-11-18 03:40:43（UTC 2026-11-17 19:40:43）。
- 站点：https://agent.wentian.org:8443
- 已合并 PR #58，合并提交 352c28a5ee74d9bdbaddabd0a04159a848f7557b。
- 实际工件源码：已审查 head 762fb468b0145512b068d582380cec23a1651488；
  Go 构建信息含该 revision 和 vcs.modified=false。
- 版本：0.9.3-ai-sdk-20261118；手工测试版本，不是新正式 Release。
- 更新 Controller 和 Web，原服务参数、数据库、PKI、模型配置及 Agent 保留。
  未复制旧数据库/程序/Web、未新增备份、未重放旧任务。
- 53 项 PostgreSQL 迁移账本与目标注册表匹配，没有新增迁移。

## 工件与站点核对

程序 /opt/serverfleet/bin/serverfleet-controller：
SHA-256 ae62283b8ce0a6b4b5e9f75378ae8b41daf31020fbaa1c08aa339a5dd69a6ab3。

Web /opt/serverfleet/web：
JS /assets/index-DVpYx6Ut.js，
SHA-256 bbfd152ffaa460875988285baa649df0ecfc9d6a5e2031fbf2148e7f257a82e4。
CSS /assets/index-DY6NFx5t.css，
SHA-256 d0af1894f0b77893fcb0be8e641f2c8fa4cc3023415c29d6c693734b1c7d6190。

公网首页与上述 JS/CSS 均返回 200，摘要与已构建工件一致；
JS 包含新接口协议、推理/摘要/计划入口，CSS 包含真实粗体的字体合成支持。
/readyz 的 agentTransport、authority、database、pki、scheduler 全部 ready。
进程程序摘要、版本与目标匹配，ActiveState=active、SubState=running、NRestarts=0。

## 验证与遗留

- 本地功能样例、AI/Controller 竞态回归、Web 55 文件/488 项用例与构建通过；
  桌面/390px 组件预览无溢出或页面错误。
- 最终 head 的 CI 44139934773：foundation、postgres-maintenance 通过。
- dependencies 保留 FAIL：旧构建工具 picomatch 通告 GHSA-fc5q-39xc-vjpf。
  已核对 picomatch 4.0.7、Vite 8.3.5、Vitest 5.0.5、fdir/tinyglobby
  与基线 d71a2f0 的版本及锁记录完全一致。
  用户明确要求优先功能可行性、安全工作后置；本轮不降级工具、不修改/关闭 CI，
  以 expected head 正常合并，未绕过分支保护。
  govulncheck 因 npm audit 前步失败而未执行，不报完整 CI 全绿。
- 原站点保留一条 2026-11-17 16:45 UTC 停在 dispatched 的固定自检任务；未强改旧结果，
  未将它作为本轮新功能成功证据，更新未触发重放。

真实供应商模型/真实节点完整流程：UNTESTED。
全产品核心流程验收：SKIP。
模型有公开推理字段才显示；旧消息未保存的推理不补造。
详见 [功能与框架接入](AI-AGENT-SDK-20261118.md)。
远程工件/回执在 awskr /home/admin/sf-ai-agent-sdk-20261118/reports，
不包含 DSN 或密钥明文。
