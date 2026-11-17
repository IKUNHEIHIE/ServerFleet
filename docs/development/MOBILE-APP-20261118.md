# 轻量移动 App

任务 MOBILE-APP-20261118；会话 codex-mobile-app-20261118-01。
基线 main 4d3e65cf683437686f10f4db2ffef599154d6d16，独立远程目录
/home/admin/sf-mobile-app-20261118，分支 feature/mobile-app-20261118。

用户要求完成此前 AI 工作后提供 App：填网址、管理员账号密码，只查看节点/资源并使用 AI 运维。
平台未指定，已询问；按已告知的 Android APK 默认推进。
允许 apps/android、Web 移动入口/共享 AI 响应式布局、Controller 静态 APK 下载类型、相关测试/构建说明与产品文档。
全部开发构建在 awskr；本机不安装工具。复用 Controller REST/SSE 与身份、授权、任务执行底座。
验证与停止条件：可安装 APK、常规登录/会话共享逻辑、移动节点与 AI 功能验证及 Web 构建通过；
不把合成测试等同真实手机/真实供应商全量验收。实施与已运行验证见下；部署随后记录。

## 已实现

原生 Android 入口三项登录 + 菜单切换连接；系统 WebView 的同源 Cookie/CSRF 会话恢复。
App 页面只含节点/AI 两个入口：卡片、详情、硬件、5 秒资源刷新；共用 AI 对话、历史、
模型推理与摘要、工具、点击授权、执行和审批。桌面资源也可键盘点击添加。
Controller 仅扩展可选 assets/serverfleet-mobile-*.apk 静态类型及下载头；无新数据库迁移。
工具安装在远程 /home/admin/.local/share/serverfleet-android；未修改 Windows 工具环境。

## 验证

- PASS：Android JDK17 / Gradle8.13 / AGP8.13.2，SDK36，testDebugUnitTest 3 项、lintDebug、assembleDebug。
  lint 0 错误/3 警告；旧版返回键方法仅作为 API29–32 回退，33+ 已注册原生 dispatcher。
- PASS：apksigner 校验 APK v2 签名。预览包 62,312 bytes，SHA256
  2fe28ffbde9ee8ee8a3be356da12be3ddee9dc470063e520253a6686e55442ff。
- PASS：Web 既有 55 文件/488 用例；新增 mobile 专项 3 用例，覆盖会话恢复、读数缺失与零值、
  节点详情、AI 历史/Markdown/推理、点击授权与带当前 CSRF 的执行提交。TypeScript 与生产构建通过。
- PASS：远程 Chromium 390x844 / 360x640 无页面错误与横向溢出；节点详情、点击授权、
  普通对话发送并获取格式化回复，均使用可控接口样例。不是生产账号/真实模型验收。
- PASS：Controller 静态 console/下载专项 -race；完整 Controller 回归、contracts、policy 最终结果补 PR 回执。
- UNTESTED：真实 Android 手机上的键盘/系统 WebView Cookie 交接与安装后操作；远程主机没有 KVM，
  本次没有把浏览器模拟视口或 JVM 单元测试声称为 Android 实机验收。
- SKIP：完整核心流程验收；本次用户不要求。未新增备份；安装包使用开发预览签名，不发正式 Release。

源代码与构建产物分开：APK/SDK/缓存与私钥不入 Git；APK 将与测试站 Web 工件一起分发。
现有构建依赖通告与上一轮一致，不以本轮局部测试声称完整依赖检查通过；CI 保持原配置。
