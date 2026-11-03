# AM-02 首审返修接管

日期：2026-11-04（Asia/Shanghai）。任务 `V09-CLOUD-AM-02-FIX1`；唯一会话 `chatgpt-am02-fix1`；审查/集成主控 `chatgpt-review-02`。

用户因AgentMore额度用尽，明确授权ChatGPT **只修本轮A1—A4**。不启动持久续传、Controller更新、实际恢复、AI/工作流或其他平台后续开发。

- 仓库：IKUNHEIHIE/ServerFleet，保持private。
- 接手 [PR #11](https://github.com/IKUNHEIHIE/ServerFleet/pull/11)，head `0e57714f67cea40dfd7beacc72efdba5fc95026a`；#10已关闭未合并。
- 已接收main `9a6f75844f711003b6abcdd7e9ef2067b7b60384`；产品基线51e0ee5。正常同步已接收文档，无他家未合并代码。
- 独立目录 `/tmp/serverfleet-am02-repair-20261104`；本地分支 `fix/cloud-platform-am02-review1`，常规追加原 `feature/cloud-platform-am02` / #11，不强推/覆盖漂移。
- 允许本轮internal/files、Controller file_distribution*.go、对应测试/历史升级夹具及AM进度。主控单写协调新R9契约、Redocly候选登记与ds/0042编号；不改原三候选/Proto/gen/根锁/CI/他家生产域。
- 停止条件：完成A1—A4、实际检查及精确head审查/接收后停止；失败保留，不启动后续四项。开发不含生产/VPS/awskr/真实供应商/云API。

## 接手时失败

原head的 [CI41682307183](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/41682307183)：dependencies success、foundation failure；Go904 PASS/7 SKIP/1 FAIL，Web348 PASS。TestAM02DistributionMultiTargetRealAgents撤权场景未在45s轮询内收敛，A dispatching/B pending/task running。四个实际产品构建已成功，不能归因缺预构建。原作者20.48s本地PASS仅作历史。

主控独立回归还复现：已完成目标可重开；摘要拒绝后staging已删但会话仍open。

## 本轮改动

- A1：真实双Agent专项改为等待A已持久确认部分字节，在不持有DB事务的单次scope check设置≤2s可取消屏障；撤销B grant后放行。用多帧有界文件替代约23MiB速度窗口；仍断言A完整实际字节/摘要、B零预留/零落盘、partial及取消，不扩大45s轮询或放宽逐帧校验。
- A2：pending→dispatching原子claim，父分发须仍dispatching；终态/已claim行不可重开。worker无新claim不接触Agent/字节并保留已有结果；终态拒绝后续帧。补两个Repo竞争、所有终态/迟到结果、无claim无执行测试。
- A3：open摘要拒绝用独立CAS，失败状态/真实摘要/错误与审计同事务，核对RowsAffected，提交后才清理staging。审计失败回滚并保留字节，仅可重试本地校验，不提交任务。补重复complete/HTTP读回/审计失败。
- A4：候选SQL字节不变，仅am/0001重编号为主控预留ds/0042_file_distribution。恢复真正25项历史fixture，排除新迁移；当前候选组合27项，不提前包含#7的cx/0010。两包日后均接收才28项。R9是新增独立契约候选，不冻结G2；错误分类补400/404/409。

## 环境与验证边界

本地Go1.27.3、Node24.22.1、npm11.9.0（仓库允许npm11）、UID0；user namespace只映射UID0，普通UID/属主切换不可用，已记录环境限制，未放宽身份/0700门槛。

已执行npm ci、npm run tools:install；files/tasks/transport/server四包单元与-race通过；本地check:policy/contracts/web/git/security均PASS，Web348 PASS，npm audit0、govulncheck无已知漏洞。历史迁移/HTTPS组合及平台升级重放专项PASS；全部11段文件迁移SQL字符串与接手head逐字一致。本地root不能替代真实非root双Agent/文件/Nginx专项，完整npm run check由GitHub普通runner对最终head实测；结果返回前标待验。

最终SHA、命令/原输出/CI在最终代码提交后的PR评论补充，不为记录绿灯反复改PR说明。完整v0.9/B03—B06/G2/发布准入不因返修关闭。
