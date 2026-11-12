# 任务登记板

## 当前：身份续期与运维可用性返修（2026-11-12 R37）

V09-CLOUD-IDENTITY-OPS-01 / chatgpt-repair-20261112，主控单会话；独立ServerFleet-repair / fix/agent-identity-and-operations-20261112，从v0.9.0/main be107188恢复并核对完整Git树ba97dd15。用户授权按已评估问题修复并发布新预编译包。允许Agent本地身份原子续期、transport当前mTLS身份续签/审计与幂等/撤销、Controller装配与批准后明确节点读取授权、auth升级预检/脱敏分类错误、通用readyz/结构化访问日志、nodes有限诊断快照、相关Web交互与独立R37产品/API候选、部署/升级/反代示例、v0.9.1发行说明与请求及必要检查。公共接缝由总控协调；保持旧49迁移/原SQL与checksum/Proto/gen/依赖锁/原始ADR不变，新增迁移只追加，不启动其他Agent、不引入依赖或Runtime、不访问真实VPS/供应商/旧生产DB/Root或发行私钥。

续期必须在有效mTLS当前身份、节点状态、信任代次及CSR持有证明下完成；重试不得重复签发，旧证书仅在新证书被实际使用后有限重叠退役，失效身份不自动重新注册，UNKNOWN任务不重放。升级错误保留fail-closed且区分缺失keyring/权限/格式/数据绑定，不重建已初始化库密钥；支持旧格式的现有兼容路径和升级前备份说明，不冒称已验证用户未提供的旧库。节点授权保持精确范围，不开放管理员通配符；日志和诊断不得泄露秘密。集中专项后一次精确head完整CI与安全、COMMENT复审/expected-SHA正常合并，再发布v0.9.1双架构unsigned包/校验/溯源，不覆盖v0.9.0；CI状态每10分钟检查。纯文档不单独安排全量CI。停止于修复、文档、检查、合并和实际Release资产核验完成。

## 当前：预编译包与 GitHub Releases（2026-11-12 R36）

V09-CLOUD-RELEASE-01 / chatgpt-release-20261112，主控单会话；独立 ServerFleet-release / chore/precompiled-release-20261112，从 main4972eb5 开始。用户明确授权在现有公开仓库发布预编译包。允许发行工作流、打包脚本、发行/部署说明及本登记；CI共享差异由总控协调。复用固定工具和已有build-release，构建Linux amd64/arm64核心Controller/Agent/Web与示例、SHA256和源码溯源，检查通过后正常PR合并并手动触发发行。无离线签名密钥时明确unsigned，不生成冒充受信任的签名材料，不绕过一键安装/更新验签；不含可选插件、状态或秘密，不改变生产代码/契约/锁/迁移/ADR，不启其他Agent。初次发行设施PR27精确head7248862的CI43149072533通过并合入43c355a。随后浏览器断开，手动触发不可用；同任务追加workflow_call和仅v0.9.0的main发行请求入口，复用已审查流程，不新建凭据、不扩展部署权限、不覆盖既有tag/Release。追加部分按新精确head CI/COMMENT审查及expected-SHA正常合并，发行请求随合并触发。十分钟一次检查运行状态；停止于实际Release及资产核验，真实部署由用户负责。

## 当前：首个正式版 README（2026-11-12 R35）

V09-CLOUD-README-01 / chatgpt-readme-20261112，主控单会话，从PR25/mainc045e7f建立独立ServerFleet-readme / docs/first-release-readme-20261112。用户明确将当前版定位为首个正式版并更新README。允许README及本任务登记：按既有版本号v0.9.0重写首发介绍、功能、构建/部署、文档导航与已知限制；不更改程序/接口/锁/迁移/原始资料，不创建tag、GitHub Release或冒称发行包已签名发布。不把版本命名视为Xray/Bearer已装配、完整G2或真实环境验收完成。核对链接/构建参数与内容差异，保留代码基线CI范围；按精确head检查/COMMENT审查及expected-SHA正常合并，停止于文档接收。


## 当前：全仓审计、现行文档与结构整理（2026-11-12 R34）

V09-CLOUD-AUDIT-01 / chatgpt-audit-20261112，主控单会话；从已合入PR24/main8aa1ac0建立独立ServerFleet-audit / chore/cloud-audit-20261112。用户明确授权代码审计、完善功能/API文档、优化目录结构、清理死代码。允许全仓生产源码审查及必要修复/同包拆分、内部无引用代码清理、现行产品/接口/结构/审计文档和防漂移检查；共享结构由主控协调。不启动其他Agent，不改原始资料/已批准ADR/历史迁移SQL/checksum/Proto字段号/生成/依赖锁，不删除历史证据/候选/可选插件或未准入功能，不引入新Runtime/第三方依赖或访问真实VPS/供应商。

先审计授权/任务与UNKNOWN、网络输入/秘密/凭据、生产装配和文档漂移，登记实际范围及证据；只清理有引用分析依据的死代码，保持公共接口和数据兼容。功能文档明确已实现、未装配、待实机验收；现行API给出覆盖范围与安全语义，不以历史候选冒充实际路由。整批改动后集中必要检查、安全与精确head CI、主控COMMENT复审及expected-SHA正常合并。停止条件为审计报告、整改、文档/目录收口、可复现验证与合并完成；完整产品/G2/插件Runtime和外部验收不因此自动关闭。


R34实现记录：确认并修复传输别名冲突和手动探测请求期限问题；同包拆分Controller目录/路由/身份/适配器，AST核对35个函数，除两项无读字段构造赋值外保持函数行为；清理8项无调用Web辅助符号。README/功能/API/结构/审计入口更新，266路由声明按精确/兼容别名关联候选，独立R34补14项操作并加入漂移检查。专项27项Web及监控deadline、Controller/monitoring/架构通过，安全扫描无漏洞；集中检查与最终精确head CI/复审/合并回执在PR评论登记。Xray生产执行和Bearer读取装配缺口保留OPEN，不假装已完成或删除端口。

## 当前：指定节点的服务监控探测（2026-11-11 R33）

V09-CLOUD-NODE-PROBES-01 / chatgpt-node-probes-20261111；主控单会话，从已接收bc0d552建立独立ServerFleet-r33 / feature/cloud-node-probes-20261111。用户继续完成整个项目；本项补原有sourceNodeId尚未实现的节点探测。允许monitoring、Agent固定只读探测、transport typed参数/回执、Controller统一任务与mTLS装配、共享纯网络探测代码及明确依赖清单、Web监控页、独立R33产品/API和追加迁移/必要检查。所有权/公共接缝由本主控协调；不启动Agent，不引入第三方依赖/插件Runtime，不改旧48迁移SQL/checksum/Proto/gen，不访问真实VPS或供应商。

只读HTTP/TCP/TLS探测走统一节点任务，目标与源节点权限、配置修订、来源、当前执行在派发和I/O持续校验。Agent公共探测拒绝私网/metadata/DNS重绑定与重定向越界，不提供任意Shell/请求正文/认证头。离线/能力缺失/调度中断/未知任务只形成unknown观察，不把源故障计为业务宕机；旧未知任务不重放。新观察保持实际任务关联及原有连续阈值/告警outbox。整批编码后集中必要检查、最终精确head CI与正常合并；真实联调由用户进行。


R33实现记录：Agent固定探测、共享netprobe、Controller持久意图／授权与typed回执、正式Web来源选择及真实任务历史、独立R33产品／API已实现。49项迁移；旧48项SQL／checksum与Proto／gen不变。集中policy／contracts／435项Web／构建／安全PASS，本地全Go仅真实Nginx用例因缺nginx失败，保留记录；受影响包及最终精确head CI、COMMENT复审与合并状态在PR评论登记，不用R32绿灯替代。

## 当前：持续监控、流量与告警投递闭环（2026-11-11 R32）

用户在R31合并后要求继续，沿既有完整项目授权推进下一项实际源码缺口。V09-CLOUD-MONITOR-ALERT-01 / chatgpt-monitor-20261111；主控单会话，从main7cc452d按Git树38675ac5恢复独立ServerFleet-next / feature/cloud-next-completion-20261111，保留其他目录。现有监控RunDue、quota.RunReminders未被Controller调度，monitor未注入Notifier、SMTP/QQ生产端口未装配，Agent流量未入配额。允许monitoring/quota/notifications、必要auth凭据用途与Controller/Agent metrics接缝、正式Web监控/流量/通知、独立R32产品/API/追加迁移与必要检查，另同步当前进度消除过期未实现标记。保持原45迁移SQL/旧fixture哈希/原ADR/Proto字段号/生成代码/依赖锁不变；不启Agent、不引入Runtime或第三方依赖、不访问生产/VPS/真实供应商。真实指标仅经mTLS身份与当前权威进入，缺失/重置/跨月不伪造全月零流量；通知先持久化意图，UNKNOWN不自动重发；当前权限/凭据版本、隔离锁和撤权需持续复核。集中编码和专项后一次同head完整CI，主控COMMENT复审及expected-SHA正常合并。停止条件为该闭环源码、页面、文档、检查与合并完成；真实账号/服务器/浏览器联调由用户进行。

