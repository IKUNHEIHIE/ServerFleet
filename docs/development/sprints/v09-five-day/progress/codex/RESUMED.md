# V09-5D-CX 继续实施记录

会话 codex-02；审查 codex-review-01+安全/契约/owner；从b61ad15继续，仍在任务单登记的主仓库.worktrees目录和feature/v09-platform-cx02。用户明确要求继续完成；此前STATUS对“无独立工作”的判断不充分，现撤回该停止判断，不撤销真实测试记录。

## R01 真实身份持久化组合

提交 `39ad9ca2928ab87e75fbbd4390bf05df4c8919c9`。

主线仍8a7dd470，没有新的接口接收；只读核对已提交DS方案（pin bb8954988fe7b2f832edf291803529d7745e4f0a），未合并/改写他人目录。原S02通用RecordTx申请由本轮auth专用SQL适配替代；auth定义业务表和永久初始化标记，不分配公共迁移编号，不在storage中建KV域逻辑。

代码：internal/auth/identity_sql.go。通过窄函数把DS Queryer/Tx映射到auth自有IdentityStore，不导入database/sql，不猜不存在的公共API。构造不自动建表，IdentitySchemaStatements只提供待注册的业务迁移语句；缺表/缺行拒绝，绝不解释为未初始化。Update必须同一BEGIN IMMEDIATE、回调一次、取消/失败回滚；永久ever_initialized防止业务payload被错误清空后重开setup。

实验：tests/poc/v09/platform/run-identity.mjs将指定提交的storage生产源读入本任务忽略reports目录，使用单独固定module/sum（modernc.org/sqlite v1.61.0）。不改根锁/依赖，不使DS候选成为共同接口。`node tests/poc/v09/platform/run-identity.mjs --bootstrap-lock` 首轮PASS 4.265s；随后固定锁使用-mod=readonly重验。真实SQLite文件/WAL双连接并发初始化、关闭重开后会话撤销/initialized、取消回滚、反初始化/删除状态均验证。没有生产数据库或真实凭据。

仍待审：auth业务schema/存储接缝正式接收、审计消费、主入口接线、完整安全候选。继续实施用户/RBAC/显式授权与HTTP；不以这些未接收事项停止包内其他工作。

## R02 认证HTTP、用户/RBAC/显式授权与重放

提交 `1a3c0452236523063996a327fa10a6fc3c3e2a95`。

实现文件：identity.go、management.go、idempotency.go、session_views.go、browser_guard.go、http.go及对应测试。增加用户创建/修订/停用、内置administrator/operator/reader角色及服务器权限目录、当前资源解析+组成员快照、逐动作/逐目标授权、授权到期/撤销、用户修订CAS、角色降权epoch撤销和最后有效管理员保护。管理员仍零节点授权；角色的操作能力与资源grant取交集，组新增成员不继承旧grant、删除成员失权；NodeReadPolicy适配既有一次prepare路径，不读库存。

HTTP为真实但**尚未由主入口挂载的候选handler**：现有auth/bootstrap-status/bootstrap/login/logout/me/sessions、users、roles、permissions、resource-grants路径，保留既有DTO分页/ETag形状；me-CSRF和bootstrap204按独立S01候选。TLS必需，同源/CSRF/双凭据防护、严格有界JSON/重复键/未知字段/null/重复参数拒绝、错误脱敏、no-store、读结果先缓冲后世代闸门。A→B登录原子撤旧Cookie会话；会话管理ID不能当秘密。未配置第二因素验证器时明确拒绝要求MFA的配置或secondFactorCode，不假装验证通过。

幂等：bootstrap只在原setup期限内同键/同主体参数/同秘密证明返回相同无秘密确认；用户/授权创建1小时持久receipt，同键改参拒绝，重放不复活已撤grant。密码请求不写快SHA摘要：只哈希非秘密元数据，并保留原加盐PBKDF2记录校验重放密码；初始化token是高熵随机量，仍仅摘要保存。根契约/锁/迁移编号未动，候选等待审查，不冻结G2。

实际测试：

