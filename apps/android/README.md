# Server Fleet Android App（预览版）

[下载 Android 0.1.0-preview](https://agent.wentian.org:8443/assets/serverfleet-mobile-0.1.0-preview.apk)。
安装后填写 https://agent.wentian.org:8443（或另一已更新的 Server Fleet 控制台）和管理员账号密码。
[本次工件与验证回执](../../docs/development/DEPLOY-MOBILE-20261118.md)。


Android 10 及以上，系统 Android System WebView 保持更新。启动填写 HTTPS 控制台网址、
管理员账号与密码；仅保留网址和账号，不保存密码。支持自定义端口、粘贴网页控制台链接。
服务端需包含 mobile-app-v1 移动入口（本次增量），无需额外端口或 AI 配置。

登录后只有节点和 AI 运维两个入口。节点卡片每 5 秒刷新，离线/过期/未采集显示缺失值；
AI 共用现有模型、推理流、工具、节点授权、审批、任务持续执行及会话历史。
App 通过普通 Controller HTTPS REST 登录，将会话写入系统 WebView CookieManager，
同源 WebView 再读取 /api/v1/auth/me 获取当前身份和 CSRF，没有额外 AI/Agent 直连。
菜单可刷新、返回节点、切换网址/账号；会话到期使用正常登录入口。

## 构建

工具全部在开发远程服务器安装；不要求 Windows 安装任何依赖。
JDK 17（本次 Corretto 17.0.21.1.2）、Gradle 8.13、Android Gradle Plugin 8.13.2、
Android SDK platform 36、Build Tools 35.0.0；ANDROID_HOME 指向 SDK。

在 apps/android 运行 gradle --no-daemon testDebugUnitTest lintDebug assembleDebug。

产物 app/build/outputs/apk/debug/app-debug.apk，预览 applicationId
org.serverfleet.mobile.preview，version 0.1.0-preview。
使用 Android 开发测试签名，可直接安装；不是应用商店正式发行物。
签名材料不提交到仓库；更新须沿用同一签名与 applicationId。
release 构建没有预置私钥，正式分发时由维护者配置签名。
仓库不包含 Android Studio、SDK、Gradle 缓存或 APK。


## 测试站下载分发

构建后可将 APK 复制到 Web 构建目录 assets/serverfleet-mobile-0.1.0-preview.apk，
随既有 Controller/Web 更新一起部署。Controller 静态快照接受该命名的 APK 并返回
Android 安装包类型与附件下载头；仍沿用已有大小/总量约束，不增加文件服务或执行端口。
仅复制构建产物，不把签名材料与源码放进 Web 目录。
