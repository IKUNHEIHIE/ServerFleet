# Wave6-R2 · 三线连续交付包

任务负责人/审查人codex-review-02；2026-11-01。负责人要求完整v0.9，五日目标/11月3日时限不变。先读根AGENTS、TASK-BOARD、[共享接收说明](WAVE6-R1-SHARED.md)及原[Wave6](WAVE6.md)。最终main交接SHA是三位统一基线；总控只在原工作区干净且HEAD未变时快进同步，不重建/覆盖目录。

本轮调整：**CX负责整个文件执行链路，DS集中发布/网关/供应商，GM负责可注入的真实验收宿主和前端**。不再让文件执行生命周期分散在两位之间。仅下列新目录/文件授权是对原整包的具体扩展；共享中央文件仍交patch，不改他人目录。

## V09-R6-CX-03 / codex-06

分支`feature/v09-r6-platform-cx06`；目录`.worktrees/feature--v09-r6-platform-cx06`；审查codex-review-02+安全/通信。沿用CX整包，另允许`internal/files/**`、新增`apps/controller/server/file_*.go`和`ai_budget_http.go`。进度`docs/development/sprints/v09-five-day/progress/codex/wave6-r2`。DS本轮不写internal/files。

1. **首要交付：一条真实文件全链路**。从HTTP创建transfer、统一tasks调度、两个真实Agent WSS会话到目标字节/SHA/transfer与任务终态。直接修改files/transport/tasks/Agent及新增file_*.go，不再只交互不兼容的模块假设。中央server.go/enrollment.go/bootstrap注册交最小可应用patch，给独立组合测试或overlay证明，不用默认放行gate。
2. **持久执行模型**。明确父任务与每node/item子执行的唯一ID、参数/路径/plan绑定和durable TaskAck顺序，解决“协调器先要求双端已接受”与“Open才发送offer”、源stat后才知道写size/SHA的矛盾。冻结源/目标/items/checksums/显式映射，处理多item；单文件阶段可先交，但必须明确拒绝尚不支持的请求。源读完成不能提前结束整个任务，不能由两个dispatcher重复发offer。
3. **负向与恢复**。私有file-root/spool、当前身份/源读/目标写/任务取消与审批/AI scope逐帧复核；链接替换、低空间、完成回执丢失、重启/reconnect及UNKNOWN不重写。保留现有新上传权限回归、失败审计和所有安全负向。未知/不支持策略明确拒绝，不用rename/overwrite的假成功。
4. **AI独立项同时推进**。基于已接收AIBudget补owner作用域的预算策略读取/管理员CAS配置HTTP，当前Cookie/Origin/CSRF/owner/profile均后端校验；策略/计数与operation fence不可由普通消息改写或清空。新增`api/openapi/revisions/v09-r7/ai-budget-candidate.yaml`可写，Redocly登记交patch。保留默认缺策略拒绝和同事务审计，给GM稳定请求/错误结构；其测试宿主可以直接使用现有程序端口，不等待此HTTP完成。
5. **平台独立项**。继续正式更新worker/冻结计划/审批/真实版本健康与存储兼容/原子审计回滚，以及恢复新权威、旧主控fence、quarantine各入口和显式释放。OwnedProcessSupervisor只管理自己启动的进程，不能接管旧awskr服务。Xray只推进可复核来源/安全扫描及独立PoC，未通过工件不得运行或选定未批准Runtime。

如需文件执行DDL，可提交`cx/0008_file_execution_bindings`候选构造器/具体语句，由总控注册；旧迁移不改。公共Proto/gen/根锁/CI/中央注册仍patch。停止条件是完成所有可独立项或逐项说明确无独立工作，不能一个共享依赖阻塞整包。

## V09-R6-DS-03 / deepseek-06

分支`feature/v09-r6-services-ds06`；目录`.worktrees/feature--v09-r6-services-ds06`；审查codex-review-02+安全/数据。沿用DS整包但**本轮不写internal/files**；文件黑盒测试可放`tests/integration/v09/services`。另允许新增`apps/agent/gateway_capability.go`及同名前缀测试、新增`apps/controller/server/gateway_tasks.go`；既有Agent/Controller注册交patch，由总控协调CX。进度`progress/deepseek/wave6-r2`（均位于本sprint目录）。

