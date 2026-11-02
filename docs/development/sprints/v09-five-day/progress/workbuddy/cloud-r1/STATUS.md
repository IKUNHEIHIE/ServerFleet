# V09-CLOUD-WB-01 · STATUS

- 任务 ID：`V09-CLOUD-WB-01`
- 唯一会话：`workbuddy-01`
- 分支：`feature/cloud-console-wb01`
- 工作目录（云端普通用户私有）：`/home/ubuntu/ServerFleet-WB01-parent/ServerFleet-WB01`
- 统一起点：`1ca79072232bfdcee2b9f4582ac020d4430d0c2b`（与派发基线一致，远端 main 未漂移）
- 审查与最终合并：ChatGPT（主控）
- 承接：V09-R6-GM-05／codex-arts-01 的剩余范围；旧包保留历史并标作转交，不宣称已完成

## 环境实测（首次开工记录）

| 项 | 实测值 |
|---|---|
| 身份 | `ubuntu` uid=1000，工作根 `/home/ubuntu/ServerFleet-WB01-parent`，属主 ubuntu，`ServerFleet-WB01` 及运行态目录 0700 |
| Go | `go1.27.3 linux/amd64`（私有安装 `~/.local/opt/go`，`GOTOOLCHAIN=local`） |
| Node / npm | `v24.22.1` / `11.19.1`（与 `scripts/tool-versions.json`、TOOLCHAIN 记录一致） |
| 浏览器 | Google Chrome 144.0.7559.132（headless + CDP，`SF_BROWSER` 显式指定） |
| 网络限制 | `proxy.golang.org`、`github.com` 直连不稳定；改 `GOPROXY=https://goproxy.cn,direct` 后模块下载成功 |
| 平台差异 | 既有正向套件依赖 Windows `wsl.exe`；本任务新增原生 Linux 专项脚本，不改动原套件 |

## 检查点

| 检查点 | 内容 | 结果 |
|---|---|---|
| CP-1 | 测试宿主的可信程序注入：`server.Config.UpdateProduct`（`-product-update-config` 私有 JSON），`SchemaFingerprint` 由宿主从真实库读取；宿主退出时闭合 `Server.Close()` | 构建/vet 通过；专项 P09 通过 |
| CP-2 | Agent 组件本地签名更新浏览器专项（原生 Linux）：check → 冻结 plan → 异人审批 → apply → 真实版本/健康 → 终态 → 失败拒绝 → 重启持久 | **20 PASS / 0 FAIL**（退出 0） |
| CP-3 | B06 灾难恢复前置条件探测（生产同构宿主，不注入恢复执行器）：备份生成与校验、预览、作业创建、作业读回、两条负向、页面实况 | **11 PASS / 3 BLOCKED / 0 FAIL**（退出 0） |

CP-2 提交：`8b298dec70baf74c2a842e37416e5c0712e8fb90`（本 PR 首个提交；文档回填提交随后）。
PR：`#1`（Draft，base `main`），head 以远端 `feature/cloud-console-wb01` 最新提交为准。

## 本检查点已证明（限定范围）

1. 真实发布构建：两个 `apps/agent` 构建（`-ldflags -X main.agentBuildVersion`）字节不同，SHA 分别记录在证据 JSON。
2. 真实安装：槽位 `active` 字节等于目标发布构建 SHA-256，非状态伪造。
3. 真实进程替换：`health.json` 的 `version` 1.0.0 → 1.0.1，`pid` 改变，且进程由 `internal/updates` 的自有进程监督器启动（`-serve -state-dir … -controller-url … -root-cert …`）。
4. 浏览器驱动：管理员经页面 check/plan/确认文本/风险勾选发起 202；另一管理员（异人）在独立浏览器会话审批同一 `planDigest`；服务端拒绝自批。
5. 终态与持久：作业 `completed` 且 `taskId` 与审批任务一致；重启宿主后恢复 1.0.1，作业终态仍在。
6. 负向：工件与清单 SHA 不一致的 1.0.2 在**计划阶段**即被拒绝（不产生作业），运行中的产品与槽位字节未变。

## B06 阻塞位置（CP-3 实测，可复现）

`apps/web/src/features/backups/backups-page.tsx`（路由 `/backups`，「备份与恢复」）**已经存在**，消费的正是 `/backups/{id}/verify`、`/disaster-recovery/preview|jobs` 四个端点，页面本身不缺。缺的是它背后的三层能力，且均已被实测定格：

| 观测 | 端点 | 实测 | 性质 |
|---|---|---|---|
| 备份校验 | `POST /api/v1/backups/{id}/verify` | `200`，`verified=true`，`backup_integrity=passed` | **可用**（真实 `VACUUM INTO` 备份，含 `schema_migrations` 账本） |
| 恢复预览 | `POST /api/v1/disaster-recovery/preview` | `200` 但 `eligible=false`；未通过 2/4 项：`target_controller_identity/target_identity_not_observed`、`old_controller_fenced/fencing_evidence_unavailable` | **产品侧关闭**：当前发行版无可信 Controller 身份观测，勾选框不构成 fence 证据 |
| 创建作业 | `POST /api/v1/disaster-recovery/jobs` | `501 RECOVERY_EXECUTOR_UNAVAILABLE` | **产品侧关闭**：`server.Config.RecoveryExecute` 无生产装配点 |
| 作业读回 | `GET /api/v1/disaster-recovery/jobs/{jobId}` | `404 RECOVERY_JOB_NOT_FOUND`（无条件） | **契约缺口**：即便作业创建成功也读不回状态与阶段 |
| 未知备份 | `POST /backups/{id}/verify` | `404 BACKUP_NOT_FOUND`，响应无路径/SQLite 细节 | 负向通过 |
| 未认证 | `POST /disaster-recovery/preview` | `403 NOT_AUTHORIZED`，不返回任何检查项 | 负向通过 |

因此：**浏览器端到端恢复在当前 main 上不可能通过**，不是因为宿主或页面缺件，而是预览恒不合格 + 执行器未装配 + 作业不可读回。本任务**没有**注入自制恢复执行器来「跑通」它——那会把验证对象换成我写的适配器，而不是 AgentMore 的产品实现。

脚本：`tests/browser/v09/run-recovery-prerequisite-probe.mjs`（`R00`—`R13`）。
证据：`evidence/CP3-recovery-probe-results.json`、`evidence/CP3-backups-page.png`、`evidence/CP3-anonymous-denied.png`。

## 明确未完成（不得据此关单）

- Controller 组件升级、在线可信发行源、分批/金丝雀：未实现，未验证。
- 完整 B05「更新浏览器/部署装配」：本检查点只覆盖 Agent 组件本地签名来源；部署装配（Controller 组件）仍缺。
- **B06 实际恢复**（实际执行、新权威、旧主控 fence、旧信任代次拒绝、撤销/过期任务不复活）：**UNTESTED**。阻塞点见上表后三行；需 AgentMore 的执行器语义经主控合入，并由 `RecoveryExecute` 在生产装配。第二 Controller 健康、独立 DB/PKI 只算隔离基础，不当灾备成功。
- B04 完整发布、B03 Xray、AC-01—24 五层证据矩阵、Live70/Stub16/严格正向总套件：本检查点未重跑，不改变其历史结论（31 PASS / 4 BLOCKED / 0 FAIL）。
- 严格正向 `--require-complete` 的 4 项 BLOCKED 仍在，未降门槛。

## 待办（本包内可独立继续）

- 把本专项接入严格正向总套件（原生 Linux 通道），并保留原 Windows/WSL 通道不变。
- 继续 Web 侧其余页面的权限/错误/可访问性项与剩余负向。