R32安全扫描实际发现Go1.27.3标准库GO-2026-6859（HTTP/1 MaxBytesReader头部竞争崩溃），官方1.27.4已修复。总控在当前授权内同步go.mod、固定工具清单及当前工具链文档为1.27.4，不增加第三方依赖、不改变运行时架构、不放宽任何检查。旧扫描FAIL保留，补丁版重新检查及精确head CI作为接收依据。

R32源码与Web/API文档已齐：新增Agent网络计数、持久采样覆盖与账期配置、监控阈值/渠道/outbox、当前所有者授权后台、真实SMTP/QQ装配和统一投递任务/借用凭据/UNKNOWN人工入口。专项通过，Web全量434PASS；初次完整检查的policy/contracts/web/git通过，Go发现新增迁移导致子集计数16→19需同步以及本环境缺nginx的已知FAIL；旧Go1.27.3安全扫描FAIL已保留。现按1.27.4重跑并在最终PR评论登记精确head原生CI，不提前视为合并或生产验收。

## 当前：证书续签安装发布与一键安装（2026-11-11 R31）

V09-CLOUD-RENEWAL-INSTALL-01 / chatgpt-delivery-20261111，主控单会话，从main b3d13b77建立独立ServerFleet-delivery / feature/cloud-renewal-install-20261111。用户明确完成续签安装发布编排及哪吒式一键安装。允许certificates/Controller证书、网关/发布必要接缝、bootstrap/Agent安装接入、Web证书/添加节点、scripts/deployment/独立R31产品API/追加迁移/必要检查与发布构建元数据；不启其他Agent、不改旧43迁移SQL/checksum、不引入Runtime/第三方依赖，不触碰生产/VPS/真实供应商/离线Root私钥。保持统一任务、当前权限、独立修改审批、审计和UNKNOWN不重放；首次信任必须HTTPS与外部核对的公开签名密钥/Root pin。离线Root流程不在线自动化。集中编码和专项负向后同head完整CI，主控按既有授权expected-SHA合并，停止条件为两项源码、页面、文档、检查、PR与合并完成。

R31 实现检查点 b7f2c0a 已保存在任务分支。续接环境恢复后核对原树 4be2a899，重建未提交的测试/文档与恢复入口。专项真实 HTTPS/签名/SQLite/CSRF、确定性后继与不重放测试通过；首次专项发现私有测试目录权限与界面夹具结构错误，已修正；旧发布回滚兼容查询转义错误已修复并全包 race 复验通过。安装专项、HTTP专项与Web五项全部通过，安全扫描无漏洞；完整构建/契约及精确 head CI 随后登记 PR 评论。真实 systemd/root、供应商及跨节点联调仍由用户负责。此前 Redocly 遥测被自动审批阻止；现显式关闭 CLI/SDK 两层遥测及 DO_NOT_TRACK，保留全部检查门。

## 当前：异常／UNKNOWN人工处置完整闭环（2026-11-11 R30）

V09-CLOUD-INCIDENT-CLOSURE-01 / chatgpt-incident-20261111，主控单会话，从main5e9acae建立独立ServerFleet-incidents / feature/cloud-incident-closure-20261111。环境中断后按Git对象摘要从main恢复全量源码，树81306c68已核对，旧目录不修改。用户明确要求完整闭环。允许internal/tasks证据／快照／后续任务结果／复核阻断与重新核查、AI最小事实端口、Controller及备份只读核验接缝、Web任务面板/API适配、独立R30产品/接口、追加迁移与必要回归。保持统一权限、任务、独立审批及审计；不改原执行结果、不重放UNKNOWN、不加执行通道、第三方依赖或Runtime，不访问生产/VPS/供应商。

本轮范围：结构化证据留档；当前任务结果快照；新鲜快照及后续任务终态阻断复核；来源变化后旧记录仍可追溯；结案依据变化自动重新核查；备份只读核验任务创建与关联；状态/来源筛选与分页。原42迁移保持，追加证据迁移为43项，历史样本数量和checksum保持。集中实现和专项检查后按精确head组合CI接收，不逐处重跑全套。停止条件为此任务代码、文档、检查及可审分支完成；按已有授权检查通过可由主控合并。


R30实现与验证记录：已完成后端证据／快照／任务约束、Controller HTTPS接口、Web工作流与独立产品/API候选。初轮Go竞态专项与真实HTTPS路由专项通过，Web处置4项通过；随后集中check/check:security和精确head CI待核验。专项使用合成任务／明确测试响应，不代替真实节点／存储／真人双账号联调；最终结果在PR评论留证，未提前声称已合并。

## 当前：备份异地上传/保留清理与异常人工处置（2026-11-10 R29）

用户明确完成这两项。V09-CLOUD-OPS-01 / chatgpt-ops-20261110，单会话主控，从main b49afc80按完整Git树恢复独立ServerFleet-ops / feature/cloud-backup-resolution-20261110，原目录保留。允许internal/recovery、tasks、ai必要观察端口、auth备份存储凭据用途、Controller受控装配、正式Web备份/任务/凭据及独立契约/产品/追加迁移/检查；不改历史SQL/原始资料/生成/锁/已批准ADR。实现密文真实远程存储、自动任务与保留预览/保护/删除回执，以及异常统一收件箱/认领/证据/复核/后续任务关联，原UNKNOWN和审批不可复活或改写成功。所有执行经统一任务/当前权限/审计，生产凭据/外部供应商由用户配置，合成专项验证。集中功能包后做必要检查，不逐处重跑全套；远端检查点与PR交付，已授权按精确head检查通过后合并，不自我APPROVE/直推main/强推。

代码已实现并保存远端检查点73d1138，正式Web入口、S3/WebDAV密文条件上传／读回验证、当前凭据租约、保留预览／独立审批／真实删除、人工处置独立复核和恢复失效均已装配。只追加cx/0021、cx/0022，组合42迁移，无依赖／历史SQL／生成文件漂移。使用见[BACKUP-OPERATIONS](BACKUP-OPERATIONS.md)，产品/API见[R29](../contracts/revisions/v09-r29/CHANGELOG.md)。

本地集中检查：npm ci和固定工具安装完成；Web类型／424项测试／构建PASS，policy／contracts／security PASS（npm audit 0，govulncheck未发现漏洞）；受影响包go vet通过。SQLite人工处置、真实本地TLS S3/WebDAV条件传输及删除、SSRF／秘密歧义、保留／下载／固定清单／重启UNKNOWN专项通过；Controller HTTP曾因测试缺少初始化幂等头失败，修正测试；再发现新增凭据用途未进入AEAD许可清单，补齐生产用途后HTTP和凭据专项PASS。不删除旧失败记录或放宽认证。补备份10分钟有界租约、官方AWS签名向量、真实副本最少数量及满事件恢复保护后，最终精确head交原生CI全套核验；只以该head的CI证明组合验收。真实供应商／VPS／离线恢复联调仍由用户完成。

最终专项`go test -race ./internal/recovery ./internal/tasks ./tests/integration/v09/platform -run TestOffsite|TestIncident|TestR29`三包PASS；新增凭据用途／10分钟租约专项PASS，官方AWS签名向量PASS；staticcheck（显式固定Go PATH）PASS。最终源码继续由远端同head Foundation CI执行完整Go/Web/契约/边界/安全检查，结果在PR评论登记，不以旧检查点绿灯替代新head。

收尾复查发现既有Web凭据创建适配器未发送后端强制的Idempotency-Key，导致新增备份凭据无法通过正式页面保存。此处作为本任务必须修复的凭据装配接缝，允许修改共享endpoints.ts的credentialsApi.create签名及设置页稳定请求编号，补现有设置页断言；不扩展凭据明文读取或未准入轮换端点。

集中全量检查的policy／contracts／Web424／git PASS；Go 2337个用例及子用例通过，但四项历史迁移用例因40→42及历史选择器遗漏FAIL，另一个既有真实Nginx集成因本地未安装二进制FAIL（不跳过或伪造）。允许修正tests/integration/v09的历史选择器及当前最终数量，保留原25／28／21样本数量、旧SQL／嵌入JSON及checksum，增加actual21数量检查；修正后仅定向复验四项迁移用例，最终原生CI复核整个组合与真实Nginx。

## 当前：合并开发代码（2026-11-10）

用户明确“开始合并”。V09-CLOUD-MERGE-01 / chatgpt-merge-20261110，单会话总控，允许当前任务分支、PR基线调整、既有实现及检查阻断的必要修复、检查记录与按精确SHA合并main；不直推/强推main、不自我APPROVE，不扩展生产操作。#17精确ca309ea的CI42651887446两job成功，已合入main c88dca45；#18 d4f54ba的CI42788427970因回滚测试重启后立即取消与健康末次核验竞态失败。保留生产UNKNOWN，修正该测试对实际退出/持久锁的核对。#18/#19已转main基线，最新组合9df049e包含全部前置代码；待同head完整检查合格后接收，旧失败不抹去。