1. **首要交付：实际发布阶段及补偿**。使用已接收ds/0081目标字段，完成DNS/证书/网关目标与revision逐阶段复核、当前权限/任务/审批、幂等/并发、取消与逆序补偿。真实目标不可退回只保存service-group ID；targetsPersisted或202不能算完成。先交可运行阶段检查点，同时推进以下独立项。
2. **受控Nginx动作**。新增Agent typed capability与Controller适配，固定参数schema/路径根/artifact SHA/固定argv，禁止Web提交任意validate/reload命令文本。所有执行仍经统一任务和当前审批，修改类不能沿用默认standard绕过审批；注册/DDL及公共消息交具体patch。先在普通用户隔离prefix/loopback端口证明实际验证、代理、失败恢复旧文件、幂等补偿；不接管既有Nginx或80/443，不使用SSH旁路。
3. **供应商秘密端口完成组合**。给当前owner/ref revision/用途/operation/plan绑定的callback适配及最小Controller注入patch；供应商I/O完整包在借用回调内，前后复权，不能扩展成通用明文读取API。继续本地TLS Cloudflare/SMTP/QQ证据，真实账号仍BLOCKED，禁止用真实凭据验证。
4. **ACME清理持久化**。保留总控对所有失败路径的cleanup error+record IDs修复，将失败记录ID/名称和重试状态持久化、可审计/可取消/有界重试；证书未验证不得返回成功。清理真实记录的操作应有当前授权且不删除不属于本次订单的记录。正式新DDL交候选，不改历史migration。
5. **契约与黑盒**。可新增`api/openapi/revisions/v09-r7/services-candidate.yaml`，记录发布目标/执行/接管审批的实际请求、终态、冲突与错误；根lint注册交patch。用公共ports黑盒验证CX文件交付，问题交具体失败用例，不直接修改其本轮所有目录。

## V09-R6-GM-03 / gemini-06

分支`feature/v09-r6-console-gm06`；目录`.worktrees/feature--v09-r6-console-gm06`；审查codex-review-02+UI/安全。沿用GM整包，另允许测试专用Go宿主`tests/browser/v09/fixturehost/**`，不能从生产导入；不改产品Go/API/Proto/根锁/CI。进度`progress/gemini/wave6-r2`。

1. **首要独立交付：AI真实浏览器生成**。用测试专用宿主组合正式server.New、LocalAIProvider及现有AIBudget；真实TLS协议端、本地合成BYOK、当前管理员/owner/profile下配置显式预算，浏览器完整设置→消息→generate→真实assistant终态。不得用production mock、绕过Cookie/CSRF/审批、直接写消息结果表或让HTTP任意指定供应商URL。当前主控程序端口已够用，不等CX预算HTTP才推进。
2. **文件真实宿主**。准备两个常驻Agent的独立私有file-root和显式Controller路径映射，使用实际源/目标路径、目前支持的skip策略；接CX checkpoint后执行`--attempt-file-flow`，同时验字节/SHA、双方任务终态、撤权/取消/回执丢失。两进程同WSL只算双端协议证据，不能叫物理双机。
3. **完整其余流程**。将B03/B05检查探针补成可审查的安装/配置/流量、签名安装/真实版本/健康与失败回滚；发布与灾备补实际终态/补偿或第二隔离Controller与fence。安全工件/后端未就绪时标明具体缺口，不能声称接线后现有探针就自动成为完整流程。不要伪造后台成功换绿灯。
4. **前端与身份**。接收预算API后补配置/余额/冲突/拒绝界面；保留总控AI CAS提示修复和上传写权限回归。Operator间歇403保留追踪：保存请求/响应/关联ID和当次角色/授权/节点快照，不能改成管理员或把403降BLOCKED。已通过的reload/CSRF、失败任务与撤权页面继续保留。
5. **门禁与证据**。500/401/403/错误请求/超时FAIL；只有精确已知依赖或明确HARNESS_INCOMPLETE才BLOCKED。恢复DTO用passed布尔值。完整验收必须`--require-complete`，20 PASS/6 BLOCKED不是全量通过。AC-01—24区分源码、模块、正常功能、实机/外部和发布准入。

## 三位共同交接与停止条件

每2—4小时或每个可运行增量交SHA、文件、命令/原始输出、正常/负向结果及剩余依赖后，继续其他独立项。必须运行根规定检查与模块检查；FAIL/BLOCKED/UNTESTED如实保留，不删测试/放宽要求。新目录权限只覆盖本任务写明位置，中央共享文件提供patch即可继续。

主控接收/合并main、登记迁移/契约/生成配置与操作awskr；三位不自行合并main、不推送、不SSH、不用真实外部秘密、不改其他作者目录。没有自动启动Agent或代发消息；完成本包全部独立项/确无独立工作后停止并交最终HEAD。
