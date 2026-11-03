# V09-CLOUD-WB-02 进度（检查点 CP1）

唯一会话：workbuddy-02　分支：`feature/cloud-console-xray-wb02`　产品起点：`51e0ee57e66493892eb5f5168b9e0690b53f3661`
环境：Linux x86_64 容器、root、**独立普通用户私有目录外的隔离目录**（见“环境限制”）、Go 1.27.3、Node 24.22.1、npm 11.19.1。

## 1. 本检查点交付（可运行、已实测）

| # | 内容 | 位置 | 证据 |
|---|---|---|---|
| 1 | 异步上传任务 Web 闭环（202/dispatching/taskId、刷新读回、取消、403/409/大小/摘要/UNKNOWN 文案） | `apps/web/src/features/files/upload-queue.ts`、`files-page.tsx`、`lib/api/dto.ts`、`lib/api/endpoints.ts` | `npm run check:web` 40 files / **361 tests PASS**（其中新增 13 项：11 纯逻辑 + 2 页面闭环） |
| 2 | Xray 配额/进程重置业务状态机 | `plugins/xray-manager/quota.go` | `go test ./plugins/xray-manager/` 新增 9 项 PASS |
| 3 | Xray 节点选择/批量审批/波次计划与 plan digest | `plugins/xray-manager/batch.go` | 同上，新增 6 项 PASS（含 digest 稳定性、目标漂移、接管审批） |
| 4 | B03 工件/准入差距包 | `ARTIFACT-ADMISSION.md` | 官方 API 抓取 + 工件本地 SHA256 与 digest 一致（PASS）+ govulncheck `-mode=binary` exit 3 / 3 项（FAIL）；**未执行工件** |
| 5 | 中央接缝精确候选（未自行改公共文件） | `CANDIDATES.md` | — |

**未做（明确不做假）**：Xray 安装/部署/重启仍未接线（`XRAY_RUNTIME_UNAVAILABLE` / 任务提交缺失），因为所需的统一任务动作类型属于 `internal/tasks`（AM 独占）与受控 Host，本包不得改；未运行任何 Xray 工件；未把 preview/健康 200/202 当成功。

## 2. 真实命令与结果（本次执行）

```
$ npm ci                     → added 127 packages（npm 11.19.1，Node 24.22.1）
$ npm run tools:install      → protoc-gen-go / staticcheck / govulncheck 1.9.0 / actionlint 安装成功
$ npm run check:web          → Test Files 40 passed (40) | Tests 361 passed (361)
$ npm run check:security     → npm audit: found 0 vulnerabilities；govulncheck ./...: No vulnerabilities found
$ go test ./plugins/xray-manager/ → ok（新增 batch/quota 全 PASS；3 项真实二进制测试保持 SKIP：未提供固定二进制）
$ npm run check:go           → FAIL（exit 1），详见 §3
```

Go 测试顶层统计（`reports/go-tests.log`）：**899 PASS / 19 SKIP / 7 FAIL**（含子测试 2113 PASS / 21 SKIP / 7 FAIL）。

## 3. 失败与阻塞（不隐瞒、不吞掉）

### 3.1 环境性既有 FAIL（与本包改动无关，已在纯净基线复现）

| 用例 | 包 | 复现 |
|---|---|---|
| `TestWave6IsolatedSQLiteRestoreKeepsQuarantineAndRevocations` | `tests/integration/v09/platform` | 在 `git worktree` 纯净 `51e0ee5` 上同样 FAIL：`recovery: reconciliation rejected 0` |
| `TestAgentFileBytesResumeHashAndIndependentReadGate` 等 6 项 | `apps/agent` | 纯净基线同样 FAIL：`agent: file capability rejected or outcome unknown` |

结论：7 项 FAIL 全部来自**本容器的 root/权限环境**（文件能力与恢复执行器在本沙箱被拒），不是本包代码引入；`51e0ee5` 在项目 CI 上是 902 PASS/7 SKIP/0 FAIL。按规则**不得**以跳过或放宽门槛消除，原样保留并交由主控在合格 non-root 环境复跑。

### 3.2 本包 BLOCKED

| 项 | 状态 | 原因 |
|---|---|---|
| B03-01 固定版本 `v26.10.29` 可核 | BLOCKED | 上游已撤版（404）；官方 v26.10.31 说明原文“v26.10.29 让位，已被撤版” |
| B03-02 上游签名/来源证明 | BLOCKED | 上游仅提供未签名 `.dgst`；无 `.sig/.asc/.sbom/.intoto`；GitHub attestations 404 |
| B03-03 工件准入（linux/amd64） | BLOCKED | 校验和一致（PASS），但**无签名**且二进制扫描 **FAIL**（exit 3 / 3 项） |
| B03-04 v26.10.31 二进制扫描 | **FAIL** | GO-2026-6866、GO-2026-6860（x/net v0.61.0）、GO-2026-5932（x/crypto openpgp） |
| B03-05 arm64 运行 | UNTESTED | 无 ARM 实机；交叉构建不算运行 |
| B06-01 Xray 安装/部署/重启真实链路 | BLOCKED | 缺统一任务动作类型（AM 域）与受控 Host 准入；本包不改 `internal/tasks` |
| B05-x 更新/发布/恢复真实宿主 | BLOCKED | AM/HW 正式接口未合入本分支基线（`UpdateProduct` 存在，但宿主验收需真实组合） |
| AC 严格模式 | BLOCKED | 需最终组合 SHA + 非 root 环境重跑，本环境 7 项既有 FAIL 使严格模式必然非零 |

## 4. 环境限制（如实记录）

- 工作目录为容器内 root 私有目录，**不是**普通用户 `0700` 目录；因此 agent/恢复类真实执行测试在本环境不可作为验收证据。
- 到 `github.com:443` 的 git 传输不可用（连接超时），`api.github.com` 可用；源码与工件均经 API/tarball 获取，最终提交经 Git Data API 建立（见 PR 说明）。
- 未访问生产/VPS/真实供应商/云 API；未使用 WASM/Eino；未启动任何 Xray 进程。

## 5. 剩余（整包未完成）

1. Xray 生命周期真实接线（待 `CANDIDATES.md` 的 C-01/C-02 被接收）。
2. 工件准入（待主控就版本与“仅校验和”模型决策；随后补 v26.10.31 二进制扫描 + arm64 环境）。
3. 其余 Web 页面负向（服务组/证书/DNS/通知/监控/配额/工作流）与 AC-01—24 严格矩阵、浏览器 Live/Stub 复跑（本检查点只做了组件级测试）。
4. 更新/发布/恢复浏览器真实宿主验收（依赖 AM/HW 主线接口）。

## 6. 对主控的请求

- 确认 B03 版本决策：`v26.10.31`（最新稳定，含 MPTCP 安全修复）是否作为新固定目标；若同意，需要一次合格环境下的二进制扫描与 arm64 运行窗口。
- 接收 `CANDIDATES.md` 的 C-01（任务动作类型）后，本包再实现安装/部署/重启的真实提交路径。