本地固定Go1.27.3、实际UID1000，`go test -race ./internal/updates -run '^TestResidentSupervisorUnhealthyRollbackAndPersistentFloor$' -count=5` PASS（8.670s），gofmt/diff检查通过。仅修改该取消测试与本登记，不改生产安全门/依赖/接口/迁移/CI；完整组合检查由最终head的原生CI完成，结果在PR评论按精确SHA记录，避免为记录结果重触发检查。

9f630d1的完整CI42828605746：dependencies PASS；foundation因旧Wave3 AI管理集成仍向固定自检/受控Root配置传入自由command字符串而FAIL（NOT_AUTHORIZED符合R28参数规则）；Go其余用例和Web422项通过。按R28更新该用例为空参数自检/完整受控Root配置，新增自由command拒绝及有效Root配置仍要求APPROVAL_REQUIRED断言，保留一次性审批、非owner拒绝和独立任务审批检查；不放宽生产实现或删skip旧断言。后续精确head重跑完整CI，不将旧head结果标成通过。

上述Wave3用例本地`go test -race ./tests/integration/v09/platform -run '^TestWave3AIManagementScopeApprovalsAndRootShellGate$' -count=1` PASS（6.817s），仅更新测试与登记；完整组合等待最新head的原生CI。真实模型/节点/Root与产品联调仍由用户进行。

## 当前：AI运维完整Agent循环（2026-11-10 R28）

用户明确要求完成AI运维完整Agent执行循环。任务V09-CLOUD-AI-LOOP-01 / chatgpt-ai-loop-20261110，从d4f54ba恢复独立ServerFleet-ai / feature/cloud-ai-loop-20261110，单会话主控，不启动其他Agent。允许internal/ai、Controller AI/命令/统一任务授权接缝、正式Web AI及契约/产品/检查记录；复用现有Go/BYOK/统一任务、审批和审计，不引入未准入Runtime、AI直连Agent或Shell，不改旧SQL/生成/原ADR。模型计划、审批等待、真实任务结果、多轮继续、取消/期限/步数和重启UNKNOWN均需实际代码。用户负责联调/bug验证，本轮集中编译/静态后交可审分支，不逐处重复测试。

本包代码已完成：四个固定真实工具、原生函数结果续传、有界后台驱动、两层审批/任务绑定、当前资源门、取消、显式恢复及正式Web；检查/使用见[AI-AGENT-LOOP](AI-AGENT-LOOP.md)，接口/产品同步R28。已保存f3806ae远端代码检查点，收尾追加恢复/披露规则与文档。状态CODE COMPLETE / 待用户联调与PR接收；不把源码/合成专项当作真实模型、节点、Root或发布准入。

## 当前：核心可运行代码交付（2026-11-10 R27）

用户明确“我自己联调和找bug吧，先把完整的、可使用的代码搞出来”，随后要求继续。V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110，单会话主控，从9e96f47恢复独立ServerFleet-usable / feature/cloud-usable-core-20261110；完整源码按Git SHA恢复，新增代码先分批保存远端。允许核心模块/CLI/构建/配置/产品契约接缝，补可运行部署/初始化/签名/分别信任与空表启动，只做必要编译/静态后交付。用户接手实际联调和bug验证；不新增验收套件，不因待验证继续冒称缺少已实现源码；不改原审批、SQL历史或秘密/生产边界。

## 当前：灾备激活阻断修复（2026-11-10 R26）

用户补充授权后继续V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110，独立原分支从70932d1接续、单会话主控。修复activation/fence固定凭据写入与授权缓存验签/容量边界；新增真实mTLS/离线Root/实际owned进程stop-wait/activation/重启负向组合，完成后仅集中检查受影响包及共享静态规则。R25精确CI已通过，新head独立检查；不访问生产/VPS/真实供应商、不提权、不绕过审批/未知状态，不提前关闭五项。

## 当前：五项核心能力组合收口（2026-11-10）

用户“继续”；V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110 在独立 feature/cloud-public-renewals-20261110、ServerFleet-resumed，从 c7520ba 接续原五项核心能力。单会话主控允许目录和共享接缝，无其他Agent、生产/VPS/真实外部账号或提权操作。当前实际UID1000，可运行此前受UID0阻挡的普通用户子进程验证。集中修复命令policy双读竞态、启动readiness请求丢失、未决mailbox重启及父进程审批时效，补实际进程/签名/续传/撤权/回滚/隔离回归后组合检查；原失败保留，仅复验受影响部分。R24及原13项提交保持；R25登记差异，无旧SQL/hash或生成改写。交接须有真实检查和可审分支；真实Root/完整双机/外部/G2仍按实证，不冒称全部DONE。

## 当前优先子包：五项核心能力（2026-11-09）

用户明确要求先完成 Controller 自身更新、远程 Agent 批次更新、实际灾备接管、Agent 执行端完整续传、普通命令与受控提权。沿用 V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01、当前集中编码分支和唯一目录，起点 2d36f9d；五项已有源码与产品入口，已执行集中检查并修复发现的问题；Web422/契约/安全/双架构候选构建通过，普通UID与新功能专项/完整灾备仍BLOCKED或待证；仍IN PROGRESS，完整验收未通过。R20—R23 含受控命令、节点确认前缀恢复、Controller/Agent 独立监督更新、实际拥有进程 fence 与新权威激活（预计39项迁移）。主控继续单写既有允许模块及必要受控 CLI/Web/独立候选/追加迁移，不启动其他 Agent，不访问生产/VPS/真实供应商或执行提权。普通/root 能力必须明确配置，Root 必须真人管理员当前审批；恢复释放必须实际验证 fence，不能用自称已隔离的布尔值/文档代替。先连续编码，完成整包后一次集中验收，任何环境硬依赖保留 BLOCKED，不把代码提交当作五项 DONE。

## 当前：集中完成全部剩余代码（2026-11-09）

## 当前：集中完成全部剩余代码（2026-11-10恢复接手）

用户要求接替共享对话 `6af2951c-bef4-83ee-914a-5be9ac5e8426`。先恢复本地2d36f9d，随后核对远端发现13项后续提交；已完整校验原始Git对象并在独立 `feature/cloud-public-renewals-20261110` / `ServerFleet-resumed` 从最新ca309ea整合，原分支和历史保持。唯一会话chatgpt-resume-20261110继续单写，无其他Agent。R24新增公开证书续签审批策略/正式Web/API/恢复失效，追加cx/0020、预计40迁移，此增量UNTESTED。R20—R23原源码、39项迁移及历史CI/验收记录保留，新代码不借旧结果标PASS。

用户最新要求：先集中写完代码，再统一一次大体验收；后续收尾由用户另行交DeepSeek，不自动派发。V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01；feature/cloud-v09-code-completion-20261109 / ServerFleet-completion；IN PROGRESS。唯一基线main9738648ecdf44546e4a2d3503f8c9b862c8a5cb3（#16接收；精确head c07bd976，CI42487322510 success，Go987顶层/1309子项PASS、7SKIP、0FAIL，Web422PASS）。环境重建后从私库重新clone，不复用旧Windows目录/秘密。

主控单写所有剩余必要生产模块及正式Web、Controller/Agent受控入口、内部公共接缝/独立候选/追加迁移、相应记录；不启动其他Agent、不恢复三平台并发。先接正式发布路由/实际HTTP健康与逆序补偿、ACME/node-local CSR及受控业务组合，再接更新固定可信来源/批次、加密备份/离线恢复/新权威fence、普通执行真实权限及其他实现缺口。新增共享接口由主控协调；旧SQL/hash/原始资料/历史ADR/原候选保持。Xray只做获准可选范围，Runtime/安全准入不能自行宣布通过，WASM/Eino不准入。

按用户最新要求覆盖旧逐增量检查节奏：本连续包期间集中编码，仅必要格式整理，不逐小改动/逐包重复测试和收尾；完成代码后再统一check/security/必要组合的大验收，保留FAIL/BLOCKED/SKIP，不降权限/版本/门槛。进度通过本任务和远端分支检查点保留，最终PR仍须精确head检查/COMMENT及expected-SHA合并，不直接推main/强推/自动合并/同账号自批。没有生产/VPS/awskr/真实供应商/云API授权，不制造外部验收或秘密。停止条件为全部可实施代码与明确剩余硬依赖登记、统一验收与可审交接；G2/发布认证和真实外部/物理/ARM/硬1C1G不因写码关单。


## 已接收检查点：正式Web与核心构建装配（2026-11-08）

V09-CLOUD-DEPLOY-IMPLEMENT-01 / chatgpt-takeover-01；feature/cloud-deploy-assembly-20261108 / ServerFleet-deploy；CHECKPOINT ACCEPTED（#16 / main9738648 / CI42487322510）。依赖#15 b4305fed7fb03fcbce6d3f87654a71e4e51be4a5已接收为main6031c1f9d9b2b840ef5a477520aa937799bb3ba5，CI42485042788 success，当前已正常合并该共享基线。原#15源码冻结等待CI，不并发修改其分支。

