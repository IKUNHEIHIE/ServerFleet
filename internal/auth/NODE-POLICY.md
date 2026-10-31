# AUTH-01A · 节点读取的默认拒绝策略候选

日期：2026-10-29（Asia/Shanghai）；会话 `codex-01`；审查/合并：总控 `codex-review-01`。仅离线策略与错误语义，不是认证、完整RBAC或真实任务授权闭环；G2未冻结。

## 目录、基线与文件

- 分支：`feature/auth-policy-cx01`；起始HEAD：`a103c5f8b83c288e8178d995195140762a7cd122`。
- 用户指定工作目录：`D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-policy-cx01`。总控原准备于 `ks\.worktrees\feature--auth-policy-cx01`；核对同一分支/基线且工作区干净后，用Git移动到用户本次指定路径，没有重建/重置或修改其他会话目录。共享任务板中的路径待总控同步。
- 只新增 `internal/auth/node_policy.go`、`policy_errors.go`、`node_policy_test.go` 和本说明。`ports.go`、nodes及其他模块、公共契约、依赖/锁文件、脚本/配置、原始资料/ADR不改，不增加依赖。

## 入口与可信边界

`EvaluateNodePolicy(ctx, Check, *NodePolicy)` 直接评估调用方明确提供的当次策略快照。`NewNodePolicyAuthorizer(PolicyReader)` 适配现有 `auth.Authorizer`，接口签名不变；每个有效且受支持的请求读取一次当前快照，再调用同一纯策略，没有授权缓存、旧策略兜底或生产内存用户库。适配器零值、nil接收者或nil读取器均拒绝，返回策略不可用。非法/未支持的请求在读取前就拒绝。

`NodePolicy.KnownNodes` 是可信后端的精确节点ID集合；`ReadGrants` 的键为已知主体，值为该主体允许读取的节点集合。它只能表达临时内部动作 `nodes.read`，不提供任意动作注册、角色继承、通配、组解析或管理员特例。未知动作（包括节点写入、AI/root/插件动作）即使主体有节点读取权限也拒绝。空授权集合是零权限，不是全局权限。

**Check中的SubjectID必须由上游可信身份绑定得出，不能直接使用客户端自报用户ID。** NodeIDs和Action只是请求选择器，不是授权凭据。策略快照/PolicyReader必须由可信后端提供，不得把HTTP请求、AI输出或客户端的角色/授权声明绑定成NodePolicy。本层没有身份认证能力，也不能从一个Go数据类型证明来源可信；这些是未来组合根的必要约束，本次没有接线。

读取器应配合ctx并返回当前、不可变的快照；快照及其map不能在评估期间被并发修改。撤权用新快照替换旧快照，下次Authorize重新读取并拒绝。纯函数本身只能判断传入快照，不能探测调用方是否传入过期策略；读取器内部若缓存旧授权会违反此接口约定。校验器不创建后台goroutine强行中断不配合ctx的读取器。

## 决策与错误矩阵

| 情况 | 结果 / errors.Is |
|---|---|
| 主体、动作、目标齐全，主体已知、动作仅nodes.read，所有目标同时存在且属于该主体授权 | nil；不表示其他权限或执行批准 |
| 已知主体无授权、未知主体、未知动作、未知节点、其他主体的节点、混合目标中任一越权 | `ErrDenied`；整请求拒绝，没有部分放行 |
| 空/非法主体或动作、nil/空目标、任一空目标、通配、重复目标、nil ctx | `ErrInvalidCheck`；空目标绝不转成全局权限 |
| nil策略/读取器、读取失败（即使同时给出可放行快照）、格式坏的可信策略、授权引用未知节点 | `ErrPolicyUnavailable`；不降级为空成功或复用旧允许 |
| 非nil但空NodePolicy、空ReadGrants或主体空授权 | 有效零授权快照，返回拒绝；与缺失策略不可用区分 |
| 调用前/读取中/评估时观察到ctx取消或到期 | 返回标准context错误，优先于允许/拒绝/策略读取错误 |
| 读取器返回包装的Canceled/DeadlineExceeded而ctx尚未体现 | 规范化为标准context错误，保留errors.Is分类，不透传包装文字 |

策略错误类型为 `*PolicyError{Kind, Field}`，可 `errors.As` 获取固定分类/字段；`Unwrap` 暴露上述三个sentinel。错误不含主体/目标值、参数、策略内容或读取器原始异常。外部包装后仍可用errors.Is/As。取消/期限不是权限拒绝或策略不可用，不强塞进PolicyError。

