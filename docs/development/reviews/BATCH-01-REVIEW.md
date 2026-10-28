# 首批独立审查：G2-R01 / N01 / W01

日期：2026-10-29，Asia/Shanghai。审查会话：codex-review-01（总控）。本轮授权为审核三项交付、验证、接收可合入工件并分配下一步，不启动其他会话、不推远程、不访问真实节点。

## 结论与提交

| 任务 / 提交 | 范围与测试 | 独立结论 | 集成处理 |
|---|---|---|---|
| G2-R01 / `8821555928326a206b11d5bba921b33f468de3fe` | 只新增指定三目录工件；185 操作矩阵、独立候选、26 个 Node 静态/模型测试，3 个 Go 顶层测试与13个 oneof 子测试通过；13项归档证据哈希一致 | **ACCEPT_REVIEW_ARTIFACTS** | 仅接收候选/审查资料及隔离测试，显式接入根契约检查；不采用运行时双 profile，不批准 API/Proto 冻结 |
| N01 / `04c45e97c76df189fa4f94b0de8034df84529442` | 改动均在 internal/nodes，ports.go 未变；已有模块测试和 Go/policy 通过；覆盖率 nodes 94.9%、testkit 100% | **CHANGES_REQUESTED**：3 个 P2 边界问题 | 不合入 main，保留原分支/Worktree，分配 N01-R1 |
| W01 / `c6302a90bd208195c24233ad790bcf4f887a68e0` | 只新增 features/nodes；35 测试通过（原3+新增32），类型/构建通过，未改 App/main | **CHANGES_REQUESTED**：2 个 P2 数据质量问题 | 不合入 main，保留原分支/Worktree，分配 W01-R1 |

覆盖率、已有测试通过不等于满足全部边界。下列探针实际调用交付实现，和候选测试中的期望模型区分。

## N01 必须修复

### N01-R01 / P2 · 最后一次授权检查吞掉取消

位置：`internal/nodes/visibility.go:39–44`（N01 Worktree）。每次循环只在调用 Authorize **之前**查 ctx.Err，调用返回的所有错误都 continue。若取消发生在唯一/最后节点的 Authorize 内，循环结束后返回 `out, nil`。

复现：一个节点，注入 Authorizer，在 Authorize 内调用 cancel 并返回 ctx.Err。实际：

```text
cancel-during-final-check: len=0 returned_error=<nil> context_error=context canceled
```

期望：返回 context.Canceled/DeadlineExceeded，不把被取消的读取当作成功空列表；VisibleFilter/VisibleClassify 同样传播。至少覆盖取消/期限发生在最后调用内，且 Authorize 错误被包装的情况。

不要求本轮凭空制定 auth 全部错误类型。当前接口没有区分拒绝与基础设施失败，其他 Authorize 错误一律过滤可能隐藏服务故障，仍是后续 auth owner 的明确设计点，不能擅改 ports.go。

### N01-R02 / P2 · 普通分组改名没有并发前提

位置：`internal/nodes/group.go:99–114`。ReplaceMembers 有原子 revision 检查，但 Rename(name, notes) 无 expectedRevision，两名管理员读同一版本后先后改名，第二次无条件覆盖第一次。mutex 仅串行写入，不能发现过期客户端。

实际：

```text
stale-rename: editor_B_read_revision=1 final_revision=3 final_name=editor-B error=<nil>
```

期望：同一锁内检查预期版本并变更，过期版本拒绝且不改状态。R1 明确授权修改本分支新引入的 Rename 内部方法及调用测试，不修改原 ports.go、公开 API 或共享模型文件。保留正确版本无变更时的 no-op 行为；不要在调用方用 Revision() 后再 Rename 的非原子组合修补。

### N01-R03 / P2 · 非有限指标被宣布可用

位置：`internal/nodes/node.go:54–61`。Usage 只验证状态/时间，不能拒绝 NaN 或正负 Inf；用新鲜在线样本可以得到：

```text
non-finite-metrics: ok=true cpu=NaN memory=+Inf
```

期望：非有限指标不能伴随 ok=true 返回；至少独立覆盖 CPU/内存的 NaN、+Inf、-Inf，保留真实0与缺失区分。0–100 的具体域约束须遵照明确的本地百分比定义，不能把原始错误读数静默夹成0/100。

## W01 必须修复

### W01-R01 / P2 · 缺失/无效采样时间仍显示有效读数