允许apps/controller正式Web静态装配/启动选项、apps/agent/runtime的已签名构建版本默认值注入、scripts可移植发布构建、相关专项与docs当前进度；主控单写，无其他Agent。目标为同源HTTPS正式控制台而非测试fixturehost、受限构建资产/保留API/隔离门、固定工具与独立输出目录的核心发行包/字节清单；可选Xray不编入核心，不添加依赖/秘密/部署行为。集中一个功能包后检查，用户手动验收。停止条件为实现与必要路径/方法/权限/启动负向及精确head检查/PR接收；完整发布/更新/恢复与G2仍OPEN，无生产/VPS/外部供应商/云API授权。


## 已接收检查点：产品上传/清理/更新启动装配（2026-11-08）

用户最新要求自行手动验证，主控优先完成剩余功能，不新增浏览器验收作为开发主线。V09-CLOUD-PRODUCT-IMPLEMENT-01 / chatgpt-takeover-01；feature/cloud-product-assembly-20261108 / ServerFleet-product；CHECKPOINT ACCEPTED（#15 / main6031c1f / CI42485042788）。统一起点main a8a093ccfaecc35c1a58cadbef3a675b57fdf035，独立CI42480445288 success。

本连续包先实现用户上传三策略/真实路径与终态/原子校验拒绝，再接cleanup实际服务装配与可信更新启动配置，继续发布/恢复独立实现。允许相关internal/files/certificates/updates/recovery、Controller组合/Agent已授权typed入口、正式Web/API适配、独立契约/共享迁移和进度文档；主控单写公共接缝。预留ds/0043_upload_policies（29项）；历史SQL/hash、旧25fixture及原始资料/既有候选/Proto/gen/根锁/CI保持，不以旧迁移重建假旧库。新契约单列R11/兼容差异。审查主控逐项复核与自动检查/COMMENT，非另一独立身份/自我APPROVE。

用户负责手动验收；整包完成后集中自动检查，不逐小改动全量测试，不造浏览器证据。停止条件为本包实现/必要负向检查及精确head PR接收；未完成项继续由主控处理，真实外部/物理/ARM/硬1C1G/G2/Runtime/发布需实际满足，不因CI或手动验证分工关单。无生产/VPS/awskr/真实供应商凭据/云API权限，不启动其他Agent。


## 已接收检查点：正式Web持久续传（2026-11-08）

V09-CLOUD-BROWSER-RESUME-01 / chatgpt-takeover-01；feature/cloud-browser-resume-20261108 / ServerFleet-browser-resume；CHECKPOINT ACCEPTED（#14 / main a8a093c；CI42480445288）。起点main f1d715b532f063cc9c7e21264eaa0f7ea4d4c73a及独立CI42458157478 success。#7/#8/#9由#12接收，#13暂存恢复/四ADR原文已接收；下方旧候选状态为历史提交前快照。

主控单写apps/web文件页/API适配、internal/files/http.go只读绑定投影/专项、独立v09-r10候选/redocly注册、相应进度/产品差异/README。原始资料/历史候选/Proto/gen/锁/CI/迁移SQL不改。审查：主控逐项复核+实际CI/COMMENT，非第二位独立账号、不自我APPROVE。停止条件：账号/节点隔离的待核验元数据、重新选文件+摘要绑定、当前服务端目标/摘要/偏移读回、有界分块、已派发/UNKNOWN不重放、换节点/身份/撤权负向、集中检查与精确head PR接收。真实外部/ARM/硬1C1G及G2/发布关卡另行OPEN。不启动其他Agent。


## 已接收检查点：持久暂存续传（2026-11-08）

任务V09-CLOUD-STAGING-RESUME-01；唯一会话chatgpt-takeover-01；分支feature/cloud-staging-resume-20261108；独立目录ServerFleet-resume。依赖#12精确候选3f2fc687a148306cdcc089c34ce7057e877a70e4及原文恢复8bd7140；未合并候选不冒称main已提供。CHECKPOINT ACCEPTED（#13 / main f1d715b；CI42458157478），原三平台不再并发写入。

写范围：internal/files的未派发上传/分发暂存写入与恢复、相应单元/程序组合专项，docs/reference/recovered-adrs-20261108原文字节清单/索引及当前进度。审查：主控逐项复核与真实CI/COMMENT，非独立账号自批。只修已授权开发环境，不访问生产/供应商/VPS。停止条件：证明崩溃后可从持久偏移继续、缺失已确认字节/撤权/已派发/非私有或符号链接均拒绝；完整浏览器续传、Agent执行恢复及发布关卡另记，不因本检查点关单。


## 2026-11-08 单会话接手（覆盖下方分工快照）

用户授权ChatGPT直接返修#7/#8/#9并独立继续完整项目，不再向三平台派发。任务V09-CLOUD-TAKEOVER-01，唯一会话chatgpt-takeover-01，分支fix/cloud-r2-takeover-20261108，独立目录ServerFleet-takeover，状态IN PROGRESS。原AM/HW/WB历史保留；尚未完成项转本任务，不标完成。原中央窗口转交主控单写。

已接收main为1cde2c3c6db55b445703ebc2948063302e4c6d11（#11检查点，27迁移）；#7/#8/#9及返修仅在候选组合，未合入前不冒称main已提供。当前组合含cx/0010共28迁移，旧25fixture和历史SQL/hash不变。详情及后续验收见[接手记录](sprints/v09-five-day/progress/chatgpt/cloud-r3/TAKEOVER.md)。集中修改后批量模块/集成验证；不按每个小修改反复全量测试。G2、B03/B04/B05/B06和外部/物理双机/ARM/硬1C1G/正式资料继续OPEN。

## 当前云端分工（2026-11-03，优先于下方历史快照）

总控/审查ChatGPT（chatgpt-review-02）。已接收产品代码基线 `51e0ee57e66493892eb5f5168b9e0690b53f3661`，独立CI run41615351426 success：Go顶层902 PASS/7 SKIP/0 FAIL、Web348 PASS，正式迁移26项。上一轮只接收可运行检查点，完整v0.9/G2/D01-D07/P1-C10/发布准入未完成。

本轮已接收证据见[云端首轮报告](reviews/V09-CLOUD-R1-REVIEW.md)及[机器摘要](reviews/V09-CLOUD-R1-RESULTS.json)。本次仅文档同步，不新增产品验收。

本轮完整范围、目录和停止条件以[Cloud R2计划](sprints/v09-five-day/cloud-r2/PLAN.md)及三份自包含提示词为准。用户最新指定：插件与Xray全部由WorkBuddy承担，AgentMore没有此域任务。中央网关接线单列CodeArts早期共享候选，主控独立审查接收。

| 任务 | 当前状态 | 唯一会话 | 分支 | 范围/证据 |
|---|---|---|---|---|
| V09-CLOUD-AM-01 | CHECKPOINT ACCEPTED / PARTIAL；剩余转AM-02，插件范围转WB-02 | agentmore-01 | feature/cloud-platform-am01（已合并） | PR #3；最终集成head4ceedd9；上传统一任务与终态，非整包完成 |
| V09-CLOUD-HW-01 | CHECKPOINT ACCEPTED / PARTIAL；剩余转HW-02 | codearts-01 | feature/cloud-services-hw01（已合并） | PR #2；最终head2d99eb5；网关部件/子账本，完整装配仍OPEN |
| V09-CLOUD-WB-01 | CHECKPOINT ACCEPTED / PARTIAL；剩余转WB-02 | workbuddy-01 | feature/cloud-console-wb01（已合并） | PR #1；最终head7767d78；Agent更新浏览器专项，非完整B05 |
| V09-CLOUD-AM-02 | READY / 开工与提交尚待实际回报核验 | agentmore-02 | feature/cloud-platform-am02 | 文件多目标/续传、更新、恢复、平台负向；不含插件/代理组件 |
| V09-CLOUD-HW-02 | READY / 开工与提交尚待实际回报核验 | codearts-02 | feature/cloud-services-hw02；独立共享候选integration/cloud-gateway-wiring-hw02 | 正式网关发布/补偿整链、ACME/cleanup与服务域 |
| V09-CLOUD-WB-02 | READY / 开工与提交尚待实际回报核验 | workbuddy-02 | feature/cloud-console-xray-wb02 | Xray可选插件/准入证据、正式Web/浏览器/矩阵；新增Go权限仅限明确Xray域 |
| V09-CLOUD-COORD-02 | DISPATCH PREPARED / 文档同步；后续按实际PR接收审查 | chatgpt-review-02 | docs/cloud-r2-dispatch-review02 | 当前登记；共享接口/迁移/顺序合并/组合审查，未启动外部Agent |
| V09-CLOUD-AM-02-FIX1 | REPAIR IN PROGRESS：2026-11-04用户授权接管#11的A1—A4；原head0e57714的CI失败，不称接收 | chatgpt-am02-fix1；主控chatgpt-review-02 | 本地fix/cloud-platform-am02-review1；常规追加原feature/cloud-platform-am02 / PR#11 | 本轮文件分发返修、测试/新候选号/R9与AM进度；完成后停止，不启动续传/更新/恢复等后续包 |

