# V09-5D-CX接续交接 / codex-03

2026-10-31，Asia/Shanghai。用户明确要求接替“完成 V09-5D-CX 开发包”（01a188b2-b181-7230-94a7-136c5356739b）继续开发。**包仍ACTIVE，完整v0.9尚未交付。** 本次是实际代码检查点，不是全部子项完成或“无独立工作”的停止结论。

## 任务和目录

- 任务V09-5D-CX，接续codex-02；新唯一会话codex-03。
- 审查codex-review-01+安全/契约/相关owner，允许目录与停止条件沿用CODEX.md/PLAN.md。
- 目录`D:\admin\desktop\codex\myproject\ks\.worktrees\feature--v09-platform-cx02`，分支feature/v09-platform-cx02。
- 派发/main基线8a7dd4707d2d7e142e137d098296788d2c80eddd；接手HEAD ecb7cd405a0630d814580d1a6123d3e9a56c3e97。保留并完成原会话7项未提交代码，无重置/清理/覆盖其他目录。
- 未改main、公共API/Proto/gen/根依赖/迁移/CI/AGENTS，未访问生产/VPS/云/真实凭据、推送、合并或启动其他Agent。

## 实现提交

1. `7f052425d0fd53964e07dbce12f711c408dac33b` — 中央实例绑定UUID/REALITY材料、完整scope AAD与共享GCM预算、先永久reserved后生成、同Tx完整typed审计、借用防重放/UNKNOWN、v2保护旧writer不丢字段。主要文件internal/auth/xray_materials.go、identity.go、identity_sql.go及对应测试/隔离SQL模板；接口与升级兼容见XRAY-CENTRAL-MATERIALS.md/S05。
2. `4ef7e53da01fbfa0a3c555aa3c273667abc98665` — 可选Xray持久用户元数据、中央引用检查、稳定统计ID/显示名分离、CAS/永久幂等/tombstone、pending/applied/unknown、任务应用证据和物理配置修订检查、当前材料复核、快照分页、真实SQLite改名同UUID/重启停用/审计回滚/事务前撤销。主要文件plugins/xray-manager/users.go、users_test.go、config.go和tests/poc/v09/platform/xray_material_sql_test.go.txt；接线见XRAY-MANAGED-USERS.md/S06。

本交接记录提交仅补充精确SHA和验证证据；上述第二提交为本轮最终代码HEAD，后续文档HEAD可由git log核对。机器记录TAKEOVER-EVIDENCE.json，逐增量历史RESUMED.md，当前缺口STATUS.md。

## 实际验证

| 命令 | 输出/证据 |
|---|---|
| npm ci | PASS，127包/25s，takeover-npm-ci.log |
| npm run tools:install | PASS，四个固定工具，takeover-tools-install.log |
| npm run check | 五组PASS，2026-10-30T17:38:25.159Z（上海10月31日），Web179项；users-r38-release-check.log |
| npm run check:security | PASS，17:38:15.568Z，npm audit 0 / govulncheck无漏洞；users-r38-release-security.log |
| node tests/poc/v09/platform/run-identity.mjs | 最终Windows SQLite15顶层PASS9.608s，实际Xray/用户组合1.27s；Agent21 PASS8.631s，Linux-only注册明确SKIP；users-r38-release-sql.log |
| node tests/poc/v09/platform/run-linux-wsl.mjs --targets=auth,sqlite | R37 Ubuntu非rootUID1000：auth67/SQLite15顶层PASS，无skip；LINUX-XRAY-MATERIALS-EVIDENCE.json |
| node tests/poc/v09/platform/run-linux-wsl.mjs --targets=xray-pure,sqlite | R38最终Ubuntu非rootUID1000：可选包17顶层PASS/3真实二进制SKIP、SQLite15 PASS/无skip；LINUX-XRAY-USERS-EVIDENCE.json |
| 最终源码/保护核对 | 最终所有源文件SHA256逐一吻合R38原生证据；受保护文件相对派发基线零差异，main仍8a7dd470；TAKEOVER-EVIDENCE.json |

所有日志位于本目录忽略的reports/_review/v09-platform，结构化源/二进制/日志哈希证据已跟踪。初轮用户SQLite FAIL为权限fixture缺必填Description，auth正确拒绝，修正说明后通过；原users-r38-sql.log保留。没有删除测试、放宽权限或将SKIP当PASS。R37旧会话首轮失败日志及R38中间证据继续保留追溯。

## 仍需实施/接收

- S05中央typed审计与auth v2升级/回退协调；首次创建材料后旧auth必须拒绝，不能清Version/claim来降级。
- S06正式Host私有元数据存储/完整typed审计schema与公共迁移、明确node权限目录、唯一任务/实际应用配置/健康证据及DTO/页面集成。当前真实的是SQLite/身份/grant/材料/cipher/fence/审计；task/profile/plugin与配置部署结果仍明确测试输入。
- 实际用户停用/断连、QR、Xray目标版本安装/接管/配置部署/恢复和流量业务尚未验收。两份已知外部发布物安全FAIL未获准入，不能运行或安装它们；3项真实二进制测试因此保持SKIP。
- CX-01—08其他缺口继续以STATUS.md为准，包含共享唯一任务/通信/审批接缝、正式Controller/Agent组合、PKI/更新恢复政策与真实环境及全部页面/AC。本轮未冻结G2、未把两个组件增量称为完整包。

后续在现有授权内继续平台工作；公共变更使用已给出的精确候选交总控独立接收。不要因为本检查点通过而放开默认授权、用假成功补真实部署、绕过中央凭据或唯一任务链，也不要重用早期HANDOFF/EVIDENCE快照覆盖本次结果。
