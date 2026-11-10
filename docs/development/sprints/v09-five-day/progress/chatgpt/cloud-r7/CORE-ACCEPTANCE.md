# 五项核心包：集中检查与剩余验证

2026-11-10更新见[本轮组合检查](CORE-ACCEPTANCE-20261110.md)：已以实际UID1000补跨进程续传/命令mTLS撤权/监督更新回滚/持久fence证据，集中修复R25；此页下方保留2026-11-09原始失败/环境，不能当当前全部状态。完整产品/Root/双机验收仍未通过。

任务 V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01，2026-11-09。源码 5c57b11134f0430db998acaef11f315c39e80647，起点 main9738648ecdf44546e4a2d3503f8c9b862c8a5cb3。先连续编码，再集中检查；发现失败后只复验受影响部分，不重跑已经通过的所有套件。**完整验收尚未通过，五项不是已认证 DONE。**

## 源码与验证状态

| 优先功能 | 已写的正式入口和执行路径 | 仍须实际证明 |
|---|---|---|
| 普通命令/受控提权 | 明确 profile、固定程序 FD/SHA/argv、普通及 root 当前人工审批、独立 root helper/mTLS/ledger、任务页 | 普通 UID/真实 root helper 环境、撤权/取消与重启；未安装 sudoers 或执行提权 |
| Agent 持久续传 | 完整源缓存、已确认 fsync 前缀、新审批任务、一次 adoption、最终发布 UNKNOWN 不重放、正式文件页 | 新恢复能力的实际进程重启/网络中断/并发 adoption/未知发布/浏览器专项 |
| 远程 Agent 批次更新 | 独立 parent、真实签名/字节/序列、防并发、实际重连/PID/nonce/版本健康、灰度1→5，非成功停批 | 新远程组合实际正常/负向/健康回滚/未知/多节点验收 |
| Controller 自身更新 | 独立进程所有权、SQLite 一致快照、维护拒绝、实际程序/版本/schema健康、兼容回滚和独立回执 | 真实受监督 Controller 更新/回滚/断电未知/审批撤销组合 |
| 灾备接管与新权威 | 离线 Root 绑定恢复、实际拥有旧进程 stop/wait、持久 fence、mTLS新 nonce持续观察、新 Intermediate/代次/撤销、显式激活、只读界面 | 新完整恢复组合、原生普通 UID 与独立主机；物理/电源/网络 fence 不由此代码或签名替代 |

## 已执行检查

固定 Go1.27.3、Node24.22.1、npm11.9.0（仓库要求11主版本）。独立私有 checkout；本沙箱 uid0。尝试已有 uid1000 的 chown/setpriv 均 EINVAL，未伪造非 root、放宽权限、添加 skip 或删除用例。

| 检查 | 对应源码 | 结果 |
|---|---|---|
| npm ci / npm run tools:install | 6c346cf | PASS，固定依赖/Go工具已准备 |
| npm run check | 6c346cf | FAIL：候选 YAML 未引用逗号描述、Controller返回签名/未用import、5项旧Web断言；原输出保留 |
| policy / git | 6c346cf | PASS |
| npm run check:contracts | fa70fb7 | PASS；原候选/Proto/生成/兼容检查保持，未修改 lint 规则 |
| Controller / Agent build、go mod verify / tidy -diff | fa70fb7 | PASS |
| 完整 Go 测试 | fa70fb7 | FAIL：952顶层与1298子项PASS、23顶层与3子项FAIL、21顶层与2子项SKIP；不累计成完成率 |
| npm run check:web | fa70fb7 | PASS，422测试及typecheck/build |
| npm run check:security | fa70fb7 | PASS，npm audit 0漏洞、govulncheck未发现漏洞 |
| 六个受影响Go包定向复验 | 361c634 | 历史25/28/21迁移、显式local-development CLI、网关probe revision、ACME redirect等已修；仍有DNS协议fixture与root恢复前提失败 |
| go vet ./... | 361c634 | PASS |
| internal/dns + internal/recovery 定向复验 | 5c57b11 | PASS，49顶层/29子项；真实tar解析扩展属性拒绝覆盖，不放宽PAX限制 |
| staticcheck ./... | 5c57b11 | PASS，修复fence错误变量遮蔽及已弃用tar字段使用 |
| 正式核心候选装配 | 5c57b11 | PASS：真实linux-amd64/arm64 Controller、Agent、Web及两组件规范manifest；UNSIGNED/UNVERIFIED，未签名/安装/部署；ARM仅交叉构建 |

旧失败不删除。已知仍受本机 root 前提阻挡的失败包括六项 Agent file/WSS 普通用户测试及 TestWave6IsolatedSQLiteRestoreKeepsQuarantineAndRevocations；保持原 FAIL，作为环境 BLOCKED 跟踪。最终同一 head 的完整原生 Linux CI 与新功能专项仍需实际结果，不能拼接不同 head/定向输出称全量绿灯。没有执行真实供应商/生产/VPS、实际 root profile 或物理双机操作。

## 证据与收口

[evidence/core-acceptance-20261109.zip](evidence/core-acceptance-20261109.zip) 保留集中失败、必要定向修复检查、命令/环境/SHA及真实未签名构建清单；[索引](evidence/core-acceptance-index.json) 给逐文件/归档摘要。未放入二进制、缓存、运行DB、私钥或PAT。

新功能主要路径已写入，计划通过 Draft PR 的普通用户Linux CI补查；不直接推main、不自动合并，不用旧CI证明新代码。正式操作接线见 CORE-PRIORITY-STARTUP.md。G2、B03—B06、实际提权/新功能完整组合、外部供应商、物理双机、ARM运行/硬1C1G与正式资料准入继续OPEN。即使通用CI变绿，也不替代这些专项与完整v0.9发布认证。