三平台环境互不共享。任务派发、开工回报及PR回传由用户手动完成；未收到实际证据时保留READY，不推断平台没有本地活动，也不自动标IN PROGRESS。只接收经审查的main+明确候选组合，不重放历史prepare/overlay。B03/B04/B05/B06、外部/物理双机/ARM实际运行/硬1C1G、正式资料和ENV-W6-01保持OPEN。没有生产/VPS/awskr/真实供应商或云API授权。

## 2026-11-02及更早历史（保留追溯）

下方旧日期、旧会话/Windows目录、当时CI待确认和暂停状态均为历史；不能覆盖上方本轮授权，也不能推断旧会话已被启动。

更新：2026-11-02。codex-review-02总控；三线Wave6-R3（CX/DS/CA-04）已审查、返修和正式共享组合，技术8df0154（根/安全及13组Linux在1069519，之后仅浏览器WSL传参修复）：Go 839顶层+1198子测试PASS/28 SKIP、Web348 PASS；原生Linux382顶层PASS/5 SKIP，实际CLI文件/Agent产品更新2组PASS；浏览器回归70/70、桩16/16、严格正向31 PASS/4 BLOCKED/0 FAIL（退出1）。已接收25项迁移、三策略文件页面/预算UI与queued取消修复；完整v0.9/G2/D01—D07/P1-C10仍未验收。新WAVE6-R4.md三线-05 READY：CX文件上传任务化/平台，DS独占完整网关工件/执行/补偿和ACME，CA直接消费UpdateProduct补B05宿主并继续独立Web/验收；不自动启动。ENV-W6-01沿用未关闭，本轮未复查/修改awskr。a70694b已确认来自Codex Arts分支，旧Gemini目录保留。