位置：`candidate-adapter.ts:65–79` 与 MetricCell。数值只需 finite，sampledAt 仅做非空字符串处理；缺失或非法时间时仍显示百分比。普通在线旧样本也无明确过期状态/可见时间，只有 title 可能带采样时间。

对 online 合成节点调用 toNodeListItem，再 formatPercent，实际：

| 输入 | 现有输出 |
|---|---|
| `{cpuPercent:37}` | `cpu={percent:37,sampledAt:null}`，显示 `37%` |
| `sampledAt="not-a-time"` | 保留非法时间，显示 `37%` |
| `sampledAt="2000-01-01T00:00:00Z"` | 显示普通 `37%`，未标明陈旧 |

期望：缺时间、非法时间、未来或超过策略阈值的样本不能当作有效当前数据。新鲜度策略/当前时间由显式输入提供并在测试注入，不把 G2 候选采样周期或日期硬编码为生产常量。可以按无有效数据处理，或明确显示陈旧/时间；不能只依赖鼠标悬停说明。真实新鲜0%仍须显示。

### W01-R02 / P2 · 百分比域越界仍作为普通指标

位置：`candidate-adapter.ts:44–46`、`view-model.ts:48–51`。MetricReading 注明0–100，但只检查 finite；现有测试还把130%当作期望。实际 CPU=-10、内存=150 输出 `-10%` / `150%`。

期望：按照该局部模型0–100的定义明确标记无效/缺失，不能夹成0/100，也不能作为普通可用百分比。若将来指标定义不同，必须先改明确模型/单位，不能默默接受。补适配和组件负向测试，保留合法0/100。

## G2-R01 的接收边界

- 只新增工件，原候选、原始资料、现有 ports、Proto/生成代码、兼容基线、锁文件未改；已有范围纪律符合要求。
- 本轮独立重跑候选入口和根契约检查。结构/lint、185矩阵、26个模型测试和真正的 Proto 往返/负向检查通过；不把测试内 authorize/precondition 等样例当作真实后端实现。
- 所提 profile 头、24h幂等TTL、ETag具体编码、会话期限/CSRF恢复等均未作为运行时决定采纳。当前没有已部署正式客户端，后续设计应先评估首发单一安全契约，避免为旧设计草案维护两套运行时行为；任何版本/兼容路径不得削弱 CSRF/RBAC/审批。旧候选仍原样留作追踪。
- 未采纳 shared-file-changes.patch 全包；总控仅手工接入新的显式检查和本轮任务状态。原目录里的“根检查不自动发现”是交付时说明；自本次接线起根 check:contracts 会显式执行 r02/run.mjs。
- 缺失 PRIV/WC/PKI-OPS/UPDATE 原文、真实安全/持久化/传输/低配 PoC 仍未补齐。G2 未冻结，没有准许真实高权限调度。

## 复验与环境

实际基线与分支使用 git/worktree 检查，三个开发工作区均无被审查人未提交的跟踪变更。N01/W01 复现只产生忽略的 reports 文件，没有改作者源文件。

| 检查 | 本轮命令 | 结果 |
|---|---|---|
| N01 模块覆盖 | `go test -count=1 -cover ./internal/nodes/...` | PASS：94.9%/100% |
| N01 编译/静态/边界 | `npm run check:go`、`npm run check:policy` | PASS |
| W01 类型/35测试/构建 | `npm run check:web` | PASS |
| W01 边界/CI lint | `npm run check:policy` | 首次缺 actionlint 而 BLOCKED；项目级安装固定版本后复验 PASS |
| G2 新候选独立验证 | `node tests/contracts/g2r02/run.mjs` | PASS，26 Node样例、3 Go顶层/13 oneof |
| G2 原工具链回归 | `npm run check:contracts` | PASS |
| G2 归档证据 | 重算 evidence/SHA256.json 的13个条目 | PASS |
| 独立边界探针 | 调用 N01/W01 实现的合成输入 | FAIL：上列5项，需要返修；不以通过原测试覆盖结论 |
| 接收 G2-R01 后集成目录 | `npm ci`、固定工具安装、`npm run check`、`npm run check:security` | PASS：5组检查、新r02显式入口；Go9顶层/17子用例，Web原3测试；npm 0漏洞，govulncheck无漏洞 |
| GitHub/真实节点安全 | 未运行 | UNTESTED；没有远程、凭据或真实节点 |

本报告记录源码审查与离线验证，不是生产安全认证。下一步为 T01-A、N01-R1、W01-R1，详见 SECOND-BATCH-WORK-PACKAGES.md。作者返修后提交新 commit，再交总控复验；不自行合并。
