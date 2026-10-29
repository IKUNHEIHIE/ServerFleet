# N03-A · 预检先行的离线节点读取用例

日期：2026-10-29；会话 `codex-01`；审查人 `codex-review-01`。
实际目录：`D:\admin\desktop\codex\myproject\ks.worktrees\feature--nodes-read-usecase-cx01`。
分支：`feature/nodes-read-usecase-cx01`；起始 HEAD：`8b2d8aa5b7d249b032d2c64abbbc2f758c739a4a`，开始时工作区干净。最终本地 commit 见本次交接。

已读根 AGENTS、README、TASK-BOARD、架构索引、模块归属/依赖规则、CODEX-N03A-WORK-PACKAGE 和 AUTH-01B-REVIEW。本轮仅新增 `read_usecase.go`、`read_usecase_test.go`、本说明三个文件。既有 auth/nodes 文件与测试、ports、契约、r03来源/限制探针、依赖、共享配置均未修改；旧 AUTH-01B 目录停止保留，未读写其他会话在编目录。

## 接口和固定顺序

`ReadVisibleInventory(ctx, subjectID, auth.PolicyReader, InventorySource)` 每次真实调用 `auth.PrepareNodeReadScope`，成功后才调用库存函数，再用这一个捕获范围逐节点 `CheckNode`。调用方不能传历史 scope 替代预检。`InventorySource` 仅为返回现有 `[]Node` 的局部函数接缝，不替换 Reader.Get，不新增通用仓储、Authorizer、查询/统计API或生产适配。

subjectID 必须由未来可信认证边界绑定，不能信任客户端自报身份/角色；两种来源都必须由可信调用方显式提供。策略来源返回当前策略，在准备期间保持其所有 map 只读；库存来源的 slice、Tags、Metrics 必须在整个 ReadVisibleInventory 返回前保持只读。来源均须配合 ctx；不使用后台 goroutine 强杀不合作来源，也不并发读取两种来源。

正常有效调用的顺序和次数为：策略1次 → 库存1次 → 同一范围检查全部节点。库存是否为空在策略读取前不可知，也不能成为跳过预检的理由；已知零授权仍读取一次库存，再成功返回空集合。没有读者、scope、列表或“上次允许”的缓存。

## 结果与错误边界

| 情况 | 真实行为 |
|---|---|
| nil ctx、非法/空主体、未知主体、nil/错误/坏策略 | 沿用 auth 的 InvalidCheck/Denied/PolicyUnavailable 或标准 context 分类，返回 nil；库存调用0次 |
| 已知主体零授权，或有效空库存 | 预检和库存各调用1次；返回非nil空 slice，不带隐藏库存的容量/计数字段 |
| nil 库存函数 | 先完成预检，再返回固定 ErrInventoryUnavailable；没有空成功/mock兜底 |
| 库存返回任意非context错误，包括单独 ErrDenied、errors.Join 的拒绝与故障 | 固定 ErrInventoryUnavailable，nil结果；即使同时给出部分数据也全部丢弃，不保存原异常文字或 unwrap 链 |
| 库存返回标准/包装 context 错误 | 规范化为 Canceled/DeadlineExceeded，去除包装文字并保留 errors.Is；不会作为纯拒绝过滤 |
| 范围检查明确纯拒绝 | 仅过滤对应节点；复用既有 classifyAuthorizationError 的保守分类，混合/非纯拒绝不能过滤成成功 |
| 空、通配、空白/控制字符、坏UTF-8等非法ID | 由真实 CheckNode 的格式校验拒绝，传播安全的 auth.ErrInvalidCheck，返回 nil；不会 trim 或跳过坏行 |
| 重复ID，包括重复的隐藏/未知节点 | 固定 ErrInvalidInventory，nil结果；不去重、不返回已经复制的前缀，不生成误导计数 |
| 调用前、来源之后、遍历/复制及最终返回处观察到取消/期限 | 标准 context 错误优先，nil结果；成功或空成功都不能覆盖已观察的取消 |

这些是内部 Go 分类，不是 HTTP 状态映射。尤其库存坏ID的 InvalidCheck 不是“客户端一定传错参数”的证据，未来错误映射仍需独立设计。敏感来源错误不输出主体、节点ID、策略内容或内部异常。同步最终检查之后发生的取消无法由返回值预知。

## 成功输出的所有权与消费

只保留授权节点的原库存顺序。新 slice 不复用来源 backing array；Tags 单独复制，Metrics 指向独立值；nil/空值及 Node 的其他字段值原样保留。两个来源节点即使共享 Tags/Metrics，返回的两个节点也不再共享这些可变对象。来源或返回方后续修改不会改写另一方。

本函数只验证节点ID格式与唯一性并执行读取授权，不发明公共 DTO、revision、状态、采样时间或新鲜度。Metrics 仍是现有原始领域数据，不代表在线/实时读数；现有 Usage 的显式时间策略、未来字段校验/公共适配不在本轮改变。

