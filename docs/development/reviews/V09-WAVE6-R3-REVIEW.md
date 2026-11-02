# Wave6-R3 · 三线-04审查、组合与续派

审查/集成codex-review-02；2026-11-02；起始main `fafbe932e2aef1b071366b0cd4e6e96318e65924`。**本轮增量经必要修复和正式共享组合后接收；完整v0.9仍未完成。严格正向31 PASS / 4 BLOCKED / 0 FAIL，实际退出1。**

## 来源与版本

| 来源 | 精确HEAD与接收内容 |
|---|---|
| CX-04 / codex-06 | `f643c4956c8cd4117052cfc6fe341e506b7a24ba`；作者代码347cefb；可信文件CLI配置、overwrite/rename、实际Agent产品更新/签名来源/计划指纹/健康与回滚 |
| DS-04 / deepseek-06 | `0b6cf7bd05cbcdd3193d42abefed872e9051e10e`；含1a98086、eed7538、2c1aee6、dda43a1、73dbebb、f8aa07b、94ffe16及证据；cleanup claim/lease/取消优先、网关审批/终态/根/UNKNOWN、发布补偿与callback绑定 |
| CA-04 / codex-arts-01 | `a70694bc423be970291b62f9684f4916d7eba9ea`；owner预算UI、B01真实文件页面/负向、第二Controller隔离宿主、24项映射与拒绝隐藏计数。负责人消息标作“Gemini”，实际ref是feature/v09-r6-console-ca01；原Gemini未开工目录保持原样 |
| 总控主要组合 | `bee827406ccd7bb3b5bf7ce2becdca2a25a65cb7`；两份CX共享patch、25项迁移、queued取消、策略验收兼容。无overlay CLI/产品测试在此通过 |
| 根/安全/13组Linux | `1069519e19a9f66e91840f78c7f4ef57ebee8800`；其间仅修私有测试根和弹窗测试时序，无产品行为放宽 |
| 最终技术/浏览器 | `8df015456b2d57c8e960f934c9a7d3f3817c1894`；相比1069519仅修新增策略检查的WSL `--exec`传参。浏览器31/4/0在此通过；后续仅文档/证据 |

CX/DS从db3cf87分叉，CA从fafbe93分叉；三方合入保留总控的Arts转交登记，没有把相对基线差异误当成作者删除文档。最终main和三位活动作者目录的精确SHA/干净快进记录在主目录`reports/v09-wave6-r3-review-handoff.json`，不重写原提交，不推送远端。

## 发现与处理

| 编号 | 结果 |
|---|---|
| R3-01 | **queued取消不收敛，已修复**。CA给出的失败场景是创建后立即cancel、没有运行上下文时卡cancelling。新增仓库回归实际先FAIL；成功取消统一任务后，以原子条件更新让仍queued的transfer进入cancelled并记录完成时间，阻止ClaimTransfer；running继续等worker证据，不覆盖并发终态/未知写。修改后新旧取消/恢复检查PASS |
| R3-02 | **跨分支策略断言过时，已协调**。CX已实现三种策略，CA/R2原验收还要求overwrite拒绝。总控明确接收services黑盒的受审修改，保留未知策略/遍历拒绝；新增真实浏览器overwrite与rename字节、实际目标路径、原文件保留及双端任务终态检查，均PASS，没有仅删除负向 |
| R3-03 | **Linux网关夹具不私有，已修复**。DS新增生产私有根检查正确拒绝测试TempDir；原生Agent首轮9个网关用例FAIL。仅让测试prefix明确0700，保持生产owner/权限检查，原生Agent完整复验PASS；Windows两组真实Nginx/WSS也复验PASS |
| R3-04 | **弹窗测试竞态，已修复**。根首轮Web347 PASS/1 FAIL：引用模态显示后过早发Escape，轮换时两个dialog仍同时存在。等待焦点进入具名引用模态、Escape后断言消失，再按名称查询轮换；保留实际键盘/关闭断言。首次新增焦点断言误匹配两个“关闭”按钮，已改查焦点在模态内，失败输出保留；最终Web348 PASS |
| R3-05 | **正式共享组合已接收**。应用CX两份R3中央patch、ds/0063迁移，更新旧schema升级/重入检查；R3真实组合runner移除prepare、overlay和候选tag。默认缺配置保持拒绝；不重复登记已存在的权限/Redocly项 |
| R3-06 | **新增浏览器脚本传参错误，已修复**。总控新增两项策略检查首轮因WSL经shell解释inline Node表达式而FAIL，尚未到产品调用；改用--exec直接argv。保留29 PASS/4 BLOCKED/2 FAIL原始输出，最终完整重跑31/4/0，未改产品或放宽断言 |
| R3-07 | **OPEN：实际发布产品组合**。DS模块已有审批前零上传、taskID工件绑定、同计划幂等/补偿，但缺真实文件通道Uploader、Agent程序配置/注册及中央bridge。queued上传与只接受running的CheckExecution语义不一致，不能直接拼接或绕过。默认仍关闭生产发布，由DS-05完整负责 |

## 实际组合与边界

