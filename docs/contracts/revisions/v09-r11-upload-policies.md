# R11：冻结用户上传策略与真实回执候选

任务V09-CLOUD-PRODUCT-IMPLEMENT-01 / chatgpt-takeover-01；协调/实现/复核ChatGPT单会话，不冒称第二个独立审核身份。基线main a8a093ccfaecc35c1a58cadbef3a675b57fdf035。本候选待精确head审查/检查，不冻结G2；原三OpenAPI、R8—R10、Proto/gen/兼容基线和历史SQL不改。

## 行为与兼容

POST用户上传支持skip/overwrite/rename并持久冻结，省略/空串沿用旧overwrite。未知策略拒绝。新增ds/0043_upload_policies，仅在旧28项之后追加两列，旧上传回填overwrite/空实际路径，保持已排队计划语义，正式29项。升级重放保留旧checksum；新列不可通过回退二进制自动删除，降级须先备份并按审查方案恢复。

GET默认和resumeBinding=true保留R8/R10的字段形状。新增显式resumeBinding=true&bindingVersion=2，在原owner和当前节点写权限检查后增加conflictPolicy，实际回执已确认时再增加actualDestination；重复参数/非法版本/缺少前提返回400。旧客户端不会主动请求新策略；新Web遇旧Controller缺少策略绑定则停止续传，不能猜测为skip/overwrite。旧Web草稿未含策略时对应原已实现的overwrite，仍须服务端确认。

策略与节点/目标/大小/摘要一起冻结进统一任务，并在worker每帧当前授权前后核对。skip返回独立skipped终态，保留旧目标字节；POST complete幂等200的files为空，不伪造旧文件摘要/大小。rename仅允许原映射目录同级路径，持久保存真实路径；completed的files使用实际路径。202依然只表示任务受理。非overwrite计划若只得到裸task completed而缺实际上传回执，收敛为failed/agent_unknown、保留暂存，不推测skip/rename结果、不重放。

已验证暂存摘要不等于skip目标摘要。checksum mismatch先在同一事务提交failed与审计，再删除暂存；审计失败回滚并保留原字节。成功/skip/取消只有终态事务成功后清理暂存。缓存仅保存待核验身份和冻结策略，不保存实际路径/偏移/成功证据；实际路径只来自当前授权读回。

## 验收与边界

批量覆盖三策略冻结/原目录约束/持久回执、缺失回执未知/不重放、checksum审计失败回滚、真实28→29升级、Web重选文件/策略不匹配/跨节点/当前权限、实际mTLS Agent三策略字节。保留所有FAIL/SKIP/BLOCKED。具体输出见本轮进度及PR评论；用户手动验收不替代安全检查，也不代表B03—B06已关闭。
