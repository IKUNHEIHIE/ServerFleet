# V09-CLOUD-R1：已接收检查点与最终主线验证

记录日期：2026-11-03（Asia/Shanghai）。总控/审查：ChatGPT。
已接收产品基线：`51e0ee57e66493892eb5f5168b9e0690b53f3661`。
合并后的产品 tree：`57ecccaf508512abce778f22faa4e9a928c60ff0`，与主控独立复验的三线组合一致。
本报告补齐仓库内进度入口；不修改原始需求/历史ADR，不作为完整v0.9发布认证。

## 接收记录

| PR | 已审查最终 head | 合并 main | 接收范围 |
|---|---|---|---|
| [#4](https://github.com/IKUNHEIHIE/ServerFleet/pull/4) | 78e5e366f198379cf1cf1040d376cefe3b722259 | cce2b290536be650c8afd4a5962a1bd5a2717276 | Linux网关测试夹具与从当前源码实际构建Controller/Agent的测试前置 |
| [#1](https://github.com/IKUNHEIHIE/ServerFleet/pull/1) | 7767d789637b57670126f5a5190db7c19a4a4156 | 8b10e32e4a083f22e9a08fdc8d376ea24df25d20 | WorkBuddy：Agent本地签名更新/真实回滚浏览器专项与恢复分类 |
| [#5](https://github.com/IKUNHEIHIE/ServerFleet/pull/5) | fc9739971c06e3ecaa93513571deffc324b3ca72 | 3a1aefb92082565282eb5e35991b975722a45d7b | 共享ds/0041与R8上传候选、固定旧数据库升级回归 |
| [#2](https://github.com/IKUNHEIHIE/ServerFleet/pull/2) | 2d99eb5c1c37e95848ec0c332a6c69984601ae6c | 36fed6239d6f8a2cca349f8c2808c0c57792260a | CodeArts：网关工件/独立子身份/逐帧授权/持久终态CAS；中央装配未接通 |
| [#3](https://github.com/IKUNHEIHIE/ServerFleet/pull/3) | 4ceedd9d79756f5ed6c34154e34af46cc01ba1f3 | 51e0ee57e66493892eb5f5168b9e0690b53f3661 | AgentMore：上传统一任务、取消/权限/摘要/异常终态；主控常规同步已接收共享定义 |

作者最初的AM返修head为e423c40445dc09f4bfb009bc1f82cf416cdaf998，最终4ceedd9只做主控常规main同步和共享迁移冲突解决，业务文件未改。正式迁移26项；未注册的gateway候选不计入数量。ds/0040旧SQL与checksum保持不变，ds/0041只增加upload任务绑定。

所有合并以审查head的expected SHA保护，未直接push main、强推、自动合并或同账号自我APPROVE。审查以PR中的COMMENT与实际证据记录；产品树有变必须重审。CodeArts审查期间追加5行文档时，旧head合并被409正确拒绝，随后审查新head及对应CI才接收。

## 最终 main 的实际 CI

[Foundation CI run41615351426](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/41615351426)，head为上述产品基线，completed/success。已读取两个实际job的日志：

| 检查 | 结果与证据范围 |
|---|---|
| foundation，job125847815996 | success；npm ci、固定工具、policy/contracts/Go/Web、生成/锁无漂移、Linux双架构交叉构建 |
| Go | 902个顶层PASS、7 SKIP、0 FAIL；不与旧套件或子测试累计折算完成率 |
| Web | 348 PASS |
| dependencies，job125847815627 | success；npm audit 0漏洞，govulncheck无已知漏洞 |
| 原资料/契约/生成 | 69原始资料及3原候选字节校验、Proto/gen/兼容和根锁检查通过 |
| Linux amd64/arm64构建 | 编译通过；不代表ARM实际运行或硬1C1G验收 |

主控还在独立临时组合中复验server/files/contracts/architecture全包-race与5项历史迁移升级/重放/跨域备份专项，均PASS；实际组合tree与最终main一致。主控容器只有UID0映射，普通用户真实执行专项在该容器的限制仍保留；不把runner证据冒称主控本地普通用户复跑。

## 专项证据与产品验收边界

- 用户上传：统一worker/独立审批、当前权限与文件帧、queued取消、真实字节/摘要及终态对账已接收。冻结/重组Controller组合不等于实际进程kill-9恢复；多目标分发和跨重启完整续传仍未完成。
- Agent更新：WorkBuddy的21条浏览器专项作者证据已核读，含真实Agent版本、槽位摘要、进程/健康、失败回滚receipt及标记。主控独立分类控制测试18个场景PASS；这些不是最终main的902个Go测试或348个Web测试之外的可累计完成率。
- 恢复探针：作者诊断11 PASS/3 BLOCKED/0 FAIL；严格模式退出1，不能当灾备恢复成功。实际恢复/新权威/旧主控fence未验收。
- 网关：独立子执行ID、冻结计划与参数、queued等价阶段门、子事件隔离、CAS及跨Observer并发负向已接收；正式程序中央接线、完整发布/HTTP字节/逆序补偿仍待实现和验收。
- 原全量严格正向31 PASS/4 BLOCKED/0 FAIL、退出1属于历史证据，本轮未重新跑通该完整套件。Live70/Stub16等历史结果不冒称本轮重跑。

## 已发现并保留的失败

1. 原Linux盘符路径/真实产品预构建前置失败经PR #4修复；不以skip/假产物/降低身份要求解决。
2. ds/0040原SQL缩进变化会改变历史checksum。主控用固定1ca7907旧SQL建立真实25项旧ledger，先复现升级FAIL，再恢复旧字节并追加ds/0041，升级/重放26项PASS。旧hash为a7b7ed891f79d592593e1eea435d1430df94965f075458dc222d1a435fe210f7。
3. 迟到Ack及并发Started覆盖子账本终态的两条原审查回归先FAIL，修复CAS后PASS；UNKNOWN不得升级或重放。
4. 原3a1主线[run41611623778](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/41611623778)因GitHub默认merge标题不符合既有policy而失败。主控后续显式使用Conventional Commits merge标题，不改规则/重写历史；最终51e0主线成功已验证收敛。
5. PR说明edited事件会触发新CI并取消同组旧运行；取消记录保留、不算PASS。最终接收使用各精确head真实成功运行。证据补充优先PR评论，避免无谓触发重跑。

[机器摘要](V09-CLOUD-R1-RESULTS.json)与上述证据一同阅读；历史失败/跳过不能删除。

## 当前剩余与下一轮

B03（Xray安全工件/Runtime）、B04（完整发布/补偿）、B05（Controller可信更新/部署装配）、B06（实际灾备/新权威/fence）均OPEN。G2/D01-D07/P1-C10/发布准入、正式资料缺项、外部供应商/物理双机/ARM实际运行/硬1C1G及ENV-W6-01未关闭。

[Cloud R2](../sprints/v09-five-day/cloud-r2/PLAN.md)：AM-02文件/更新/恢复/平台；HW-02网关/发布/ACME-cleanup及服务域；WB-02独占Xray全部工作和正式Web/浏览器。现登记READY，开工与提交尚待实际回报核验。没有新任务运行证据不等于推断平台没有本地活动，不自动标IN PROGRESS或DONE。所有权转交不等于Runtime批准；原包只接收检查点，不称整包完成。