正式迁移现为**25项**（新增ds/0063_cleanup_claim），历史checksum保留，旧19/21 schema升级及重复应用通过。cleanup token/attempt预留、cancel胜过晚到I/O、lease过期unknown不重放已有模块证据；cleaner默认nil，无自动真实删除。实际provider错误的可重试/未知结果分类及证书产品调用仍待组合。

文件`-file-deployment`已成为真实Controller CLI启动路径；Linux配置当前UID、0700父目录、0600单链接文件与显式根/spool检查在PKI/DB打开前完成。skip/overwrite/rename现在是真实统一任务行为，rename最多32次/同目录/实际路径回写。用户上传统一任务化、多目标、跨主控重启续传和当前64MiB等需求差距仍未关闭。

UpdateProduct可信程序装配已接收：实际签名inbox/active初始来源，统一审批、schema版本+DDL指纹、实际Agent版本/健康和兼容回滚；只拥有自己启动的进程。**已验证的是Agent组件、本地签名来源和程序注入，不是完整Controller升级、默认CLI发行源或B05浏览器交付。** 恢复入口门/第二Controller隔离也不等于真正恢复/新权威/旧主控fence。

预算UI和B01测试宿主已接收，测试→生产依赖方向/私有配置/loopback只读遥测/正式Cookie+CSRF预算配置保持。R7 services候选更新502/unknown/409等语义；原三OpenAPI、69原文、Proto/gen/兼容基线/r03不变，G2及D01—D07/P1-C10未冻结。

## 独立验证

| 检查 | 结果与范围 |
|---|---|
| npm ci；npm run tools:install | PASS；固定版本，用已有同版本本机模块缓存，不改manifest/锁 |
| npm run check | 最终全部PASS；Go **839顶层+1198子测试PASS / 28 SKIP / 0 FAIL**；Web **348 PASS**。首轮仅Web竞态FAIL，原日志保留 |
| npm run check:security；Linux amd64 govulncheck | PASS；npm 0漏洞，无已知Go漏洞 |
| 13组原生Linux | UID1000、**382顶层PASS / 5 SKIP**。Agent53、transport58、AI34、budget1、Controller1、updates21、recovery9、restore1、files62、DNS25、notifications31、certificates40、publishing46。两helper仅由父子进程用例调用、两Nginx因WSL未安装跳过、Windows专用路径跳过；未把SKIP隐藏 |
| 实际Controller CLI/Agent产品组合 | **2 PASS / 0 SKIP**，正式25项迁移、无overlay/tag：65,000字节/SHA、两端终态、三种文件策略/未知根/17项拒绝；实际Agent 1.0.1安装、1.0.2版本不符回滚、审批后schema漂移UNKNOWN |
| 实际Nginx与WSS | Windows普通用户私有prefix、高位loopback；两组PASS，含真实WSS+mTLS→nginx验证/reload→HTTP代理字节与移除/幂等；不接管系统实例。该测试不是完整Controller发布任务链 |
| cleanup竞争专项 | Concurrent/CancelWins/RetryDueDoesNotDuplicate/StaleClaim/RecoverStale筛选重复10次PASS；未运行race detector，不等于所有provider I/O未知情况均完成 |
| 真实Controller浏览器 | **70/70 PASS** |
| 契约桩浏览器 | **16/16 PASS**，先完成dist构建再运行 |
| 严格正向浏览器 | **31 PASS / 4 BLOCKED / 0 FAIL（35步）**，实际退出**1**；B01字节/双方终态与取消/撤权/丢回执/缺源/路径负向、overwrite/rename真实字节、B02真实生成、P20预算CAS/计数保留、第二Controller隔离基础通过 |

Windows、Linux、模块与组合用例有重叠，不相加为独立覆盖率；较前轮新增验收步骤，31/35不能换算为v0.9完成率。源码版本区别及全部SKIP/摘要/失败输出见[机器结果与52项证据](V09-WAVE6-R3-RESULTS.json)。压缩工件解码往返/SHA-256验证，不提交测试DB、私钥或可执行二进制。

## 未完成与下一步

四个正向阻塞仍是 **B03 Xray安全工件/Runtime，B04实际发布/补偿，B05更新浏览器/部署装配，B06真实灾备执行/新权威/旧主控隔离**。此外文件上传任务化/多目标/完整续传、Controller组件更新、ACME产品调用、真实provider验收等仍有工作，不能把四个步骤看成仅剩四个小任务。

本轮无SSH/云API/真实外部秘密/生产部署或既有服务修改，未重新观察awskr；ENV-W6-01沿用未关闭。物理双机、ARM、硬1C1G、正式PRIV/WC/PKI-OPS/UPDATE缺项、Xray历史工件安全及G2发布准入保持未完成，11月3日完整交付存在实质风险。

续派见[共享实现](../sprints/v09-five-day/WAVE6-R3-SHARED.md)和[WAVE6-R4](../sprints/v09-five-day/WAVE6-R4.md)：CX-05文件上传任务化/剩余平台，DS-05独占整条网关工件/执行/补偿及ACME/cleanup组合，CA-05直接消费UpdateProduct补B05真实浏览器宿主并继续其他独立项。作者不自动启动；仅总控在原HEAD未变且干净时快进同步。