- `go test ./internal/auth ./tests/architecture -count=1` PASS 8.892s/1.483s；`go vet ./internal/auth` PASS；固定staticcheck PASS（第一次工具调用PATH缺go失败，补本worktree固定Go PATH后重跑通过）。
- `node tests/poc/v09/platform/run-identity.mjs` 固定锁-mod=readonly PASS 4.900s；真实本机HTTPS客户端+SQLite/WAL验证初始化→登录→幂等创建用户→显式授权→管理员无默认节点→退出→数据库关闭报503→重开旧Cookie仍401→用户/grant持久化与只读403。资源解析仅用固定测试资源集合，此证据**不是**真实Agent/节点/浏览器完整验收。
- 单元HTTP负向涵盖重复JSON键/非法null/未知角色字段/双凭据/Origin/CSRF/弱或坏If-Match/旧修订/重复查询、源故障、提交前撤权不输出缓冲的身份/CSRF。null密码首轮错误地归401，补严格JSON-null校验后重跑PASS；未删测试。

仍待完成：凭据CRUD/受控服务使用、审计最终sink事务消费、受限Bearer、MFA验证器/完整政策、跨存储资源世代的输出窗口、实际Controller组合与页面、1C1G及真实Linux端到端。继续包内实现。

## R03 审计同事务消费与观察到的setup过期防回拨

提交 `519b689dc3be4cdfaa71dccfd98de4ddde57a7dd`。

IdentitySQLStatements增加AppendAudit候选回调，必须绑定同一个DS Tx，用SQLRecorder.AppendTx写结构化事件，再在同一事务清空auth出箱并写auth状态。失败/取消/提交错误两边回滚；nil回调只保留真实持久出箱，不假装事件已到audit域。HTTP生成的请求ID经私有上下文传入，记录web/system来源；固定字段/类别，授权增删记录冻结NodeIDs，没有密码/Cookie/CSRF/token。已认证HTTP权限拒绝也写固定denied事件，审计写失败报503。

隔离实验增加DS audit提交源（同一bb8954988fe7b2f832edf291803529d7745e4f0a）；仅机械替换其storage导入到隔离模块别名，未改业务源/另一个worktree/根配置。真实SQLRecorder+SQLite注入“已写审计后错误”和“auth+audit均写完、提交前错误”，均证明用户/receipt/audit一起回滚；最终成功仅一条创建审计，重放不重复创建。`node tests/poc/v09/platform/run-identity.mjs` 4项PASS，最终5.235s。`go test ./internal/auth ./tests/architecture -count=1` PASS 8.367s/1.383s，staticcheck PASS。

额外修复：观察setup状态过期和提交过期setup请求会保存时钟观察点，回拨不能恢复旧setup有效性；补负向测试。auth编码写入也限制16MiB，不能先返回成功再使下一读取超限。当前审计数据仍依赖总控接收rich audit/迁移；通用资源引用（如target userId/credentialId）需与DS审计schema另行协调，不能把缺字段说成完整审计验收。接下来继续凭据平台等包内工作。

## R04 加密凭据生命周期

提交 `3237c19a55a8b833d9adc84f9184f84e0465c311`。`go test ./internal/auth ./tests/architecture -count=1` PASS 9.151s/1.356s，staticcheck PASS。

auth/credentials及HTTP可选接线、单元与真实SQLite测试；详细候选为CREDENTIAL-WIRING.md。支持限定用途的只写创建、元数据读取、密文暂存轮换、CAS替换/撤销、程序注册业务回调短时使用、调用前后gate、持久operation防重放与intent/终态审计。AES256GCM完整AAD绑定、注入密钥组、逐key持久预算先提交；没有默认密钥、通用解密口或假供应商验证。HTTP只提供实际已实现的create/list/references；rotate/revoke的202接线等待唯一tasks正式Controller本地目标，未造假任务。

实际验证：新增单元覆盖密文/元数据不回显、幂等改秘密冲突、轮换暂存不改旧值、跨key替换/旧key退役、旧revision/过期stage/撤销拒绝、缺gate/引用源失败关闭、调用错误/恐慌/中途撤销/终态审计失败都不重试，借用缓冲清零，key预算耗尽拒绝。固定锁`node tests/poc/v09/platform/run-identity.mjs` 五项PASS 7.515s，真实SQLite/WAL关闭重开后凭据持久、预算保留、UNKNOWN operation不重跑、撤销后不使用。该测试的task gate为明确测试fixture，不是统一任务/审批的完整验收。

