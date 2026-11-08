# 持久暂存续传检查点

V09-CLOUD-STAGING-RESUME-01 / chatgpt-takeover-01。IN PROGRESS；模块检查通过，整包尚未验收。

已发现：WriteUploadContent/WriteDistributionContent在文件Sync之后、进度提交之前崩溃会留下长于数据库偏移的尾部，原实现随后一律拒绝续写。计划只在当前身份获准且状态仍open时，以持久偏移为准裁掉未确认尾部；缺失已确认字节、非私有文件、链接与终态拒绝，最终摘要照常验证。AppendChunk使用同一恢复约束，未知Agent任务不重放。

资料来源：四份原始已批准ADR已逐字节恢复，旧69资料与三OpenAPI不变；本包连同来源索引提交，不声称G2/Runtime或安全验收已通过。

验证按整批修改集中执行；不逐处跑全套，不靠无改动重跑取绿灯。

首轮集中模块检查暴露夹具temp目录不是0700，以及实现把新恢复前提扩展到了无尾部普通写入。已把严格目录验证限定于新增的自动尾部恢复（仍强制Linux/当前owner/0700/0600/单硬链），普通无尾部写入保留原兼容语义并新增锚定/普通文件检查；恢复专项显式准备0700。首轮失败保留，不删除负向要求。后续按本批新修复复验。

## 集中检查结果

- 新增staging_resume.go共用锚定文件句柄；写入与complete均以持久偏移为准，Linux当前owner/0700目录/0600普通单硬链文件才可裁掉未确认尾部；少于已确认长度拒绝。Complete对同一检查过的句柄计算摘要，避免接受缺失字节后继续派发。
- AppendChunk、HTTP上传流、多目标暂存共同使用；已派发/终态仍先拒绝，最终SHA-256照常验证，无Agent重放路径。Windows等平台新增尾部恢复仍拒绝，未伪造ACL等价性。
- 复验前发现分发暂存本来是owner绑定，节点当前权限在complete和worker逐帧执行重检；修正新专项的断言以严格要求撤权后不得complete/派发，不把暂存写入误当节点执行。没有改这一生产权限语义。
- 首轮Go61顶层PASS/17FAIL/1SKIP（新恢复目录前提误扩展）；中间77PASS/1FAIL/1SKIP（新专项对分发暂存的错误假设）；最终79顶层PASS/1SKIP/0FAIL，含所有子项124PASS。实际SQLite关闭重开，确定性构造write+Sync到进度提交之间的崩溃状态；不声称已执行进程kill/真实双机。命令：Go1.27.3 go test ./internal/files -count=1 -json；staticcheck ./internal/files退出0。
- 新的normal/negative合计34个恢复子场景；链接/硬链/公开目录或文件/错误offset/缺失字节/撤权/已派发拒绝与最终摘要均保留。一个SKIP是既有普通用户要求，本机UID0不冒充该证据。
- 全量检查、普通用户产品组合及最终接收仍待候选PR的完整CI；浏览器跨重启重新选择文件、Agent续传/执行未知和其余发布关卡继续OPEN。

## 接收回执

已由PR#13合入main f1d715b；精确head81ea92b，CI42456874996为Go971PASS/7SKIP/0FAIL、Web380PASS，独立main CI42458157478也success。上方候选/首轮失败是历史过程，不删除。Web重新选文件续传转V09-CLOUD-BROWSER-RESUME-01，远端Agent执行恢复仍未因此完成。
