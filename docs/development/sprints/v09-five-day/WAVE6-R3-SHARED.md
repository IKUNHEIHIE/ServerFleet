# Wave6-R3 · 已接收共享实现与下一轮边界

2026-11-02；总控codex-review-02。本轮来源：CX f643c495、DS 0b6cf7b、Codex Arts a70694b。负责人消息中的“Gemini a70694b”实际对应`feature/v09-r6-console-ca01` / codex-arts-01；旧Gemini目录保留，不与新目录混用。

最终功能版本`8df015456b2d57c8e960f934c9a7d3f3817c1894`；根/安全和13组原生Linux在`1069519`验证，之后只改浏览器新增策略检查的WSL传参，无Go/Web产品变更。真实CLI/产品Agent更新专项在`bee8274`验证，之后仅测试夹具/脚本修正。最终main交接SHA含后续文档/证据，以主目录`reports/v09-wave6-r3-review-handoff.json`为准。

## 已实际组合

1. **文件可信CLI配置**：已应用`controller-file-cli.patch`。`-file-deployment`需要管理HTTPS模式，调用`server.LoadFileExecutionConfig`，在打开PKI/DB前检查Linux普通用户拥有的0700配置目录、0600单链接配置文件、明确nodeID逻辑根与私有spool。不支持的平台拒绝该装配入口；默认无配置不启用文件执行，不授予AI/plugin额外资源。
2. **文件策略**：skip/overwrite/rename已贯通正式worker与Agent。overwrite原子替换并复查；rename限定同目录、最多32次且回写实际`destinationPath`；不改Proto/gen。未知策略、遍历、未登记根和超过16项请求拒绝。当前64MiB单文件等限制、上传任务化、多目标、跨主控重启续传仍须按需求继续。
3. **queued取消修复**：成功取消统一任务后，在同一数据库更新中让仍queued的transfer立即进入cancelled/记录完成时间，与ClaimTransfer竞争只允许一个成功；running仍等worker最终证据。不能把已运行/未知结果改写成“未发生”。新回归先FAIL再PASS；原运行期取消/恢复测试保留。
4. **真实Agent产品更新装配**：已应用`controller-product-updates.patch`，新增`server.Config.UpdateProduct`/`Server.ProductUpdates`，绑定真实任务、当前管理员、RuntimeGate和真实WSS连接，配置`UpdateProduct`与旧执行注入同时存在则拒绝。`ProductDeploymentOptions`仅可信程序提供，初始安装来源需匹配签名inbox及active实际字节；schema版本和sqlite_master指纹绑定计划/审批，漂移UNKNOWN。启动/关闭只管理自己创建的Agent进程。此交付是**Agent组件、本地签名来源、程序组合**，不是完整Controller升级或默认CLI在线发行源。
5. **清理claim迁移**：已将`certificates.CleanupClaimMigrations()`注册，正式总数**25项**，新增`ds/0063_cleanup_claim`，保留旧checksum。原19/21升级夹具与重复应用已更新。取消与claim token竞争、attempt预留、过期lease转unknown不重放已接收；默认cleaner仍nil，无自动RetryDue。未知结果分类仍须与真实provider适配一起复核，不能把任何网络错误视为安全重试。
6. **正式候选与权限**：R7 services候选已同步502失败/unknown/409等实际语义；根Redocly在上轮就已登记，不要重复新增。两个gateway.nginx.site_apply/remove的ExtraPermissions也已登记；Agent运行注册/工件链路仍有下述独立缺口。原三OpenAPI、69原文、Proto/gen/兼容基线/r03未改。
7. **管理台**：正式owner预算界面、管理员CAS/409、已用剩余/只读/拒绝、文件真实宿主和第二隔离Controller基础已接收。测试宿主无未认证配置路由，合成上游秘密仍走私有文件。它仅用于验收，不能作为产品启动方式。

## 不再重放历史prepare/overlay

CX两份R3补丁已正式应用；不要再次运行`prepare-wave6-r3-cli.mjs`/`prepare-wave6-r3-product.mjs`，它们会按旧上下文重写候选。R1/R2历史prepare和补丁同样不再应用。当前`run-wave6-r3.mjs --final`已移除overlay/build-tag，使用正式代码/25项迁移并要求干净HEAD。

旧CX/DS/CA进度是各原分支交付时的快照，涉及24项迁移、overwrite拒绝、权限/候选待注册、queued取消未修等描述，以本文件和本轮报告为最新接收事实。

## 实际发布仍未完整组合

DS提供的NewProductionPublishingBridge、gatewayTaskDispatcher及Agent capability已经审查/模块测试，真实WSS+Nginx+HTTP代理字节也有独立证据。但是默认server.New仍使用现有LocalBridge；未注册完整网关程序配置/Agent能力和实际GatewayArtifactUploader，不能宣称统一产品发布闭环完成。

关键接缝：当前gateway dispatcher在任务queued、人工审批已通过时上传，再交Agent dispatcher；`Tasks.CheckExecution`要求running。**不得拿running接口直接套在queued上传阶段，也不得跳过任务/审批或使用SSH修补。** DS-05负责交一套一致的生命周期/逐帧门/当前权限、实际文件通道上传和真实终态；可改新增gateway_*.go，中央已有注册交精确patch。CX不再被当作此网关工件链的等待负责人。

发布的权限/同计划幂等、目标revision、工件taskID、失败阶段补偿身份和DNS部分清理已接收为模块能力；有序补偿/UNKNOWN不得由“HTTP202/queued”冒称成功。真实供应商账号和既有站点接管仍未授权。

## 下一轮

见[WAVE6-R4连续包](WAVE6-R4.md)。CX-05优先文件上传任务化/剩余能力与恢复/平台；DS-05独占网关工件/执行完整链和ACME/cleanup产品组合；CA-05先用已正式接收的UpdateProduct接口补B05实际Agent更新浏览器宿主，再发布/灾备验收。G2、正式资料缺口、物理双机/ARM/硬1C1G及真实外部验收未关闭，不自动启动作者或代发消息。