剩余：真实密钥安装与恢复防回退key轮换、精确引用源和供应商有效性验证、Controller本地异步任务接线/页面；继续受限Bearer与Agent平台，不以此检查点结束。

## R05 受限Bearer

提交 `fce5ea844b8bcc192e702cbc43fcbc5eaeb26111`。

实现auth/bearer及候选HTTP（默认未启用），详见RESTRICTED-BEARER.md。令牌256位一次交付、持久摘要/epoch/期限/用途/只读动作/精确节点上限，签发与每次使用求交当前RBAC/grant/组成员。单独BearerIdentity/CommitGate避免subject适配丢失上限；拒绝浏览器头/混合凭据/重复头，不可认证Cookie管理API/执行/AI/Host。认证观察保存时钟底线，撤销和口令epoch变化均立即失效。

`go test ./internal/auth -count=1` PASS 9.322s，staticcheck PASS。第一轮两个测试resolver对node返回多个ID导致invalid check，修正测试资源fixture后通过，未放宽生产校验。固定锁真实SQLite组合六项PASS 6.026s，令牌摘要持久、重启后可使用、撤销后再次重启不复活。尚未与DS节点list prepare/SSE接线，也不声称Bearer迁移已获批准。继续Agent接入/传输平台。

## R06 生产连接状态机与真实WSS组合实验

提交 `7c43ce49f62f3e759ff2c7f6bf799945f3ef2e09`；额外staticcheck PASS，architecture PASS 2.128s。

internal/transport/controller_link.go实现既有Sender和ControllerLinks连接状态机；握手阶段、能力求交、每帧当前证书重查、事件与发送回调、连接替换、读写超时/有界帧、写失败UNKNOWN和禁止盲目重发。精确接线候选CONTROLLER-LINK-WIRING.md；FrameSocket不绑定待审第三方运行时，实际适配在独立固定依赖PoC，根锁未变。

`go test ./internal/transport -count=1` PASS 3.675s；固定`node tests/poc/v09/platform/run.mjs` PASS 5.311s。真实本机WSS执行生产hub的Hello→offer→ack事件，未准入发送被拒、重连关闭旧连接、旧离线不污染新连接、吊销后收发均拒绝。业务回调/Admit是fixture，未替代DS任务/节点实现，未把局部实验记为完整Agent执行验收。继续接入身份/其他平台功能。

## R07 接入凭据、CSR、签发与重装撤销

提交 `c537c2b98dddc655f035bc8e6deb351801cc788f`。

transport新增CertificateIssuer、EnrollmentService、SQLEnrollmentStore与测试；精确候选ENROLLMENT-WIRING.md。短期一次性token仅摘要持久、首次绑定CSR/request、默认pending、当前管理员gate/revision决定、证书与映射同Tx、重装原子吊销旧证书、指纹查当前撤销，临时测试CA私钥不进入Git/响应/持久状态。保留真实审计出箱，需接入统一audit Tx；不批准任何生产PKI profile。

单元测试含改CSR/请求重放、无审批不返回cert、提交失败仍pending、审批重放、CSR恶意CA/SAN不继承、重装旧证书拒绝、token期限/观察后时钟回拨、假管理员gate拒绝。第一次独立SQLite测试因引入transport缺protobuf sum失败；只在隔离module/sum补入根已固定protobuf v1.36.12，根锁未动。随后实际SQLite→关闭重开保留pending→失败回滚→批准→签发证书真实本机TLS握手→撤销→关闭重开仍拒绝，七项组合PASS 8.367s（bootstrap锁步骤）。固定锁复验记录随后补入。gate是fixture，未把本机TLS验成实际节点/统一任务/正式接入页面。

固定锁重验PASS 6.082s；继续加入真实DS SQLRecorder同Tx消费enrollment出箱，故意在auth状态+audit均写完后提交前失败，证明批准审计与证书映射一起回滚，最终七项PASS 8.366s。`go test ./internal/transport ./tests/architecture -count=1` PASS 3.470s/1.320s，staticcheck PASS。继续插件/Xray、AI/工作流、更新恢复等包内项。

## R08 两套Xray模板与真实目标版本测试

