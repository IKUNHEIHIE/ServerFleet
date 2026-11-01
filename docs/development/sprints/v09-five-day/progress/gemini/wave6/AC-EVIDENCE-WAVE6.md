# AC-01—24 证据矩阵（gemini-06 · Wave 6）

- 会话 `gemini-06`；分支 `feature/v09-r6-console-gm06`；起始基线 `73d5dd4`；本轮增量 `9a04c87`、`1e16ba5`。
- 五列口径（`WAVE6.md` GM-3）：
  - **代码具备**：功能代码在审查基线内存在且有单测锁定（`npm test --workspace @serverfleet/web` → 38 文件/332 项 PASS @ `1e16ba5`）。
  - **本地集成**：本机对真实组合 Controller（Windows 或 WSL 内 linux/amd64，均 `bootstrap.Compose` 全组合）+ 生产构建前端实测，含诚实 fail-closed 断言。
  - **实机**：真实 Linux 主机/Agent/双机条件。本轮"WSL Ubuntu（本机）上真实 linux/amd64 Agent CLI"计入此列并明确标注；awskr 物理机、双架构、1C1G、物理跨机仍为独立收证项。
  - **外部供应商**：Cloudflare/ACME/QQ/SMTP/Xray 官方制品等真实外部系统。本轮全部 NOT RUN（未授权真实凭据/账号，任务单明令禁止）。
  - **发布准入**：签名/发布基础设施与准入流程。本轮全部 NOT RUN。
- 证据命令（本机实测输出，均有控制台记录/截图）：
  - `node tests/browser/v09/run-acceptance.mjs` → 16/16 PASS（下称 **Stub16**）。
  - `node tests/browser/v09/run-live-controller-acceptance.mjs` → 70/70 PASS（下称 **Live70**；步骤号=S##，截图 `evidence/live/live-*.png`）。
  - `node tests/browser/v09/run-positive-success-acceptance.mjs` → 12 PASS / 6 BLOCKED / 0 FAIL（下称 **Pos18**；步骤号=P##/B##，截图 `evidence/positive/positive-*.png`）。
- 诚实缺口不以 501/503/500 反向通过关闭；未列出的缺口与历史登记见 `SHARED-CHANGES.md`。

图例：✅=已收证；⚠️=部分/有条件（附说明）；❌=未收证（附原因/依赖）。

