# V09-R6-CX-03 / codex-06

审查codex-review-02；同步基线c9013f4231c5f0a586eb0af26386f6e265996c0c，已核原分支和干净目录。沿用CX范围，按WAVE6-R2第1项扩展internal/files及统一tasks worker，新增Controller file_*.go/ai_budget_http.go；中央server/enrollment/bridge/迁移登记/根配置只交patch。未使用其他Agent、消息、awskr/SSH/云/真实外部秘密，不自行合并main。

首检查点实施中：统一调度器持久reserve父任务后启动程序注册worker，每node/item一个唯一子执行；源读冻结stat/SHA后创建目标写offer。子TaskAck在Controller持久记录后才发Open，子Finished不提前结束父任务；同连接观察/完整offer/当前task/approval/源读目标写/路径映射逐帧复核。历史R1 patch不再应用。

首检查点实际WSL非rootPASS：正式Controller HTTPS/登录/入网/授权/transfer HTTP→唯一tasks worker→两个实际Agent `-serve -file-root`进程，三个文件项（99,000/48,000/0字节）、六个唯一子执行，目标实际字节/SHA和transfer/父task完成。第二次显式skip不写已有目标；overwrite在创建前400拒绝。真实SQLite触发器拒绝持久接受记录，未开放Open/目标写，父任务UNKNOWN。实际目标已发布字节后额外当前scope gate拒绝回执，父任务UNKNOWN；重开Controller数据库/监听并让原常驻Agent实际重连，未增加子执行或重写目标。中央注册/新cx/0008迁移只经候选overlay执行，无直接共享入口改动。

初验FAIL如实保留：测试bootstrap幂等键短于正式16字节下限，修夹具不降门；随后负向暴露真实tasks收口缺陷：两个target均UNKNOWN后纯batch gate仍返回Incomplete，父任务留running。只在持久调度服务增加UNKNOWN终态处理，保留纯batch gate原语义，新增真实SQLite回归和worker占用/无dispatcher/重建不重放测试后PASS。日志file-flow-first-bootstrap-fail、file-flow-unknown-parent-before-fix留reports/wave6-r2。另修测试局部变量遮蔽的编译错误，未改产品校验。

ControllerLinks新增有界Close，先封闭新连接并等待本hub已拥有socket/domain回调，再关数据库；只关闭本对象连接，不触碰外部进程/服务。未复用旧版R1补丁。本检查点继续收口模块检查后提交，后续推进AI预算HTTP和剩余平台项。
