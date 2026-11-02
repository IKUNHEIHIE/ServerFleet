# V09-R6-CX-04 / codex-06 · HANDOFF（2026-11-02）

## 身份、目录与基线

- 任务：V09-R6-CX-04；唯一会话 codex-06；审查 codex-review-02。
- 分支：`feature/v09-r6-platform-cx06`；目录 `.worktrees/feature--v09-r6-platform-cx06`。
- 本轮同步基线：`db3cf87d44022be79bbec55eb4012b95f307dac7`；接手 HEAD：`0bad5461`。
- **固定证据的代码 HEAD：`347cefb55492de5c49c7ee70ccc4e887525ea9e4`**，`git status` 空；本文件与 STATUS 的提交在其后，仅文档，不改代码。
- 未合 main、未推送、未 SSH、未使用真实外部秘密/云资源、未启动其他 Agent。

## 提交清单（0bad5461 之后）

| SHA | 说明 |
|---|---|
| `4e3c7bd` | feat(updates): 可信产品更新绑定 schema 指纹/审批摘要、有界健康读取、失败路径加固 |
| `c8500f0` | feat(files): Agent 实现 skip/overwrite/rename 冲突策略 |
| `9ac640a` | test(files): services 黑盒期望改为接受三种受审策略（需总控复核） |
| `c3b454e` | docs: 登记黑盒期望变更 |
| `8af6f57` | docs: 冲突策略根/安全证据 |
| `5ceadcc` | test(updates): 审批后 schema 漂移 UNKNOWN、残留 update 标记拒启动 |
| `2b29407` | feat(files): overwrite/rename 贯通真实统一任务 worker，实际路径回写 item |
| `cc54686` | test(files): 17 项请求创建前 400 |
| `347cefb` | test(files): rename 32 次耗尽失败关闭；收尾依赖登记 |

第一增量 `0bad5461`（文件 CLI 私有部署装配）仍为起点基线，未改写。

## 固定 CLEAN SHA 组合证据

`node tests/poc/v09/platform/run-wave6-r3.mjs --final` 在 HEAD `347cefb`、**workingTree=CLEAN** 下运行：

- 2 PASS / 0 SKIP（UID1000）：
  - `TestR3TrustedFileDeploymentActualControllerAndTwoAgentCLI`：65,000 字节/SHA、两节点父 task/transfer 终态、重复 skip、overwrite 原地替换、rename `source (1).bin`、17 项 400、未登记路径 400、不安全启动拒绝。
  - `TestR3ActualProductAgentHTTPApprovedSignedInstallAndCompatibleRollback`：1.0.1 安装完成、1.0.2 版本不符兼容回滚、审批后 schema 漂移 `unknown`。
- 工件 SHA256：controller `2598fbd26669c7790c1ca75d67d13830864ec4ed8644b2604a6ef82fb335bba7`；agent `78f9d78e6da42744015df8fd347da4425eba17dd3b0e792df62b4be35256d4cc`；v1 `31a85010747ca762e8a8ee6baad33dbc7c3cd0b91e3c0e01cea835c521a2349c`；v2 `61f50f33d9ec48ed2e1d3b95f8f8a5d02e97f608fe090d6d54077c1ab5b58a01`；test `b34fc9b8477ca4f454758e056099887af5d305ba2e8fd579428be787a40454e6`；native.log SHA256 `84ad12e2111de03db3339c32d1421d2d0edfd6304f1f5432eb964f9695f22d91`。
- 证据：`reports/wave6-r3/final-clean-result.json`、`final-clean-native.log`。
- 说明：CLEAN 指作者树干净；组合仍需两份**未接收**中央候选 overlay（`controller-file-cli.patch`、`controller-product-updates.patch`）编译实际 Controller。不是已集成 main 的发布准入。

## 环境与根检查（本轮真实执行）

