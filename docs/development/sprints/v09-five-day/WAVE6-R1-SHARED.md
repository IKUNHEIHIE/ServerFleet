# Wave6-R1 · 主控接收与下一轮接缝

总控codex-review-02；2026-11-01。接收CX `17017a6a1aba0e9af5c03fdbb89894bc52496be8`、DS `361098283b3ed0e987c70e2292839acf42d6b341`、GM `27c92be56b40cab725cca8a260c2ebe67fef078b`；被测最终代码`9ddd5c64a5887e4fb05a85fb362fd2c71e6bcea9`。最终main SHA以交接为准。三位继续原会话/分支；新任务见[连续实施包](WAVE6-R2.md)。

## 已接收的共享变更

- CX三份patch已审查接收并作组合调整。`Config.AgentFileGate`、`Server.AgentFiles`与桥接File*路由已提供；只有显式trusted gate时才协商minor1/文件能力。默认CLI仍不装配文件worker或路径映射，不能只设置一个放行gate来开启。
- `Config.LocalAIProvider`只允许程序显式注入loopback TLS供应商；模型设置不能传入URL。`Server.AIBudget`是当前默认持久预算端口，缺策略仍拒绝；配置只接受当前管理员、同owner和当前启用profile。没有HTTP预算管理路由或默认无限策略。
- 新正式迁移`cx/0007_ai_budgets_and_model_audit`与`ds/0081_publishing_targets`已登记，集中迁移数19→21；旧19项语句/编号不改，升级与二次应用测试保留旧发布记录。不要重建同名表或重复登记。预算fence/typed审计/通用审计同DS事务，模型typed审计默认启用；旧patch中的`AITypedAudit`开关不再需要。
- CX主控HTTP/TLS供应商测试已取消overlay/build tag和手工DDL，直接验证正式组合、预算上限、owner/profile拒绝及审计回滚。Linux runner的controller目标改跑正式组合；`prepare-wave6-r1-overlays.mjs`及作者旧patch仅为历史候选，不能再应用到新基线。
- 发布目标字段已正式持久：gateway IDs、zone、certificate、HTTPS及target revision。生产发布器、Agent Nginx apply/reload与审批链仍未完成；`targetsPersisted=true`不代表发布成功。
- DS供应商callback-scoped秘密端口、完整CA信任链/authorization/origin检查、文件原子claim/有界spool/进度/UNKNOWN与上传当前授权增量已接收。真实Cloudflare/SMTP/QQ/ACME账号仍未授权，主控默认没有通用解密/供应商自动装配。

## 主控返修须保留

- ACME所有退出路径都执行一次DNS清理；签发失败与清理失败同时保留，返回DNS record IDs，不返回未验证证书。原来仅成功签发后处理清理错误的实现不再使用。
- AI模型设置普通503不显示409；真正CAS冲突提示在读到新revision后保持。身份世代变化重建面板，保存成功才清冲突。
- Go测试临时子目录在Linux默认可能是0755，spool夹具显式0700；生产检查未放宽。首次15项Linux夹具失败保留。
- 上传会话是owner绑定的写流程：状态/PUT content/complete复查`files.write`，不附加远端`files.read`。新写权限专用HTTP测试先403后通过，同时验证没有获得下载读权限、撤销后状态不可读。缺通道仍503+失败审计，不能恢复Controller本机fallback。
- 正向脚本不再把403/500/文件失败或超时统称BLOCKED；恢复预演使用实际`passed: boolean`，空/错shape响应FAIL。B01默认明确HARNESS_INCOMPLETE，完整测试宿主配置后才用`--attempt-file-flow`，失败/超时必须FAIL。B03/B05仍不是完整安装/更新流程。

## 文件端到端仍缺的组合

当前DS协调器的“执行前两端已接受”、多item共用每节点executionID，与CX每个持久TaskOffer只绑定一个文件/不可变参数的生命周期尚未统一。源Open stat后才得到size/SHA，目标write offer必须冻结该观察；同一个execution不能跨item复用或重连重写。

下一轮由CX单独负责整个文件路径，含`internal/files`及新Controller file_*.go，DS暂停该目录写入。必须冻结transfer/items/显式路径映射、子执行ID/原始参数、当前身份/审批/任务/取消/AI范围，并将Agent TaskAck持久记录后再发Open；不得与通用dispatcher重复发offer，源单文件完成不能提前把整个跨节点任务标成功。只实现的skip/new-file明确说明；rename/overwrite/createParents/nonzero resume不可静默降级。

GM当前双Agent夹具未配置file-root，原B01写入实际fixtures却请求不存在的`/opt/serverfleet-qa`路径，且使用尚未支持的overwrite。主控已纠正请求路径/skip并明确测试宿主缺口；最终字节/SHA、双方任务终态与中断/撤权仍待验收。

## 仍开放

GM报告的Operator间歇403，本轮URL-safe ID向量/精确授权/撤销测试及实际Operator浏览器链路未复现，根因未证实，保留追踪，不扩大角色权限解决。持续拒绝必须FAIL并保存原始HTTP关联证据。

ENV-W6-01只读复查：awskr原有Agent仍root/auto-restart、MainPID0、退出码1，重启计数10461；未诊断根因或修改服务。本轮Linux补验在本机WSL普通用户隔离目录，不新增awskr部署/服务修改。

G2/D01—D07/P1-C10、正式PRIV/WC/PKI-OPS/UPDATE原文、Xray安全工件/Runtime、真实更新/恢复fence/生产与外部准入保持OPEN。没有ARM运行/1C1G/物理双机文件成功证据。
