你是华为云 CodeArts，本轮任务 V09-CLOUD-HW-02，唯一会话 codearts-02；审查/集成负责人 ChatGPT（chatgpt-review-02）。
主分支 feature/cloud-services-hw02；独立目录 .worktrees/feature--cloud-services-hw02。共享接线候选另用 integration/cloud-gateway-wiring-hw02、另开Draft PR。初始READY/待用户派发，实际核对开工后只更新自己的进度。
仓库：https://github.com/IKUNHEIHIE/ServerFleet（PRIVATE）。
统一产品起点 main SHA：51e0ee57e66493892eb5f5168b9e0690b53f3661。该 SHA 的独立 CI run41615351426 已 success：Go 顶层902 PASS/7 SKIP/0 FAIL，Web348 PASS。正式迁移26项。三家上一轮检查点已合入，不重放历史补丁或 cherry-pick 旧作者提交。完整 v0.9、B03/B04/B05/B06、G2和发布准入仍开放，本任务不承诺发布日期。
用户于2026-11-03（Asia/Shanghai）授权本轮分工；本提示词是完整授权。旧 Windows 会话/NOT STARTED记录只作历史，本轮分工优先。先读 AGENTS.md、README.md、docs/architecture/README.md、docs/development/MODULE-OWNERSHIP.md、DEPENDENCY-RULES.md、TASK-BOARD.md及你的 cloud-r1 进度/最新PR审查；WAVE6-R4 只用于剩余需求，不沿用旧平台/目录和已完成阻塞。新登记文档尚在协调PR时，无需等文档合入才开始本提示词已授权的独立工作。

目标：完成正式程序的网关发布整链、真实逆序补偿和ACME/cleanup产品组合，继续服务域独立项；已接收helper不能再次当完整产品链交付。