| 任务 | 状态 | 唯一会话 | 分支 / Worktree | 允许写入 | 审查人 / 基线 |
|---|---|---|---|---|---|
| V09-GITHUB-UPLOAD | DONE：2026-11-02正常推送d7d57f9，ls-remote及连接器已核验；远端历史保留，干净目录综合/安全PASS；GitHub Actions尚未确认；下一轮仍未开始 | codex-review-02 | main集成；复验integration/github-upload-cx02 / `.worktrees/integration--github-upload-cx02` | 指定仓库IKUNHEIHIE/ServerFleet的主线上传、origin配置、进度/上传记录；普通推送和核验后停止 | 项目负责人；起始cf88dfc；结果见reviews/V09-GITHUB-UPLOAD.md |
| V09-WAVE6-R3-REVIEW | 三线-04审查/必要返修/正式共享组合及根安全、Linux、浏览器复验完成；接收/干净目录同步和续派后停止 | codex-review-02 | integration/v09-wave6-r3-review-cx02 / `.worktrees/integration--v09-wave6-r3-review-cx02` | 本轮必要修复/中央注册/迁移/契约、证据与后续任务；不改既有远端服务 | 项目负责人；起始fafbe93；技术8df0154；见reviews/V09-WAVE6-R3-REVIEW.md |
| V09-W6R3-CHECKPOINT | 负责人要求保存当前进度，明确下一轮尚未开始；仅文档记录/核验后停止 | codex-review-02 | main / 主目录（集成负责人） | CURRENT-PROGRESS.md、TASK-BOARD、WAVE6-R4开工状态及README入口；不实现/不启动作者/不改作者目录 | 项目负责人；已验收基线65c759c |
| V09-R6-CX-05 | TRANSFERRED：Windows旧包未启动；原范围经AM-01检查点接收后续转AM-02；插件/代理组件现转WB-02；整包未DONE | codex-06 | feature/v09-r6-platform-cx06 / `.worktrees/feature--v09-r6-platform-cx06` | WAVE6-R4.md的CX包；独占files，允许新增R8文件候选/新迁移候选，中央注册/公共生成交patch | codex-review-02+安全/通信；起点65c759c |
| V09-R6-DS-05 | TRANSFERRED：Windows旧包未启动；原范围经HW-01检查点接收后续转HW-02；整包未DONE | deepseek-06 | feature/v09-r6-services-ds06 / `.worktrees/feature--v09-r6-services-ds06` | WAVE6-R4.md的DS包；新增gateway_artifact/registration等明确授权；中央已有文件交patch，不写files | codex-review-02+安全/数据；起点65c759c |
| V09-R6-GM-05 | TRANSFERRED：Windows旧包未启动；原范围经WB-01检查点接收后续转WB-02；整包未DONE | codex-arts-01 | feature/v09-r6-console-ca01 / `.worktrees/feature--v09-r6-console-ca01` | WAVE6-R4.md的CA包；Web/tests/browser/v09/测试专用fixturehost，progress/codex-arts/wave6-r4；不改产品Go/共享配置/锁 | codex-review-02+UI/安全；起点65c759c |
| V09-GM04-TRANSFER | 转交登记/独立CA目录/完整提示词完成，policy/git检查PASS；快进新CA目录后交接停止，未启动实现或代发消息 | codex-review-02 | main / 主目录（集成负责人） | 本次交接文档、TASK-BOARD、WAVE6-R3的GM执行人/目录/进度记录；不改作者代码/历史证据或同步CX/DS目录 | 项目负责人；起始db3cf87；见GM04-CODEX-ARTS-HANDOFF.md |
| V09-WAVE6-R2-REVIEW | 三线-03审查/必要返修/正式组合及根安全、Linux、浏览器复验完成；完成接收/干净目录快进和续派后停止 | codex-review-02 | integration/v09-wave6-r2-review-cx02 / `.worktrees/integration--v09-wave6-r2-review-cx02` | 本轮增量必要修复、中央注册/契约/迁移、证据/后续任务；未改既有awskr服务 | 项目负责人；起始c9013f4；技术23e56d5；见reviews/V09-WAVE6-R2-REVIEW.md |
| V09-R6-CX-04 | f643c49增量及两中央patch已接收；CLI三策略/实际Agent更新通过，上传/多目标/续传/恢复等未完成项转CX-05 | codex-06 | feature/v09-r6-platform-cx06 / `.worktrees/feature--v09-r6-platform-cx06` | 原WAVE6-R3.md的CX包；保留历史证据和patch，不重放 | codex-review-02+安全/通信；起始db3cf87 |
| V09-R6-DS-04 | 0b6cf7b增量及ds/0063已接收；网关/发布/cleanup模块验证通过，完整产品组合/真实provider仍OPEN转DS-05 | deepseek-06 | feature/v09-r6-services-ds06 / `.worktrees/feature--v09-r6-services-ds06` | 原WAVE6-R3.md的DS包；不默认启真实cleaner/生产发布 | codex-review-02+安全/数据；起始db3cf87 |
| V09-R6-GM-04 | a70694b（实际Codex Arts）已接收；B01/预算UI/第二主控隔离通过，策略检查由总控对齐并修queued取消，余项转GM-05 | codex-arts-01 | feature/v09-r6-console-ca01 / `.worktrees/feature--v09-r6-console-ca01` | 原WAVE6-R3.md和Arts交接范围；保留旧Gemini历史，原本轮未启动Gemini | codex-review-02+UI/安全；起始fafbe93 |
| V09-WAVE6-R1-REVIEW | 三线审查/必要修复/共享接收及根安全、Linux、浏览器验证完成；本轮合并/干净目录同步交接后停止 | codex-review-02 | integration/v09-wave6-r1-review-cx02 / `.worktrees/integration--v09-wave6-r1-review-cx02` | 本轮增量必要修复、Controller组合/共享差异、验证/证据及后续任务；仅既有授权awskr隔离测试，不改原服务 | 项目负责人；起始379d5e0；技术9ddd5c6；见reviews/V09-WAVE6-R1-REVIEW.md |
| V09-R6-CX-03 | 本轮增量26f7cf6及全部中央补丁已接收，正式24项迁移/双Agent组合PASS；剩余项转CX-04，原包完成停止 | codex-06 | feature/v09-r6-platform-cx06 / `.worktrees/feature--v09-r6-platform-cx06` | 原WAVE6-R2.md的CX范围；保留作者证据/历史补丁，不在新基线重放 | codex-review-02+安全/通信；起始c9013f4 |
| V09-R6-DS-03 | 增量3efebe8经发布副作用/终态安全返修后接收；实际发布和cleanup并发仍OPEN转DS-04；不默认启用 | deepseek-06 | feature/v09-r6-services-ds06 / `.worktrees/feature--v09-r6-services-ds06` | 原WAVE6-R2.md的DS范围；原包完成停止，不写files | codex-review-02+安全/数据；起始c9013f4 |
| V09-R6-GM-03 | 增量b37636c经无认证宿主配置删除/真实预算API和nodeID修复后接收，正向21/5/0；剩余项转GM-04 | gemini-06 | feature/v09-r6-console-gm06 / `.worktrees/feature--v09-r6-console-gm06` | 原WAVE6-R2.md的GM范围；保留原403证据，原包完成停止 | codex-review-02+UI/安全；起始c9013f4 |
| V09-WAVE6-REVIEW | 三线增量审查、组合修复、根安全/浏览器/awskr与WSL验证完成；三作者干净目录快进同步；本轮交接后停止 | codex-review-02 | integration/v09-wave6-review-cx02 / `.worktrees/integration--v09-wave6-review-cx02` | 本轮收到增量的必要修复/主控组合、共享差异、隔离验证与证据/续作任务；不改作者未提交内容或awskr既有服务 | 项目负责人；起始73d5dd4；最终技术9d3aa2；见reviews/V09-WAVE6-COMBINED-REVIEW.md |
| V09-R6-CX-02 | 17017a6增量已接收，主控接收patch/正式迁移与预算装配；文件任务worker及平台缺口转CX-03 | codex-06 | feature/v09-r6-platform-cx06 / `.worktrees/feature--v09-r6-platform-cx06` | 原WAVE6-FOLLOWUP.md CX范围；作者本包完成停止 | codex-review-02+安全/通信；起始379d5e0 |
| V09-R6-DS-02 | 3610982增量已接收，补ACME全路径清理、Linux夹具与上传写权限回归修复；剩余转DS-03/CX-03 | deepseek-06 | feature/v09-r6-services-ds06 / `.worktrees/feature--v09-r6-services-ds06` | 原WAVE6-FOLLOWUP.md DS范围；作者本包完成停止 | codex-review-02+安全/数据；起始379d5e0 |
| V09-R6-GM-02 | 27c92be增量已接收，补AI冲突状态/验收判定；20 PASS/6 BLOCKED仍PARTIAL，余项转GM-03 | gemini-06 | feature/v09-r6-console-gm06 / `.worktrees/feature--v09-r6-console-gm06` | 原WAVE6-FOLLOWUP.md GM范围；作者本包完成停止 | codex-review-02+UI/安全；起始379d5e0 |
| V09-CONTROL-RESUME | 本次接管/awskr实机CLI基线及本地组合安全检查完成；总控身份持续，后续接收Wave6检查点 | codex-review-02 | integration/v09-control-resume-cx01 / `.worktrees/integration--v09-control-resume-cx01` | 独立审查接收、当前授权/任务/证据、隔离测试部署与必要集成接线；仅awskr指定主机，不改其既有服务/数据库/站点/防火墙，不创建云资源或扩展其他主机 | 项目负责人；起始2da203c；见reviews/V09-CONTROL-RESUME.md；本次交接后报告 |
| V09-R6-CX | 本轮可独立增量4a3e31e已接收并补集成修复，未完成项转CX-02连续包 | codex-06 | feature/v09-r6-platform-cx06 / `.worktrees/feature--v09-r6-platform-cx06` | 原WAVE6.md范围；保留原提交和作者证据，不替代全量验收 | codex-review-02+安全/契约；起始73d5dd4 |
| V09-R6-DS | 本轮增量fd2e015已接收并补集成修复，未完成项转DS-02连续包 | deepseek-06 | feature/v09-r6-services-ds06 / `.worktrees/feature--v09-r6-services-ds06` | 原WAVE6.md范围；保留原提交和作者证据，不替代全量验收 | codex-review-02+安全/契约；起始73d5dd4 |
| V09-R6-GM | 9a04c87/1e16ba5/e88e9b0已接收并补主控验收门控与真实任务显示修复；6项正向缺口转GM-02 | gemini-06 | feature/v09-r6-console-gm06 / `.worktrees/feature--v09-r6-console-gm06` | 原WAVE6.md范围；保留作者提交/证据，部分套件不替代全量验收 | codex-review-02+UI/安全；起始73d5dd4；最终e88e9b0 |
| V09-INDEPENDENT-COMPLETION-AUDIT | 独立核验完成：本地70步及根检查可复现；完整v0.9未完成/尚不可发布，报告后停止 | codex-review-01 | integration/v09-independent-completion-audit-cx01 / `.worktrees/integration--v09-independent-completion-audit-cx01` | 仅审查登记/报告/证据；不修业务、不改主线或作者目录；原始Go代理失败和9项SKIP保留 | 项目负责人；精确代码基线2da203cac40ccc2bfc4c7bb644978005cd99dab1；见reviews/V09-INDEPENDENT-COMPLETION-AUDIT.md |
| V09-WAVE5-REVIEW | 三线Wave 5独立审查/NetProber与LocalStagingTransport默认主控接线/WSL双进程CLI闭环与70步真后端浏览器验收通过（Go 636+1134、Web 329、Live 70/70、0漏洞），合入main | agy-review-01 | integration/v09-wave5-review / `.worktrees/integration--v09-wave1-review` | 三线Wave 5审查、apps/controller默认探测与暂存传输接线、审查报告/任务板/README | 项目负责人；起始3adf891；代码aef21ae；见reviews/V09-WAVE5-REVIEW.md |
| V09-WAVE5-DISPATCH | Wave 5本地全栈收官/Wave 4新接口前后端打通/双进程CLI连接任务单已登记，三作者目录已快进同步至最新main；交接后等待Wave 5检查点 | agy-review-01 | integration/v09-wave5-dispatch / `.worktrees/integration--v09-wave1-review` | sprints/v09-five-day/WAVE5.md、任务板、README及三干净作者目录快进同步 | 项目负责人；起始f4609f5；见sprints/v09-five-day/WAVE5.md |
| V09-WAVE4-REVIEW | 三线Wave 4独立审查/主控桥接接线/组合安全与58步真后端浏览器验收通过（Go 602+1132、Web 316、Live 58/58、0漏洞），合入main | agy-review-01 | integration/v09-wave4-review / `.worktrees/integration--v09-wave1-review` | 三线Wave 4审查、apps/controller桥接端口接线、审查报告/任务板/README | 项目负责人；起始0f9a75e；代码0d762f0；见reviews/V09-WAVE4-REVIEW.md |
| V09-WAVE4-DISPATCH | Wave 4本地闭环补齐/全模块真后端浏览器E2E/AC-01~24证据盘点任务单已登记，三作者目录已快进同步至最新main；交接后等待Wave 4检查点 | agy-review-01 | integration/v09-wave4-dispatch / `.worktrees/integration--v09-wave1-review` | sprints/v09-five-day/WAVE4.md、任务板、README及三干净作者目录快进同步 | 项目负责人；起始9453718；见sprints/v09-five-day/WAVE4.md |
| V09-WAVE3-REVIEW | 三线Wave 3独立审查/DS四域主控接线/组合安全与真后端浏览器验收通过（Go 573+1128、Web 306、Live 32/32、0漏洞），合入main | agy-review-01 | integration/v09-wave3-review / `.worktrees/integration--v09-wave1-review` | 三线Wave 3审查、apps/controller与internal/tasks组合接线、scripts/check.mjs与单测竞态修复、审查报告/任务板/README | 项目负责人；起始98f39e0；代码038ecaf；见reviews/V09-WAVE3-REVIEW.md |
| V09-WAVE3-DISPATCH | Wave 3全量路由补齐/Agent回路/真后端浏览器验收任务单已登记，三作者目录已快进同步至最新main；交接后等待Wave 3检查点 | agy-review-01 | integration/v09-wave2-wiring / `.worktrees/integration--v09-wave1-review` | sprints/v09-five-day/WAVE3.md、任务板、README及三干净作者目录快进同步 | 项目负责人；起始a9a76d7；见sprints/v09-five-day/WAVE3.md |
| V09-WAVE2-WIRING | Controller SQLite WAL组合接线、Web会话CSRF/Bootstrap对齐及HTTPS端到端集成测试通过（Go 547+1116、Web 283、0漏洞），合入main | agy-review-01 | integration/v09-wave2-wiring / `.worktrees/integration--v09-wave1-review` | apps/controller组合服务、internal/auth与internal/tasks组合辅助、apps/web会话对齐、tests/integration/v09及审查文档 | 项目负责人；起始4f5a459；代码c840800；见reviews/V09-WAVE2-REVIEW.md |
| V09-WAVE1-REVIEW | 三线独立审查/共享协调/组合及安全检查通过（Go 546+1116、Web 282、0漏洞），合入main并推进Wave 2 | agy-review-01 | integration/v09-wave1-review / `.worktrees/integration--v09-wave1-review` | 三线提交审查、go.mod与tests/browser/v09及g2r03 provenance协调、审查证据/任务板/README | 项目负责人；起始8a7dd47；代码5d103a0；见reviews/V09-WAVE1-REVIEW.md |
| V09-5D-DISPATCH | 全量派发包/24AC覆盖/13新域依赖及14方向检查/组合安全检查通过，三目录准备完成；交接后停止 | codex-review-01 | integration/v09-five-day-dispatch / `.worktrees/integration--v09-five-day-dispatch` | 全量责任/每日里程碑、三复合任务及目录、依赖协调/架构负向、状态与证据；不实现业务或替代安全冻结 | 项目负责人；起始6052f59；结果见reviews/V09-FIVE-DAY-DISPATCH.md |
| V09-5D-CX | WAVE 1, 3, 4 & 5 ACCEPTED（aef07e5 / 04ff698 / b02b157 / cad136b）：完成Agent CLI -connect-once单次WSS+mTLS连接与双进程任务闭环；已完成停止 | codex-02 / codex-03 / codex-04 / codex-05 | feature/v09-platform-cx02 / `.worktrees/feature--v09-platform-cx02` | CX-01—08及WAVE5.md：auth/transport/plugins/Controller+Agent及AI/工作流/更新恢复、可选Xray、专用测试/进度；禁改DS/GM目录 | agy-review-01；Wave 5见reviews/V09-WAVE5-REVIEW.md |
| V09-5D-DS | WAVE 1, 3, 4 & 5 ACCEPTED（07f4154 / a858817 / 122506d / df8eebd）：完成防DNS Rebinding真实NetProber+本地目录LocalStagingTransport；已完成停止 | deepseek-02 / deepseek-04 / deepseek-05 | feature/v09-services-ds02 / `.worktrees/feature--v09-services-ds02` | DS-01—09及WAVE5.md：storage/nodes/tasks/audit及文件/监控通知/配额/DNS/证书/网关/服务组/发布、专用测试/进度；禁改apps/controller | agy-review-01；Wave 5见reviews/V09-WAVE5-REVIEW.md |
| V09-5D-GM | WAVE 1, 3, 4 & 5 ACCEPTED（86e0cf5 / 2aebec3 / 9b32a5b / 639eff2）：完成工作流/文件字节上传/监控历史与探测/发布执行回滚Web打通+70步真后端浏览器E2E；已完成停止 | gemini-02 / gemini-04 / gemini-05 | feature/v09-console-gm02 / `.worktrees/feature--v09-console-gm02` | GM-01—09及WAVE5.md：正式Web全部功能/真实API适配/tests/browser/v09真后端浏览器验收；禁改Go代码与公共配置 | agy-review-01；Wave 5见reviews/V09-WAVE5-REVIEW.md |
| READ-DESIGN-R1-REVIEW | 返修/组合/安全检查通过，接收两设计并关闭原四P2；交接后停止 | codex-review-01 | integration/read-design-r1-review / `.worktrees/integration--read-design-r1-review` | 原精确复审/接收及文义澄清、审查证据/状态/README/P1-C10与下一步具体待批范围；不启动实现或批准详细D项 | 项目负责人验收；起始3c817f8 |
| W07-LEGACY-VIEW | SUPERSEDED：范围纳入V09-5D-GM的GM-03，不再单项待批/另派 | gemini-02（新复合包） | 使用feature/v09-console-gm02；旧拟目录未创建 | 旧提案保留历史，不以其8文件限制覆盖本次完整管理台授权 | 见本轮PLAN/GEMINI.md；仍需真实模块/浏览器验收 |
| READ-DESIGN-REVIEW | 复审/组合/安全检查完成：接收认证提案，HTTP/Web各退回2P2，交接后停止 | codex-review-01 | integration/read-design-review / `.worktrees/integration--read-design-review` | 原三设计审查/工件/证据/状态与限定返修/组合清单范围；不批准详细D项或启动实现 | 项目负责人验收；起始4196b2e |
| READ-AUTH-DESIGN-01 | 文档接收为PROPOSED，具体参数/迁移待批；任务完成停止 | codex-01 | docs/node-read-auth-design-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\docs--node-read-auth-design-cx01` | 原3份新增Markdown；未改代码/候选/共享端口或实施凭据 | 总控+安全/契约；48409bac，起始4196b2e |
| READ-HTTP-DESIGN-01 / R1 | 文档接收为PROPOSED，HD01-R01/R02关闭；完成停止 | deepseek-01 | docs/node-read-http-design-ds01 / `.worktrees/docs--node-read-http-design-ds01` | 原3文档；总控在集成目录澄清有状态凭据权威查询，无处理器/来源/数据库/OpenAPI实现 | 总控+安全/契约/节点；原ece5efd、R1 4fbdcf5，原基线4196b2e |
| READ-WEB-DESIGN-01 / R1 | 文档接收为PROPOSED，WD01-R01/R02关闭；完成停止 | gemini-01 | docs/node-read-web-design-gm01 / `.worktrees/docs--node-read-web-design-gm01` | 原3文档；总控在集成目录澄清viewEpoch/终止错误门控，无TSX/App/fetch/依赖变化 | 总控+UI/契约/安全；原63b0feb、R1 39b2b7c，原基线4196b2e |
| READ-DESIGN-DISPATCH | 方向/任务配置及组合/安全检查完成，准备三目录并交人工派发后停止 | codex-review-01 | integration/read-design-dispatch / `.worktrees/integration--read-design-dispatch` | 原方向/范围记录、三任务/目录及验证交接范围；不写作者设计或运行时代码/候选 | 项目负责人验收；起始0569c1a；结果见READ-DESIGN-DISPATCH复审记录 |
| W06R1-REVIEW | 精确审查/3项独立浏览器补验/组合及安全检查通过，交接后停止 | codex-review-01 | integration/w06r1-review / `.worktrees/integration--w06r1-review` | 原报告/证据审查、必要补验与澄清、状态/README/P1-C10和具体负责人选择包；不改运行时代码/候选或启动新开发 | 项目负责人验收；起始157549e |
| FOLLOWUP04-REVIEW | 审查/组合/安全检查通过：接收返修方案文档，W06补验；交接后停止 | codex-review-01 | integration/followup04-review / `.worktrees/integration--followup04-review` | 原精确审查/集成/必要文档事实澄清、证据/任务状态和限定补验任务范围；不改运行时代码/候选，不批准新实现 | 项目负责人验收；起始69d6d43 |
| READSEC01-REVIEW | 独立审查/组合/安全检查通过，交接后停止 | codex-review-01 | integration/readsec01-review / `.worktrees/integration--readsec01-review` | 精确审查安全证据/引用原文、合入通过文档、证据/任务板/README及收口状态；不干预READ-PLAN-01-R1/W06，不启动新实现 | 项目负责人验收；起始ddf8c5b |
| FOLLOWUP03-REVIEW | 审查/组合/安全检查通过：接收W05/R2，READ-PLAN-01退回，交接后停止 | codex-review-01 | integration/followup03-review / `.worktrees/integration--followup03-review` | 精确审查/引用与UI复验、合入通过项、证据/任务板/README/P1-C10补记及两位后续安排；不干预READ-SEC-01 | 项目负责人验收；起始7ef7877 |
| N03A-REVIEW | 独立审查/组合/安全检查通过，交接后停止 | codex-review-01 | integration/n03a-review / `.worktrees/integration--n03a-review` | 精确审查N03-A/隔离探针、合入通过项、证据/任务板/README及Codex后续任务/目录；不干预READ-PLAN-01/W05-R2 | 项目负责人验收；起始98a1f6b |
| FOLLOWUP02-REVIEW | 审查/组合/安全检查完成：接收TASK-01C、W05-R1继续返修，交接后停止 | codex-review-01 | integration/followup02-review / `.worktrees/integration--followup02-review` | 核对TASK-01C实际交付、精确审查W05-R1、隔离探针/合入通过项、证据/任务板/README及两位后续安排；不干预N03-A | 项目负责人验收；起始8b2d8aa |
| AUTH01B-REVIEW | 独立审查/组合/安全检查通过，交接后停止 | codex-review-01 | integration/auth01b-review / `.worktrees/integration--auth01b-review` | 精确审查AUTH-01B/隔离探针、合入通过项、证据/任务板/README及Codex下一步任务/目录；不干预TASK-01C/W05-R1 | 项目负责人验收；起始a652696 |
| FOLLOWUP01-REVIEW | 审查完成：接收TASK-01B、W05退回；组合/安全检查通过，交接后停止 | codex-review-01 | integration/followup01-review / `.worktrees/integration--followup01-review` | 精确审查/隔离探针、合入通过项、证据/任务板/README及两位后续任务/目录；不干预AUTH-01B | 项目负责人验收；起始1f8f8cb |
| M0-P1 | 已完成 | codex-p1-local | main / 主目录 | 原 P1 范围 | 交付 15814c6 |
| G2-R01 | 审查通过，仅接收候选/评审工件；不批准运行时提案或冻结 | codex-01 | contract/g2-review-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--g2-review-cx01` | 原三项授权目录 | 总控；被审查提交 8821555 |
| N01 / N01-R1 | 复审通过，3个P2关闭，接收原实现与返修 | deepseek-01 | feature/nodes-domain-ds01 / `.worktrees/feature--nodes-domain-ds01` | 原任务范围，已完成停止 | 总控；原04c45e9、返修b848104、最终0fa6f88 |
| W01 / W01-R1 | 复审通过，2个P2关闭，接收原组件与返修 | gemini-01 | feature/web-nodes-mock-gm01 / `.worktrees/feature--web-nodes-mock-gm01` | 原任务范围，已完成停止 | 总控；原c6302a9、返修86d4722 |
| BATCH01-REVIEW | 审查与验证完成，接收G2-R01，返修任务已分配；交接后停止 | codex-review-01 | integration/batch01-review / `.worktrees/integration--batch01-review` | 审查文档、任务板、AGENTS/README 状态、契约检查接线；只合入 G2-R01 工件 | 项目负责人验收；起始 15814c6 |
| T01-A | 复审通过，接收离线校验候选；没有真实通信/执行准入 | codex-01 | feature/proto-validation-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--proto-validation-cx01` | 原任务范围，已完成停止 | 总控；fd918d1，基线36ab5cf |
| BATCH02-REVIEW | 三项独立复审/组合检查通过，交接后停止 | codex-review-01 | integration/batch02-review / `.worktrees/integration--batch02-review` | 集成三项已审查提交；审查文档/任务板/README，准备下一批Worktree | 项目负责人验收；起始36ab5cf |
| AUTH-01A | 审查通过，接收离线策略/错误分类；已完成停止 | codex-01 | feature/auth-policy-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-policy-cx01` | 原任务范围，ports.go不变 | 总控；8e87570，起始a103c5f |
| A01 | 审查通过，接收校验/受限视图/测试适配；已完成停止 | deepseek-01 | feature/audit-domain-ds01 / `.worktrees/feature--audit-domain-ds01` | 原任务范围，ports.go不变 | 总控；d99d7ef，起始a103c5f |
| W02 | 审查通过，接收局部搜索/筛选；已完成停止 | gemini-01 | feature/web-node-filters-gm01 / `.worktrees/feature--web-node-filters-gm01` | 原任务范围 | 总控；5c6ceff，起始a103c5f |
| BATCH03-REVIEW | 三项独立审查及组合/安全检查通过，交接后停止 | codex-review-01 | integration/batch03-review / `.worktrees/integration--batch03-review` | 只读审查精确提交、集成通过项；审查证据/任务板/README，准备下一批目录 | 项目负责人验收；起始a103c5f |
| T01-B | 审查通过，接收有界离线帧解码候选；完成停止 | codex-01 | feature/frame-decoder-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--frame-decoder-cx01` | 原任务范围；ports/Proto/gen不变 | 总控；3488197，起始ff66dea |
| N02 / N02-R1 | 复审通过，N02-R01关闭，接收原N02及返修；完成停止 | deepseek-01 | feature/nodes-auth-errors-ds01 / `.worktrees/feature--nodes-auth-errors-ds01` | 原范围，两个testkit文件调整获批 | 总控；原ea7ed5c、返修fdeb3e6，保留历史 |
| W03 | 审查通过，接收只读详情/列表选择；完成停止 | gemini-01 | feature/web-node-details-gm01 / `.worktrees/feature--web-node-details-gm01` | 原任务范围 | 总控；64465c4，起始ff66dea |
| BATCH04-REVIEW | 审查完成：接收T01-B/W03，N02退回；交接后停止 | codex-review-01 | integration/batch04-review / `.worktrees/integration--batch04-review` | 独立审查/隔离探针、集成通过项；审查证据/任务板/README及下一批目录 | 项目负责人验收；起始ff66dea |
| T01-C | 审查通过，接收离线单连接对账/序列候选；完成停止 | codex-01 | feature/connection-guard-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--connection-guard-cx01` | 原任务范围 | 总控；b118aa6，起始189112e |
| W04 / W04-R1 | 复审通过，W04-R01关闭，接收原W04与返修；完成停止 | gemini-01 | feature/web-node-accessibility-gm01 / `.worktrees/feature--web-node-accessibility-gm01` | 原features/nodes及忽略reports临时验收范围 | 总控；原8e95c3c、返修b315046，保留历史 |
| BATCH05-REVIEW | 审查完成：接收T01-C及N02-R1，W04退回；交接后停止 | codex-review-01 | integration/batch05-review / `.worktrees/integration--batch05-review` | 精确提交审查/隔离验证、合入通过项；证据/任务板/README、P1-C10登记及下一批目录 | 项目负责人验收；起始189112e |
| G2-R02A | 审查通过，仅接收评审候选/工件；D01–D07未批准，完成停止 | codex-01 | contract/nodes-read-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\contract--nodes-read-cx01` | 原三个r03新增目录；作者未改共享文件 | 总控；8165057，起始0fef8ba |
| TASK-01A | 审查通过，接收纯快照/等待规则；完成停止 | deepseek-01 | feature/task-waiting-rules-ds01 / `.worktrees/feature--task-waiting-rules-ds01` | 原internal/tasks范围；ports.go不变 | 总控；0204d29，起始0fef8ba |
| BATCH06-PARTIAL-REVIEW | 两项部分验收通过，交接后停止；不代表整个第六批完成 | codex-review-01 | integration/batch06-partial-review / `.worktrees/integration--batch06-partial-review` | 审查TASK-01A/W04-R1、隔离验证/合入通过项、审查证据/任务板/README及两位后续目录；不干预Codex在编任务 | 项目负责人验收；起始0fef8ba |
| TASK-01B | 审查通过，接收默认分批与当前批纯判定；完成停止 | deepseek-01 | feature/task-batch-rules-ds01 / `.worktrees/feature--task-batch-rules-ds01` | 原internal/tasks范围，ports/快照/等待规则未改 | 总控任务/安全审查；e23a4c5，起始adce532 |
| W05 / W05-R1 / W05-R2 | 复审通过，接收原实现与两次返修；W05-R01/R02关闭，完成停止 | gemini-01 | feature/web-node-loader-gm01 / `.worktrees/feature--web-node-loader-gm01` | 原features/nodes范围，未接App/真实API | 总控UI/数据隔离审查；原0ebe812、R1 54df33e、最终fcb9382 |
| G2-R02A-REVIEW | 独立复审/组合检查通过；D08技术协调完成，交接后停止 | codex-review-01 | integration/batch06-contract-review / `.worktrees/integration--batch06-contract-review` | 审查/合入候选、check.mjs接线、两处Web来源注释/固定生成器重算provenance、审查/任务板/README/P1-C10及Codex后续目录 | 项目负责人验收；起始adce532 |
| AUTH-01B | 审查通过，接收离线预检/不可变范围；完成停止 | codex-01 | feature/auth-read-preflight-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--auth-read-preflight-cx01` | 原internal/auth新增文件范围，现有auth/端口/其他模块未改 | 总控授权/安全审查；14b9eac，起始1f8f8cb |
| TASK-01C | 审查通过，接收不可变顺序批次进度；完成停止 | deepseek-01 | feature/task-batch-progress-ds01 / `.worktrees/feature--task-batch-progress-ds01` | 原internal/tasks新增文件范围，现有规则/端口未改 | 总控任务/安全审查；实际交付e169afa，起始a652696 |
| N03-A | 审查通过，接收离线预检先行读取用例；完成停止 | codex-01 | feature/nodes-read-usecase-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\feature--nodes-read-usecase-cx01` | 原internal/nodes新增文件范围；现有文件/端口/真实接线未改 | 总控授权/节点安全审查；f57fe0d，起始8b2d8aa |
| READ-PLAN-01 / READ-PLAN-01-R1 | 文档接收，RP01-R01/R02关闭；仍PROPOSED，完成停止 | deepseek-01 | docs/node-read-plan-ds01 / `.worktrees/docs--node-read-plan-ds01` | 原3份文档；总控接收时澄清DTO省略与VM可null；不改代码/候选或批准状态 | 总控产品/契约/安全审查；原d0709fc、返修4da769c，起始98a1f6b |
| READ-SEC-01 | 审查通过，接收来源/安全证据文档；完成停止，等待收口后新分配 | codex-01 | docs/read-security-evidence-cx01 / `D:\admin\desktop\codex\myproject\ks.worktrees\docs--read-security-evidence-cx01` | 原3份新增文档范围，代码/候选/批准状态未改；本任务不再扩展 | 总控安全/契约审查；f3cdf73，起始7ef7877 |
| W06 / W06-R1 | 文档/隔离浏览器证据接收，W06-R01/R02关闭；完成停止 | gemini-01 | chore/web-node-browser-qa-gm01 / `.worktrees/chore--web-node-browser-qa-gm01` | 原报告及忽略预览/证据范围；总控只补验/澄清，未改生产组件/App/依赖或作者目录 | 总控UI/安全审查；原99cd663、返修2c1c8bc，原基线ddf8c5b |

原分支/Worktree保留追溯，不重置/清理他人依赖/报告/未保存内容；旧三设计任务保持完成停止。新三项V09复合实施各用独立02会话/分支/目录，精确起点由本次最终交接给出；每包多子项连续推进、检查点提交后继续无依赖项，不等每个小项单独再批准；所有子项结束或无独立工作时才停止。

此前设计证据见READ-DESIGN-R1-REVIEW；本轮新授权/分工/全量验收/环境风险见`sprints/v09-five-day/PLAN.md`、CODEX/DEEPSEEK/GEMINI.md和ACCEPTANCE.json。完整v0.9五日为目标而非保证；不缩减需求、不降安全门槛、不自动冻结G2、不把mock/UNTESTED当完成。真实外部环境/凭据/生产/VPS/云/远程仍须明确授权；当前无自动运行/消息派发或线上CI。
