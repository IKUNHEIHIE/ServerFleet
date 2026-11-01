# Wave6 · 主控接收的共享增量

总控/审查：codex-review-02；2026-11-01。来源CX `4a3e31edad46051c2f9de3c9f54a16c843a3f93c`、DS `fd2e015b6de99349a34f8061bbb39485f119ab7c`；精确集成SHA见本轮审查交接。本文件是增量组合说明，G2/D01—D07/P1-C10及生产准入仍OPEN。

## 已接入的主控组合

- `server.Config.CredentialKeyDir`、CLI `-credential-key-dir`；默认数据库旁`credential-keys`。Linux当前owner+0700/0600；Windows检查owner/DACL，只接受当前用户、SYSTEM和本机管理员，其他主体的allow ACE拒绝。新建目录使用原子私有权限，主密钥由密码学随机源产生，重启沿用；初始化库丢钥/旧版本不能自动生成替代密钥。旧固定local-default不迁移，awskr现有库不改。
- `auth.EnsureCredentialKeyDirectory`仅新建最后一级并验证，不修宽权限或跟随链接；Windows同目录WRITE_THROUGH发布，Linux硬链接/rename+目录同步。撤销凭据擦除密文属于合法状态，不再使重启失败。
- `server.Config.AIGenerationQuota`及`AIRuntime/AIModelsHTTP/AIGenerationHTTP`；凭据借用只登记AI当前生成操作，其他来源保持拒绝。模型配置为owner CAS；生成显式触发、上游SSE缓冲至终态和撤权复查后才返回完整消息。未配置配额仍拒绝调用；没有浏览器逐token流、真实外部供应商或持久配额验收。模型typed审计保留在durable outbox，完整审计适配尚待补齐。
- `server.Config.UpdateCheck/UpdatePlan/UpdateApply/RecoveryExecute/PluginRuntime/XrayTaskHost`；默认nil继续关闭，接缝不代表真实supervisor/恢复释放/插件Runtime已准入。
- `server.Config.SMTPSender/QQSender/DownloadTransport`；DNS使用已有`DNSProvider`。供应商协议实现可注入，秘密借用、实际账号和自动装配仍需各自证据；不在本轮使用真实秘密。
- 默认文件服务不再构造Controller本机节点目录。`LocalStagingTransport`只允许测试显式注入；保留旧本地字节成功测试并改为显式fixture，不将它记录为远端互传。真实WSS文件链路未接入前，缺通道返回失败。

## 本轮审查修复

1. 新增撤销→重启负向复现；修复合法擦除密文导致启动失败。
2. 文件协调器在每项读前、读完写前、写后检查当前授权；补第一项完成后撤权、第二项不得写入的回归。真正分块中的撤权仍须Agent/通道逐帧门控，尚未准入默认链路。
3. ACME下载证书须绑定本次CSR公钥、申请域名和当前有效期，拒绝CA叶子；补不同公钥、域名和过期证书拒绝。完整信任链/异常授权域名/外部CA验收仍有后续项。

## 公共候选与兼容性

- 新增独立`api/openapi/revisions/v09-r6/ai-candidate.yaml`，纳入Redocly根门禁；三份原候选及69份原始资料不改。
- `GET/PUT /api/v1/ai/model-settings`，PUT要求当前真人管理员、Origin/CSRF及expectedRevision；`POST /api/v1/ai/sessions/{sessionId}/messages/{messageId}/generate`无请求体/查询，响应为完整消息，不是浏览器token流。
- 当前实现Cookie名`__Host-sf_session`与旧候选`sf_session`的差异登记到P1-CANDIDATE-REVIEW，不用新增候选暗中修改旧合同。
- DS新增`POST /api/v1/publications/{id}/rollback-all`及带publishing前缀别名仍是实现候选；授权/逆序/失败停止/续跑通过模块测试，不代表真实发布桥已验收。
- 已接收可编译的R6文件消息候选：Envelope oneof33/34为`FileChunkRequest/Reply`，35/36为`FileOpenRequest/Reply`；固定Buf/protoc-gen-go生成，保留所有旧字段号和原兼容基线。`files.transfer.v1`能力及minor>=1预留给后续完整实现，当前默认语义校验、CLI能力与任务准入继续拒绝File*。新测试验证四类候选往返且两个方向都不能默认获准。原未知字段测试改用仍未知的100，保留原拒绝/扩展检查；r03 provenance及原始资料不变。

## 文件链路下一检查点

CX继续私有Agent/transport实现，DS继续files协调器；给总控可应用且同步的Proto/生成/组合补丁。至少一起解决：

双方直接使用`gen/go/serverfleet/agent/v1`新类型，不再重复申请同一组字段。Chunk的1—7沿用CX提议、8为cancel（取消不得带data/readBytes/finish）；Open的路径/方向/策略/大小/SHA/planDigest必须与已接受任务绑定核对。相对路径映射、执行ID、取消和冲突策略按下述检查点完成后，总控再审查默认语义校验和WSS接线，不可只打开switch使其通过。

- 当前DS每项transferID含冒号，CX只接受`[A-Za-z0-9_-]{1,128}`；executionID长度也必须统一，不能仅靠fake peer通过。
- DS输入是节点绝对路径，CX只接受file-root相对路径。不能静默去掉斜杠；必须由已冻结任务映射，执行前源读/目标写各自授权。
- 下载的大小/SHA需要受控stat/open并绑定不可变任务；上传的skip/rename/overwrite/父目录策略须真实实现或明确拒绝，不能用CX当前“只允许新文件”冒充覆盖。
- TaskOffer接受及执行ID/参数摘要/能力/minor协商是file_open前提；每个响应验证版本、任务/执行/transfer/方向及尺寸，不把文件字节送入任务日志。
- 完成回执丢失、重连、取消、低磁盘、符号链接竞态和持久UNKNOWN不能靠自动重复副作用解决。只有总控接入组合后才能收双端WSS成功证据。

## 未开启的外部能力

awskr只用于普通用户独立目录、临时loopback端口及合成数据。已有Controller/Agent/nginx/MySQL不改、不重启；不新建云资源，不调用真实Cloudflare/SMTP/QQ/ACME账号，不安装安全FAIL的Xray工件。隔离Nginx渲染/代理验证只证明配置可运行，不能作为统一任务发布已实现的证据。