| AC | 代码具备 | 本地集成 | 实机 | 外部供应商 | 发布准入 |
|---|---|---|---|---|---|
| AC-01 节点接入、身份与生命周期 | ✅ `node-add-page.test` 3 项、`node-detail-page.test` 5 项、`node-loader.test` | ✅ Pos18 P03–P08（令牌一次性/审批/签发/WSS+mTLS 在线）+ Live70 S05/S10（零授权隔离/授权后可见） | ⚠️ 本机 WSL Ubuntu 真实 Agent CLI 全链路 ✅；吊销/过期凭据/坏 CA/重连风暴：❌ 需实机长期演练（Pos18 未覆盖） | 不适用 | ❌ |
| AC-02 普通分组与自动分类 | ✅ `groups-page.test` 5 项 | ✅ Live70 S11/S12（创建+expectedRevision 成员更新）；取消不保存由单测覆盖 | ⚠️ 并发成员更新竞态：单测级，未双端实测 | 不适用 | ❌ |
| AC-03 文件互传 | ✅ `files-page.test` 4 项（上传字节/SHA-256/503 诚实分支） | ⚠️ 上传→LocalStaging 完成+校验 ✅（Live70 S65）；两端 Agent 字节互传 ❌ **BLOCKED：B01 `POST /api/v1/transfers` → 500 internal_error（通道未接线；已登记 SHARED-CHANGES §2）** | ❌ 依赖字节通道 + 双机 | 不适用 | ❌ |
| AC-04 服务组 | ✅ `service-groups-page.test` | ✅ Live70 S13/S14（成员集合授权/expectedRevision） | ⚠️ 同 AC-02 | 不适用 | ❌ |
| AC-05 批量任务 | ✅ `tasks-page.test` 3 项 + **`task-detail-page.test` 3 项（本轮新增）** | ✅ **Pos18 P09/P10：真实派发→执行→completed/succeeded（本轮新增的正向证据）** + Live70 S15–S23（审批/SSE/自批 409） | ✅ 本机 WSL Agent 真实执行；⚠️ 分批/金丝雀/TTL：契约缺口 UNTESTED（SHARED-CHANGES §7） | 不适用 | ❌ |
| AC-06 Nginx 网关 | ✅ 网关面板（gateways-page 集成于 Live70 断言） | ⚠️ 安装任务 202 ✅（S27）；路由创建 503 `gateway_unavailable` 诚实（S28，Checker 未注入）；`nginx -t`/实机安装 ❌ | ❌ 总控 awskr 范围 | 不适用 | ❌ |
| AC-07 DNS 管理 | ✅ `dns-page.test` + `compat.test` 双形状 | ✅ Live70 S25/S26（providers/credentialRef 建号、令牌不回显） | ⚠️ Controller 侧逻辑即本列主要对象：✅；真实 Zone 操作 ❌ | ❌ 未授权真实 Cloudflare 账号 | ❌ |
| AC-08 公网证书与内部 TLS | ✅ `certificates-page.test` | ⚠️ 内部分发列表诚实空态（S37）；ACME 端点未入契约（页面显示契约缺口） | ❌ | ❌ ACME 未接入 | ❌ |
| AC-09 监控告警与 QQ/SMTP | ✅ `monitoring-page.test` 5 项 | ✅ Live70 S29/29b/34/35/36 + S63/S64（真实历史+NetProber 探测 200 down 落库） | ⚠️ 探测走本机真实网络栈 ✅；QQ/SMTP 投递 ❌ | ❌ 未授权外部平台 | ❌ |
| AC-10 AI 会话资源授权 | ✅ `ai-pages.test` | ✅ Live70 S41–S43（默认拒绝/零授权/授权交集）；生成 BLOCKED：**B02 503 `MESSAGE_SOURCE_UNAVAILABLE`（模型执行器未接线）** | ⚠️ 授权/审批逻辑属 Controller：✅ | ❌ 真实外部模型 NOT RUN（CX 亦以本地受控 TLS 供应商验证协议，非外部验收） | ❌ |
| AC-11 AI 三级审批 | ✅ full_access 确认流/限时单测 | ✅ Live70 S44–S46（模式切换/一次性审批 canonicalArgumentsHash/受控执行 202 经任务服务） | ⚠️ AI root Shell 真人审批由后端强制（`internal/ai`），前端无豁免入口 ✅ | ❌ 同 AC-10 | ❌ |
| AC-12 插件平台 | ✅ `plugins-page.test`（缺 SHA-256 不可安装/停用入口消失）+ `compat.test` phase→status | ✅ Live70 S47/S48（来源/目录/安装列表；刷新 501 `PLUGIN_CATALOG_SOURCE_UNAVAILABLE` 诚实） | ❌ **BLOCKED：B03 无已签名发行制品**（安装/热启停 UNTESTED） | ❌ 官方制品未发布 | ❌ |
| AC-13 更新中心 | ✅ `updates-page.test`（含备份门控禁用 apply） | ✅ Live70 S50–S52（检查/计划 503 诚实、作业空态）；备份校验真链路见 AC-18 | ❌ **BLOCKED：B05 503 `UPDATE_SOURCE_UNAVAILABLE`**（签名/安装/回滚需发布基础设施） | ❌ | ❌ 未建发布源 |
| AC-14 工作流、用户权限与审计 | ✅ `workflows-page.test` 6 项、`users-page.test`、`settings-audit.test` | ✅ Live70 S57–S62（创建/启停/触发/任务 ID/统一任务中心）+ S30/S56/S68（审计无秘密） | ⚠️ 同 Controller 域：✅ | 不适用 | ❌ |
| AC-15 服务发布向导 | ✅ `publishing-page.test` 4 项 | ✅ Live70 S38–S40（调度资格门控/preview eligible/三阶段计划）+ S66/S67（run/rollback 502 诚实）；正向成功 ❌（B04：本套件无服务组/网关/DNS 数据集，属 DS W6+供应商范围） | ❌ | ❌ 真实 DNS/网关 NOT RUN | ❌ |
| AC-16 服务可用性监控 | ✅ `monitoring-page.test` | ✅ Live70 S63/S64（history 真实落库+probe 200）；SSRF 拒绝 `.internal` 已实证（Wave 4） | ⚠️ 本机真实探测 ✅；断网/恢复窗口区分：⚠️ 部分单测 | 不适用 | ❌ |
| AC-17 安全批量执行 | ✅ 预演级别/取消不冒充回滚（文案+单测） | ✅ Live70 S15（host.root requiresApproval）/S18（自批 409） | ⚠️ 冻结目标/审批失效即拒：Controller 负向单测；熔断阈值/TTL 契约缺口 UNTESTED | 不适用 | ❌ |
| AC-18 节点恢复/主控灾备 | ✅ `backups-page.test` | ✅ Live70 S53（真实 VACUUM INTO 备份经 `backup_integrity` 通过）+ S54/S55（恢复预演 fail-closed、作业保持禁用）；B06 404 `BACKUP_NOT_FOUND` 诚实 | ❌ 第二隔离主控演练 NOT RUN（POC-08/总控 awskr 范围） | 不适用 | ❌ |
| AC-19 安全凭据 | ✅ `settings-audit.test` | ✅ credentialRef 引用不回显（Live70 S26）+ 通知策略 PUT（S29b）；导出 Root 私钥功能不存在于界面 ✅ | ⚠️ 轮换/撤销服务动作：页面有入口，写路径 UNTESTED；**密钥管理状态端点 BLOCKED（CX W6 未交付，SHARED-CHANGES §4）** | 不适用 | ❌ |
| AC-20 流量配额与到期提醒 | ✅ `node-detail-page.test`（404=未配置/非 404 禁编辑）、`legacy-adapter.test` | ✅ Live70 S32/S33（500GB 配置 PUT/GET 一致）+ S34 | ⚠️ 正向套件真实 Agent 心跳/指标 ✅；到期提醒通知渠道 NOT RUN | 不适用 | ❌ |
| AC-21 Xray 安装 | ✅ 入口门控（`plugins-page.test` 联动） | ✅ Live70 S49（未启用时声明式控制台被拦截） | ❌ 同 AC-12（无制品） | ❌ | ❌ |
| AC-22 Xray 配置 | ✅ 声明式渲染器（stub `11-xray-*.png`，不执行插件脚本） | ⚠️ 启用前不可达（门控正确）；真实配置/预检/差异 ❌ | ❌ | ❌ | ❌ |
| AC-23 Xray 用户与受控导出 | ✅ UI 已实现（`xray-page.tsx`） | ⚠️ 同门控 | ❌ | ❌ | ❌ |
| AC-24 Xray 流量 | ✅ `sourceType=proxy_only` 口径与缺口显示 | ⚠️ 同门控 | ❌ StatsService 真实采样 NOT RUN | ❌ | ❌ |

## 结论与剩余项

- **本地集成列**：24 项 AC 中 13 项 ✅、9 项 ⚠️（附诚实缺口）、2 项关键缺口为 BLOCKED
  （AC-03 两端字节互传、AC-05 正向已补齐）；全部缺口引用服务端精确响应，无一以
  诚实拒绝反向关闭。
- **本轮新增的正向证据**：AC-01（真实接入/审批/连接生命周期）、AC-05（真实任务执行
  完成）、AC-20（真实 Agent 指标心跳）——此前所有波次这三处只有"诚实失败"口径。
- **不能宣称完成的项**（依赖矩阵）：两端 Agent 字节互传（CX/DS W6+总控接线）、
  AI 生成/流式（CX W6 模型执行器）、Xray 安装/配置/用户/流量（CX W6+官方制品）、
  发布/补偿成功（DS W6+外部供应商）、更新签名/安装/回滚（发布基础设施）、
  第二隔离主控灾备（POC-08/awskr 实机）、外部供应商四类（未授权凭据）、
  双架构/1C1G/物理跨机（各自收证）。完整 v0.9 不因本轮结果改为"本地阶段完成"。