提交 `67c4c1d229cc2c9bf498014d1ded5eb23085f27f`；可复现run-xray.mjs复验PASS 2.990s。

可选包新增固定26.6.1两套RAW/Vision模板、当前预检/插件/版本/接管/revision/端口/证书/REALITY校验、指定业务凭据借用与短时配置回调。默认仅声明UI，不引入WASM生产runtime、不import核心、不调用Shell/SSH。详细输入/接线/边界见XRAY-TEMPLATES.md。

下载公开官方Windows64发布物，核对release asset SHA256和解包exe SHA256，未下载/接入Linux节点。真实Xray -test与两套回环inbound/API进程启动通过。初轮TLS临时证书测试把旧解析parent与新key混用，x509正确拒绝；改为新临时template/匹配key并同步预检DER后全通过，没放宽生产校验。显式SF_XRAY_LOCAL_TEST_BINARY的`go test ./plugins/xray-manager -count=1 -v` PASS 3.082s；staticcheck PASS。通用未供binary的运行SKIP该项。Linux双架构/完整客户端流量仍未验证，继续平台生命周期和其他子项。

## R09 持久工作流编排

提交 `b4aaa33405c213f6d46d86ae034199b481b975ed`。

新增Engine/SQLWorkflowBackend：固定action规范化、CAS定义、可信触发/等值条件、冻结计划、step幂等、统一taskId顺序推进/审批等待/失败停止/UNKNOWN暂停、禁用和修订后的派发checker、同Tx域/审计/提交端口。严格校验持久计划摘要与状态，没把unknown重新当pending。精确接缝WORKFLOW-WIRING.md；发现DS pin提交没有SubmitTx，已明确列为具体S03依赖，不在写事务内调用另一个Submit造成死锁，不复制第二套任务服务。

`go test ./internal/workflows -count=1 -v` PASS 2.031s；第一次staticcheck发现测试中的重复db赋值未使用，修正后staticcheck PASS；architecture PASS 2.083s。单元正向任务为明确double；真实SQLite组合只验证真实持久/重启/审计回滚与缺端口不能推进，不伪造任务成功。继续AI/插件/更新恢复包内项。

固定锁SQLite组合八项PASS 8.306s，包含实际workflow定义/冻结计划重开、域+audit提交前故障一起回滚及缺统一任务端口拒绝。

## R10 AI会话持久授权与逐动作限制

提交 `f738f491140f6cb341d8f3334765753da7b9ef8a`。

实现ai.SessionService/SQLSessionStore，精确组合见AI-SCOPE-WIRING.md。零资源/按资源动作的grant、组成员快照、当前权限求交、CAS/撤销/关闭、真实Web与资格gate、15/30/60分钟完整访问及有效模式、过期回拨拒绝、服务器规范工具参数/摘要/审批需求评估。补严早期纯Scope资源授权未限制细分动作的问题：新增NodeActions/GroupActions，缺动作拒绝，正向fixture也明确动作；未删除任何负向测试。多个不同快照的同组grant不能合并扩大动作，重复组grant明确冲突。

真实持久组合：auth只读当前观察+AI+DS audit同一SQLite文件，不嵌套writer；注入commit失败无grant/审计半提交，重启保留grant/policy，真实Cookie会话撤销后Assess拒绝。固定锁九项PASS 8.751s（后续视图字段对齐复验随后补入）。`go test ./internal/ai ./internal/auth ./tests/architecture -count=1` PASS 2.075s/9.811s/1.461s，staticcheck PASS；更新Eino实验仍PASS 4.032s，Sonic对Go1.27回退warning仍公开保留，没有生产框架选择/真实模型验收。

字段对齐既有AIApprovalPolicy、撤销审计节点快照后复验：AI PASS 1.962s，固定SQLite九项PASS 8.499s。

剩余scope审批/唯一tasks接线、消息/SSE/真实BYOK、资源推荐、HTTP与页面、完整PoC。当前继续包内其他功能。

## R11 更新工件私有暂存与复验

提交 `c986bf9d9acaffb1eaf14bad3e67afd919955908`；更新模块/架构复验PASS 1.913s/1.904s。

