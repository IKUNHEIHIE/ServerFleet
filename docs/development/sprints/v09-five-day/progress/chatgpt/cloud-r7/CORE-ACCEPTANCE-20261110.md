# 五项核心包：2026-11-10组合收口

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110；起点c7520ba，原ca309ea及13项后续实现保持。当前实际UID1000，/proc/self/uid_map为1000→外部0，根目录在此命名空间归属1000。未执行sudo/真实Root helper、生产/VPS/外部账号操作。固定Go1.27.3、Node24.22.1和npm11。无历史SQL/hash、依赖锁、生成或CI改写。

## 本轮修复

- 普通命令只使用同一次已校验policy摘要/程序/参数边界的快照，移除校验后忽略摘要的二次读取竞态。
- parent启动前冻结旧mailbox请求，初始readiness期间提交的新批准请求正常处理；未决update-active即使尚无slot锁也阻止再次启动child。
- mailbox严格单JSON文档；Controller停机交接共享task/revision/plan/独立审批和默认30分钟期限，未来/零时间、自批、过期与旧版本拒绝；admit须实际running/dispatched。
- 修正上一轮R24 YAML flow描述未引用逗号的6个struct错误；命令负向回归用原子路径替换模拟程序变化，保留原Linux ETXTBSY失败；上传恢复大文件测试给摘要/分块异步操作有界5秒等待，仍逐项断言精确PUT次数、偏移和202语义。

## 实际检查

| 检查 | 结果 |
|---|---|
| 固定模块下载、npm ci、tools:install | PASS |
| 完整Go，go test -json -count=1 -p 2 ./... | **原始FAIL，退出1**；1008顶层PASS、2顶层FAIL、13顶层SKIP；不改写原输出 |
| 上述失败一：命令测试写入执行过的inode触发ETXTBSY | fixture改用原子替换路径；仅Agent受影响定向11项（含子项）PASS、0FAIL |
| 上述失败二：TestCloudR3GatewayActualAgentPublicationAndReverseCompensation | 当前PATH无实际nginx；**ENVIRONMENT BLOCKED**，未删测试/加skip/假造网关结果 |
| Web typecheck / 完整Vitest | typecheck PASS；完整421PASS、1FAIL，上传恢复异步状态等待超时，原输出保留 |
| Web受影响上传恢复文件复验 / 正式Vite build | 10PASS / build PASS；未重复跑全Web套件 |
| 初始contracts / 修正后完整contracts | 原6项struct错误保留；修正后结构/全部OpenAPI/Buf/兼容/生成及正负探针PASS |
| policy / 模块边界 / actionlint | PASS |
| mod verify / tidy -diff / 两个入口build / go vet / staticcheck | 全部PASS |
| npm audit / govulncheck | PASS，0漏洞/未发现漏洞 |

本地没有把不同阶段的定向结果合并成“最终同一head完整全绿”。最终精确head原生CI另行读取；未完成前不称已接收或main可用。没有跑第二遍本地完整Go或完整Web套件；实际失败只对受影响检查复验。

## 实际跨进程证明与范围

| 核心能力 | 已新增并通过的真实验证 | 仍不能据此证明 |
|---|---|---|
| 普通命令/受控Root | 真正pinned FD子进程产生副作用；配置/字节变化拒绝；合成专用mTLS下撤权杀死实际进程组 | 真实Root helper/sudoers、正式Controller命令审批到提权完整链 |
| Agent执行续传 | 两个实际OS进程先写确认前缀再接续；旧执行失效、并发只一方adopt、损坏/未知发布/撤权拒绝 | 新恢复任务的完整Controller/WSS/浏览器组合 |
| 远程Agent批次更新 | Agent模式parent消费实际签名子程序、就绪窗口不丢任务、替换/重启不重放、不健康回滚且高水位保持 | 正式Controller→灰度任务→WSS→parent→真实新版Agent的完整多节点组合 |
| Controller自更新 | Controller模式实际owned child替换/重启、停机后撤权UNKNOWN、未决mailbox不重新启动；既有实际SQLite任务/回滚用例通过 | 真实Controller二进制自身替换、维护、schema变化和故障恢复全链 |
| 灾备接管 | 可信control实际stop/wait自己的Controller模式子程序；持久fence读回、不同权威拒绝、parent重启不复活 | 外部mTLS/离线Root授权→activate完整联调、真实双机/物理电源或网络隔离 |

子程序使用私有测试就绪协议，不是假的健康成功但也不是正式产品就绪适配器；不能把这层通过扩大成整套产品验收。Root/完整产品联调/浏览器、物理双机、ARM实跑、硬1C1G、外部供应商及G2/B03—B06/发布继续OPEN。五项总体仍IN PROGRESS，较上轮增加可重现的正常/负向/重启实证。

## 原始证据

原始25份日志/JSON、原失败与必要复验，以及归档/源文件SHA256索引保留在本地 reports/core-priority-20261110。自动审批以“本轮未明确授权共享原始测试报告”为由拒绝上传原始归档；远端不包含该归档或原始索引，只记录本页检查结果摘要及可复现的源代码。执行二进制、模块缓存、运行DB和私钥不提交。
