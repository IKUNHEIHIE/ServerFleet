# GM-04 → Codex Arts · 可直接交付的开工提示

2026-11-02负责人授权：Gemini本轮尚未开始，将完整GM-04任务交给Codex Arts。原GM-03的代码和总控返修已合入main；原Gemini目录保留追溯。本轮没有Gemini未完成修改需要搬运，不重复审查/重做历史交付。下面内容可直接用于Codex Arts新会话。

总控交接检查：本次仅修改TASK-BOARD、WAVE6-R3及本提示三份文档；`npm run check:policy`、`npm run check:git`通过，文档链接/任务执行人/范围核对通过。没有运行产品代码或新增业务验证，业务结果仍引用Wave6-R2的已审查技术版本。新分支快进及最终SHA记录在主目录忽略的`reports/v09-gm04-codex-arts-handoff.json`，不变更CX/DS工作区或重写上轮交接快照。

---

你接手Server Fleet完整v0.9的管理台、测试宿主和浏览器验收任务，按已授权范围直接开展实现并持续交付。

## 身份、目录与起点

- 任务ID：`V09-R6-GM-04`（沿用来源编号）。
- 唯一会话ID：`codex-arts-01`；执行人标签Codex Arts；审查/集成负责人`codex-review-02`，兼UI/安全审查。
- 唯一写入工作目录：`D:/admin/desktop/codex/myproject/ks/.worktrees/feature--v09-r6-console-ca01`。
- 分支：`feature/v09-r6-console-ca01`。总控已准备该目录；在此目录工作，不在main或原Gemini目录写代码，不切换其他作者分支。
- 已接收功能基线：`db3cf87d44022be79bbec55eb4012b95f307dac7`。实际起点是包含本次纯文档转交的后续提交，以主目录`D:/admin/desktop/codex/myproject/ks/reports/v09-gm04-codex-arts-handoff.json`的`finalHead`及负责人粘贴的SHA核对。不要切回功能基线，保留交接登记。
- 开始先报告任务ID、会话ID、当前分支/SHA和工作区状态。若目录/分支/基线或干净状态不符，先报告差异；禁止reset、clean、自动stash或覆盖他人修改。

## 先读当前资料

以下路径均相对你的独立工作目录；读取时以该目录下的实际文件为准：

1. `AGENTS.md`、`README.md`、`docs/architecture/README.md`。
2. `docs/development/MODULE-OWNERSHIP.md`、`DEPENDENCY-RULES.md`、`TASK-BOARD.md`。
3. `docs/development/sprints/v09-five-day/WAVE6-R3.md`的GM-04部分及共同交接要求。
4. 同目录`WAVE6-R2-SHARED.md`、`GEMINI.md`整包范围、`PLAN.md`及现行原始需求/24项产品验收要求。
5. `docs/development/reviews/V09-WAVE6-R2-REVIEW.md`和对应RESULTS.json。

Gemini旧STATUS是历史快照。最新共享说明和总控报告已修正其旧依赖/安全描述：正式文件worker、AI预算HTTP、24项迁移已经接收；无认证`/fixture/configure`已删除，不能恢复；P13按稳定nodeID选目标并核对真实快照，不能放宽授权解决403。

## 当前事实与交付目标

当前Web 341项、真实Controller回归70/70、契约桩16/16通过；正向浏览器21 PASS / 5 BLOCKED / 0 FAIL。B02已在本地TLS合成上游完成真实AI生成；后端已有无overlay的真实双Agent多文件证据。完整v0.9仍未验收，五个阻塞为B01文件页面、B03 Xray、B04实际发布、B05正式更新、B06完整灾备。

按以下顺序推进整包，每个可运行增量提交后继续不依赖它的其他项，避免等待单个后端接缝而整包停工：