新增ArtifactStager/StagedArtifact，更新签名/平台/序列/哈希验证后写私有文件，再同FD复验；向唯一任务安装器仅借限长只读流，没有任意文件路径或写者，前后当前策略gate、一次使用、失败UNKNOWN不重跑，清理仅自己随机目录文件。原始UPDATE缺失不阻塞这些安全IO逻辑，但不批准实际安装profile/健康协议/自动迁移回退。

`go test ./internal/updates -count=1 -v` PASS 2.028s，staticcheck PASS；实际文件负向见UPDATE-STAGING.md。Windows未作Linux断电持久/真实安装证明。继续审批/插件/恢复与其他包内未完成项。

## R12 持久审批及完整消费绑定

提交 `5cb1801b95fed00518bebdaf26a3e6a989787269`。

实现ApprovalService/Identity aggregate记录：服务器准备计划引用、当前主体/批准者双侧节点动作及tasks.approve授权、AI任意root真人管理员、epoch/策略世代/期限/CAS、单次消费、同DS Tx写审计及已消费taskId。ConsumeTx强制实际任务的source/plugin/AI session/scopeRevision/action等完整字段，不能只按摘要/节点匹配。精确S03补充和已核对的DS缺口在APPROVAL-WIRING.md，未改公共接口/生成/schema/锁。

单元验证首轮PASS 10.675s，完整binding补严后PASS 20.995s（同时有隔离Go构建占用CPU），staticcheck PASS。固定锁真实SQLite组合十项PASS 6.015s，含批准持久、消费+审计在外层失败一起回滚、重启后批准仍可用、最终消费后再次重启拒绝重放。计划和任务metadata为明确fixture，不是完整统一任务/浏览器批准/Agent执行验收。main与共享接收状态仍8a7dd470，继续其他独立项。

## R13 插件私有工件与持久生命周期

提交 `2ce83a8a8ef5d4df2192b835cdf15cec8eb5dc73`。

新增PackageVault/LifecycleManager/SQLLifecycleStore：精确签名源/工件验证、默认禁用、显式启动停止、严格新版本与能力重新consent、卸载保留节点资源、单调revision/taskId防重放、未知运行态不假装卸载。唯一runtime实例ID防旧实例借新租约/晚到结果覆盖；禁用先封Host再停实例。没有正式运行时/源/密钥就不启用、不构造假官方包，不增加根依赖。详见PLUGIN-LIFECYCLE.md。

实际测试：真实SQLite/私有文件/DS审计事务组合十一项PASS 6.574s，含安装+审计故障回滚、重启保留disabled/防重放、nil runtime不能enable、卸载不删除工件。runtime正向和并发为明确double；没有假记实际WASM/Xray业务执行。首轮staticcheck提示等价struct可直接类型转换，修正后PASS。补入晚启动实例隔离/严格升级/新增权限consent测试后`go test ./internal/plugins ./tests/architecture -count=1` PASS 2.290s/1.926s，staticcheck PASS。继续恢复/入口组合及仍可独立实现项。

## R14 数据库外持久恢复隔离

提交 `daf38b13cdf681c41df75981a0921d0713e4d457`。

新增recovery.QuarantineLatch及RECOVERY-QUARANTINE.md：Begin先独立落盘、绑定外部expected hold/快照/新主控/Root/信任与撤销底线；记录丢失/半写不当新安装，解除后不复用恢复回合。Release只有可信程序回调完成实际离线核验/对账/审计后才能写不可覆盖的解除记录；每次Check仍查当前权威/旧主控隔离，未注入verifier保持quarantine，无HTTP claimed-fenced放行。Linux文件/目录Sync；Windows证据不等于断电持久。

`go test ./internal/recovery -count=1 -v`首轮PASS 2.421s，staticcheck PASS；补入已解除目录不能再次Begin。固定锁实际SQLite十二项PASS 14.646s，包含真实BackupTo→撤销Cookie→RestoreFile到新文件：旧库确含撤销前Cookie，但数据库外hold及重新打开的闸均拒绝访问/解除，已有目标拒绝覆写。真实离线材料/旧主控物理隔离/会话epoch与任务对账事务仍BLOCKED，单元正向verifier明确double。继续包内可独立项。

