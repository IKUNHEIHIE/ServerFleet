# Wave 6 · 按审查缺口补齐可用功能

日期：2026-11-01；总控codex-review-02；完整v0.9及11月3日目标延续。代码起点2da203cac40ccc2bfc4c7bb644978005cd99dab1，作者精确起点为本轮最终交接main SHA。先读根AGENTS、当前任务板、模块/依赖规则、[独立审查](../../reviews/V09-INDEPENDENT-COMPLETION-AUDIT.md)与[最新SSH授权](CONTROL-RESUME.md)。不是再做纯设计轮，不以已有70步错误提示通过代替功能交付。

## 三线复合任务

| 任务 / 唯一会话 | 分支 / 独立Worktree | 允许目录 |
|---|---|---|
| V09-R6-CX / codex-06 | feature/v09-r6-platform-cx06 / `.worktrees/feature--v09-r6-platform-cx06` | apps/agent、internal/auth/transport/ai/plugins/updates/recovery、可选plugins/xray-manager；tests/integration/v09/platform、tests/poc/v09/platform；progress/codex/wave6 |
| V09-R6-DS / deepseek-06 | feature/v09-r6-services-ds06 / `.worktrees/feature--v09-r6-services-ds06` | internal/files/nodes/tasks/storage/audit/monitoring/notifications/quota/dns/certificates/gateway/servicegroups/publishing；tests/integration/v09/services、tests/poc/v09/services；progress/deepseek/wave6 |
| V09-R6-GM / gemini-06 | feature/v09-r6-console-gm06 / `.worktrees/feature--v09-r6-console-gm06` | apps/web/src、apps/web/public、tests/browser/v09；progress/gemini/wave6；前端锁/根配置仍总控单写 |

目录由总控从已验证main准备，初始核对路径/分支/精确HEAD/干净状态，不沿用旧分支未提交内容。各progress均在`docs/development/sprints/v09-five-day/`下，写自己的STATUS/HANDOFF/SHARED-CHANGES。总控独占apps/controller组合入口、API/Proto/gen、依赖锁/迁移公共编号/CI/打包/共享文档及awskr；作者有具体补丁即可提交，不等待整包做完才交。

## CX：优先关闭真实执行与平台缺口

1. **凭据密钥**：auth提供可持久安全密钥/借用策略接口，总控接入server/bootstrap。消除固定local-default材料，使用真实随机主密钥并验证私有文件权限、重启可解密、并发建立、轮换与失败关闭。旧库缺密钥/未知版本不能静默生成新密钥导致旧秘密不可读，不自动迁移/覆盖awskr已有服务数据；实际密钥不进Git/聊天/日志。
2. **Agent常驻与受控能力**：在单次模式基础上补常驻连接/有界重连/持续心跳/指标/取消和对账，已持久UNKNOWN不重跑；支持与DS协调的真实文件读写及必要正常运维操作。客户端不能任意上传Shell当已批准能力，必须服务端当前授权、不可变参数/目标、审批、真实降权/沙箱和逐动作白名单。缺正式PRIV/准入的root动作保持关闭，其他正常操作继续。给出Linux可复现启动/停止与安装候选，不碰既有服务。
3. **AI真实调用**：复用已有BYOK组件，交可注入ModelProfile/Generate及配置/秘密借用契约，移除主线固定不可用接缝；正常响应/流式/超时/取消/配额与撤权负向齐备。用本地受控TLS供应商验证实际HTTP协议，标清它不等于真实外部模型验收。
4. **Xray、更新、恢复**：将已有组件接成可注入的实际执行器/供应源/Host与HTTP能力，不能仅留unavailable处理器或只测501。仅可选Xray分发行，核心不import业务包；WASM/Host按PoC和安全评审准入，不擅改运行模型。候选工件安全FAIL不得安装；升级需签名/版本/备份/进程健康及恢复隔离/旧主控fence/撤销不复活证据。先给总控具体依赖/接口差异，推进其余子项，不能靠关闭安全门“打通”。

## DS：把本地暂存与抽象适配接到真实业务

1. **跨节点文件**：与CX确定主动WSS下的有界文件能力与数据帧/校验/取消/恢复，交总控独立Proto/生成/兼容差异；产品不得另造SSH通道或把文件秘密塞入普通任务日志。将现有files上传/下载/跨节点任务接入真实Agent通道，验证源读/目标写分别授权、符号链接/路径逃逸/空间不足/断线/部分结果、字节SHA及恢复。本地LocalStagingTransport保留明确测试用途，不冒充远端。
2. **供应商/网关/发布**：落实可注入Cloudflare、ACME、QQ/SMTP、Nginx受控执行及发布/逆序补偿。服务成功与供应商确认/实际配置结果绑定；默认缺配置应诚实拒绝，但“缺适配”不得记功能完成。网关检查和人工准入分开，既有站点不覆盖，DNS传播不当作即时回滚，PKI私钥独立持有。
3. **一致性与任务**：与CX当前授权/审批/执行ID、返回日志和字节通道接缝一致；保留快照/TTL/批次/熔断、持久审计、取消非回滚。提交真实成功路径及错误/重启/撤权案例；维护动作/幂等字段按当前共享端口，不平行造新任务中心。
4. **实机交接**：提供非交互、只用指定临时数据的测试步骤/输入/预期/清理和资源采样，由总控在awskr执行；未经专门授权不修改现有root服务/业务目录，不操作Cloudflare真实账号或外部秘密。

## GM：以功能成功为标准做前端与验收

1. 保留现有70步负向/退化回归，不删除“正确拒绝”验证。新增独立的正常成功用例：实际运维任务完成、两端Agent字节互传、AI生成/流式、Xray安装/配置/用户/流量、发布/补偿、更新/恢复执行。缺实现/缺环境应报FAIL/BLOCKED/UNTESTED，不用501/503/502反向通过来关闭这些功能。
2. 对齐后端新配置/端点/任务事件及安全密钥管理状态，不把敏感值返回普通页面/存浏览器；会话/主体/参数切换和错误首提交清旧行与计数，迟到结果不覆盖，403不清认证。补齐常驻节点/真实任务/文件状态与配置入口，既有断线/权限/键盘/窄屏功能回归。
3. 按原AC-01—24逐项给“代码具备/本地集成/实机/外部供应商/发布准入”证据矩阵，引用精确SHA/目标/命令/正常和负向输出。真实Node/供应商未跑只能留缺口，不从截图/源码文件数推断完成。输出当前剩余项及依赖，不擅自把完整v0.9改为本地阶段完成。

## 接口、节奏与停止

- 第一检查点优先交密钥配置/借用、Agent文件/常驻与模型执行器的**具体增量契约和代码**；总控集中接收共享差异，发布统一SHA。其他无依赖实现同步推进，避免整条线等待。
- 每2—4小时或每个可运行增量独立提交，交SHA继续本包下一项。常规修复直接做；仅对应硬依赖暂停，确无独立工作或全部完成时才停止。主线只由codex-review-02审查/合并，原总控不并行部署。
- 根门禁及模块负向照常；不删除/弱化旧测试、忽略FAIL或掩盖Xray扫描阻断。新增真实适配未提供安全证据不开放生产，G2/原始契约/已批准历史ADR不静默改写。
- awskr仅作为指定Controller隔离测试机；现有WSL可作后续本机测试Agent，物理跨机/双架构/1C1G/外部账号分别收证。本次没有授权购买/新建云资源或泄露真实秘密，也不自动启动/代发其他Agent消息。