局部输入规则：ID/动作非空、合法UTF-8，拒绝Unicode空白/控制字符和 `* ? [ ]` 通配符号；不静默trim、不做模式展开或UUID假设。重复目标视为无效。字面 `all` 或服务组标识没有特殊权限，只有作为确切已知节点ID且确切获授权才可能通过。这些是本轮局部保守约定，不是公开API ID格式冻结。若可信策略包含坏键/悬空授权，整个当次策略不可用，不静默跳过坏规则。

## 取消、不可变性与撤权边界

入口检查ctx；读取后即使读取器返回nil错误也再次检查；评估策略/目标时检查；函数返回前再次检查。取消/期限优先于最后一次读取给出的允许或失败。适配器在调用读取器前复制Check.NodeIDs，避免读取器通过调用方共享slice改变当次目标。普通调用方也必须遵守输入只读，不得并发写Check或策略map。

授权完成后才发生的撤权不可能通过当前Check接口与后续副作用原子关联。本轮不新增revision/授权票据或任务事务。未来真实任务派发仍需当时身份、RBAC、AI会话快照、插件能力、强制审批及当前策略的联合复核，不能复用本轮返回nil绕过这些层。

## 给nodes调用者的迁移建议（未接线）

只读检查发现当前 `nodes.Visible` 已传播ctx错误，但仍把所有其他Authorize错误作为不可见跳过。这在策略不可用时可能产生“成功空列表/部分列表”。建议总控另行授权nodes owner迁移：仅 `errors.Is(err, auth.ErrDenied)` 可作为权限过滤；`ErrPolicyUnavailable`、`ErrInvalidCheck`、ctx错误及未识别错误应中止并返回错误，不对外伪造成功结果/计数，避免返回半截成功列表。

既有 `tests/testkit.ErrDenied` 与新的 `auth.ErrDenied` 不是同一sentinel；未来迁移调用方时应同步明确测试替身错误语义，不能按错误文字判断或把任意error映射为拒绝。本次没有修改nodes、旧测试适配或生产组合根，不假装该接线已经完成。

## 实际验证与交接

固定工具：Go1.27.3、Node24.22.1、npm11.19.1。运行时只读使用主目录工具，依赖和固定Go工具安装于本Worktree忽略目录；没有改系统/锁文件或全局开放依赖脚本。业务代码仅标准库，无新依赖。

| 命令 | 实际结果 |
|---|---|
| `npm ci` / `npm run tools:install` | PASS；127个npm包及固定4个Go工具安装完成 |
| `go test -count=1 -cover ./internal/auth/...` | PASS；94.6%语句覆盖率；14个顶层测试、185个命名子测试（全包JSON复核） |
| `npm run check:go` | 最终PASS：gofmt、module verify/tidy、双入口构建、全包测试、vet/staticcheck；首次FAIL于负向测试故意传nil ctx的SA1012，修正局部测试说明后复验通过 |
| `npm run check:policy` | PASS；69原始文件/3原候选字节校验、8工具测试、Go/Web边界、分支及actionlint |
| `npm run check:security` | PASS；npm found 0 vulnerabilities；govulncheck No vulnerabilities found |

失败场景实测：reader-A只获synthetic-A读取权，提交synthetic-B或A+B均由真实EvaluateNodePolicy/Authorize返回ErrDenied；撤销A授权后下一次调用拒绝；曾放行之后策略读取失败返回ErrPolicyUnavailable；最后一次读取内取消ctx后即使给出允许快照也返回context.Canceled。测试直接调用新增实现，不在测试中另写一套权限判定器。

Go检查首次的两条SA1012只针对刻意传nil Context的负向测试。为这两处调用添加有明确理由的行级lint说明，保留测试和生产保护，没有关闭生产检查、修改全局规则或删除测试。初次失败结果保存在 `reports/auth01a-check-go-initial.json/.log`；最终输出在 `reports/check-go.json`、`check-policy.json`、`check-security.json`、`go-tests.log`、`auth01a-module.log` 和各console日志。机器UTC记录换算+08:00为本次2026-10-29。

未验证：真实身份/登录/凭据、可信策略的存储读取与版本一致性、真实撤权与任务派发的原子性、多角色/AI/插件/审批权限域、HTTP/数据库/审计持久化、生产组合根接线、Linux CI/race和性能。没有连接Controller/Agent/云或真实节点，没有部署、合并、推送。仅创建本地提交（编号见最终交接）后停止，交总控独立复审。