| 命令 | 结果 |
|---|---|
| `npm ci` | **PASS**（exit 0）；`reports/wave6-r3/npm-ci.log` |
| `npm run tools:install` | 首次外部 Go proxy 连接失败（`tools-install.log` 保留）；按固定版本模块缓存以 `GOPROXY=file:///.../.cache/gopath/...` 重试 **PASS**（`tools-install-cache.log`），未改 pins/校验 |
| `npm run check` | **exit 0**；test2json 统计 pass 2037 / skip 30 / fail 0；Web 341 PASS；契约/policy/git PASS；`root-check-final3.log` |
| `npm run check:security` | **exit 0**；npm 0 漏洞、govulncheck 无已知漏洞；`root-security-final3.log` |
| `go test ./tests/architecture` | PASS |
| 原生 Linux 模块 | `internal/updates`、`internal/files`、`internal/transport`、`apps/agent` 全 PASS（含新增冲突策略、rename 耗尽、schema 指纹事务内读取、残留标记拒启动） |
| staticcheck | Windows 与 Linux 0 问题 |

保留的首次失败/异常：tools 代理失败；`root-check-after-files.log`（services 黑盒旧期望）；`product-schema-expectation-fail.log`（期望 failed、实测 unknown）；最早的两次 CLI 404 夹具路由错误日志。

## 中央候选（待 codex-review-02 接收）

1. `controller-file-cli.patch`：`-file-deployment` 与管理 TLS 必需、PKI/DB 前失败终止。
2. `controller-product-updates.patch`：`Config.UpdateProduct`、产品 worker/HTTP、启动/关闭生命周期。
- 两者 `git apply --check` 均通过；未改中央源码/Proto/gen/DDL 公共序列/根锁/CI。
- `tests/integration/v09/services/file_transport_blackbox_test.go` 的期望已从“overwrite/rename 继续拒绝”改为“三种受审策略接受、其余拒绝”，属 services 套件，**需总控明确复核**。

## 剩余风险与未完成项（逐项依赖）

1. **恢复（未开始实现）**：没有两目录/双 Controller 的实际新 CA/信任世代与旧 fence 证据；现有 RuntimeGuard/QuarantineLatch 只是入口门，上轮 verifier 含 double。缺正式 PKI-OPS/PRIV/WC 原文；未伪造 verifier/确认短语。这是本包未完成的主要范围。
2. **Xray/插件（本轮无新外部工件工作）**：仓库内只有声明式 `plugins/xray-manager` Go 源与测试，已被本轮全量 `npm run check` 覆盖；本轮未获取/审查任何外部 Xray 二进制工件（历史 26.10.29 获取失败、安全 FAIL/Runtime 未准入保持），未运行、未改 WASM/Eino。
3. **上传走统一任务/审批**：`CompleteUpload` 仍在校验摘要后直接走 transport；需新增 `ActionUpload` worker、`UploadSubmitter`、上传状态查询（OpenAPI 候选）+ 中央 `server.go` 注册 patch。
4. **多目标分发**：模型仅单 `DestinationNodeID`；需公共 DTO/OpenAPI/Proto 与迁移候选，由总控登记。
5. **断连续传跨主控重启**：需持久化 item offset/绑定并冻结恢复语义；未实现。
6. **真实 ENOSPC/inode**：只有 RLIMIT_FSIZE（EFBIG）专项；WSL 普通用户无法挂载小文件系统，需总控提供隔离受限挂载；未把 EFBIG 冒充 ENOSPC。
7. **物理双机、ARM、1C1G、外部账号、浏览器 B01/B04/B05/B06、发布准入**：外部资源/GM 范围，未由本会话完成。
8. **同计划并发 apply、修改类任务串行、Close 仅终止自有进程**：代码已有相应门，但本轮新增产品链未补专门的并发/关停负向测试。

## 复现

```powershell
. ./scripts/env.ps1
node tests/poc/v09/platform/prepare-wave6-r3-product.mjs
node tests/poc/v09/platform/run-wave6-r3.mjs --final
npm run check
npm run check:security
```

不要运行 R2 历史 prepare/overlay/补丁脚本；不要重放已接收的 24 项迁移或五份 CX-03 补丁。