后续只对 **err==nil 的输出** 调用既有 Filter/Classify，错误路径不能消费 nil结果为成功计数。真实测试使用隐藏节点独有名称、地址、标签和区域，确认授权输出后的搜索和分类没有这些信息；没有新增全量计数或查询入口。现有 Filter/Classify 不承担认证，也不会把误传的原库存变安全，未来可信调用方必须保持该顺序。

## 时点语义与未接线限制

库存回调可以在预检结束后改变原策略；本次过滤仍只用已捕获的同一范围，新调用重新准备才观察撤权。测试中节点a一直存在，仅其授权被移除并换为b：第一次仍输出a，第二次输出b；之后策略或库存失败都返回nil，不回退旧列表。这是显式的时点语义，不是即时撤权保证。

两个来源顺序调用不等于同一存储事务，不能证明策略/库存属于同一版本，也不能把撤权和输出序列化原子绑定。没有在返回前再读一次并声称消除TOCTOU，没有 scopeRevision、锁、事务或缓存。真实身份绑定、来源当前性、输出前授权复核/版本一致性都待后续安全/节点/存储审查。

新入口没有接 Controller 或改变旧 Visible/VisibleFilter/VisibleClassify，r03 的原有限制探针继续保持原样。**D01–D07/P1-C10 仍 OPEN，G2 未冻结**；离线组合通过不代表真实只读 API 或整个系统的空集合/原子性问题已解决。

## 实际验证与证据

Windows amd64，固定 Go1.27.3、Node24.22.1/npm11.19.1，依赖与固定工具版本未变。通过主目录 scripts/env.ps1 使用已安装运行时，本工作目录 .tools/.cache/node_modules/reports 为忽略文件。没有真实网络节点、数据库或云输入。

| 命令 | 实际输出/结果 |
|---|---|
| npm ci；npm run tools:install | PASS，127包/固定4个Go工具；原Buf postinstall许可提醒保留，未修改许可或依赖，实际Buf检查通过 |
| go test -count=1 -cover ./internal/nodes/... ./internal/auth/... | PASS：nodes **96.3%**，nodes/testkit **100.0%**，auth **98.6%** |
| go test -count=1 -cover '-coverprofile=reports/n03a-cover.out' ./internal/nodes/... ./internal/auth/...；go tool cover '-func=reports/n03a-cover.out' | PASS：新增 ReadVisibleInventory **100.0%**；auth PrepareNodeReadScope/CheckNode 保持100.0% |
| npm run check:go | PASS：gofmt、mod verify/tidy、两个入口构建、全包201个顶层/782个子测试，0失败，vet/staticcheck；nodes62/218、auth28/233；其中新增用例12个顶层/105个命名子测试 |
| npm run check:policy | PASS：69原文件/3原候选字节完整性、8项工具测试、Go/Web边界、分支规则、actionlint |
| npm run check:contracts | PASS：旧OpenAPI、Buf/Proto兼容/生成一致性、r02及已接线r03；r03来源/候选14项JS、原限制探针4个Go顶层/5个子测试保持通过，未重算或修改来源矩阵 |
| npm run check:security | PASS：npm `found 0 vulnerabilities`；govulncheck `No vulnerabilities found` |
| git diff --check；git diff --cached --check | PASS；仅本目录3个新增文件，所有旧跟踪文件保持不变 |

直接负向测试覆盖：预检失败库存0次；空库存未知主体/坏策略拒绝；零授权不扩权；两种来源失败后没有旧范围/旧列表兜底；部分数据+错误、混合拒绝/故障、敏感包装context错误均不泄露数据/原文；坏/重复的隐藏ID中止；取消/期限在最后来源即使同时返回允许数据和故障也优先；授权后再搜索/计数；slice/Tags/Metrics双向隔离。

遍历/复制/返回取消测试以成功调用校准实际 Err 观察次数，在每个观察点切换到真实已取消或固定已到期的context，验证一律nil结果和正确分类；没有硬编码检查次数，没有sleep或调度竞态，也没有在测试中另写读取用例算法。一次补测patch因gofmt后的上下文不匹配而未应用、未运行该次检查命令；重新读取并缩小patch后完成上述真实验证，未删用例或降低门槛。

原始输出/摘要位于忽略的 reports/go-tests.log、check-go.json、check-policy.json、check-contracts.json、check-security.json、n03a-results.json、n03a-cover.out、g2r03-results.json 及各工具日志。以上最终规定检查均PASS。

UNTESTED：真实身份/会话、PolicyReader与库存的存储当前性/版本/事务、撤权与输出绑定、Controller/HTTP/公开DTO/缓存、Linux/race、GitHub CI和生产性能。没有开发登录、数据库、WSS/mTLS、审批/任务/执行或审计持久写入。仅本地提交，交 codex-review-01 复审后停止；不合并main、不推送、不启动其他Agent或修改其他会话任务。