全量检查正在运行。npm ci PASS（127包/50s），首次tools:install因公共proxy.golang.org IPv6连接超时FAIL；GOPROXY=off复验因deprecation元数据缺失FAIL。随后仅本命令环境切换公开goproxy.cn并保留Go校验及固定版本，四工具安装PASS，未改项目配置/锁。具体日志在自己reports/_review/v09-platform/resumed-*，最终check结果另记，不能把启动命令视为已通过。

R14完成后结果：recovery/architecture复验PASS 2.390s/2.080s，staticcheck PASS。全量npm run check五套policy/contracts/go/web/git全部PASS，check:security PASS（npm audit 0、govulncheck无漏洞），对应reports/check-all.json 2026-10-30T06:19:48Z及check-security.json 06:19:58Z；覆盖R01—R14当前代码（后续改动另验），并非完整v0.9/真实Linux验收。

## R15 Xray自定义配置审查及管理API代理隔离

提交 `d4e45fe9659d897a8a7de5c9452f1f85c41f03f9`；补充模块/architecture复验PASS 2.772s/1.909s，staticcheck PASS。

新增ReviewCustomJSON，精确边界见XRAY-CUSTOM-REVIEW.md。保留原JSON并绑定摘要，严格有界/重复字段拒绝、精确文件授权、API回环/专用路由及代理端口拒绝、固定风险warning；没有用最小审查冒充目标完整schema。标准模板补入管理端口/回环目标blackhole，避免普通代理客户端绕进本机API。未改公共契约/锁或将Xray编入核心。

首轮模块测试PASS 2.658s/staticcheck PASS。首次实际Xray模板-test FAIL：新增IPv4-mapped CIDR不被目标26.6.1接受，使用无秘密最小配置确认具体错误（CIDR104>32）；移除该不受支持写法，保留127/8、::1/128及独立管理端口拒绝。随后run-xray.mjs PASS 2.957s，含两模板实际-test/启动、两份custom实际-test、真实目标拒绝未知协议，以及本机真实SOCKS代理不能取得私有API的HTTP/2响应（直接连接先确认API真实可用）。不是Linux部署/完整VLESS流量/Host准入证明。继续包内独立功能。

## R16 AI持久消息、生成未知防重放及真实SSE

提交 `68233b86b63a51efb7846492360ee065b4648265`。

新增ConversationService/消息和生成状态/持久事件、未挂载MessageEventsHTTP，复用scope聚合和审计同Tx，无自动执行器。消息脱敏与模型配置/Provider无默认实现，BYOK仍须受控凭据桥；文本输出不能声明真实taskId。精确行为/容量/共享字段候选见AI-MESSAGES-WIRING.md。补入乱序排队历史成对组合，生成前持久fence，出错/停用/撤权/超限/提交失败UNKNOWN不重放，忽略emit错误也不能将部分结果标成功。

初轮AI测试PASS 2.075s/staticcheck PASS；新增SSE与修订后AI/auth/architecture PASS 2.875s/27.588s/1.556s。继续加固后AI复验PASS 2.924s/staticcheck PASS。固定锁SQLite首次FAIL：领域unknown到DS审计的原因码大写不被允许，终态保持generating、未误报成功；改用明确小写ai_generation_unknown映射（领域仍unknown，未改DS枚举）。随后十二项实际SQLite组合PASS 8.869s，含消息+审计提交前故障、回答+审计回滚、重启后保留unknown防重放、真实HTTPS Cookie SSE/游标续读/撤销关闭。模型/过滤器为明确double，没有实际模型或真实秘密。继续包内实施，R15—16后的全量检查另记。

R15—R16新一轮全量npm run check五套全部PASS（reports/check-all.json 2026-10-30T06:55:32Z），check:security PASS（06:55:37Z，npm audit0、govulncheck无漏洞）。未重新安装或改动未变的依赖，沿用本轮已PASS的npm ci/tools:install。main仍8a7dd470，未接收共享差异；DS自己分支新增隔离VM/供应商证据，仅只读核对文档，不把它当成本会话已获准连接VPS或已验证CX链路，未读取凭据/接入该VM。

## R17 Agent持久开始阶段及重连快照

提交 `62f3e7d75fb3933b69629bad1c6b0d7ab5126781`。

