# Cloud R2：连续整包与共享接线窗口

日期：2026-11-03（Asia/Shanghai）。总控/审查：ChatGPT，唯一会话 chatgpt-review-02。
产品统一起点：`51e0ee57e66493892eb5f5168b9e0690b53f3661`；main独立CI [41615351426](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/41615351426) success：Go顶层902 PASS/7 SKIP/0 FAIL、Web348 PASS。正式迁移26项。仅文档登记不改变这个产品基线或发布结论。

三家环境完全独立；本文件与提示词只是 READY/待用户手动派发，不代表已启动。用户手动派发、回传PR；不代发平台消息、不启动其他Agent。三家同一个GitHub账号不等于三位独立审批身份。

| 任务 | 唯一会话 | 分支 | 状态 | 首检查点及连续范围 |
|---|---|---|---|---|
| V09-CLOUD-AM-02 | agentmore-02 | feature/cloud-platform-am02 | READY/待用户派发 | 真实多目标文件/取消/部分结果；持续续传、更新、恢复、平台负向；没有任何插件/代理组件工作 |
| V09-CLOUD-HW-02 | codearts-02 | feature/cloud-services-hw02 | READY/待用户派发 | 网关共享接线与真实正常路径；持续逆序补偿、ACME/cleanup和服务域独立项 |
| V09-CLOUD-WB-02 | workbuddy-02 | feature/cloud-console-xray-wb02 | READY/待用户派发 | Xray工件/准入差距及异步上传Web；持续Xray插件、全部正式Web/浏览器矩阵 |
| V09-CLOUD-COORD-02 | chatgpt-review-02 | docs/cloud-r2-dispatch-review02 | DISPATCH PREPARED | 当前文档登记；后续共享接缝审查/编号/顺序合并/独立验证由总控承担 |

完整独立提示词：[AgentMore](AGENTMORE-02.md)、[CodeArts](CODEARTS-02.md)、[WorkBuddy](WORKBUDDY-02.md)。每份包含独立访问、精确基线、目录、验收、PR和停止条件，可单独复制。

## 本轮所有权变更

Xray全部转WorkBuddy，包括可选业务包、插件平台、Xray专属凭据适配、正式Web和证据；AgentMore不负责这些事项，也不作为Xray前置依赖。WorkBuddy原“不改生产Go”仅在新提示词列出的Xray领域内被本次用户授权取代。核心不能编入可选插件源码，Runtime仍须正式准入，不因换owner而开启；WASM/Eino未获准入，旧安全条件不变。

AgentMore独占files/tasks/transport/updates/recovery/ai/workflows及列明程序文件。CodeArts独占服务域和gateway_*.go。WorkBuddy独占Web/浏览器/测试fixturehost及插件领域。其他包不能修改这些文件；公开端口变化尽早交Draft，主控协调契约/schema/生成/锁/CI。

## 防止中央接线再次长期悬空

CodeArts本轮须尽早提出独立共享候选PR `integration/cloud-gateway-wiring-hw02`，与常规服务能力PR分开。唯一中央候选窗口限网关所需server.go/agent_links.go/Controller main/Agent connect-main-runtime及对应测试、必要迁移与全局断言；允许准备和本地验证精确差异，不是正式接收。总控不与该窗口同时改同批文件；其他两家把中央请求放在自己进度的精确patch里。窗口以主控正式接收/明确释放结束，不以时间自动失效。候选必须真实可编译、接口/路由/构造和关闭顺序实际验证，不能用伪代码当已装配。

预留候选号 `cx/0010_gateway_artifact_bindings`，仅在本共享PR经总控审查和升级验证后登记，当前仍26项；不能复用或抢其他编号。只加此项时完整Controller应27，按实际集合验证，不把所有测试计数盲目+1。所有历史SQL/hash保持；历史升级测试须固定真正旧版本，不用当前定义重建假旧库。其他新增schema由总控逐项分配。

本包自己的能力PR与共享候选可在独立临时组合验证，必须公布两head/共同基线/tree，只称候选结果，不把未合并共享提交夹入能力分支。消费别家未合并PR须主控另行明确依赖/组合。共享接收后正常fetch/merge main复测；不重放旧prepare/overlay、不cherry-pick旧已接收作者提交。

## 上轮接收与剩余边界

上一轮AM/HW/WB-01只接收检查点：PR #1/#2/#3及共享#4/#5已合入本基线，整包剩余转新-02。旧CX/DS/GM-05是历史Windows任务，转交而非再次标DONE。上传已统一任务；Agent组件本地签名更新/回滚浏览器已验证；网关工件/子账本已接收但中央装配仍待做。保留终态CAS、未知不重放、skip/overwrite/rename、预算/409/只读等成果。

B03/B04/B05/B06、G2/D01-D07/P1-C10/发布准入、外部供应商/物理双机/ARM运行/硬1C1G、正式资料缺口及ENV-W6-01保持OPEN。原严格31 PASS/4 BLOCKED/0 FAIL是历史未完成证据，不自动改为通过。不得按测试数累加百分比或承诺交期。

## 检查点、权限及审查

每个可运行增量或约2—4小时交Draft检查点，并继续其余独立项；不是整包交期保证。最终代码提交后用PR评论补CI证据，避免改PR说明/追加记录提交触发edited流水线并替换旧运行。PR base=main、Conventional Commits、真实证据和所有FAIL/BLOCKED/SKIP保留。同账号无自我APPROVE；主控COMMENT审查并以expected SHA、合规merge标题顺序合并，重审漂移和组合。

开发只限各自普通用户私有测试目录及合成材料。三平台登录不等于私库App授权已验证；不索要密码/token、不改公开。没有生产/VPS/awskr/真实外部账号或云API授权。未知副作用不自动重放，不新增生产mock，不弱化身份/权限或版本要求。
