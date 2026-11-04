# 本包 AC 相关矩阵（CP1）

本文件是本包自己的观察，不修改 `docs/development/sprints/v09-five-day/ACCEPTANCE.json`（主控汇总）。
判定口径：只有真实接口 + 真实执行/浏览器证据才可写 PASS；缺前提写 BLOCKED；本轮没跑写 UNTESTED；**任何 5xx/401/403/错误 shape/超时都不算通过**。

| AC | 名称 | 本包覆盖内容 | 状态 | 缺什么 |
|---|---|---|---|---|
| AC-03 | 文件互传 | 上传会话→PUT 字节→complete；**202/dispatching/taskId** 受理语义、刷新/断线 GET 读回、任务取消、403/409/413/503/摘要/UNKNOWN 文案；R2 后证据分级（缓存只存身份元数据、失败读回不得显示完成）与节点/身份作用域绑定；保留 skip/overwrite/rename、预算、409、只读成果 | PARTIAL | 真实 Agent 两节点端到端（真实传输通道）与非 root 环境；浏览器 Live 专项；多目标/续传后端属 AM 域 |
| AC-12 | 插件平台 | 生命周期状态机已存在（install/upgrade/enable/disable/uninstall，`installed_disabled` 持久意图）；本包新增批量计划/审批绑定与配额/进程重置状态机；R2 后审批需**重算当前计划**（策略/波次/资格/目标集合），过期 digest 不再被接受 | PARTIAL | 真实 Host 准入；安装/部署提交路径（C-01）；权限目录（C-03）；审批身份与台账在控制器 |
| AC-20 | 流量配额 | 配额评估（total/uplink/downlink、窗口、80% 预警防溢出）、计数器复位→unknown 不伪造 0、进程重置把段内用量折算进 `ConsumedBytes` 并归零 offset（150→151 组合回归）、窗口滚动以当前绝对计数为新 offset（新窗只计新字节）、缺来源/段不匹配/重放窗口一律拒绝且不清零 | PARTIAL | StatsService 真实采集（需 C-01/C-02 与结构化遥测端口）；页面数据源仍为 501/UNAVAILABLE |
| AC-21 | Xray 安装 | 目标筛选/离线/架构未知/端口冲突/已有实例需接管审批（`batch.go`）；安装受生命周期与准入双门 | BLOCKED | 工件准入（B03）+ C-01 + C-02；**未运行任何工件** |
| AC-22 | Xray 配置 | 模板 vless+tls+vision、vless+reality+vision、custom-json 的既有校验器保留；新增计划波次与 digest 一致性校验 | BLOCKED | 真实 `xray run -test`（需准入工件）；部署通道 |
| AC-23 | Xray 用户与受控导出 | 用户/导出的既有候选实现保留；配额与进程重置不影响导出 gate | BLOCKED | C-03 权限目录 + 中央材料 Host 接收 |
| AC-24 | Xray 流量 | 配额/重置数学 + 缺口处理（缺失即 unknown） | BLOCKED | 真实 StatsService 观测端口（AM Proto 承载 int64 的限制未解） |

## 严格模式（strict）说明

本轮**没有**执行 AC-01—24 严格矩阵：需要最终组合 SHA 与合格 non-root 环境。本环境已有 7 项环境性既有 FAIL（见 `STATUS.md` §3.1），严格模式必然非零退出，因此如实登记 **BLOCKED**，不产生“严格通过”结论。

## 18 项恢复分类控制回归

未在本轮重跑（属 AM 域执行 + 需真实恢复宿主演练）；不吞掉缺失/额外/重复检查，也不把恢复密钥错误当通过——保持原状态，未做任何“降级通过”。