executeObserved增加claim持久后ack、started marker持久后start、最后ctx/TTL复查后Run；通知失败不执行并保留UNKNOWN防重放。增加Run异常UNKNOWN、重放终态enum/输出验证、既有StateSyncReply有界私有ledger对账。精确行为/旧记录兼容/截断及容量局限见AGENT-EXECUTION-SYNC.md，不启用生产运行配置。

首次新增测试编译FAIL（误用不存在的validOffer fixture名称），改为现有testOffer，未调整生产准入或删除测试。随后`go test ./apps/agent ./tests/architecture -count=1` PASS 3.223s/1.847s，staticcheck PASS。实际私有文件阶段/失败重启/不重复副作用/异常与损坏记录/截断/取消已测；不是Linux/WSS完整执行链。继续主动连接/事件循环等独立项。

## R18 Agent连接应用循环与真实WSS执行实验

提交 `5fd0ba3e74fa5d95dc47e82fb98dbc56e2917f48`；随后Agent/transport/architecture复验PASS 4.718s/3.489s/1.488s，staticcheck PASS。

新增私有runAgentSession：Root pin/真实TLS来源/当前信任与本地Admit无默认放行、有界Hello/能力/帧协商、Proto绑定/序列、真实Probe心跳、持久Ack→Started→Result、在途去重/并发限额/取消/live对账覆盖、断线UNKNOWN与合作worker等待。未接main/根WSS锁/生产profile，日志暂未转发并明确truncated。精确边界AGENT-SESSION-WIRING.md。

首次staticcheck报未被调用的私有循环（U1000），补入有意义的原循环测试和隔离实际WSS调用，不加抑制。根Agent测试PASS 3.626s/staticcheck PASS。run.mjs原样复制应用源码与测试到隔离agentapp，记录逐文件hash；真实WSS测试PASS 2.10s，Agent应用测试包PASS 5.285s，原WSS/WASM/controller实验也PASS。真实临时文件副作用、重连相同Offer只一次、持久终态对账、真实Ping/Pong和吊销断线在途UNKNOWN均已测；业务授权/审批/审计是明确fixture，不声称Linux/正式任务/完整v0.9。继续日志等独立项。

## R19 Agent日志落盘前脱敏与生产游标

提交 `1dcfcf2d5c4ab6445d8b2dd252e7eec1ff6d94a2`。

新增FilterOutput（缺失/失败即扣留）、绑定脱敏终态哈希的不可覆盖output manifest、最多64帧的既有TaskOutput发送及StateSync生产序号。作用结果/源端截断/接收缺口明确分离，重放不重新产生作用或日志。精确语义和DS缺的execution/streamSequence/日志补读接缝见AGENT-OUTPUT-WIRING.md；未改公开Proto/根锁。

`go test ./apps/agent -count=1` PASS 5.046s/staticcheck PASS。固定WSS/WASM/Controller+Agent隔离实验PASS，Agent包9.323s；真实WSS脱敏日志、游标重开、实际首帧+第二帧前注入故障、重连缺口/同offer不再执行已测。过滤器缺失/错误/异常不持久合成secret；篡改终态、分块重解释、64片段截断负向通过。不是Controller持久日志ACK、正式能力、Linux/实际root执行证明。继续包内工作，本轮全量复验随后记录。

R17—R19全量复验：npm run check五套PASS（2026-10-30T07:23:09Z），check:security PASS（07:23:18Z，npm audit0、govulncheck无漏洞）；隔离WSS/WASM/Agent含测试依赖govulncheck PASS；Controller/Agent Linux amd64/arm64四个交叉编译PASS，入口仍未启用真实profile，不当完整发行物。受保护go.mod/go.sum/npm manifests/锁/dependencies/API/Proto/gen/CI/AGENTS相对基线零差异，main仍8a7dd470。

## R20 自定义Xray出站覆盖目标的API隔离补严

复核固定上游Freedom源码发现，routing后的redirect/出站端点可改写实际目的地，仅检查原始端口/路由tag不足。增加出站endpoint/URL/structured address+port到管理端口的拒绝，含DNS别名与非规范前导零；redirect/finalRules追加明确风险码。只修自己候选审查，不接生产或修改原始需求/契约。根模块/实际版本测试随后记录；首次run-xray复验PASS 3.343s，新增危险配置“实际-test语法通过、安全审查拒绝”回归，不启动该配置。
