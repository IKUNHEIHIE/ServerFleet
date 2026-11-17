# Android App 与测试站移动入口交付回执

任务 MOBILE-APP-20261118；会话 codex-mobile-app-20261118-01。
用户要求在此前 AI 增量完成后提供轻量 App；沿用本轮测试站部署和“不新增备份”要求。
本次默认 Android APK（曾询问平台并告知默认选择）；不是 iOS 或应用商店正式发行物。

## 用户入口

- 下载：https://agent.wentian.org:8443/assets/serverfleet-mobile-0.1.0-preview.apk
- Android 10+；启动填写控制台网址、管理员账号和密码，支持非默认端口与粘贴控制台链接。
- 当前测试站网址：https://agent.wentian.org:8443
- 进入后只有节点和 AI 运维；节点信息/资源每 5 秒刷新。AI 使用控制台配置与会话历史，
  展开“节点与资源授权”后点击节点，选择“执行任务”即可使用现有运维与审批流程。
- 顶部连接选项可刷新、返回节点、切换网址/账号。只保存网址与账号，密码每次填写。

## 准确工件

时间：北京时间 2026-11-18 04:22:47（UTC 2026-11-17 20:22:47）。
实际工件源码 6652faa09913e781bd3bdc22811afc29251ba244；部署版本 0.9.3-mobile-20261118。
Go 构建信息 vcs.modified=false。PR #60 已正常合并为 288c9d4c1d9ab5c0ce0fb93fec1000055e577715。

Android applicationId org.serverfleet.mobile.preview，versionCode 1，versionName 0.1.0-preview。
APK 62,312 bytes；SHA256 2fe28ffbde9ee8ee8a3be356da12be3ddee9dc470063e520253a6686e55442ff。
apksigner v2 校验与 zipalign 均通过；开发测试签名，签名私钥留在远程，不放入仓库/网页。
证书 SHA256 8c69c4528d53f55a9d3b67e0bc038f0539de014ab248557d82043bd7a6e92060。

Controller /opt/serverfleet/bin/serverfleet-controller：
SHA256 d8c243c38220361bdedd9ebfd36fc77fb8dcc399d72cca5072aedc63851d0fed。

Web /opt/serverfleet/web：
- index.html：9d0318e7a039a850243089ad1b0f9ea9e5279bab815d831be16ca50b41fe3415。
- /assets/index-BEd2dDrj.js：4db6e4084768a54b247e9d7aaa1d0ca21c1bac5ab038e50b9f5cee043caf52c9。
- /assets/index-BEknkhiT.css：0afca39bd16980e6537f011819095e9e321e25e6342e298e46c4ccb613711a36。

公网首页、JS、CSS、APK 均为 200，摘要与构建产物一致；APK HEAD 长度/类型/附件名正确。
readyz 的 agentTransport、authority、database、pki、scheduler 均 ready。
service active/running，NRestarts=0；原启动参数摘要一致。
只更新 Controller/Web 与可选 APK；无迁移、数据库操作、模型配置修改或 Agent 更新。
未新增备份；未重放或改写旧任务。

## 验证与限制

Android 单元 3 项、lint 0 错误/3 警告、构建/签名/对齐通过；
Web 55 文件/488 既有用例与移动专项 3 项、生产构建通过；
Controller 完整 -race 与 vet、contracts、policy 通过。
远程 Chromium 390/360 宽度下节点详情、点击授权、普通对话发送、Markdown/推理展开无页面错误或横向溢出。
这些对话用可控接口样例验证，没有调用生产账号或真实供应商执行验收。
公网实际部署的手机匿名入口同样已用 390px 浏览器验证：登录字段可见，meta 特征正确，
无脚本错误和横向溢出；未使用真实登录凭据。

真实 Android 手机安装后键盘、系统 WebView Cookie 交接：UNTESTED。
完整核心/全产品验收：SKIP（用户未要求）。
精确 head CI 44168051348（#241）：foundation、postgres-maintenance PASS；dependencies FAIL。
已核对本次仍是旧构建工具 picomatch 通告 GHSA-fc5q-39xc-vjpf，锁文件未变。
按用户功能优先要求留作后续，未降级工具、关闭/修改 CI 或绕过分支保护。
govulncheck 因前步 npm audit 失败未执行，不报完整 CI 全绿。

远程源目录 /home/admin/sf-mobile-app-20261118；
reports 保留构建、测试、视口截图与不含凭据的部署摘要。
原未提交目录未改动，Windows 未安装开发工具。