1. **首个检查点：B01真实文件页面闭环。** 扩展`tests/browser/v09/fixturehost`，通过可信程序配置注入`server.Config.FileExecution`、私有spool、明确的nodeID→逻辑根映射；两个真实常驻Agent均配置各自私有`-file-root`。从浏览器真实页面创建transfer，经统一任务/当前权限/所需审批、实际WSS双方执行，校验目标字节/SHA、transfer和任务终态。先完成现已支持的skip，再做取消/撤权/回执丢失等负向。保持节点映射受控，不新建无认证HTTP配置入口。双进程同WSL只作为双端协议证据。
2. **并行可独立项：AI预算正式界面。** 使用已存在的GET/PUT `/api/v1/ai/model-settings/{profileId}/budget`，展示owner的窗口/限额/已用/剩余；管理员CAS配置、409冲突、普通用户只读、缺策略拒绝、身份切换/撤权/迟到响应隔离。计数以服务端为准。保留真实B02，不把预算配置挪回未认证宿主端口。
3. **继续B04/B05/B06完整验收。** 按DS发布、CX更新/恢复的稳定端口准备隔离宿主、合成签名工件和第二Controller测试结构，逐步验证实际发布/失败补偿、更新实际版本/健康/回滚、恢复实际执行/新权威/旧主控隔离。先交不依赖新后端的前端和宿主结构，再接后端增量。遇到缺口给精确请求/响应/最小失败用例和所需端口，不把202、check或preview当成功。
4. **继续整包独立Web项。** 保持Gemini已交付的页面、导航、任务错误列、身份/CSRF恢复及同名节点回归。补未完成的错误/空/权限/部分结果界面。Xray工件/运行时尚未准入，B03按事实保留阻塞，继续可独立UI与权限项；不擅定Runtime或运行未通过安全审查的工件。无需重写技术栈或重做整体UI。
5. **形成可复核验收证据。** 更新AC-01—24，分别标注源码、模块、真实页面正常/负向、外部/实机、发布准入；关联精确干净SHA、命令、原始输出和脱敏截图。P13保留Operator的最小节点权限，选nodeID后核验真实任务targetNodeIdsSnapshot，记录脱敏target/subject/grant/响应证据。

## 可写范围与协作边界

- 可写：`apps/web/src/**`、`apps/web/index.html`、`apps/web/public/**`；`tests/browser/v09/**`（包括测试专用Go宿主fixturehost）；你的忽略`reports/**`。
- 新进度目录：`docs/development/sprints/v09-five-day/progress/codex-arts/wave6-r3/**`，维护STATUS、HANDOFF、SHARED-CHANGES和证据索引。保留Gemini历史进度/证据。
- 生产Go、公共OpenAPI/Proto/gen、公共DDL、根/前端依赖清单与锁、生成/构建配置、CI和TASK-BOARD由总控协调；需要时在自己的进度目录交精确patch及兼容/安全说明，继续其他独立项。
- CX负责files/tasks/transport/AI/更新/恢复后端；DS负责发布/网关/供应商/cleanup。只能消费其已接收端口或提交精确问题，不修改其工作目录。
- Web只调用Controller REST/SSE；所有执行走统一任务、授权、审批和审计。fixturehost只能测试→生产单向导入，不能进入产品依赖；不能直接写数据库伪造结果、直连Agent或另造SSH执行通道。
- 保留Cookie/Origin/CSRF、owner/profile、稳定nodeID、当前授权与UNKNOWN不重放边界。合成秘密仅私有文件注入，遥测只读且loopback，不进URL、Git、截图或日志明文。
- 已接收中央补丁不再是等待条件。不要运行历史`prepare-wave6-r2-*-overlays.mjs`/update-contract脚本或重放旧patch；当前正式组合无需overlay。
- 仅总控可操作指定awskr隔离测试；你不SSH、不碰生产/云API、不用真实外部秘密、不合并main、不推送、不启动其他Agent或代发消息。

## 验证、检查点与停止条件

- 根要求`npm ci`、`npm run tools:install`、`npm run check`、`npm run check:security`及相关专项/负向检查；工具用仓库固定版本，失败保留原输出，不改依赖版本或降低门槛。
- 浏览器须在前端构建完成后运行，避免与同一dist重建并发。既有真实回归70步、桩16步和正向套件分别报告；正向运行`--ai-host --attempt-file-flow --require-complete`及明确WSL发行版，保留实际退出码。
- 500/401/403/错误shape/超时按失败处理；明确预期的负向拒绝单独验证。已知未配置/未交付依赖精确标BLOCKED；任何BLOCKED下严格全量验收仍应退出1。不得降FAIL为BLOCKED、删断言或把探针改名为完整成功。
- 如需要库/框架/SDK/API/CLI具体文档，按项目要求先Context7 resolve-library-id再query-docs；普通业务逻辑/脚本/审查不必查询。
- 每个可运行增量或2—4小时提交一次：SHA、文件、运行方法、正常/负向结果、原始输出/证据、剩余依赖，然后继续独立项。首个检查点优先B01，同时推进预算UI。
- 全部可独立项完成，或逐项说明没有独立工作可继续的具体硬依赖后停止；最后交全部SHA、干净HEAD、命令/输出、证据路径、候选依赖和剩余风险，等待codex-review-02审查。不得以“一个接口未接”为由提前结束整包。

请现在开始核对目录、读取资料并实施；无需再次询问是否可以开始，交付须包含可运行成果。
