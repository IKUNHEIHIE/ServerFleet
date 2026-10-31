# 实际验证与限制

2026-10-29，在 README 所列独立工作目录执行；Windows amd64，Node **v24.22.1**、npm **11.19.1**、Go **1.27.3**、Redocly **2.72.0**、yaml **2.9.1**、Buf **1.75.0**，均沿用固定依赖。使用主工作目录 `scripts/env.ps1` 注入已安装的固定 Node/Go，工具与报告在本工作目录忽略的 .tools/.cache/node_modules/reports。未修改任何依赖文件或原候选。

## 命令与输出

| 实际命令 | 实际结果 |
|---|---|
| `npm ci` | PASS，added 127 packages；npm 提醒 Buf postinstall 尚未列入 allowScripts，未改许可配置；后续实际 Buf 命令全部成功 |
| `npm run tools:install` | PASS，安装固定 protoc-gen-go/staticcheck/govulncheck/actionlint |
| `node tests/contracts/g2r03/build-candidate.mjs --write` | PASS，仅生成 r03 的 YAML、provenance.json、MATRIX.md |
| `npm run check:contracts` | PASS；`69 original files and 3 byte-exact candidates`；原 Core 29路径/34操作、Merged 147/185、Xray 19/23；OpenAPI、Buf lint/format/build/breaking、生成字节一致、往返、真实 Proto 负向检查通过；既有 r02 的26项 JS及 Go/原始 wire 检查通过 |
| `npm exec -- redocly lint --config tests/contracts/g2r03/redocly.yaml api/openapi/revisions/g2-r03/nodes-read-candidate.yaml --format=summary` | PASS；`Your API description is valid`；最终配置无 warning，候选2路径/2操作，真实媒体/Schema 示例规则为 error |
| `node tests/contracts/g2r03/run.mjs` | PASS；结构、显式 lint/schema examples、直接 YAML/来源/矩阵检查、真实负向工具和现有 Go 离线调用全部通过；JS **14/14**，0失败；Go **4个顶层+5个子测试**通过 |
| 入口内 `node --test tests/contracts/g2r03/candidate.test.mjs tests/contracts/g2r03/negative.test.mjs` | PASS；来源 hash/缺失属性/required/旧字段、operationId、query/path参数、认证、错误媒体/状态、矩阵与候选直接比较；不是测试自写 DTO 校验器 |
| 入口内 `go test -count=1 -v ./tests/contracts/g2r03` | PASS；真实调用 auth/nodes，纯拒绝过滤，混合故障 nil 结果；同时复现空集合不读策略和每节点不同策略快照的限制 |
| `npm run check:policy` | PASS；69原始文件/3候选完整性，8项工具测试、Web边界、分支规则、Go模块边界、actionlint |
| `npm run check:security` | PASS；`found 0 vulnerabilities`，`No vulnerabilities found` |
| `git apply --check docs/contracts/revisions/g2-r03/shared-file-changes.patch` | PASS；共享 patch 只做可应用性检查，未应用 |
| `git diff --check`、暂存后 `git diff --cached --check` | PASS；范围复核仅三个授权目录新增，原跟踪文件无变更 |

结构计数及 JSON 测试结果见 [RESULTS.json](RESULTS.json)。最终候选 SHA-256：`dcb499b108fc6eab67412e08833100f9cb13c93d843863661faab56535180cfd`。

## 真实负向和兼容样例

独立入口把坏候选放进忽略的 reports/g2r03-negative，不改交付 YAML。以下均实际调用固定 Redocly CLI，预期退出1；测试验证非零退出和对应 rule，不以自写 predicate 冒充 OpenAPI 验证。

| 输入 | 实际诊断 |
|---|---|
| 不存在的 schema ref | no-unresolved-refs，1 error；结构检查亦拒绝 |
| 两个 GET 重复 operationId | operation-operationId-unique，1 error；结构检查亦拒绝 |
| 删除 nodeId 路径参数 | path-parameters-defined，1 error；结构检查亦拒绝 |
| 16个非法载荷：缺ID、online类型错、未知状态、online/status两向矛盾、revision=0/缺失、CPU越上界/错误类型、内存越下界、错误采样时间、详情lastSeenAt=null/多余字段、Problem状态与HTTP不符、旧Error缺code | **32个** no-invalid-media-type-examples 错误；每个样例均有真实诊断 JSON pointer，逐项断言16/16被拒绝 |

正例由真实工具验证：旧列表无状态/revision/时间、0与100、null/省略指标、五种状态、空items、详情原required全部满足。另实际通过格式正确的未来时间、离线历史数值及旧开放 NodeSummary 上的扩展名碰撞样例；这些 PASS 仅说明旧/新 shape 的真实边界，分别证明 schema 不判断 freshness、旧数值不等于当前值、可选扩展仍可能收窄原任意扩展。

证据已留在 [evidence](evidence)：候选 lint、14项摘要、3类结构拒绝输出、32项 schema 错误的规则/位置、合法形状与旧扩展冲突输出、真实 Go 调用及 LIMITATION 日志。归档文本日志仅清理行尾空白和尾部空行，清理前后 hash 留在 RESULTS.json；完整原运行日志仍在忽略的 reports，提交中的紧凑证据和来源 hashes 可供重跑。

## 首次失败和修正

首次独立入口：**11 PASS / 1 FAIL**，入口退出1。实际 Redocly 已拒绝15个坏载荷并报告31个 schema 错误，但 harness 在 `--format=stylish` 的简洁输出中搜索样例名称，该输出只有行列而没有名称，导致测试误判。保留 [初次结果](evidence/initial-results.json) 和 [完整失败输出](evidence/initial-contract-tests.log)。随后改用真实 `--format=json` 的 ruleId/severity/location.pointer，逐样例核对；补旧 Error 缺code和来源/旧扩展兼容证据后，最终14/14通过。没有删除负向用例或压低规则严重性。

首次局部 Redocly 配置继承根配置时，继承文件的 apis 字段产生1条配置 warning；改为局部 extends: spec，并显式保留根4条错误规则和新增示例规则，未修改根配置。手写共享 patch 首次可应用性检查因 hunk 行数错误报 `corrupt patch`，修正计数后重验通过；没有将 patch 应用到共享文件。

Redocly 示例规则和 YAML 解析选项已通过 Context7 官方库文档核对：`/redocly/redocly-cli`、`/eemeli/yaml/v2.9.1`；实际运行使用仓库锁定版本，没有升级工具。

## 未验证/未批准

根 `check:contracts` 当前接入原3候选和 r02，**不自动发现 r03**；本轮独立入口是新候选的通过依据。共享接线 patch 仍待总控批准。

HTTP服务、可信Cookie/Bearer身份、撤销、授权后字段/计数、空库存入口预检、整次政策快照与读取原子性、跨主体缓存、实际旧客户端以及真实指标时效均 **UNTESTED**。离线 Go 探针 PASS 包含对当前缺口的复现，不能解释为缺口关闭。未运行服务器、网络连接、真实存储或任务；未做协议冻结或性能验收。D01–D08 仍 OPEN，见 DECISIONS.md。

交付仅本地提交，完成后停止，交 `codex-review-01` 复审。
