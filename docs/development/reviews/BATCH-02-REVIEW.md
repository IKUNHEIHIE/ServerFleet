# 第二批独立复审：T01-A / N01-R1 / W01-R1

日期：2026-10-29（Asia/Shanghai）；审查人：总控 codex-review-01。复审依据用户提供的精确提交，保留旧提交历史；不启动其他 Agent，不推送远程，不连接真实节点。

## 决定与范围

| 任务 | 精确被审查 HEAD | 独立结果 |
|---|---|---|
| T01-A / codex-01 | `fd918d13a9bde9d1cec9d6cd251724fa8882e332` | 允许接收离线语义校验候选。仅4个 internal/transport 新文件，ports.go/Proto/gen/契约/锁文件未变；不证明mTLS、授权或执行安全 |
| N01-R1 / deepseek-01 | `0fa6f8878feeba69fcdae5a423b02ef2b1a79240` | 上轮3个P2问题闭环；接收原N01领域实现及返修。保留原04c45e9、返修b848104和期限回归0fa6f88，未重写历史 |
| W01-R1 / gemini-01 | `86d472265f2e75e6a49be862d2d76cfe7758fe05` | 上轮2个P2问题闭环；接收原W01组件及返修。保留c6302a9；App/main、全局样式、共享组件、依赖均未改 |

三个作者工作区在复审时均无跟踪文件未提交变更。总控只在忽略的 reports 下运行探针/保存日志，未修改作者源码；组合验证在独立 integration/batch02-review Worktree 进行。集成无冲突；只有全量检查通过后才前移main。

## 上轮5个问题的关闭证据

| 问题 | 本轮代码与独立复现 | 状态 |
|---|---|---|
| N01-R01 最后授权吞取消/期限 | Authorize后及返回前检查ctx；errors.Is传播包装取消/期限。独立最后节点取消返回context.Canceled。作者另覆盖真实期限到期及VisibleFilter/Classify；相关用例重复20次通过 | CLOSED |
| N01-R02 改名覆盖过期写入 | Rename(expectedRevision, name, notes)在同一mutex内比较并更新。独立旧版本改名被拒绝且状态不变；32个并发同版本写入仅1成功/31冲突 | CLOSED |
| N01-R03 非有限指标可用 | Usage拒绝CPU或内存的NaN/+Inf/-Inf；独立6种输入均拒绝，真实0/0仍可用 | CLOSED |
| W01-R01 无效/过期时间 | 明确注入now/maxSampleAgeMs。独立缺时间、非法时间、未来、陈旧输入各得missing/invalid/stale；陈旧读数通过组件可见文本标记和采样时间，不依赖title | CLOSED |
| W01-R02 越界百分比 | 局部0–100范围外为invalid且无数值；独立-10/150无普通百分比，新鲜0/100保留；适配与组件均有负向测试 | CLOSED |

独立原始探针输出见 evidence/batch02-node-probes.log、evidence/batch02-metric-probes.json；复审结果摘要见 BATCH-02-RESULTS.json。

## T01-A 源码审查

- ValidateEnvelope是真正可调用的纯函数；测试调用实际实现，并覆盖13种消息真实marshal/unmarshal后校验，不是测试里另写一套放行模型。
- 上下文、时钟与所有资源限制显式注入；没有默认生产帧/采样/心跳参数。空输入、typed-nil、方向/阶段/版本、ID、枚举、允许动作、参数大小/JSON/摘要形状、offer期限等有负向测试。
- 错误仅含固定Code/Field，不回显输入；输入消息、允许列表只读。有效拒绝ControllerHello、UNKNOWN真实终态、未完成StateSync记录均没有被冒充执行成功。
- offer期限只限制新offer，不用于抛弃迟到执行结果；校验通过不形成身份、资源、审批或派发许可。
- 本轮没有发现需要阻止此次离线候选接收的新增问题。握手ID/正序号、minor精确匹配、JSON object等仍是局部候选约定，不据此宣布协议冻结。

## 实际复验

以下均使用项目固定Go1.27.3/Node24.22.1工具；每个分支按实际路径执行。T01-A实际路径已由作者按用户指定移至 `D:\admin\desktop\codex\myproject\ks.worktrees\feature--proto-validation-cx01`，本轮同步任务板，不移动其工作区。

| 检查 | 命令 | 结果 |
|---|---|---|
| T01-A模块与覆盖 | `go test -count=1 -cover ./internal/transport/...` | PASS，99.0%；14顶层/124命名子测试（全包JSON复核） |
| T01-A编译/静态/边界 | `npm run check:go`、`npm run check:policy` | PASS |
| N01模块与覆盖 | `go test -count=1 -cover ./internal/nodes/...` | PASS，nodes95.2%、testkit100% |
| N01期限/改名重复回归 | `go test -count=20 ./internal/nodes -run 'TestVisiblePropagatesExpiredDeadlineDuringFinalCheck\|TestOrdinaryGroupRename'` | PASS |
| N01完整Go/边界 | `npm run check:go`、`npm run check:policy` | 清理总控历史探针扫描影响后PASS，详见下节 |
| W01前端 | `npm run check:web` | PASS，60测试，类型检查及构建通过 |
| W01边界 | `npm run check:policy` | PASS |
| 总控独立回归探针 | 实际调用nodes实现及toNodeListItem/describeMetric | PASS，5个旧问题关闭，并验证32写入原子性与0/100边界 |
| 组合集成 | `npm ci`、`npm run tools:install`、`npm run check`、`npm run check:security` | PASS：Go59顶层/171子用例、Web60测试；5组检查通过，npm0漏洞、govulncheck无漏洞；完整结果已归档 |
| 本地程序smoke | `node scripts/runtime-smoke.mjs` | PASS：health=200、任务写入路由=404；所有测试进程关闭 |

### 复验过程中的总控遗留工件

N01第一次完整Go检查失败于 `reports/review-probes`：这是上轮总控创建、被Git忽略但未被Go忽略的临时main.go，仍调用旧Rename签名。失败不是返修实现的问题，模块测试及独立回归已通过。

核对为总控创建的确切文件、路径位于该Worktree reports目录后，将旧和本轮探针源码逐文件改名为 `main.go.txt` 保存，未删作者文件。随后完整Go/静态/边界检查重跑通过。注意Git ignore不等于Go忽略；后续Go临时探针应位于 `_review` 等Go会跳过的目录，或执行后保留为非.go文本。失败摘要与干净复验分别归档，不隐去中间失败。

## 未验证边界与后续

- GitHub线上CI/Linux race仍UNTESTED；本地Windows测试不代替它们。没有远程仓库、推送或生产操作。
- T01-A没有原始wire帧限额、解码分配/重复oneof防护、mTLS身份、单调序号、防重放、真实计划绑定、任务持久去重等能力；不能仅调用本函数后执行任务。
- nodes的非ctx授权错误暂按不可见处理，拒绝与基础设施错误的分类留给下一批auth任务；当前没有真实节点HTTP/DB接线。
- 前端质量依赖受控适配入口、显式时钟/策略，仍是局部模型；严格时间格式/最终API迁移、实际采样与页面接线未冻结。
- 原始资料、已批准ADR、OpenAPI/Proto/生成物与依赖锁文件保持不变；G2仍未冻结，1C1G/SQLite/WASM/真实Agent未验收。

下一批为AUTH-01A、A01、W02，均在新独立分支/Worktree中，仅离线策略/审计基础/受控列表交互。见 THIRD-BATCH-WORK-PACKAGES.md；不自动启动会话。