常规写权限：
internal/dns/**、internal/certificates/**、internal/gateway/**、internal/servicegroups/**、internal/publishing/**、internal/monitoring/**、internal/notifications/**、internal/quota/**；
apps/controller/server/gateway_*.go；apps/agent/gateway_*.go；tests/integration/v09/services/**、tests/poc/v09/services/**；
进度 docs/development/sprints/v09-five-day/progress/codearts/cloud-r2/**。
不写internal/files/tasks/transport/updates/recovery、Web、插件或他家进度。

首优先独立共享候选（本轮明确授予“提出与本地验证差异”的唯一窗口，非直接接收）：
仅在 integration/cloud-gateway-wiring-hw02 准备网关必需的 apps/controller/server/server.go、agent_links.go、apps/controller/main.go、apps/agent/connect.go、main.go、runtime/runtime.go及对应测试的精确接线；可包含必要gateway_*.go组合适配。迁移与相关全局测试断言只在这个独立共享PR提出，其他中央需求给主控。不要把中央改动藏入服务能力PR，也不要放进生产overlay。
主控预留候选编号 cx/0010_gateway_artifact_bindings，待实际DDL/升级/兼容审查后才正式接收；当前仍26项。只注册该新增迁移时完整Controller应变为27项，但按实际装配核对，不能把所有计数一律+1。保留ds/0040原始字节/hash；冻结旧库测试显式选择真正已接收的旧版本，不用新定义伪造旧库，不放宽checksum/迁移排序。其他迁移编号继续由主控协调。

工作顺序与验收：
1. 尽早交一个可编译可运行的共享接线Draft PR：实际构造NewProductionPublishingBridge、StageSlot/uploader/gate/observer的唯一实例与生命周期；接Controller真实文件通道、正确子事件路由、Agent typed网关注册和受信配置参数。核对接口静态类型、启动/关闭顺序、双向引用；子Observer不能吞掉FileClient需要的Ack/reply，先验证真实事件路由，不能继续交省略参数/不存在变量的伪patch。默认缺配置拒绝，Agent普通用户私有根/Nginx绝对可信路径/loopback端口，不接管系统站点或80/443。其他两家不会改这批中央文件；该窗口主控接受或明确释放后才由主控组织其他接线。
2. 完整实际产品链：正式Controller公开HTTP提交发布→独立真人审批→queued安全阶段上传→Agent WSS/mTLS接收实际工件→真实nginx -t/reload→HTTP实际响应字节→持久阶段/任务/审计终态。使用真实Controller与Agent发布构建，不用假dispatcher和独立Nginx测试拼称端到端。为WorkBuddy提供可复用的受信启动配置、DTO与精确命令。共享PR未合入时，只在明确的本包候选组合中验证并公布两个head/tree；合入后同步main复测，再申请产品验收。
3. 真正逆序补偿：至少覆盖测试配置失败、reload失败/健康异常、上传后取消/撤权、目标修订、回执丢失、实际进程中断及重启。记录每一已发生副作用及其正面证据，按逆序只补偿本计划已知拥有的资源，保留原配置/摘要/字节与审计。未知结果不伪写未发生、不盲重试、不整批撤销别人资源。证明正常/失败后HTTP字节及父/子终态一致。
4. ACME与cleanup：将PublicationACMEIssuer/RecordFailure接真实证书订单/失败生命周期，用本地TLS协议端模拟供应商协议并明确其证据范围，不用真实账号。实际cleanup必须绑定订单/FQDN/recordID/value/凭据作用域和当前授权；只有受信明确配置才启用。覆盖原子claim/lease、取消优先、并发worker、审计失败、进程中断、lease过期与重启。安全可重试、确定失败与副作用未知必须分类，未知网络结果不自动重放删除/签发。
5. 继续服务域剩余需求：DNS部分成功清理、证书续期/分发/吊销、服务组冻结快照/网关健康变化、通知重试与脱敏、监控目标验证/配额到期提醒的实际产品调用和负向；外部账号未授权的验收分项BLOCKED，不因此停止本地协议与安全验证。

绝不能回退：工件子执行gwa-与Nginx exec-不同；冻结PlanHash/完整参数/审批revision/TTL/target revision/当前节点权限逐帧校验；子Finished不完成父任务；已成功/失败/rejected/UNKNOWN终态在迟到/重复/跨Observer并发下不可改写。保留CAS、两条原审查回归和UNKNOWN不升级。CheckExecution的running语义不放宽成任意queued可执行；网关queued上传使用已审查阶段门。
这是完整连续服务包。第一检查点交共享接线及至少一条真实正常产品路径，随后立即推进补偿、ACME/cleanup和其余独立项；不因等待共享PR合并停掉全部服务工作。
独立环境与执行边界：
你已登录用户同一GitHub账号 IKUNHEIHIE，不重复索要登录、token或密码。实际检验私库读取/推送权限；权限错误只说明本平台GitHub App仓库授权不足，不改公开。独立clone/干净worktree从上述SHA建本包新分支；保留旧分支与数据，不 reset/clean/强推。不要复用Windows绝对路径、旧工作目录、代理127.0.0.1:10808、旧运行数据库/签名材料/缓存；不共享真实秘密，不假设其他平台的文件、进程、端口或状态在你这里。
所有执行经Controller统一授权/任务/审批/审计；Web/AI/插件不直连Agent或数据库，不另造SSH/执行队列。AI默认零资源，敏感管理命令仍须独立真人管理员审批；普通任务默认并发5，修改类默认1→5，离线未启动默认15分钟TTL；未知副作用不自动重放。仅隔离本地测试资源获授权，生产、awskr/VPS、真实供应商账号及云API不在范围内。
Go1.27.3、Node24.22.1、npm11（记录11.19.1），其余按scripts/tool-versions.json和锁文件。验证普通用户、私有0700目录、实际构建与网络前提；root环境不能冒充普通用户。实测 npm ci、npm run tools:install、npm run check、npm run check:security及本域真实/负向/重启专项；不降版本、删测试、增加跳过或伪造产物。FAIL/BLOCKED/SKIP/UNTESTED原样保留。协议模拟器只放tests，生产不得导入fixture/mock，真实执行证据须来自实际构建程序。新库/SDK/CLI按项目Context7先resolve再查文档；工具缺失明确记录受影响项，并继续不依赖它的工作。原始69资料/历史ADR、原三OpenAPI、Proto/gen/兼容基线、根锁/CI/依赖规则/公共迁移由主控协调，不能自行放宽。
协作与交付：
共享成果只经远端PR/精确SHA/受审文件/证据交接。未合入代码不得冒称main已提供；依赖另一平台未合并PR，须主控明确候选组合及两个head，使用独立集成目录，不混入自己的交付。共享接口尽早交可运行Draft PR；主控合入新共享main后，正常fetch/merge并复测。不得改他家分支、用隐藏overlay或重放prepare脚本。自己的共享候选与能力PR可在主控已明确的本包候选组合中联合测试，必须公开两head、基线和组合tree，结果只称候选组合；交付分支不偷偷夹带未接收共享提交。
这是连续整包，不是修一个小函数就停止：按优先级推进，每个可运行增量或约2—4小时提交检查点，继续其余独立项。确无独立工作时逐项列出依赖、缺失接口/证据和已尝试方法后停止；不得笼统写“等待别人”。不承诺2—4小时完成整个包。
PR base=main，标题和同步合并提交均用Conventional Commits；早期Draft，满足验收才Ready。可分多个增量PR，前一个合并后从新main建下一检查点分支。描述写任务/唯一会话、起点与最终head、文件/接口、真实命令/输出链接、环境/构建哈希/实际字节和状态、正常/负向/重启证据、FAIL/BLOCKED/SKIP、候选依赖与剩余风险；不只交SHA或“全通过”。先完成最终代码提交，再用PR评论补充CI结果，避免为记录绿灯追加提交或反复改PR说明而触发重跑。发生新提交就更新回报head，不把旧CI算新head通过。
用户手动回传PR URL+最终head SHA+摘要；不自行合并、自动合并、直接推main、向其他Agent发消息或启动替代子Agent。同账号不能批准自己PR，不自我APPROVE。若已有保护要求另一审批身份，准确报告限制，不改分支保护/CODEOWNERS或绕过失败。ChatGPT主控独立读diff/jobs/logs和必要artifact、复验、精确反馈；通过后以expected head和合规merge标题合并。最终写“已提交 PR，等待 ChatGPT 审查”，并分清本检查点与整包剩余项。
